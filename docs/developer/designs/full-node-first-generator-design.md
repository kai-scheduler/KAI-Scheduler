# FullNodeFirst Reclaim Scenario Generator

## Summary

`FullNodeFirst` (`sg-fullnodefirst`) is a reclaim scenario generator for the reclaim generator
portfolio ([portfolio design](./reclaim-generator-portfolio-design.md)). For a pending gang
whose every task needs a **whole node's worth** of a resource, it scans the nodes once and
proposes freeing one reclaimable node per task — a node-scoped "free this node = evict its
occupants" scenario — instead of searching victim *combinations*.

The point of the generator is **how fast reclaim finds a set of victims to evict, not how many
it evicts.** All three generators reach the same minimal eviction on these shapes; what differs
by orders of magnitude is the scenario-search **time**. `NodeLocalGreedy` and `MultiNodeGang`
both search combinatorially and slow down as the cluster grows and as victims fragment across
priority tiers and gangs; `FullNodeFirst` proposes node-scoped scenarios directly and stays in
near-constant time. It only *proposes* — the simulator and post-sim validator remain
authoritative, and victim gangs are kept whole (#1537).

This document describes what the generator does, **the scenarios whose search it speeds up**, and
**the reclaimers for which it should be skipped**, backed by the PR's live and offline benchmarks
plus a sub-node control.

## Motivation

`FullNodeFirst` was proposed in #1790. The bottleneck for whole-node reclaim on a large cluster
is **scenario-search time**. `NodeLocalGreedy` assembles victims by draining a prefix of the
priority-ordered victim queue until one node is covered; `MultiNodeGang` searches gang-aware
victim combinations. Both costs grow with cluster size, and worsen when a node's occupants are
**scattered across many priority tiers and small gangs** (the common fragmented cluster). On
large clusters this is seconds to minutes of reclaim search per cycle, and can hit the
per-generator search budget and time out.

A generator that reasons about nodes directly can emit the node-scoped victim set in
near-constant time, independent of cluster size and victim fragmentation.

## Design

Reclaim-only (`NewFullNodeFirstGenerator` returns nil otherwise). On first `Next()` it
initializes, then emits scenarios until its demand is met or it is exhausted.

### Necessary condition (the gate that decides when it runs)

1. **Per-size demand.** Pending tasks are grouped by request into `sizeDemand{req, count}` (so
   heterogeneous whole-node sizes are supported). Inert if there are no pending tasks or any task
   requests nothing.
2. **Whole-node match.** A node matches a size only if `needsEntireNode(req, node)` — the task
   request exactly equals at least one of the node's allocatable resource dimensions (e.g. an
   8-GPU task on an 8-GPU node). A node is skipped if it is too small, if the size already fits on
   the node's idle capacity (no reclaim needed), or if a prior probe recorded it as freed.

The whole-node match is the key gate: **a task that does not fully fill a node dimension matches
no node, so the generator emits nothing and the portfolio falls through to the next generator.**
That is what makes `FullNodeFirst` safe to keep in the portfolio — a no-op for reclaimers it does
not serve.

### Scenario derivation

For a matched node, `reclaimableFor` collects the node's reclaimable victim **gangs** (reusing
`ssn.ReclaimVictimFilter`, excluding the reclaimer's own queue) and checks idle + reclaimable
covers the task. `Next()` scans nodes once, assembling a window of one matched node per demanded
task, and emits a single `ByNodeScenario` evicting those nodes' whole gangs (plus any recorded
victims from earlier incremental gang probes). The solver commits the first solved scenario.

### Phases

- **Phase 1 (this PR):** emit node-scoped whole-node scenarios. Bounds *how many* victims free a
  node, but treats all matching nodes as interchangeable.
- **Phase 2 (future):** `killCost` node scoring to emit candidate nodes cheapest-first (fewest /
  lower-priority victims, penalize cross-node gang breakage, prefer over-quota borrowers). Phase 1
  does not choose *which* node to free; see [Limitations](#limitations-and-when-to-skip).

## When it helps: search time vs NodeLocalGreedy and MultiNodeGang

All numbers are the scheduler's own reclaim scenario-search time
(`kai_scenario_search_duration_seconds_sum{action="reclaim"}`), **lower is better**. Every
generator seats the reclaimer with the same minimal eviction; only the time to find the victims
differs.

### Live kind + kwok

Shape: `N` × 8-GPU nodes fully packed by cross-node victim gangs of sizes **2/4/8** across **8
priority tiers**; reclaimer = gang of **4 × 8-GPU** (needs 4 whole nodes freed). Victims
over-quota, reclaimer under-quota.

| Nodes (victim pods) | NodeLocalGreedy (s) | MultiNodeGang (s) | FullNodeFirst (s) |
|--------------------:|--------------------:|------------------:|------------------:|
| 1024 (8,192)        | 10.1                | 10.2              | **0.024** |
| 2048 (16,384)       | 118.6               | 20.2              | **0.070** |
| 4096 (32,768)       | >600 (timed out)    | 23.4              | **0.037** |

### Offline benchmark (per-reclaim search time, s)

Reclaimer = gang of **8 × 8-GPU**; 8-GPU nodes; 8 fill rounds. `†` ≈ the ~30 s per-generator
`NodeLocalGreedy` budget (a single large simulation can overrun).

**8 priority tiers, victim gang size 2** (highly fragmented — the #1790 shape):

| Nodes | NodeLocalGreedy (s) | MultiNodeGang (s) | FullNodeFirst (s) |
|------:|--------------------:|------------------:|------------------:|
| 1024  | 3.35                | 0.75              | **0.018** |
| 2048  | 22.0                | 6.06              | **0.048** |
| 4096  | 30†                 | 18.9              | **0.029** |
| 8192  | 44.6                | 96.9              | **0.037** |

**8 priority tiers, victim gang size 8:**

| Nodes | NodeLocalGreedy (s) | MultiNodeGang (s) | FullNodeFirst (s) |
|------:|--------------------:|------------------:|------------------:|
| 1024  | 8.29                | 0.31              | **0.126** |
| 2048  | 18.6                | 0.80              | **0.137** |
| 4096  | 30†                 | 3.66              | **0.130** |
| 8192  | 30†                 | 19.6              | **0.130** |

(The same-priority variants of these shapes behave similarly: `NodeLocalGreedy` tens of seconds,
`MultiNodeGang` cheap at small `N` but climbing past `MultiNodeGang` 90 s+ at 8192 with small
gangs, `FullNodeFirst` flat at tens of milliseconds.)

**Reading the data.** `NodeLocalGreedy` grows with cluster size and times out past a few thousand
nodes. `MultiNodeGang` is much better than `NodeLocalGreedy` but is **not** constant: with many
small victim gangs spread over priority tiers its search still climbs into tens of seconds
(96.9 s at 8192 nodes). `FullNodeFirst` stays at tens of milliseconds across every size and
fragmentation, because it proposes node-scoped scenarios instead of searching victim
combinations. That constant-time target-finding — against **both** existing generators — is the
contribution.

## Limitations and when to skip

### Skip for sub-node reclaimers (small pods / less than a full node)

If the reclaimer's tasks do not each fill a node, the whole-node gate matches nothing and
`FullNodeFirst` contributes nothing; it should be skipped for such reclaimers (today a harmless
no-op; a generator-selection policy, #1757, could avoid constructing it).

Control (8 nodes, 64 single-GPU borrowers, full portfolio FNF→NLG→MNG), **scenarios simulated**:

| Reclaimer | Request type | FullNodeFirst | NodeLocalGreedy | Evicted | Minimal | Solved by |
|----------:|--------------|--------------:|----------------:|--------:|--------:|-----------|
| 8 GPU | whole-node | **2** | 0 | 8 | 8 | FullNodeFirst |
| 4 GPU | sub-node   | **0** | 2 | 4 | 4 | NodeLocalGreedy |

The 4-GPU reclaimer is correctly served by `NodeLocalGreedy` (minimal eviction 4); `FullNodeFirst`
rightly emits nothing rather than over-evicting a whole node.

### No speedup when victim search is already cheap

When the cluster is **not** fragmented — few priority tiers and victims already in whole-node-sized
gangs — `NodeLocalGreedy` and `MultiNodeGang` already find targets in milliseconds, and
`FullNodeFirst` offers no speedup (it is comparable or marginally slower; e.g. same-priority,
gang-size-8 shapes run ~0.02–0.13 s for all three). `FullNodeFirst`'s advantage is specifically
the **large, fragmented** whole-node case.

### Deprioritize for gangs whose victims span many nodes (until Phase 2)

Phase 1 bounds how many victims free a node but not *which* node, and ignores cross-node
entanglement. Because victims are evicted as whole gangs, freeing a node hosting one member of a
wide gang drops that gang below its minimum and tears down all its members across every node it
occupies. Choosing the lowest-disruption node is the Phase 2 `killCost` job; the cross-node
cascade is also addressed at victim-ordering level by `gangcascadeorder`. Until Phase 2,
`FullNodeFirst` is best suited to single-full-resource consumers (a whole-node job reclaiming
smaller borrowers).

### Adversarial node ordering grows the scan

When reclaimable nodes are sparse and late in scan order, the per-scan-window emission can grow
roughly linearly in `N` rather than staying flat; the result is still correct and minimal, only
the search is larger. Phase 2 node ordering would also help here.

## Decision rule (which reclaimers to run it for)

Run `FullNodeFirst` only when **every pending task of the reclaimer requires a whole node**. It
pays off most on **large, fragmented** clusters where `NodeLocalGreedy`/`MultiNodeGang` victim
search is slow; it is a no-op for sub-node reclaimers and adds little when victim search is
already cheap. Until Phase 2 `killCost` ships, treat it as best-effort for reclaimers whose likely
victims are wide multi-node gangs, where `MultiNodeGang` and `gangcascadeorder` are better suited.

## Test plan

- **Unit:** whole-node necessary-condition matching incl. heterogeneous capacities; per-node
  minimal victim derivation with whole gangs preserved; sub-node and zero-request reclaimers emit
  nothing.
- **Benchmark:** the in-process `reclaim_fullnode_benchmark_test.go` ships the fragmented shape as
  a committed fixture; assert search time is ~constant in `N` while `NodeLocalGreedy`/
  `MultiNodeGang` grow.
- **Regression:** non-whole-node reclaimers are unaffected (generator emits nothing; portfolio
  falls through).

## Future work

- **Phase 2 `killCost` node scoring** — emit matching nodes cheapest-first to minimize disruption
  and cross-node gang breakage; prerequisite for multi-node gang victims.
- **Smart generator selection (#1757)** — prioritize `FullNodeFirst` for matching workloads and
  skip constructing it otherwise.
- **Multi-full-resource shapes** (CPU/memory-defined full nodes) once the GPU-count case is
  validated.

## Alternatives

- **Rely on `NodeLocalGreedy` + `MultiNodeGang` only.** Correct, but both slow down on large,
  fragmented whole-node reclaim (seconds to timeout); `FullNodeFirst` keeps target-finding in
  near-constant time.
- **`DisruptionBounded`.** Complementary: bounds victim-set size generally but does not exploit
  whole-node structure to find a node-scoped solution directly.
