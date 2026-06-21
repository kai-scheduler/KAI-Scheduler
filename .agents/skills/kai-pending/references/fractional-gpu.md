# fractional GPU (GPU sharing)

The pod requests a slice via a `gpu-fraction` / `gpu-memory` annotation (the dump header shows
it; its container resources carry **no** `nvidia.com/gpu`). KAI backs a slice with a
**reservation pod** that holds one **whole** physical GPU, in `kai-resource-reservation`. With no
free whole GPU, the reservation pod can't be placed -> Pending. The verdict message is the generic
"no nodes with enough resources".

**Check**
- In the dump, is any node's `GPU f/a` free side >= 1 (a free whole GPU for the reservation pod)?
- `kubectl get pods -n kai-resource-reservation` - an existing reservation with spare room could
  also host the slice (same node, total <= 1.0).

**Fix**
- Free a whole GPU (finish/evict a whole-GPU workload), or add GPU capacity, or pack onto a node
  whose reservation pod has room.
- If a whole GPU is free but it still fails: check the `nvidia` RuntimeClass exists (the
  reservation pod needs NVML) - that surfaces as "Reached timeout waiting for GPU reservation pod".
