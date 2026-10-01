# Whole Node Reclaim: Do We Need a FullNodeFirst Generator?

## Conclusion

No. `MultiNodeGang` already solves whole node reclaim with minimal eviction and in a
constant number of scenarios, so a dedicated `FullNodeFirst` generator adds no measurable
benefit and, in the cross node case, is actively worse. Investigating #1790 surfaced two
real gaps, and neither needs a new generator:

1. **Search cost.** The default portfolio runs `NodeLocalGreedy` first, which is `O(N)` in
   cluster size on fragmented victim layouts. This is a generator ordering problem, fixable
   in config.
2. **Over eviction from cross node cascade.** The victim order among equally preemptible
   gangs is arbitrary, so reclaim can tear down a wide gang that spans many nodes. This is a
   victim ordering problem, fixed by a small tiebreak plugin (`gangcascadeorder`).

## Background

`FullNodeFirst` was proposed in #1790 on the hypothesis that whole node reclaim on a packed
cluster over evicts and does `O(N)` scenario search, because `NodeLocalGreedy` cannot cheaply
assemble one node's worth of victims and reclaim falls through to the wide `MultiNodeGang`
search. Measuring each generator in isolation does not support adding a new generator.

## Measurement 1: search cost per generator

Fixture: `kind` plus `kwok`, `N` nodes of 8 GPU each, packed with reclaimable single GPU
borrowers placed round robin so a node's occupants are spread through the victim order (the
fragmented layout that stresses `NodeLocalGreedy`). One reclaimer requests 8 GPUs on one
node. The minimal solution evicts exactly 8. Each generator is run as the **only** enabled
generator (single policy, isolated). Cells are scenarios simulated.

| Nodes | NodeLocalGreedy only | MultiNodeGang only | FullNodeFirst only | Evicted (all three) |
|------:|---------------------:|-------------------:|-------------------:|--------------------:|
| 2     | 16   | 4 | 2 | 8 |
| 4     | 30   | 4 | 2 | 8 |
| 8     | 54   | 4 | 2 | 8 |
| 16    | 111  | 4 | 2 | 8 |
| 32    | 226  | 4 | 2 | 8 |
| 64    | 450  | 4 | 2 | 8 |
| 128   | 839  | 4 | 2 | 8 |
| 256   | 1794 | 4 | 2 | 8 |
| 512   | 3586 | 4 | 2 | 8 |

### Analysis

`NodeLocalGreedy` grows about seven scenarios per node, so it is `O(N)`. `MultiNodeGang`
(flat 4) and `FullNodeFirst` (flat 2) are both `O(1)`, and all three reach the same minimal
eviction of 8. The scenario search commits the first solved scenario, and the default
portfolio orders `NodeLocalGreedy` before `MultiNodeGang`, so `MultiNodeGang` never runs and
the cluster pays the `O(N)` cost even though a constant time generator is already present.
At scale this is real wall time: on the same shape the default path simulates 855 scenarios
at N=128 and 7123 at N=1024, spending 0.15s up to 8.7s in reclaim search, while the constant
time generators stay near zero.

`FullNodeFirst` (2) edges `MultiNodeGang` (4), but both are constant. The improvement over
`NodeLocalGreedy` is a property of any whole node aware generator, not something unique to
`FullNodeFirst`, and `MultiNodeGang` already ships.

### Gap 1 and its fix

The cheap fix is generator order. Running `MultiNodeGang` before `NodeLocalGreedy` (or gating
`NodeLocalGreedy` for whole node requests) makes reclaim `O(1)` on this shape with no code
change. We suggest the maintainers validate swapping their positions in the portfolio config,
since there may be a reason `NodeLocalGreedy` is placed first that is not visible to us.

## Measurement 2: cross node cascade and over eviction

Fixture: 9 packed nodes. Nodes 0 to 7 each host one member of eight wide gangs, where every
wide gang spans all eight nodes with `minMember` 8. Node 8 hosts eight self contained single
node gangs. All victims share one priority. One reclaimer requests 1 GPU. Because gang
eviction is all or nothing, freeing 1 GPU on a wide node drops those gangs below minimum and
tears down 8 pods, while freeing 1 GPU on node 8 evicts one gang, 1 pod. Optimal eviction is 1.

| Config | Node chosen | Pods evicted (optimal 1) |
|--------|-------------|-------------------------:|
| Baseline default victim order | wide node | 8 |
| `gangcascadeorder` enabled    | node 8    | 1 |

### Analysis

The victim order among equally preemptible gangs falls back to creation time and UID, so the
baseline choice is effectively arbitrary and here lands on a wide node, an eight fold cascade.
`FullNodeFirst` does not help: its Phase 1 orders candidates by fewest local victims, and with
every node tied it also picks a wide node. `MultiNodeGang` is gang aware and often lands well,
but that too depends on which victims the queue happens to surface first, so it is luck, not a
guarantee.

The durable fix is at the victim order, not in a generator. `gangcascadeorder` is a victim
order tiebreak that, among gangs the priority and fairness chain already treats as equal,
evicts the gang with the smaller cross node footprint first (fewest spanned nodes, then fewest
pods). It only breaks ties, so it never reorders victims of different priority and preserves
every preemption and fairness guarantee. It turns the arbitrary 8 pod cascade into a
deterministic 1 pod eviction.

## Recommendation

1. Do not add a `FullNodeFirst` generator. `MultiNodeGang` already covers whole node reclaim
   with minimal eviction in constant time.
2. Address Gap 1 by generator order in config (proposed, to be validated by maintainers).
3. Address Gap 2 with the `gangcascadeorder` victim order tiebreak plugin, tracked separately
   with its own benchmark.

## Notes on method

All numbers are single policy runs (one generator or one plugin toggle at a time) on `kind`
plus `kwok`, read from the scheduler's `kai_scenario_search_scenarios_total` and
`kai_pod_group_evicted_pods_total` metrics. An earlier revision of this document reported
`8*(N-1)` evictions and `~9*N` scenarios; that was a quota artifact from pre binding borrowers
via `nodeName`, which bypasses admission and left the parent queue above its GPU limit so
reclaim was enforcing quota rather than freeing a node. With a correctly sized quota the
numbers are as above.
