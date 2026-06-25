# fractional GPU (GPU sharing)

The slice is requested via a `gpu-fraction` / `gpu-memory` annotation; the container resources
carry **no** `nvidia.com/gpu`. KAI backs it with a **reservation pod** holding one **whole**
physical GPU in `kai-resource-reservation`. With no free whole GPU that pod can't be placed ->
Pending, and the verdict is only the generic "no nodes with enough resources" (it does **not**
break down fractional usage).

## Steps

1. In the dump, is any node's `GPU f/a` free side `>= 1` (a free whole GPU for the reservation pod)?
2. `kubectl get pods -n kai-resource-reservation` - an existing reservation with spare room could
   host the slice too (same node, total `<= 1.0`).

## Fix

- Free a whole GPU (finish/evict a whole-GPU workload), add GPU capacity, or pack onto a node whose
  reservation pod has room.
- If a whole GPU is free but it still fails: check the `nvidia` RuntimeClass exists (the reservation
  pod needs NVML) - that surfaces as `Reached timeout waiting for GPU reservation pod`.
