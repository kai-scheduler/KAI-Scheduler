# fair-share / contention

There is **no typed reason** here - the verdict is the same generic `PodSchedulingErrors` as a node
shortage, but capacity exists and is just held by others. You're further back than them: over-quota
capacity is split between sibling queues by `overQuotaWeight` + priority.

## Steps

Read **live** `Queue.status` (not the spec - the spec is only the configured weights, it cannot
tell you who is holding capacity right now):

```bash
kubectl get queues -o json | jq -r '.items[] | "\(.metadata.name) parent=\(.spec.parentQueue) alloc=\(.status.allocated["nvidia.com/gpu"] // 0) req=\(.status.requested["nvidia.com/gpu"] // 0)"'
```

1. Your queue shows `allocated` ~0 while `requested` > 0; a **sibling** (same `parentQueue`) whose
   `allocated` holds the GPUs = fair-share starvation, working as designed.
2. `overQuotaWeight` / priority (from the spec) only explain **why** the split landed that way. The
   same live numbers are also exported as Prometheus metrics if a metrics stack is present.

## Fix

- Wait - capacity rotates as siblings finish. Or raise this queue's `quota` / `overQuotaWeight`, or
  the pod's priorityClass to win ordering.

It's a derived conclusion, not a typed reason - say so.
