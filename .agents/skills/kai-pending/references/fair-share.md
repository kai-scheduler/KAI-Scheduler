# fair-share / contention

In the dump **no node has free room now**, but `match` nodes' **total** GPU >= request - capacity
exists, it's just held by others. There is **no typed reason** for this; the verdict is the same
generic `PodSchedulingErrors` as a node shortage. It's the Slurm "you're low priority / others are
ahead" case: over-quota capacity is split between sibling queues by `overQuotaWeight` + priority.

**Check**
```bash
kubectl get queues -o json | jq -r '.items[] | "\(.metadata.name) parent=\(.spec.parentQueue) oqw=\(.spec.resources.gpu.overQuotaWeight) alloc=\(.status.allocated["nvidia.com/gpu"] // 0)"'
```
- Your queue: `allocated` 0 / `requested` > 0. A **sibling** (same `parentQueue`) with higher
  `overQuotaWeight`/priority holds the capacity = fair-share starvation, working as designed.

**Fix**
- Wait - capacity rotates as siblings finish. Or raise this queue's `quota` / `overQuotaWeight`,
  or the pod's priorityClass to win ordering. State it's a derived conclusion, not a typed reason.
