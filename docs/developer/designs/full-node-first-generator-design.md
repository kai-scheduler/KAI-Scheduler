# FullNodeFirst Reclaim Scenario Generator

## Summary

Add a `FullNodeFirst` scenario generator to the reclaim generator portfolio
([design](./reclaim-generator-portfolio-design.md)). For a pending task that needs a whole
node's worth of a resource, it emits one candidate scenario per **potential node** (a node that
passes the job's predicate checks), "free this node = evict its occupants", before falling back
to `MultiNodeGang`. The simulator and validator remain the sole authority for accepted
solutions; the generator only changes which scenarios are proposed and in what order.

## Motivation

`FullNodeFirst` was proposed in #1790 on the hypothesis that whole-node reclaim on a packed
cluster over-evicts and does `O(N)` scenario search: `NodeLocalGreedy` supposedly cannot cheaply
assemble a whole node's victims, so reclaim falls through to the wide `MultiNodeGang` search.

Measuring it on a **correctly configured** cluster does not bear that out. With a sane queue
quota, the current portfolio already handles the packed single-GPU case optimally: it evicts
exactly one node's worth and solves in a small, flat number of scenarios, and `MultiNodeGang`
never runs.

### Scope

This analysis targets **single-full-resource consumers**: a cluster packed with single-GPU
workloads (a common shape for inference) borrowing idle GPUs, into which a whole-node (8-GPU)
training job arrives and must reclaim. Multi-node gang consumers behave differently (freeing one
node can cascade across nodes, see the [warning below](#known-drawback-addressed-in-phase-2))
and are out of primary scope.

### Measured behavior (real values)

`kind` + `kwok`, `N` nodes of 8 GPU each, packed with reclaimable single-GPU borrowers (8 per
node). One reclaimer requests 8 GPUs on a single node; the minimal solution evicts exactly 8.
Default config (both generators), `main` scheduler, with a correctly sized parent quota:

| Nodes | Borrowers | NodeLocalGreedy | MultiNodeGang | Scenarios | Evicted | Minimal | Evicted ÷ minimal |
|------:|------:|--:|--:|--:|--:|--:|--:|
| 2   | 16   | 15 | 0 | 15 | 8 | 8 | 1× |
| 4   | 32   | 16 | 0 | 16 | 8 | 8 | 1× |
| 8   | 64   | 9  | 0 | 9  | 8 | 8 | 1× |
| 16  | 128  | 9  | 0 | 9  | 8 | 8 | 1× |
| 32  | 256  | 9  | 0 | 9  | 8 | 8 | 1× |
| 64  | 512  | 9  | 0 | 9  | 8 | 8 | 1× |
| 128 | 1024 | 9  | 0 | 9  | 8 | 8 | 1× |

`NodeLocalGreedy` and `MultiNodeGang` are the `state="simulated"` counts per generator; Scenarios
is their sum. `NodeLocalGreedy` solves every case with a small, cluster-size-independent number
of scenarios (~9), evicting exactly one node's worth (8). `MultiNodeGang` never runs. There is
**no over-eviction and no scenario blow-up** in this fixture.

### Correction: the earlier "over-eviction" was a quota artifact

An earlier version of this document reported `8*(N-1)` evictions and `~9*N` scenarios growing
with cluster size. That was wrong. The borrowers were pre-bound directly to nodes (via
`nodeName`), which bypasses KAI admission, so the `exp-parent` queue sat far above its GPU limit.
Reclaim was then correctly enforcing the parent quota by shedding borrowers back under the cap.
The scheduler log makes the real reason explicit:

```
[reclaim] Job: <exp/recl100> is over capacity. Reason: exp-parent quota has reached the
allowable limit of GPUs. Limit is 16 GPUs, currently 32 GPUs allocated and workload requested 8 GPUs
```

With a parent quota that actually allows the borrowing, the numbers collapse to the table above:
evict 8, ~9 scenarios, flat in `N`. This also confirms the review feedback on #2231 that there is
no actual over-eviction here.

### Goals

- Emit per-node whole-node victim scenarios, best-first, for full-node pending tasks.
- Keep the solved-scenario count for this shape small and independent of cluster size, should a
  fixture be found where node-local reclaim degrades (see the open question below).
- Keep every accepted solution validator-approved and whole victim gangs intact (#1537).

### Non-Goals

- Not a replacement for `MultiNodeGang` (runs before it, falls through on non-match).
- No general disruption budget (that is the separate `DisruptionBounded` generator).
- No runtime generator-selection policy (#1757).

## Open question: where does whole-node reclaim actually blow up?

> [!WARNING]
> **This design is not yet justified by a reproduced problem.** On a correctly configured
> cluster the existing portfolio already solves whole-node reclaim in ~9 flat scenarios with
> minimal (one node) eviction, so `FullNodeFirst` shows no measurable benefit in the fixture
> above. Before implementing it, we need a fixture where node-local reclaim genuinely degrades
> for a whole-node job. Candidate scenarios to investigate (all currently unproven):
>
> - **Fragmented / interleaved victims.** When a node's occupants are far apart in the victim
>   priority order, `NodeLocalGreedy`'s accumulator must drain a longer prefix before any single
>   node is fully covered. Round-robin borrower placement (a node's victims interleaved across
>   the queue) raised the count from ~9 to ~58 scenarios at N=8, but it still solved and still
>   evicted 8. That is a mild increase, not a blow-up.
> - **Heterogeneous victim sizes / partial-node usage,** where the whole-node victim set is a
>   specific mix the priority-ordered accumulator reaches at different times.
> - **Multi-node gang victims,** where freeing one node cascades across nodes. This is arguably
>   the `TopologyFirst` / gang generators' territory rather than `FullNodeFirst`.
> - **The maintainers' scale tests** (e.g. the `unschedulable-distributed-job` benchmark), which
>   target distributed gangs, not whole-node consumers.
>
> The design below stands on its own, but the motivating measurement is still open: if none of
> these scenarios make node-local reclaim degrade, `FullNodeFirst` may not be worth adding.

## Design

The generator ships in two phases: Phase 1 emits node-scoped whole-node scenarios; Phase 2 adds
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
one node, the committed victims are exactly that node's occupants, and the generator proposes
these node-scoped sets directly rather than accumulating them from the victim queue. Whether that
is cheaper than `NodeLocalGreedy`'s accumulation depends on the fixture (see the
[open question](#open-question-where-does-whole-node-reclaim-actually-blow-up) above); in the
packed single-GPU case measured, both reach the same minimal solution.

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

- **Rely on the current portfolio (`NodeLocalGreedy` + `MultiNodeGang`):** the default path
  already solves the packed single-GPU case optimally (see [Measured behavior](#measured-behavior-real-values)),
  so this remains the baseline unless a fixture is found where node-local reclaim degrades.
- **`DisruptionBounded` instead:** complementary; bounds victim-set size generally but does not
  exploit whole-node structure to find a node-scoped solution directly.
