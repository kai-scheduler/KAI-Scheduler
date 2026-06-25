# node-fit: too big for any single node

Verdict message: e.g. `... in a single node ... topped at N` / `MaxNodePoolResources`. The pod is
bigger than any single node, and a pod cannot span nodes.

## Steps

1. Compare `requests: gpu=N` against the largest `match` node's `f/a` allocatable.
2. Check the CPU/MEM columns too - the blocker may be CPU or memory, not GPU.

## Fix

- Lower the per-pod request to fit the largest node, or split the work across pods.
- Add/grow nodes in the pool (check the `kai.scheduler/node-pool` label on nodes).
