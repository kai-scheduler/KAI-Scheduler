# Background pods — POC implementation

A minimal end-to-end implementation of [the design](README.md), enough to run the scenarios in [`examples/`](examples) and see the topology case resolve correctly.

One plugin, in the scheduler.
No binder changes, no API change, no new RBAC.

## Why it is only a scheduler plugin

The first sketch of this POC had the scheduler annotate the BindRequest with a displaced set and a binder plugin carry out the evictions.
That does not work, because of how `Releasing` is accounted.

`Statement.Evict` moves the task to `Releasing` and calls `node.UpdateTask`, and `addTaskResources` treats that status specially (`node_info.go:489`): the capacity is added to `ReleasingVector` and never reaches `IdleVector`.
`IsTaskAllocatable` reads idle only, so a user pod contending for a background pod's capacity fails it.
`IsTaskAllocatableOnReleasingOrIdle` passes, which is the **pipeline** path.
The pod is pipelined rather than allocated, `commitPipeline` never calls `BindPod`, so no BindRequest is created and no mutate hook fires.
Nothing is evicted, so the next session sees the same cluster and pipelines the same pod again, forever.

That behaviour is correct: `Releasing` means the pod is genuinely going away, and until the eviction is committed it is not.
So rather than working around it, the POC leans into it.
Virtual eviction puts capacity into `Releasing`, contending pods pipeline, the scheduler commits the evictions that turned out to be necessary, and those pods bind in the next session once the background pods are actually gone.

This is exactly how KAI already resolves the same ordering problem for preemption victims, which is why it needs no new machinery.
It corresponds to the "scheduler evicts and the pod is pipelined" alternative in the design, not to the preferred approach there.
The trade-off is one extra scheduling cycle for any pod that displaces something, and it lands only on those pods.

## Plugin functionality

`pkg/scheduler/plugins/backgroundpods/`, registered in `pkg/scheduler/plugins/factory.go`, built from `New(framework.PluginArguments) framework.Plugin`.

### 1. Configuration

The selector comes from plugin arguments, defaulting to a label selector on `kai.scheduler/background=true`.
A namespace selector is accepted as an additional conjunct.
Parsed once at construction.

### 2. Session state

Held for the duration of one session, created in `OnSessionOpen` and dropped in `OnSessionClose`:

- the `*framework.Statement` carrying the virtual evictions
- `map[nodeName][]*pod_info.PodInfo`, the background pods removed from each node

Plugin instances are reused across sessions, so this must be reset on every open.

### 3. OnSessionOpen — virtual eviction

1. Create a statement with `ssn.Statement()`.
2. Walk `ssn.Nodes`, and for every pod in `node.PodInfos` matching the selector, call `stmt.Evict(pod, ...)` and record it under its node.

`Statement.Evict` only touches in-memory state.
The API call lives in `commitEvict`, reached solely from `Commit()`, so until the plugin decides to commit, nothing has happened to the pod.

Each evicted background pod's capacity moves into the node's `ReleasingVector`, so `NonAllocatedResource` (idle plus releasing) now reflects a node with the background pods gone, while `IdleVector` does not.

Only background pods that KAI scheduled can be handled: `Statement.Evict` requires the pod's PodGroup to be in `ssn.ClusterInfo.PodGroupInfos`.
Pods with no PodGroup are skipped, which bounds the POC to the opt-in case.

### 4. During the session — nothing

The plugin registers no other hooks and takes no part in the actions.

Allocate, preempt, reclaim, and consolidate see nodes whose non-allocated capacity includes what the background pods were holding.
A user pod that fits in idle is allocated and bound as usual, and no background pod is involved.
A user pod that needs background-held capacity is pipelined onto the node, and its demand is subtracted from `ReleasingVector`.

That subtraction is what makes the next step work: after the actions, a node's remaining idle-plus-releasing is exactly the capacity that nothing has claimed.

### 5. OnSessionClose — re-placement and commit

For each node that had background pods, walk them in a deterministic order and ask whether each can stay:

- `node.IsTaskAllocatableOnReleasingOrIdle(pod)` — does the unclaimed capacity still cover it
- `ssn.PredicateFn(pod, job, node)` — does it still satisfy the node's predicates

If both pass, call `stmt.Unevict(pod)` (`statement.go:506`) to restore it, which returns its capacity to `Releasing` and takes it out of the statement.
If either fails, leave the eviction in the statement.

Then call `stmt.Commit()`.
Only the evictions still in the statement are committed, and those become real eviction requests through `Cache.Evict` — the same path preemption and reclaim victims take, with the same events and metrics.

Restoring one pod reduces what is left for the next, so the order decides which of several competing background pods on a node survives.
Per the design's non-goals, no effort goes into choosing a good order, but it must be deterministic.

Worked example, on an eight-GPU node holding two background pods of one GPU each:

| state | idle | releasing | unclaimed |
|---|---|---|---|
| start | 6 | 0 | 6 |
| after virtual eviction | 6 | 2 | 8 |
| after a 7-GPU pod is pipelined | 6 | -5 | 1 |
| after restoring the first background pod | 6 | -6 | 0 |

The second background pod needs 1 and has 0 left, so its eviction commits.
Exactly one pod is evicted, which is the minimum.

### 6. Next session

The evicted background pods are gone for real, so their capacity is idle.
The pipelined user pod now passes `IsTaskAllocatable`, is allocated, and binds normally.

The evicted background pods are themselves KAI workloads with a pending pod, so KAI will place them again whenever capacity frees up.
Nothing special is needed for that, and it gives the "come back later" behaviour for free.

## Known gaps

Acceptable for a POC, listed so they are not mistaken for working.

**Background pods stay visible to predicates.** `Statement.Evict` uses `node.UpdateTask`, which is `RemoveTask` followed by `addTask`, so the pod remains in `node.PodInfos` and is re-added to `PodAffinityInfo`.
A user pod with anti-affinity against a background pod will still have the node rejected, and topology spread still counts it.
This fails safe — the node is not chosen, so nothing is misplaced and nothing is evicted — but the "fully invisible" property in the design is not met for these constraints.

**One extra scheduling cycle** for every pod that displaces something.

**Evictions can be wasted.** If the pipelined job disappears, or fails to allocate next session, the background pods were evicted for nothing.
Nothing corrupts; the next session re-derives from real cluster state.

**Flapping is not handled.** An evicted background pod may be rescheduled onto another node and displaced again there.

**No fractional GPU, MIG, or DRA.** The selector should not match background pods holding these.

**PDBs are not honoured.** Eviction goes through the existing evictor, which issues a direct delete.

## Things to verify early

The parts most likely not to behave as written.

- **The pipeline path is actually taken.** Confirm that a job whose tasks only fit on idle-plus-releasing is pipelined by the allocate action rather than skipped, and that this holds for gang jobs where only some tasks contend with background pods.
- **`Unevict` restores cleanly.** It goes through `unevict`, which replays status, GPU groups, NUMA placement, and resource claims.
  Confirm the node's vectors return exactly to their pre-eviction values, including for GPU-sharing nodes.
- **Bulk eviction at session open is not treated as churn.** Nothing should interpret a large number of virtual evictions as preemption activity, in metrics, in PodGroup conditions, or in `LastEvictionTimestamp`.
- **Background pod PodGroups do not react.** Their gang state now has a `Releasing` member; confirm no action tries to reschedule or otherwise act on it during the same session.
- **`commitEvict` failures.** The existing code already calls `unevict` when the API eviction fails, so a partial failure should leave the node consistent. Confirm.

## Testing

Unit tests in the plugin package: node vector accounting after virtual eviction, restore-versus-commit decisions against a table of node states, and determinism of the ordering.

End-to-end, reusing [`examples/`](examples) with the background pods moved to an ordinary queue and given the background label:

- Idle cluster: background pods run, nothing is evicted.
- One full-node pod: exactly one background pod evicted, and the user pod binds in the following cycle.
- Topology scenario: all four job pods land in one rack and the two background pods in that rack are evicted.
  This is the case the current workaround gets wrong, so it is the one worth watching.
- A node that received nothing keeps all of its background pods.
