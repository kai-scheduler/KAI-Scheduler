# FullNodeFirst Reclaim Scenario Generator

## Summary

Add a `FullNodeFirst` scenario generator to the reclaim generator portfolio
([design](./reclaim-generator-portfolio-design.md)). For a pending task that needs a whole
node's worth of a resource, it emits one candidate scenario per feasible node, "free this
node = evict exactly its occupants", before falling back to `MultiNodeGang`. The simulator and
validator remain the sole authority for accepted solutions; the generator only changes which
scenarios are proposed and in what order.

## Motivation

The default portfolio runs `NodeLocalGreedy` then `MultiNodeGang`. For a whole-node job on a
fragmented cluster, `NodeLocalGreedy` cannot free a single node, so reclaim falls through to
`MultiNodeGang`, which accumulates victims in node-agnostic priority order and commits the whole
accumulated set. The result is severe **over-eviction** that scales with cluster size: placing
one whole-node job evicts far more than the one node's worth actually required.

### Proof at scale

`kind`+`kwok`, `N` nodes of 8 GPU each, packed with reclaimable single-GPU borrowers (8/node).
One reclaimer requests 8 GPUs on a single node. Ideal is to evict exactly 8. Default config
(both generators), `main` scheduler:

| Nodes | Borrowers | NodeLocalGreedy | MultiNodeGang | Scenarios | Evicted | Ideal | Over-eviction |
|------:|------:|--:|--:|--:|--:|--:|--:|
| 2   | 16   | 15   | 0   | 15   | 8    | 8 | 1× |
| 4   | 32   | 32   | 4   | 36   | 24   | 8 | 3× |
| 8   | 64   | 64   | 8   | 72   | 56   | 8 | 7× |
| 16  | 128  | 128  | 19  | 147  | 120  | 8 | 15× |
| 32  | 256  | 256  | 33  | 289  | 248  | 8 | 31× |
| 64  | 512  | 512  | 64  | 576  | 504  | 8 | 63× |
| 128 | 1024 | 1024 | 128 | 1152 | 1016 | 8 | **127×** |

NodeLocalGreedy and MultiNodeGang are the `state="simulated"` counts per generator; Scenarios is
their sum. NodeLocalGreedy grows as `8·N` and MultiNodeGang as `≈N`. At N=2 MultiNodeGang is 0
and eviction is the ideal 8: NodeLocalGreedy solves that case alone. From N=4 onward MultiNodeGang
activates and eviction follows `8·(N-1)`.

### Root cause

1. **Node-agnostic victim order**: `JobsOrderByQueues.PopNextJob` pops victims by queue/priority,
   not by node, so `MultiNodeGang`'s accumulator frees ~1 GPU per node; no node reaches full-free
   until almost the whole queue is consumed.
2. **Whole-set commit**: `byPodSolver.solve` evicts the entire accumulated set
   (`EvictAllPreemptees`) and never trims to the minimal per-node subset.

`FullNodeFirst` proposes per-node whole-node victim sets directly, so the first solved scenario
is already minimal: ~1 scenario, exactly 8 evicted, independent of `N`.

### Live evidence (single controlled run, N=64)

One reclaimer (8 GPU, single node) submitted against 64 nodes of 8 GPU packed with 512
single-GPU borrowers. Scheduler counters were zeroed immediately before submission; the snapshot
below is taken at the moment the reclaimer schedules.

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

`kai_pod_group_evicted_pods_total{action="reclaim",...}` emitted 504 series (one per evicted
pod group), each with value 1.

The Scenarios column in the table above counts `state="simulated"`. For this run that is 512
(NodeLocalGreedy) plus 64 (MultiNodeGang), which is the 576 shown in the N=64 row. The `emitted`
and `duplicate` states are additional scenarios the portfolio proposed but fingerprint-skipped
before simulation, and are not part of the table.

The Evicted column (504) is visible in both the metric above and the scheduler log line below,
whose victim list contains 504 task entries. The reclaimer's 8-GPU request appears in the log as
the resource vector `<[0 2.68435456e+08 8 1 0 0 0 0 0]>`. The Scenarios count is metric-only at
this verbosity; per-scenario log lines require `V(5)`.

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
- Reduce both solved-scenario count and evictions for this shape to O(1) in cluster size.
- Keep every accepted solution validator-approved and whole victim gangs intact (#1537).

### Non-Goals

- Not a replacement for `MultiNodeGang` (runs before it, falls through on non-match).
- No general disruption budget (that is the separate `DisruptionBounded` generator).
- No runtime generator-selection policy (#1757).

## Design

The generator ships in two phases: Phase 1 delivers the correctness/minimality fix; Phase 2
adds node scoring to pick the cheapest node among valid candidates.

### Phase 1: Whole-node scenarios (correctness)

On each solve attempt:

1. **Necessary-condition check**: cheaply match only probes whose per-task request meets a
   feasible node's full allocatable; otherwise emit nothing and let the portfolio advance.
2. **Per-node scenario derivation**: for each feasible node, build the victim set that frees
   that whole node (including recorded victims from earlier probes), keeping whole gangs intact.
3. **Emit best-first** through the normal portfolio, under the existing generator time budget.

In Phase 1, candidate nodes are ordered by **reusing the existing priority-based victim
ordering**, no new scoring. The solver accepts the first solved, minimal scenario.

This alone removes the `8·(N-1)` over-eviction. The blow-up in `MultiNodeGang` comes from two
compounding properties: victims are popped in node-agnostic priority order, so freed capacity
lands fragmented (~1 GPU per node), and the solver commits the *entire* accumulated victim set
rather than trimming to what the winning placement actually used. Because no single node reaches
full-free until nearly the whole victim queue is drained, the committed set balloons to almost
every reclaimable gang on the cluster.

`FullNodeFirst` breaks that chain by construction. Each scenario it emits is scoped to exactly
one node, its victim set is precisely the gangs occupying that node, and nothing else can be
added to it. So when a scenario solves, the committed victims are exactly that node's occupants:
one node's worth (8 in the fixture), whether the cluster has 2 nodes or 128. The number of
victims is bounded by node capacity, not by how far the search had to walk, turning the
`8·(N-1)`, cluster-size-dependent eviction count into a flat constant.

> [!WARNING]
> **Known drawback (addressed in Phase 2):** Phase 1 bounds *how many* victims free the target
> node, but not *which* node is chosen, and it ignores how entangled that node's occupants are
> with the rest of the cluster. Two ripple effects can follow:
>
> 1. **Rescheduling churn:** evicted gangs return to pending, and the scheduler may re-place them
>    by preempting on other nodes.
> 2. **Cross-node gang tear-down:** victims are evicted as whole gangs (per #1537), because gang
>    scheduling is all-or-nothing. If a gang occupying the chosen node also has members on other
>    nodes, freeing this one node evicts those members too. Dropping any gang below its minimum
>    membership makes the *entire* gang non-viable, so its pods on every other node are torn down
>    as well. Choosing a node packed with multi-node gangs therefore spreads disruption
>    cluster-wide even though only one node's capacity was needed (and the total pods evicted can
>    exceed one node's worth, unlike the single-pod-gang fixture above).
>
> Phase 1 keeps the *direct* freed capacity at one node; minimizing these second-order ripples is
> exactly what the Phase 2 `killCost` node scoring does, by scoring gang breakage and preferring
> nodes whose victims are self-contained and fully reclaimable.

### Phase 2: Node scoring (killCost)

Phase 1 makes every emitted scenario minimal but treats all qualifying nodes as equal, it
frees whatever node the default ordering surfaces first. Phase 2 makes the generator emit
candidate nodes **cheapest-first** by a disruption score, so that among many valid single-node
solutions it picks the one that hurts running work the least.

**Scoring inputs (per candidate node).** For the set of gangs that would be evicted to free
node `n`, compute a `killCost(n)` that increases with:

- **Number of victim gangs** on the node: fewer disrupted workloads is better.
- **Victim priority / importance**: prefer evicting lower-priority, preemptible gangs; strongly
  penalize higher-priority or non-preemptible ones.
- **Gang breakage**: penalize evicting a task that drops a gang below its minimum membership,
  which cascades to killing the whole gang, including its members on other nodes, versus
  evicting fully-reclaimable filler.
- **In-quota vs. borrowing**: prefer reclaiming capacity from gangs running *above* their
  queue's deserved share over those within quota.
- **Age / runtime** (optional): avoid repeatedly evicting the same near-complete work; interacts
  with `min-runtime`.

**Emission order.** Nodes are sorted by ascending `killCost` and emitted best-first, so the
first solved scenario is both minimal (Phase 1) and lowest-disruption (Phase 2). The simulator
and validator remain authoritative; scoring only changes emission order, never acceptance.

**Bounds / relationship to `DisruptionBounded`.** `killCost` here selects the cheapest single
*full node*; it is not a global disruption budget. The separate `DisruptionBounded` generator
remains the general mechanism for capping victim-set size across non-full-node shapes; Phase 2
can reuse its cost primitives where practical.

## Test Plan

- Unit: necessary-condition matching (incl. heterogeneous capacities); per-node minimal victim
  derivation with whole gangs preserved and recorded victims composed in.
- Unit (Phase 2): `killCost` ordering, given nodes with differing victim priority/count/gang
  breakage, the lowest-cost node is emitted first.
- Scale: reproduce the fixture above; assert evicted == one node's worth and solved-scenario
  count is O(1) in `N`.
- Regression: non-full-node jobs are unaffected (generator emits nothing).

## Future Work

- **Smart generator selection** (#1757) could prioritize `FullNodeFirst` for matching workloads.
- **Multi-full-resource shapes** beyond a single countable resource (e.g. CPU/memory-defined
  full nodes) once the GPU-count case is validated.

## Alternatives

- **Rely on `MultiNodeGang`**: rejected: demonstrated O(N) over-eviction and search cost.
- **`DisruptionBounded` instead**: complementary; bounds victim-set size generally but does not
  exploit whole-node structure to find the minimal set directly.
