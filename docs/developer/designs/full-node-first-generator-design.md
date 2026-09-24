# FullNodeFirst Reclaim Scenario Generator

## Summary

Add a `FullNodeFirst` scenario generator to the reclaim generator portfolio
([design](./reclaim-generator-portfolio-design.md)). For a pending task that needs a whole
node's worth of a resource, it emits one candidate scenario per **potential node** (a node that
passes the job's predicate checks), "free this node = evict its occupants", before falling back
to `MultiNodeGang`. The simulator and validator remain the sole authority for accepted
solutions; the generator only changes which scenarios are proposed and in what order.

## Motivation

The default portfolio runs `NodeLocalGreedy` then `MultiNodeGang`. For a whole-node job on a
packed cluster, `NodeLocalGreedy` cannot free a single node (all of its per-node scenarios come
back unsolved), so reclaim falls through to `MultiNodeGang`, which accumulates victims in
node-agnostic priority order until the job fits somewhere. This produces two costs that both
grow with cluster size:

1. **Scenario-search blow-up.** The portfolio simulates `~9*N` scenarios (plus thousands of
   fingerprint-deduplicated duplicates) to place one job. This is the scalability concern
   #1790 targets.
2. **Eviction/restart churn.** The winning reclaim decision evicts a broad, cluster-wide set of
   running consumers (`8*(N-1)` in the fixture below) rather than the one node's worth actually
   required.

> [!NOTE]
> This is **not** permanent over-eviction. Most of those victims are pipelined (their freed
> capacity is reserved for them to reschedule), so the net *permanent* displacement is bounded
> by the single node the job lands on. The real harm is that many running consumers are evicted
> and must restart/rebind, plus the wasted search. See
> [Gross vs net eviction](#gross-vs-net-eviction).

### Scope

This analysis targets **single-full-resource consumers**: a cluster packed with single-GPU
workloads (a common shape for inference) borrowing idle GPUs, into which a whole-node (8-GPU)
training job arrives and must reclaim. Multi-node gang consumers behave differently (freeing one
node can cascade across nodes, see the [warning below](#known-drawback-addressed-in-phase-2))
and are out of primary scope.

### Proof at scale

`kind` + `kwok`, `N` nodes of 8 GPU each, packed with reclaimable single-GPU borrowers (8 per
node). One reclaimer requests 8 GPUs on a single node. The minimal solution evicts exactly 8.
Default config (both generators), `main` scheduler:

| Nodes | Borrowers | NodeLocalGreedy | MultiNodeGang | Scenarios | Evicted (gross) | Minimal | Evicted ÷ minimal |
|------:|------:|--:|--:|--:|--:|--:|--:|
| 2   | 16   | 15   | 0   | 15   | 8    | 8 | 1× |
| 4   | 32   | 32   | 4   | 36   | 24   | 8 | 3× |
| 8   | 64   | 64   | 8   | 72   | 56   | 8 | 7× |
| 16  | 128  | 128  | 19  | 147  | 120  | 8 | 15× |
| 32  | 256  | 256  | 33  | 289  | 248  | 8 | 31× |
| 64  | 512  | 512  | 64  | 576  | 504  | 8 | 63× |
| 128 | 1024 | 1024 | 128 | 1152 | 1016 | 8 | **127×** |

NodeLocalGreedy and MultiNodeGang are the `state="simulated"` counts per generator; Scenarios is
their sum. NodeLocalGreedy grows as `8*N`, MultiNodeGang as `~N`. At N=2 MultiNodeGang is 0 and
eviction is the minimal 8: NodeLocalGreedy solves that case alone. From N=4 onward MultiNodeGang
activates, and gross eviction follows `8*(N-1)`.

### Root cause

1. **Node-agnostic victim order.** `JobsOrderByQueues.PopNextJob` pops victims by queue/priority,
   not by node, so `MultiNodeGang`'s accumulator frees ~1 GPU per node; no node reaches full-free
   until nearly the whole victim queue is consumed.
2. **Broad accumulated victim set.** `byPodSolver.solve` evicts the accumulated set
   (`EvictAllPreemptees`), then re-allocates the victims the preemptor does not need (they become
   pipelined). The committed set is `preempted + pipelined`; the pipelined majority reschedule,
   but every one of them is evicted first, which is the churn.

`FullNodeFirst` scopes each scenario to a single node, so the winning scenario evicts one node's
worth instead of a cluster-wide set, cutting both the search and the churn.

### Gross vs net eviction

The Evicted column is **gross** eviction (pods actually evicted at commit), confirmed by the
metric and log below. It is not net permanent displacement:

- Submitting fresh borrowers onto the "freed" capacity after a reclaim leaves them **Pending**:
  the capacity is reserved for the pipelined victims, not released. So most of the 504 are
  reschedules, and net permanent displacement is bounded by the one node the job takes.
- The fixture uses bare pods with a fixed `nodeName`, so pipelined victims cannot recreate to
  claim their reserved slots; that is why the fixture's running count stays low and the gross
  number looks permanent. A managed (reschedulable) workload would restart and rebind.
- Toggling `allowConsolidatingReclaim` (off by default) does not change the gross count: N=64
  evicts 504 with the flag both off and on.

The point stands for both costs that scale: `~9*N` wasted scenarios, and `8*(N-1)` running
consumers evicted and restarted to place a single whole-node job.

### Live evidence (single controlled run, N=64)

One reclaimer (8 GPU, single node) submitted against 64 nodes of 8 GPU packed with 512
single-GPU borrowers. Scheduler counters were zeroed immediately before submission; the snapshot
is taken at the moment the reclaimer schedules.

Metrics (`/metrics`, verbatim):

```
kai_scenario_search_scenarios_total{action="reclaim",generator="NodeLocalGreedy",state="emitted"} 512
kai_scenario_search_scenarios_total{action="reclaim",generator="NodeLocalGreedy",state="simulated"} 512
kai_scenario_search_scenarios_total{action="reclaim",generator="MultiNodeGang",state="emitted"} 3073
kai_scenario_search_scenarios_total{action="reclaim",generator="MultiNodeGang",state="simulated"} 64
kai_scenario_search_scenarios_total{action="reclaim",generator="MultiNodeGang",state="duplicate"} 3009
kai_scenario_search_duration_seconds_count{action="reclaim",generator="NodeLocalGreedy",result="unsolved"} 512
kai_scenario_search_duration_seconds_count{action="reclaim",generator="NodeLocalGreedy",result="generators_exhausted"} 1
kai_scenario_search_duration_seconds_count{action="reclaim",generator="MultiNodeGang",result="solved"} 2
kai_scenario_search_duration_seconds_count{action="reclaim",generator="MultiNodeGang",result="unsolved"} 62
kai_scenario_search_duration_seconds_count{action="reclaim",generator="MultiNodeGang",result="duplicate"} 3009
```

`kai_pod_group_evicted_pods_total{action="reclaim",...}` emitted 504 series (one per evicted pod
group), each with value 1. NodeLocalGreedy simulated 512 scenarios (`8*N`) and solved none
(`unsolved=512`, then `generators_exhausted`); MultiNodeGang solved the placement but at the cost
of 3009 fingerprint-deduplicated duplicates.

The Scenarios column counts `state="simulated"` (512 + 64 = 576 for the N=64 row). The `emitted`
and `duplicate` states are proposed-but-skipped and are not in the table. Evicted (504) appears
in both the metric and the scheduler log line below (its victim list is 504 entries). The
reclaimer's 8-GPU request appears as the resource vector `<[0 2.68435456e+08 8 1 0 0 0 0 0]>`.
Per-scenario log lines require `V(5)`; the scenario counts are metric-only at this verbosity.

Scheduler logs (verbatim; the reclaim line's victim list is 504 entries, truncated here for
length):

```
2026-09-24T01:33:47.377Z	INFO	reclaim/reclaim.go:122	[RGCSvL] [reclaim] Attempting to reclaim for job: <exp/recl100> of queue <reclaimer-q>, resources: <[0 2.68435456e+08 8 1 0 0 0 0 0]>
2026-09-24T01:33:48.806Z	INFO	reclaim/reclaim.go:99	[RGCSvL] [reclaim] Reclaimed resources for job <exp/recl100>, evicting reclaimee tasks: <[<exp/bb70> <exp/bb352> <exp/bb408> <exp/bb236> <exp/bb24> <exp/bb228> <exp/bb34> <exp/bb511> ... ]>
2026-09-24T01:33:53.920Z	INFO	allocate/allocate.go:108	[yV7MUv] [allocate] Succesfully allocated resources for job: <exp/recl100>
2026-09-24T01:33:53.920Z	INFO	cache/cache.go:335	[yV7MUv] [allocate] Creating bind request for task <exp/recl100> to node <kwok-1> gpuGroup: <[]>, requires: <[0 2.68435456e+08 8 1 0 0 0 0 0]> GPUs
```

### Goals

- Emit per-node whole-node victim scenarios, best-first, for full-node pending tasks.
- Reduce solved-scenario count for this shape to O(1) in cluster size.
- Reduce eviction churn to the occupants of the one node the job lands on.
- Keep every accepted solution validator-approved and whole victim gangs intact (#1537).

### Non-Goals

- Not a replacement for `MultiNodeGang` (runs before it, falls through on non-match).
- No general disruption budget (that is the separate `DisruptionBounded` generator).
- No runtime generator-selection policy (#1757).

## Design

The generator ships in two phases: Phase 1 delivers the search-and-churn reduction; Phase 2 adds
node scoring to pick the cheapest node among valid candidates.

### Phase 1: Whole-node scenarios

On each solve attempt:

1. **Necessary-condition check.** Cheaply match only probes whose per-task request needs a whole
   node's worth of a resource; otherwise emit nothing and let the portfolio advance.
2. **Potential-node set.** Candidate nodes are the nodes that pass the job's predicate checks
   (`ssn.PredicateFn`: node pool, affinity, taints, and total-capacity fit such as an 8-GPU pod
   not matching a 4-GPU node). This is deliberately **not** `FeasibleNodesForJob`, which filters
   by idle/releasing GPUs and returns nothing on a fully packed cluster. Potential nodes include
   occupied nodes the job could run on once freed.
3. **Per-node scenario derivation.** For each potential node, build the victim set that frees
   that whole node (including recorded victims from earlier probes), keeping whole gangs intact.
4. **Emit best-first** through the normal portfolio, under the existing generator time budget.

In Phase 1, candidate nodes are ordered by reusing the existing priority-based victim ordering,
no new scoring. The solver accepts the first solved scenario. Because each scenario is scoped to
one node, the committed victims are that node's occupants (8 in the fixture) instead of the
`8*(N-1)` cluster-wide set, and the search terminates in O(1) solved scenarios instead of `~9*N`.

> [!WARNING]
> **Known drawback (addressed in Phase 2).** Phase 1 bounds *how many* victims free the target
> node, but not *which* node is chosen, and it ignores how entangled that node's occupants are
> with the rest of the cluster:
>
> 1. **Rescheduling churn.** Evicted gangs return to pending, and the scheduler may re-place them
>    by preempting on other nodes.
> 2. **Cross-node gang tear-down.** Victims are evicted as whole gangs (per #1537), because gang
>    scheduling is all-or-nothing. If a gang occupying the chosen node also has members on other
>    nodes, freeing this node evicts those members too, and dropping any gang below its minimum
>    membership tears down the entire gang across every node it occupies. This is the case
>    outside the single-GPU scope above, where the clean "one node's worth" result does not hold.
>
> Minimizing these second-order effects is exactly what the Phase 2 `killCost` node scoring does.

### Phase 2: Node scoring (killCost)

Phase 1 makes every emitted scenario node-scoped but treats all potential nodes as equal. Phase 2
emits candidate nodes **cheapest-first** by a disruption score, so among many valid single-node
solutions it picks the one that hurts running work the least.

**Scoring inputs (per candidate node).** For the set of gangs that would be evicted to free
node `n`, compute a `killCost(n)` that increases with:

- **Number of victim gangs** on the node: fewer disrupted workloads is better.
- **Victim priority / importance:** prefer evicting lower-priority, preemptible gangs; strongly
  penalize higher-priority or non-preemptible ones.
- **Gang breakage:** penalize evicting a task that drops a gang below its minimum membership,
  which cascades to killing the whole gang, including its members on other nodes, versus
  evicting fully-reclaimable filler.
- **In-quota vs. borrowing:** prefer reclaiming from gangs running above their queue's deserved
  share over those within quota.
- **Age / runtime** (optional): avoid repeatedly evicting the same near-complete work; interacts
  with `min-runtime`.

**Emission order.** Nodes are sorted by ascending `killCost` and emitted best-first, so the first
solved scenario is both node-scoped (Phase 1) and lowest-disruption (Phase 2). The simulator and
validator remain authoritative; scoring only changes emission order, never acceptance.

**Relationship to `DisruptionBounded`.** `killCost` selects the cheapest single full node; it is
not a global disruption budget. The separate `DisruptionBounded` generator remains the general
mechanism for capping victim-set size across non-full-node shapes; Phase 2 can reuse its cost
primitives where practical.

## Test Plan

- Unit: necessary-condition matching (incl. heterogeneous capacities); potential-node selection
  via predicate checks; per-node minimal victim derivation with whole gangs preserved.
- Unit (Phase 2): `killCost` ordering, given nodes with differing victim priority/count/gang
  breakage, the lowest-cost node is emitted first.
- Scale: reproduce the fixture; assert solved-scenario count is O(1) in `N` and committed
  eviction is the target node's occupants.
- Regression: non-full-node jobs are unaffected (generator emits nothing).

## Future Work

- **Smart generator selection** (#1757) could prioritize `FullNodeFirst` for matching workloads.
- **Multi-full-resource shapes** beyond a single countable resource (e.g. CPU/memory-defined full
  nodes) once the GPU-count case is validated.
- **Multi-node gang consumers**, where freeing one node cascades; needs the Phase 2 gang-breakage
  scoring to be effective.

## Alternatives

- **Rely on `MultiNodeGang`:** rejected; demonstrated `~9*N` search and `8*(N-1)` eviction churn
  for a common single-GPU-consumer shape.
- **`DisruptionBounded` instead:** complementary; bounds victim-set size generally but does not
  exploit whole-node structure to find a node-scoped solution directly.
