# metric: `kai_pod_group_evicted_pods_total` inflated by gang_size × tasks-per-PG, not pod count

## Summary

Reading the eviction code path, `kai_pod_group_evicted_pods_total` appears to increment by `gang_size × tasks-per-PG-in-batch`, not by the number of pods evicted, on every multi-pod eviction. For an N-pod gang from a single PodGroup, the counter grows by **N²** instead of N. For decisions spanning multiple PodGroups, each PG's counter grows by `(its tasks in the batch) × N`.

We've found this while drafting a redesign of this metric (follow-up to #1573 — design PR upcoming). Our existing design replaces the buggy emission, so we can fold the fix into that PR. But the bug is independently reproducible and worth tracking as its own issue. If the maintainers disagree with this read of the code, we'd appreciate the correction — happy to be wrong.

## Walk-through

Take a preemption of 4 pods, all from the same PodGroup. The eviction loop in `EvictAllPreemptees`:

```go
preempteeTasks = [pod1, pod2, pod3, pod4]   // N = 4
len(preempteeTasks) = 4

for _, task := range preempteeTasks {       // runs 4 times
    stmt.Evict(task, message, eviction_info.EvictionMetadata{
        EvictionGangSize: len(preempteeTasks),  // = 4, every iteration
        ...
    })
}
```

Each `stmt.Evict` reaches the cache, which calls `StatusUpdater.Evicted()`, which calls:

```go
metrics.RecordPodGroupEvictedPods(..., evictionMetadata.EvictionGangSize)
```

And the metric function does:

```go
counter.Add(float64(count))   // count = EvictionGangSize = 4
```

So the loop produces:

| Iteration | counter delta | running total |
|---|---|---|
| 1 | +4 | 4 |
| 2 | +4 | 8 |
| 3 | +4 | 12 |
| 4 | +4 | 16 |

**Evicting 4 pods writes 16 to the counter.** N=10 pods writes 100. N=100 writes 10,000.

For decisions spanning multiple PodGroups (which happens through `scenario.RecordedVictimsTasks()` — that method flattens pods from multiple recorded victim jobs into one slice passed to `EvictAllPreemptees`), the same multiplier applies per PG: if 6 of the 10 tasks belong to PG-A and 4 to PG-B, then PG-A's counter grows by `6 × 10 = 60` and PG-B's by `4 × 10 = 40`.

## Affected actions

All four eviction-emitting actions go through the same code path or an equivalent one:

| Action | Path | Status |
|---|---|---|
| `preempt` | by-pod solver → `EvictAllPreemptees` | inflated |
| `reclaim` | by-pod solver → `EvictAllPreemptees` | inflated |
| `consolidation` | by-pod solver → `EvictAllPreemptees` | inflated |
| `stalegangeviction` | own per-task loop with the same shape | inflated |

Consolidation is arguably the worst-affected by intent — its purpose is moving multiple pods at once to free nodes, so gang sizes for consolidation are routinely >1. Any panel showing "pods evicted via consolidation" is wildly off whenever consolidation touches more than one pod.

## Why this likely went unnoticed

The inflation collapses when `EvictionGangSize == 1`. Every test fixture we found in the repo uses `EvictionGangSize: 1`, and in production any single-pod eviction path produces N²=1, so the counter looks correct. The bug only manifests for gang-scheduled multi-pod evictions and for consolidation — both real and meaningful, but neither universal.

## Caveat

We read the code path on paper, we did not run a focused test. Possible scenarios we have not ruled out:

- A dedup or batch in the statement commit path we missed
- The metric path being short-circuited somewhere we did not trace
- `EvictionGangSize` being mutated between iterations

Likelihood of these: low — the code reads cleanly — but unverified.

## Proposed fix

Replace the `+EvictionGangSize per call` behavior with `+1 per pod evicted`. The metric then reflects what its name promises (total pods evicted), and the existing label set (or the redesigned one being proposed in the upcoming design PR) continues to apply.

This change is already part of the design PR draft following up on #1573 and could land together with that work. Alternatively it can ship as a standalone bug fix on a faster timeline if maintainers prefer separation.

## Disagreement welcome

If maintainers read the same code paths and conclude this is intended behavior (e.g., `EvictionGangSize` is being used as a deliberate weighting and the metric name is what is wrong rather than the value), please push back — we will adjust the design PR accordingly.
