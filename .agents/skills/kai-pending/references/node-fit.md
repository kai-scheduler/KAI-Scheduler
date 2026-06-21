# node-fit: too-big

In the dump, **no `SELECTOR match` node has total GPU >= the request** (the `f/a` column's
allocatable side). The pod is simply bigger than any node - a single pod cannot span nodes.
The verdict message says e.g. `... in a single node ... topped at N` / `MaxNodePoolResources`.

**Check**
- Compare `requests: gpu=N` against the largest `match` node's `f/a` allocatable.
- Also check CPU/MEM columns - the blocker may be CPU or memory, not GPU.

**Fix**
- Lower the per-pod request to fit the largest node, or split the work across pods.
- Add/grow nodes in the pool (check the `kai.scheduler/node-pool` label on nodes).
