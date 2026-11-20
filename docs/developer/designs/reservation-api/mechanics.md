# Reservation API: scheduler mechanics and recovery

Companion to [README.md](README.md), which holds the design and defines
the [terms](README.md#terms) used here. This document says how the
scheduler implements the first version as defined in the README's
[Scope](README.md#scope), how records are kept durable, and what happens in
every failure and edge case. The later features listed there, `Shared`
holds, pre-allocation and the rest, are not covered. Readers who only need
the API and its rules can stop at the README.

## Snapshot

Each cycle the cache lists Reservations and builds:

- `Pending`, no `status.placement`: a pending virtual task in its holder
  subgroup, built from the template through the normal pod path
  (`pod_info.NewTaskInfo`), carrying the template's labels and
  `kai.scheduler/subgroup-name`, so predicates treat it as a pod. If the
  holder or subgroup does not exist: no task, and `PodGroupFound` or
  `SubGroupFound` false, as a pod with a missing PodGroup waits.
- `Reserved`: an allocated virtual task on `placement.nodeName`. If that
  node no longer exists, the hold is treated as `Pending` and the placement
  is cleared at commit.
- `Consumed`: no task, nothing in the queue; counts for the holder's gang
  only. Its pod, named in `podRefs`, is an ordinary pod with an ordinary
  BindRequest, and nothing is read from the hold about it.
- `Expired`: nothing.

## Queue accounting

Held capacity is charged to the holder's queue from the moment it is
reserved. Four places must know that, or consumers of a queue at its limit
could never start, or could be charged twice:

- **Early job check.** `IsJobOverQueueCapacityFn` runs before a job is
  placed (`actions/common/allocate.go`) and charges the pending tasks' full
  requests against the queue limit
  (`capacity_policy.IsJobOverQueueCapacity`). For a job with eligible
  holds, the required quota is reduced by the free eligible holds its tasks
  can take. This only lets evaluation proceed; it grants no credit.
- **Placement-time check.** `IsTaskAllocationOnNodeOverCapacityFn` runs per
  task and node (`capacity_policy.IsTaskAllocationOnNodeOverCapacity`). A
  task placed through a hold is charged the transfer actually made, zero
  with equal request vectors; a task placed on ordinary capacity is charged
  its full request. A pod never places on ordinary capacity on the strength
  of a hold it does not consume.
- **Consume operation.** Removing the hold's task and allocating the pod is
  one Statement operation, so queue usage moves from hold to pod in one
  step and rollback reverses both.
- **Reporting.** The podgroupcontroller counts `Reserved` holds in the
  holder's `allocated`, never `Consumed` ones; from commit on the pod is
  counted through its own PodGroup. The controller sees the hold and the pod
  at different times, so `allocated` can dip for a moment, the same window
  a pod's own BindRequest has today.

## Fit and preference

For an eligible pod, fit is evaluated per candidate hold on the node: the
pod fits through hold H if its effective request vector equals H's. Only
vectors are compared; the pod's node selector, affinity and tolerations run
as predicates on H's node, and a pod they exclude does not fit through H.
The predicates run against the node with H's task removed, pod affinity
state, pod count and host ports included, and the view is restored after
the check. Otherwise the hold blocks its own consumer: its task carries the
workload's labels, so a required anti-affinity on them would match it, and
it occupies a pod slot and any host ports the template declares. The
removal becomes permanent only through the `consume` operation. Holds are
never summed. The plugin records the hold chosen during fit, per
task and node, and the action reads it at placement: a recorded hold means
`consume`, none means the ordinary allocate or pipeline branch on the node's
free capacity, with the holds counted as used. This is the one asymmetry in
the design, the equivalent of Koordinator restoring a reservation only for
matched pods.

Holds come first, through node ordering alone. The allocation loop sorts
all nodes by score and takes the first that fits (`allocateTask` in
`actions/common/allocate.go`), so no separate feasibility pass is needed:

1. Before any node is examined, the plugin collects the pod's matching
   holds from the snapshot: `Reserved`, same namespace, selector matches,
   request vectors equal. One small lookup, independent of nodes.
2. Its `NodeOrderFn` places those holds' nodes ahead of every other node,
   with a dominating score or a two-tier sort. Among them the ordinary
   scores still order.
3. The loop takes the first hold node that fits. Only if every hold node
   fails a predicate does it reach ordinary nodes and place the pod on free
   capacity; the hold stays `Reserved`, unused.

Two filters run before any of this and must count, for a given job, the
capacity of holds its pods are eligible for as idle, or fully held nodes
vanish before matching: `FeasibleNodesForJob`
(`actions/common/feasible_nodes.go`), used by preempt, reclaim and
consolidation, which keeps only GPU nodes with idle or releasing GPUs; and
the topology plugin's domain filtering, which sums idle and releasing
capacity per domain.

Scheduling signatures need the same awareness. Reclaim, preempt and
consolidation skip a job whose constraints signature matches a job that
already failed in this cycle with a footprint no larger
(`MinimalJobRepresentatives` in `actions/common`). The signature hashes a
pod's node selector, affinity, tolerations, priority, topology spread,
storage and host ports, not its resources, and two pods with identical
constraints are not equally hard to place if one has holds waiting for it.
So a pod's signature also hashes the identities of the holds it is eligible
for in this snapshot. The value lives one cycle, like the signature itself,
so holds created or re-placed later change it from the next cycle on. The
holder's virtual tasks get their signature through the normal pod path.

## Status writes

The scheduler is the only writer of Reservation status, and nothing in KAI
deletes one. Placement and consumption are written synchronously at commit,
where pod BindRequests are created today, and before any BindRequest that
depends on them. The written object is inserted into the informer store at
once, as `createBindRequest` does for BindRequests, so the next snapshot
normally sees it before the watch event. That is not a guarantee: a watch
event for an older version, such as the asynchronous updater's reason
write, can land after the insertion and show the hold `Reserved` again
until the newer event arrives. So every write is a JSON patch that tests
before it writes, refused with a 422 otherwise. Placement tests that
`placement` is absent, or that `status` is absent on a fresh object, where
the patch adds the whole status. Consumption tests that `state` is
`Reserved` and `placement.nodeName` is the node the decision was made
against, since a hold evicted and re-placed is `Reserved` again. Both test
`metadata.uid`, against a Reservation recreated under the same name. Field
tests rather than a `resourceVersion` precondition, which the asynchronous
updater would keep invalidating. A write whose response is lost is treated
as a failed commit, like a failed BindRequest create today; if it did land,
the next attempt against a stale view fails its test, which is the
reconciliation.

Informational status, such as the reason a hold is still `Pending`, goes
through the ordinary asynchronous updater, which patches only its own fields
and never performs a full status update, so it cannot clobber `placement`
whatever the ordering.

## Consuming

`consume` is a new Statement operation alongside allocate, pipeline, evict
and unevict:

- **Applied**, it removes the hold's task from the node and the queue and
  allocates the pod in its place as one entry, so no pass of the action
  sees the hold gone and the pod not yet there, and a rollback undoes both.
- **Reversed**, it unallocates the pod and restores the hold's task.
- **Committed**, it patches the hold to `Consumed` with the pod's name and
  UID in `podRefs`, then creates the pod's ordinary BindRequest. A failed
  patch stops the commit before the BindRequest, as a failed allocate
  commit stops today. From here on the pod is an ordinary pod and the hold
  is done: a failed bind, a lost response, or deletion of the BindRequest
  by the failed-request cleanup (`cleanStaleBindRequest` in `cache.go`) or
  an operator is handled as for any pod: the pod reschedules as an ordinary
  pod. The hold is spent, not kept: it holds no capacity, so whatever the
  pod did not take is free for anyone, and no pod can consume it again.

The commit order matters. A crash after the patch and before the create
spends the hold: the pod has no BindRequest and is placed normally next
cycle, possibly elsewhere. The reverse order would leave a pod binding onto
a hold that still looks free, and a second pod could take it. A spent hold
costs the pod its reservation; a double-taken hold costs correctness.

Exclusivity therefore holds before any BindRequest exists: within a cycle
the statement has removed the hold's task; across cycles the immediate
store insertion shows the hold `Consumed` to the next snapshot; after a
restart the snapshot lists Reservations before the first cycle. A `Single`
hold with two entries in `podRefs` indicates corrupted state; if ever seen,
the first entry is honored and the other pod is treated as an ordinary pod
needing its own capacity, with an event.

Hold eviction is the other new commit path: an evict operation whose task
is a hold commits as a status patch to `Pending` instead of a pod deletion.

## Expiry

Expiry is checked and written by the scheduler in its commit, so a cycle
either consumes a hold or expires it, never both; the scheduler refuses to
consume a hold past its deadline even if the status still says `Reserved`.
On expiry the placement is cleared and the state set to `Expired`, with
the same kind of patch.

## Recovery

- **Gang commit across a crash.** Placements are written one by one, like
  pod BindRequests, so a crash mid-commit leaves a partial holder gang
  recorded. The rule is roll forward: the recovered holds are allocated
  tasks and the next cycle tries to complete the gang. A creator acts when
  every Reservation it waits for is `Reserved`, so a partial gang is visible
  as partial. A holder that cannot complete is stale after
  `stalenessGracePeriod` and its `Reserved` holds expire.
- **Lost placement record.** If `status.placement` is wiped, by a client
  with status rights or a bug, the hold heals the way a pod with a deleted
  BindRequest does: the next snapshot treats it as `Pending` and re-places
  it, possibly on another node, within the template's node selector. The
  capacity on the old node is unprotected for one cycle. A `Consumed` hold
  whose status is wiped is re-placed too, while its pod keeps running as an
  ordinary pod; the creator sees a hold it did not expect and deletes it.

## What changes where

| Component | Change |
| --- | --- |
| API, CRD, RBAC, chart | `Reservation` type and clients; verbs for `reservations` and `reservations/status`. BindRequest unchanged |
| Scheduler cache | Reservation informer; snapshot materialization; synchronous placement and consumption patches with field tests, two placement forms, immediate store insertion |
| Reservation plugin, new | Eligibility including priority class; hold selection during fit with the hold's task removed from the predicate view, recorded per task and node; `NodeOrderFn` putting hold nodes first |
| Statement | New `consume` operation with apply, reverse and commit; evict of a hold commits as a status patch |
| Proportion plugin | Hold-aware required quota in the early job check and in the placement-time check |
| Preempt, reclaim, consolidation | `Consumed` holds count toward minimums and are never victims; `FeasibleNodesForJob` counts eligible held capacity; eligible holds in the scheduling constraints signature; code that assumes every task has a real pod, especially eviction paths |
| Topology plugin | Domain filtering counts eligible held capacity |
| Stale gang eviction | For holders: count placed holds in `IsStale` instead of running pods; expire the `Reserved` holds instead of evicting pods |
| Podgroupcontroller | `allocated` counts `Reserved` holds. No deletion of Reservations |
| Admission | Reservation validation; the template itself is validated by the full CRD schema |
| Podgrouper, later | Holder creation for Reservations without `podGroupRef`, as for bare pods; the scheduler resolves the holder through the `pod-group-name` annotation |
| Binder | Untouched: holds have no BindRequest, consumers bind as ordinary pods |

Downstream, runai-engine keeps KAI RBAC by hand and needs the new verbs.
