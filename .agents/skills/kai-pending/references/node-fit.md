# node-fit: too big for any single node

Verdict message: e.g. `... in a single node ... topped at N` / `MaxNodePoolResources`. The pod is
bigger than any single node, and a pod cannot span nodes. The message names the blocking
resource and the pod's full request - it can be CPU, memory, or an extended resource, not
only GPU. For extended resources, verify the reported reason against node `allocatable`.

## Fix

- Lower the per-pod request to fit the largest node, or split the work across pods.
- Add/grow nodes in the pool (check the `kai.scheduler/node-pool` label on nodes).
