# fair-share / contention

There is **no typed reason** here - the verdict is the same generic `PodSchedulingErrors` as a node
shortage, but capacity exists and is just held by others. You're further back than them: over-quota
capacity is split between sibling queues by `overQuotaWeight` + priority.

## Steps

Read **live** `Queue.status` (not the spec - the spec is only the configured weights, it cannot
tell you who is holding capacity right now):

```bash
kubectl get queues -o json | jq -r '.items[] | "\(.metadata.name) parent=\(.spec.parentQueue) quota=\(.spec.resources.gpu.quota // 0) alloc=\(.status.allocated["nvidia.com/gpu"] // 0) req=\(.status.requested["nvidia.com/gpu"] // 0)"'
```

(`quota` is from the **spec** - status has no deserved/quota field, only `allocated` / `requested`.)

1. Your queue shows `allocated` ~0 while `requested` > 0; a **sibling** (same `parentQueue`) whose
   `allocated` holds the GPUs = fair-share starvation, working as designed.
2. `overQuotaWeight` / priority (from the spec) only explain **why** the split landed that way. The
   same live numbers are also exported as Prometheus metrics; with no metrics stack, read them
   directly from the scheduler pod - it logs the per-queue allocation each session.

## Why the holder isn't evicted for you

KAI evicts a holder only if it is **preemptible** (priorityClass `value < 100`), you **outrank** it,
and it has run past its protection window. So the pod stays Pending when the holder is
non-preemptible (`value >= 100`, never evicted), or preemptible but still young - check the holder's
`spec.priorityClassName` -> `value` and the queue's `preemptMinRuntime` / `reclaimMinRuntime`.

## Fix

Compare `quota` to `requested`: the excess (`requested - quota`) is over-quota and starves;
`overQuotaWeight` only re-divides it, never reclaims.

- `quota` < request -> raise `quota` to cover it (admin; bounded by parent quota + real capacity).
- `quota` >= request but still Pending -> not fair-share -> recheck step 4 / protection window /
  non-preemptible holders.
- else -> wait (siblings finish), or raise priorityClass for ordering.

It's a derived conclusion, not a typed reason - say so.
