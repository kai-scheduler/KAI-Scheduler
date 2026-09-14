# Background Pods

## Summary

Background pods are maintenance and infrastructure pods that the cluster operator needs to run, but that must never affect the users of the cluster: node health checks, device diagnostics, monitoring agents, cloud-provider daemons.
Today, KAI treats them the same as regular workloads. While admins can assign them to a very low priority queue (see the workaround described below), their existence still causes unwanted effects on scheduling of end-user workloads.

This proposal lets a cluster administrator declare a set of pods as *background*.
KAI then treats the resources they hold as free when making scheduling decisions, and evicts them on demand when a real workload is placed on top of them.

The proposed mechanism is a virtual eviction: at the start of each scheduling session the scheduler removes background pods from its in-memory view of the cluster, schedules as if the nodes were empty, and then determines per node which of them can be restored and which must actually be evicted. It is implemented as a single scheduler plugin.

## Motivation

A cluster operator, typically a cloud provider or an infrastructure team, needs to run maintenance workloads on the cluster they operate.
These pods should stay out of the user's way: they must not consume any user's quota, must not block a user's workload from running, must not degrade the placement a user's workload would otherwise get, and need to keep performance overhead to a minimum.

From the scheduler's point of view they are currently indistinguishable from real work.
They hold resources on a node, so that node is not empty, and every decision KAI makes treats that as a fact about available capacity.

While it's possible to work around this (see more below), these pods' presence still effects scheduling behavior in the cluster. For example, for workloads that have preferred topology requirements:
Background pods scheduled on a domain can cause sub-optimal placement of the real workloads, as the scheduler will attempt a naive allocation before resorting to reclaims and preemptions.

[Issue #1936](https://github.com/kai-scheduler/KAI-Scheduler/issues/1936) is a good example of this use case.

### The current workaround

The behavior can be approximated today by placing background pods in a dedicated queue configured with `quota: 0` and `overQuotaWeight: 0`.
Such a queue has a fair share of zero, so everything it holds is permanently above its fair share and any other queue is entitled to reclaim from it.

A working example is in [`examples/`](examples).

This works, but has several problems:

**Every allocation becomes a reclaim.** If background pods are spread across the cluster, most nodes will hold at least one, and almost any placement of a real workload requires displacing one.
The allocate action fails and the work falls to reclaim, which runs a scenario search per allocation.
On a large cluster this can be very costly.

**Sub optimal placement decisions.** Naive allocation is attempted before reclaim.
If a feasible placement exists without displacing anything, the scheduler takes it, even when displacing a background pod would have produced a much better placement.

**It does not cover pods you cannot edit.** The pods must carry a queue label and be scheduled by KAI, which rules out anything created by a controller outside the user's control. (At this point, this use case is only hypothesized, and further thought needs to be given as to wether we want kai to control pods that do not explicitly opt-in).

Topology is a good example: consider two racks of four nodes, eight GPUs each, with two background pods per rack holding one GPU apiece:

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

The root cause in both cases is that the workaround only makes background pods *reclaimable*, but the scheduling cycle will still attempt a "naive" allocation with 0 evictions before it attempts a reclaim, even of very low-priority pods.
If a naive solution exists, the scheduler will not attempt to find a more optimized placement in the reclaim action.
In addition, the reclaim action is inherently computationally heavy and non-exhaustive, so the scheduler might not even find the optimal solution, or could spend a long time looking for one.
The desired behavior by the operator would be to perform the scheduling cycle and make all scheduling decisions as if the background pods are not there (or evicting).

## Goals

- Let an administrator declare a set of pods as background.
- Treat capacity held by background pods as free for the purpose of scheduling decisions, so that placement quality — topology, bin-packing, node ordering — is computed against the cluster as if these pods don't exist.
- Keep background pods out of all queue accounting.
  They belong to no queue and contribute to no queue's allocated, requested, or fair share.
- Minimize the impact on scheduling performance.
  Avoiding the reclaim scenario search is the main example: displacing a background pod should not require solving a victim-selection problem, which is the main cost the current workaround incurs.
- Do not displace background pods unnecessarily.
  Their disruption cost is unknown to KAI and is not assumed to be zero. Avoid evicting background pods if it won't contribute to placement of actual user pods. Note that this doesn't mean we try to **optimize** for the absolute least amount of evictions (see non-goals).
- **Support external pods — deferred.** It could be that some use cases involve cloud provider / vendor pods that cannot be controlled by the user or the cluster admin. This can be addressed by allowing the admin to configure a generic selector (by label / namespace) that will allow kai to evict such pods. However, this would be a precedent that allows kai to evict external pods, and should be given more thought. Deferred for now until requested explicitly by a user.

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

### Virtual eviction

`OnSessionOpen` creates a `Statement`, iterates the nodes, and calls `Statement.Evict` on every pod matching the selector, recording what it removed per node.
(`Statement.Evict` only touches in-memory state). The resources used by background pods are considered as `Releasing`, so the scheduler is aware that any pods placed on them is actually pipelined.

This solves both problems the workaround has.
Placement decisions such as topology domain selection and bin-packing are computed against real availability, so the scheduler picks the rack it should pick.
Displacing a background pod also costs no scenario search, because from the session's point of view nothing is being displaced at all.

Only background pods that KAI scheduled can be handled: `Statement.Evict` needs the pod's PodGroup from the session, so pods with none are skipped.

### During the session

The plugin registers no hooks and takes no part in the actions.

Because the freed capacity is *releasing* rather than *idle*, a workload contending for it fails `IsTaskAllocatable` and passes `IsTaskAllocatableOnReleasingOrIdle`, which is the pipeline path.
It is pipelined onto the node rather than allocated, and its demand is subtracted from `ReleasingVector`.
A workload that fits in idle capacity is unaffected and binds in the same cycle.

### Re-placement and commit

`OnSessionClose` iterates each node's background pods in a deterministic order and checks whether each can stay (virtually re-allocate).
Two checks have to pass.
The first is capacity, `IsTaskAllocatableOnReleasingOrIdle`: does the unclaimed capacity still cover the pod.
It cannot be the plain `IsTaskAllocatable` used by the allocation path, because that reads idle capacity only, and the pod's own resources are sitting in `ReleasingVector` at this point, so it would never fit on the node it is already running on.

The second is the k8s predicates, re-run for the pod against its own node.
This is needed because a workload that fit in idle capacity was allocated and bound during the session without the background pod being visible to the predicates (see above), so restoring the pod can put two pods on a node that must not share one.
Anti-affinity is the clear case: a user pod with a required anti-affinity term matching a background pod passes the filter while that pod is out of the affinity index, fits in the idle capacity next to it, and binds.
Nothing downstream catches this, since kubelet does not enforce inter-pod anti-affinity, so the check has to happen here, and a background pod that now conflicts with what the node received is evicted rather than restored.

The predicates run only on nodes that received a pod during the session.
The plugin records each node's pod keys after the evictions in `OnSessionOpen` and compares them at close, so a node that gained nothing skips the check entirely.
A node that received nothing cannot have gained a conflict, and this keeps the common case, where most nodes are untouched, free of predicate evaluation.
That matters for cost: the inter-pod affinity prefilter walks the cluster's nodes for each pod it is called on, so running it for every background pod on every node would scale badly.
`PrePredicateFn` has to run immediately before `PredicateFn` rather than being reused from earlier in the session, because it is what populates the pod's cycle state and it has to see the arrivals.

A pod that fits is restored with `Statement.Unevict`, which marks its eviction operation undone.
A pod that does not fit stays in the statement.
`Statement.Commit()` then issues real eviction requests for only what remains valid, through `Cache.Evict` — the same path preemption and reclaim victims take, with the same events and metrics.

The displaced set is minimal by construction.
Only pods that no longer fit are evicted, and background pods on nodes that received nothing are restored trivially, which is the common case: most nodes are untouched in any given session and nothing on them is disrupted.

A background pod is not restored onto a *different* node.
Relocating them is a non-goal, KAI cannot move a pod it does not own, and many background pods are pinned to their node by design.

### Plugin lifecycle ordering

Statement operations fire the session's event handlers, and those handlers belong to other plugins.
This gives the plugin two ordering requirements.

It must **open after** any plugin whose handlers should observe the eviction.
Plugin open order follows `config.Tiers` and is deterministic, and the plugin's low priority already places it last.
The proportion plugin's `DeallocateFunc` is the one that matters: it is what removes the background pods from their queue's allocated share for the session.

It must **close before** those plugins, because `Unevict` fires `AllocateFunc` and several plugins discard their session state on close.
Today, `CloseSession` iterates the plugin map, so the order is random and can cause crashes or unexpected behavior.
We propose to close plugins in reverse open order, making the call to CloseSession LIFO - the first plugin to register is the last to run it. 
This is not strictly necessary for this feature (the only conflict we have today is with the proportion plugin, which doesn't really take action in `CloseSession`), but it makes sense for the scheduler as a whole.

### Effect on node ordering

The effect of a background pod on node ordering is minimized, but not zero.
The `nodeavailability` plugin adds its score only to nodes where the task passes `IsTaskAllocatable`, which is checked against idle capacity alone, so a node holding a background pod scores `scores.Availability` (1000) lower than an otherwise identical node whose capacity is actually idle.
Between two equivalent candidates the scheduler takes the one it does not have to disturb, which matches the goal of not displacing background pods for nothing.
Any higher scoring plugin overrides it, since the score constants are spread by orders of magnitude: topology is 100x availability, so a node in the preferred domain is chosen over an idle node outside it, and the background pod there is displaced.

## Usage

1. All background pods need to be marked with the label. By default `kai.scheduler/background: "true"`.
  a. The label can be overridden by a custom selector in the config
2. The background pods need to be assigned to a 0-quota, 0-over-quota-weight queue, by labeling the pods / workload. See the example below.
3. It's recommended to set the `terminationGracePeriodSeconds` for background pods to a very low value (< 5), preferably 0, to prevent them delaying the creation of actual user workloads.




```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: healthcheck
spec:
  replicas: 8
  selector:
    matchLabels:
      app: healthcheck
  template:
    metadata:
      labels:
        app: healthcheck
        kai.scheduler/background: "true"
        kai.scheduler/queue: maintenance
    spec:
      schedulerName: kai-scheduler
      terminationGracePeriodSeconds: 5
      containers:
        - name: main
          image: ubuntu
          command: ["bash", "-c"]
          args: ["trap 'exit 0' TERM; sleep infinity & wait"]
          resources:
            limits:
              nvidia.com/gpu: "1"
```