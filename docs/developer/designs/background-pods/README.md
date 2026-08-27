# Background Pods

## Summary

Background pods are maintenance and infrastructure pods that the cluster operator needs to run, but that must never affect the users of the cluster: node health checks, device diagnostics, monitoring agents, cloud-provider daemons.
Today KAI accounts for the resources they hold, so nodes carrying them are never seen as fully available and the capacity is effectively lost to real workloads.

This proposal lets a cluster administrator declare a set of pods as *background*.
KAI then treats the resources they hold as free when making scheduling decisions, and evicts them on demand when a real workload is placed on top of them.
Background pods belong to no queue, contribute nothing to any queue's quota or fair share, and are never protected from displacement.

The proposed mechanism is a virtual eviction: at the start of each scheduling session the scheduler removes background pods from its in-memory view of the cluster, schedules as if the nodes were empty, and then determines per node which of them can be restored and which must actually be evicted.

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

At the start of every scheduling session the scheduler evicts all background pods from its in-memory snapshot, schedules as though they were never there, and then tries to allocate each of them back onto its own node.
Those that no longer fit are the ones actually evicted, by the scheduler, once the session ends.
The binder does not evict anything; it waits for the eviction to complete before binding the user's pod.

### Selecting background pods

Since external pods are deferred, the first version can rely on an opt-in that the pod itself carries, such as a `kai.scheduler/background: "true"` label.
This keeps the blast radius small: KAI only ever ignores and evicts pods that explicitly asked to be treated this way.

Open questions - possible validations needed:
- Should we block regular users from using this label? It could be used to surpass the quota mechanisms
- Should we validate that `terminationGracePeriodSeconds=0` for background pods? Or just recommend it?
- Should we validate that the label is not set on pods with `system-cluster-critical` or `system-node-critical` priority?

### Virtual eviction

At the start of each scheduling session, the scheduler evicts every background pod from its in-memory snapshot and records what it removed, per node.
No API call is made and no pod object is touched.
From this point the session proceeds normally: allocate, preempt, reclaim, and consolidate all see nodes as though the background pods were not there.

This solves two issues from the workaround:
- Placement decisions such as topology domain selection, bin-packing, and node ordering are computed against real availability, so the scheduler picks the rack it should pick.
- Displacing a background pod also costs no scenario search, because from the scheduler's point of view nothing is being displaced at all.

What is needed is the in-memory half of eviction without the API call.
`ssn.Evict` and `Statement.Evict` both reach `Cache.Evict`, so the plugin needs the node-state path (`NodeInfo.RemoveTask` and the session's pod-status update) without the eviction request.

### Re-placement

Once the scheduler knows what it has committed to a node, it asks the inverse question: **which of the background pods removed from that node at session start can be allocated back onto it?**

Each background pod is run through the usual allocation path: predicates, resource fit, and device assignment — restricted to its original node.
A pod that allocates successfully is restored, and its eviction is cancelled before it was ever issued.
A pod that fails to allocate is displaced, and gets evicted.

Reusing the allocation path is what makes this correct for constraints that are not quantities.
Affinity, anti-affinity, topology spread, hostPorts, node selectors, and taints are all evaluated by the same predicates that placed the user's workload, so a background pod that no longer fits for any of those reasons is displaced without the plugin knowing anything about them.
The same holds for placement inside a node: re-allocating a pod that held a GPU device, a MIG slice, a NUMA zone, or a DRA claim requires finding a concrete one, so if the user's workload took the one it had, re-allocation fails and the pod is displaced.
No per-dimension special casing is needed anywhere.

It also makes the displaced set naturally minimal.
Only pods that genuinely no longer fit are evicted, and background pods on nodes that received nothing are restored trivially, which is the common case: most nodes are untouched in any given session, and nothing on them is disrupted.

A background pod is never restored onto a *different* node.
Relocating them is a non-goal, KAI cannot move a pod it does not own, and many background pods are pinned to their node by design.

The cost is one allocation attempt per background pod per session, each against a single known node.
That is O(number of background pods) predicate evaluations rather than a scenario search, which is what keeps this within the performance goal.

**Open question — when to compute it.** BindRequests are created during the session, at `Statement.Commit()`, not at session close.
Two shapes are available:

- *Incremental.* At each bind, re-place the target node's background pods against what is committed to it so far, and stamp the failures on that BindRequest.
  The set only grows over the session, which makes the per-node sets a nested chain.
  That nesting is what makes concurrent, out-of-order binding safe: for any subset of a node's BindRequests that have bound, the union of their displaced sets equals the set belonging to the latest one, and the demand of those binds is bounded by what that set frees.
  No coordination between BindRequests is needed.
- *Deferred.* Compute once at session close, which requires either delaying BindRequest creation until after the last action or creating them in a paused state and patching them afterwards.
  Both change the timing of binding for every workload, not just this feature.

The incremental form appears to produce the same answer at lower cost, but this has not been settled.

Either way, re-placement must not leave a restored pod in the session's node state.
The session is still running and later actions must continue to see the node as though background pods were not there.
`Statement.Checkpoint` and `Rollback` already provide this shape: checkpoint, attempt the allocations, record which succeeded, roll back.

**Open question — ordering.** When several background pods on one node compete for what is left, the order in which they are re-placed decides which of them survives.
The order must at least be deterministic, so the same session state produces the same decision.
Per the non-goals, no effort is spent choosing an order that produces a better outcome.

**No node scoring.** Background pods do not influence node selection.
The scheduler will not prefer a node holding none over a node holding three, and will therefore sometimes displace pods it could have avoided by choosing a different node.
That is accepted deliberately: adding a score term would make background pods affect scheduling decisions, which the non-goals rule out, and it would put a cost on every node evaluation rather than only on nodes that actually receive a workload.
Re-placement is the only mechanism limiting disruption, and it is enough to guarantee that a background pod is never evicted unless a user's pod genuinely took its place.

### Committing evictions

A user's pod must not be bound to a node before the background pods it displaces are gone.
A terminating pod still counts against node allocatable until it disappears, so a bind that lands too early is rejected by the kubelet.
The pod is already bound at that point, so this is not a retryable bind failure — it ends up `Failed`.

**The scheduler evicts, and the binder waits.**
Once re-placement has determined the displaced set, the scheduler evicts those pods itself, through the same path it already uses for preemption and reclaim victims.
The BindRequest for the user's pod carries the displaced set as a gate: the binder does not delete anything, it only refuses to bind until every pod on the list is gone, then binds as usual.

This keeps eviction in one component.
Events, metrics, PDB handling, and the evictor itself stay in the scheduler, where they already exist, and the binder gains no new decision-making role and no delete permission.
It also costs no scheduling cycle: the user's pod is allocated normally and binds as soon as the node is actually clear.

What it needs:

- A field on `BindRequestSpec` carrying the displaced pods.
- A wait-and-requeue path in the BindRequest reconciler, which must not consume the backoff limit while it waits.
- The binder to distinguish "not ready yet" from "failed".

The window between eviction and bind is a scheduling cycle wide at worst, so another scheduler could take the freed capacity before the binder gets there.
That is a failed bind and a retry, not a corrupted state, and the next session re-derives from reality.

### Alternatives for committing evictions

Kept for reference in case the chosen approach runs into trouble.

**The binder evicts.** The BindRequest carries the displaced set and the binder deletes those pods itself before binding.
Eviction and binding become adjacent in one actor, which narrows the race window, at the cost of a second eviction path with its own RBAC, plugin, and metrics.

**The scheduler evicts and the pod is pipelined.** Instead of gating the bind, the user's pod is pipelined rather than allocated, so it binds in a later session once the background pods are gone.
Needs no API change and no binder change at all, and reuses exactly how KAI resolves this ordering problem for preemption today.
Costs an extra scheduling cycle for every pod that displaces something, and requires converting an already-committed allocation into a pipeline after the fact (`Statement.ConvertAllAllocatedToPipelined`), which is not a clean seam.

**The binder decides and evicts.** The smallest possible version: the scheduler only ignores background pods in its snapshot, there is no re-placement step and no displaced set, and the binder greedily evicts from the target node's live state until the incoming pod fits.
Nothing is communicated between the components.
It gives up modelling anything that is not a plain resource quantity — it cannot tell which background pod holds a conflicting GPU index, MIG slice, NUMA zone, or DRA claim, and it cannot see a predicate-based conflict such as anti-affinity, which the kubelet will not catch either.
It also needs a per-node lock, since two BindRequests for one node could each conclude they fit and overcommit it.
Viable for background pods holding whole devices and plain resources, but extending it later is a rewrite rather than an addition.

### Open questions

**Transport.** A typed field on `BindRequestSpec` alongside the other decisions the binder cannot re-derive (`SelectedGPUGroups`, `PredictedNUMAZones`), or an annotation via the existing `BindRequestMutateFn` hook.
The typed field is clearer and validated; the annotation avoids a CRD change.
Either way the entry should carry the pod UID as well as its name, so a background pod deleted and recreated under the same name between session start and bind is not mistaken for its predecessor.

**Eviction mechanism.** A direct `DELETE`, as the scheduler's evictor does today, or the `pods/eviction` subresource so PodDisruptionBudgets are honoured.

**Flapping.** A displaced pod's controller may recreate it immediately, possibly onto another node, where it may be displaced again.
Whether a per-owner cooldown is needed, or whether a metric is enough to find out, is unresolved.

### What is not yet designed

- Background pods holding DRA resource claims.
  Deleting the pod does not synchronously free the claim, so "wait until it is gone" is not sufficient.
- Interaction with consolidation, which will now plan moves against a cluster that looks emptier than it is.
- Observability: which events to emit on a pod KAI does not own, and how to keep these evictions out of the preemption and reclaim metrics so fairness reporting stays meaningful.

## Alternatives Considered

**Internal zero-quota queue.** Have KAI create a hidden queue with zero quota, zero over-quota weight, and an unlimited limit, and assign background pods to it internally.
This is the workaround made automatic.
It inherits both of the workaround's scheduling problems: every allocation becomes a reclaim, and naive allocation still runs first, so the topology case above is unchanged.
It also requires KAI to own the pods, which closes off the deferred external-pod goal permanently rather than leaving room for it.

**Evict at session close from the scheduler.** Simpler in that no information has to reach the binder, but it separates eviction from binding by a full scheduling cycle, widening the window for another scheduler to take the freed capacity, and it risks the workload being bound while victims are still terminating.

**Binder decides what to displace.** The binder sees live cluster state, so it could compute displacement itself with no scheduler bookkeeping at all.
Two concurrent reconciles targeting the same node can each conclude they fit without evicting, both bind, and overcommit the node.
It also cannot reproduce the scheduler's device-level choices for GPU, MIG, or NUMA placement.
