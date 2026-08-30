# Background Pods

## Summary

Background pods are maintenance and infrastructure pods that the cluster operator needs to run, but that must never affect the users of the cluster: node health checks, device diagnostics, monitoring agents, cloud-provider daemons.
Today KAI accounts for the resources they hold, so nodes carrying them are never seen as fully available and the capacity is effectively lost to real workloads.

This proposal lets a cluster administrator declare a set of pods as *background*.
KAI then treats the resources they hold as free when making scheduling decisions, and evicts them on demand when a real workload is placed on top of them.
Background pods belong to no queue, contribute nothing to any queue's quota or fair share, and are never protected from displacement.

The proposed mechanism is a virtual eviction: at the start of each scheduling session the scheduler removes background pods from its in-memory view of the cluster, schedules as if the nodes were empty, and then determines per node which of them can be restored and which must actually be evicted. It is implemented as a single scheduler plugin.

## Motivation

A cluster operator, typically a cloud provider or an infrastructure team, needs to run maintenance workloads on the cluster they operate.
These pods should stay out of the user's way: they must not consume any user's quota, must not block a user's workload from running, must not degrade the placement a user's workload would otherwise get, and need to keep performance overhead to a minimum.

From the scheduler's point of view they are currently indistinguishable from real work.
They hold resources on a node, so that node is not empty, and every decision KAI makes treats that as a fact about available capacity.

While it's possible to work around this (see more below), these pods' presence still effects scheduling behavior in the cluster. For example, for workloads that have preferred topology requirements:
Background pods scheduled on a domain can cause sub-optimal placement of the real workloads, as the scheduler will attempt a naive allocation before resorting to reclaims and preemptions.
There is no way today to make the scheduler completely ignore these background pods.

[Issue #1936](https://github.com/kai-scheduler/KAI-Scheduler/issues/1936) is a good example of this use case.

### The current workaround

The behavior can be approximated today by placing background pods in a dedicated queue configured with `quota: 0`, `overQuotaWeight: 0`, and `limit: -1`.
Such a queue has a fair share of zero, so everything it holds is permanently above its fair share and any other queue is entitled to reclaim from it.

A working example is in [`examples/`](examples).

This works, but has several problems:

**Every allocation becomes a reclaim.** If background pods are spread across the cluster, most nodes will hold at least one, and almost any placement of a real workload requires displacing one.
The allocate action fails and the work falls to reclaim, which runs a scenario search per allocation.
On a large cluster this can be very costly.

**Sub optimal placement decisions.** Naive allocation is attempted before reclaim.
If a feasible placement exists without displacing anything, the scheduler takes it, even when displacing a background pod would have produced a much better placement.

**It does not cover pods you cannot edit.** The pods must carry a queue label and be scheduled by KAI, which rules out anything created by a controller outside the user's control. (At this point, this use case is only hypothesized, and further thought needs to be given as to wether we want kai to control pods that do not explicitly opt-in).

Topology is a good example.
Consider two racks of four nodes, eight GPUs each, with two background pods per rack holding one GPU apiece:

```text
legend:   ▪ background pod      · idle GPU      █ job pod

                   rack-0                                         rack-1
  node0      node1      node2      node3         node4      node5      node6      node7
[▪·······] [▪·······] [········] [········]    [▪·······] [▪·······] [········] [········]
```

A job now asks for four whole nodes, eight GPUs each, with a rack-level preferred placement.
Only two nodes per rack are fully free, but a feasible placement exists across the two racks, so allocate takes it and reclaim is never reached:

```text
                   rack-0                                         rack-1
  node0      node1      node2      node3         node4      node5      node6      node7
[▪·······] [▪·······] [████████] [████████]    [▪·······] [▪·······] [████████] [████████]
                      └───── 2 pods ──────┘                          └───── 2 pods ──────┘
```

The rack preference is silently unsatisfied, and the workload runs with worse network locality for its entire lifetime.
The placement it should have received displaces the two background pods in one rack and takes that rack whole:

```text
                   rack-0                                         rack-1
  node0      node1      node2      node3         node4      node5      node6      node7
[████████] [████████] [████████] [████████]    [▪·······] [▪·······] [········] [········]
└─────────── 4 pods, one rack ────────────┘
```

A reproduction is in [`examples/topology/`](examples/topology).

The root cause in both cases is that the workaround only makes background pods *reclaimable*, while leaving them *visible*.
A reclaimable pod is still counted, still occupies the node as far as every allocation decision is concerned, and still has to be argued away one scenario at a time.
What the operator actually wants is for them not to be counted at all.

## Goals

- Let an administrator declare a set of pods as background.
- Treat capacity held by background pods as free for the purpose of scheduling decisions, so that placement quality — topology, bin-packing, node ordering — is computed against the cluster as if these pods don't exist.
- Keep background pods out of all queue accounting.
  They belong to no queue and contribute to no queue's allocated, requested, or fair share.
- Minimize the impact on scheduling performance.
  Avoiding the reclaim scenario search is the main example: displacing a background pod should not require solving a victim-selection problem, which is precisely the cost the current workaround incurs.
- Do not displace background pods unnecessarily.
  Their disruption cost is unknown to KAI and is not assumed to be zero. Avoid evicting background pods if it won't contribute to placement of actual user pods. Note that this doesn't mean we try to optimize for the absolute least amount of evictions (see non-goals).
- **Support external pods — deferred.** It could be that some use cases involve cloud provider / vendor pods that cannot be controlled by the user or the cluster admin. This can be adressed by allowing the admin to configure a generic selector (by label / namespace) that will allow kai to evict such pods. However, this would be a precedent that allows kai to evict external pods, and should be given more thought. Deferred for now until requested explicitly by a user.

## Non-Goals

- **Minimizing disruption for background pods.** While we do not want to disrupt background pods for no reason at all (as stated in goals), we also do not want to waste time considering several reclaim scenarios just to minimize background pod disruption. The scheduler will allocate user pods as usual, evict any background pods that stand in the way, and will not attempt different scenarios or let background pods affect scheduling decisions.

## Design

At the start of every scheduling session the plugin evicts all background pods from the in-memory snapshot, without committing, so their capacity is accounted as *releasing*.
The session then runs untouched: workloads that fit in idle capacity are allocated and bound as usual, and workloads that need background-held capacity are pipelined onto it, exactly as they would be over a preemption victim.
At session close the plugin attempts to re-place every background pod its original node.
Those that still fit are unevicted and are not disturbed; the rest have their evictions committed, and the pipelined workloads bind in a later session once they are gone.

### Selecting background pods

Since external pods are deferred, the first version can rely on an opt-in that the pod itself carries, such as a `kai.scheduler/background: "true"` label.
This keeps the blast radius small: KAI only ever ignores and evicts pods that explicitly asked to be treated this way.

Open questions - possible validations needed:
- Should we block regular users from using this label? It could be used to surpass the quota mechanisms
- Should we validate that `terminationGracePeriodSeconds=0` for background pods? Or just recommend it?
- Should we validate that the label is not set on pods with `system-cluster-critical` or `system-node-critical` priority?

### Virtual eviction

`OnSessionOpen` creates a `Statement`, iterates the nodes, and calls `Statement.Evict` on every pod matching the selector, recording what it removed per node.
(`Statement.Evict` only touches in-memory state). The resources used by background pods are considered as `Releasing`, so the scheduler is aware that any pods placed on them is actually pipelined.

This solves both problems the workaround has.
Placement decisions such as topology domain selection and bin-packing are computed against real availability, so the scheduler picks the rack it should pick.
Displacing a background pod also costs no scenario search, because from the session's point of view nothing is being displaced at all.

Only background pods that KAI scheduled can be handled: `Statement.Evict` needs the pod's PodGroup from the session, so pods with none are skipped.
That is consistent with the pod-carried opt-in above, and it is what the deferred external-pod goal would have to change.

### During the session

The plugin registers no hooks and takes no part in the actions.

Because the freed capacity is *releasing* rather than *idle*, a workload contending for it fails `IsTaskAllocatable` and passes `IsTaskAllocatableOnReleasingOrIdle`, which is the pipeline path.
It is pipelined onto the node rather than allocated, and its demand is subtracted from `ReleasingVector`.
A workload that fits in idle capacity is unaffected and binds in the same cycle.

### Re-placement and commit

`OnSessionClose` iterates each node's background pods in a deterministic order and asks whether each can stay (virtually re-allocate), using `IsTaskAllocatableOnReleasingOrIdle`: does the unclaimed capacity still cover it.

A pod that fits is restored with `Statement.Unevict`, which marks its eviction operation undone.
A pod that does not fit stays in the statement.
`Statement.Commit()` then issues real eviction requests for only what remains valid, through `Cache.Evict` — the same path preemption and reclaim victims take, with the same events and metrics.

The displaced set is minimal by construction.
Only pods that genuinely no longer fit are evicted, and background pods on nodes that received nothing are restored trivially, which is the common case: most nodes are untouched in any given session and nothing on them is disrupted.

A background pod is never restored onto a *different* node.
Relocating them is a non-goal, KAI cannot move a pod it does not own, and many background pods are pinned to their node by design.

### Plugin lifecycle ordering

Statement operations fire the session's event handlers, and those handlers belong to other plugins.
This gives the plugin two ordering requirements.

It must **open after** any plugin whose handlers should observe the eviction.
Plugin open order follows `config.Tiers` and is deterministic, and the plugin's low priority already places it last.
The proportion plugin's `DeallocateFunc` is the one that matters: it is what removes the background pods from their queue's allocated share for the session.

It must **close before** those plugins, because `Unevict` fires `AllocateFunc` and several plugins discard their session state on close.
Proportion nils its queue map in `OnSessionClose`, and its handler then dereferences a nil queue.
`CloseSession` originally iterated the plugin map, so the order was random and this crashed intermittently.
It now closes plugins in reverse open order, which gives the invariant directly: a plugin that opened after another is torn down before it.

Proportion's two handlers were also hardened to skip a job whose queue is absent, rather than dereferencing it.
That was reachable without this plugin — `cleanQueueOrphans` drops orphan queues and their children during snapshot — so it is a fix in its own right.

### Known limitations

**Predicates are not re-run at re-placement.** The check is resource capacity only.
An earlier design had re-placement go through the full allocation path, so that constraints which are not resource quantities would be covered for free.
That does not work while the pod is merely `Releasing`: it is still bound to its node as far as the predicate snapshot is concerned, so it is counted against itself and nothing is ever restored.
Affinity, anti-affinity, topology spread, and hostPorts are therefore not covered, and neither is intra-node placement such as a specific GPU index or NUMA zone.

The failure mode is conservative rather than silent for the predicate cases: a workload with anti-affinity against a background pod has the node rejected outright, so nothing is misplaced, but the node is not "seen as free" after all.

This can probably be fixed with some effort if there's a need for it.


### What is not yet designed

- Background pods holding DRA resource claims.
  Deleting the pod does not synchronously free the claim.
- Interaction with consolidation, which now plans moves against a cluster that looks emptier than it is.
- Observability: which events to emit, and how to keep these evictions out of the preemption and reclaim metrics so fairness reporting stays meaningful.
- Whether background pods should still appear in their queue's externally reported `allocated`, given the goal of keeping them out of queue accounting entirely.

## Alternatives Considered

**Internal zero-quota queue.** Have KAI create a hidden queue with zero quota, zero over-quota weight, and an unlimited limit, and assign background pods to it internally.
This is the workaround made automatic.
It inherits both of the workaround's scheduling problems: every allocation becomes a reclaim, and naive allocation still runs first, so the topology case above is unchanged.
It also requires KAI to own the pods, which closes off the deferred external-pod goal permanently rather than leaving room for it.

Options for committing the evictions, kept in case the chosen approach runs into trouble.
All of them replace the pipelining step with something that coordinates with the binder, and all of them cost an API change.

**The scheduler evicts and the binder waits.** The BindRequest carries the displaced set purely as a gate: the binder deletes nothing, it only refuses to bind until every listed pod is gone.
The workload is allocated normally rather than pipelined, so no scheduling cycle is lost.
Costs a `BindRequestSpec` field, a wait-and-requeue path in the reconciler that must not consume the backoff limit, and the binder distinguishing "not ready yet" from "failed".
This was the preferred approach before the implementation showed that pipelining falls out of the existing machinery for free.

**The binder evicts.** The BindRequest carries the displaced set and the binder deletes those pods itself before binding.
Eviction and binding become adjacent in one actor, which narrows the race window, at the cost of a second eviction path with its own RBAC, plugin, and metrics.

**The binder decides and evicts.** The smallest possible version: the scheduler only ignores background pods in its snapshot, there is no re-placement step and no displaced set, and the binder greedily evicts from the target node's live state until the incoming pod fits.
Nothing is communicated between the components.
It gives up modelling anything that is not a plain resource quantity — it cannot tell which background pod holds a conflicting GPU index, MIG slice, NUMA zone, or DRA claim, and it cannot see a predicate-based conflict such as anti-affinity, which the kubelet will not catch either.
It also needs a per-node lock, since two BindRequests for one node could each conclude they fit and overcommit it.
Viable for background pods holding whole devices and plain resources, but extending it later is a rewrite rather than an addition.
