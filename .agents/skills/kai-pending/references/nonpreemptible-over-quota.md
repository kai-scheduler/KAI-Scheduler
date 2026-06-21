# NonPreemptibleOverQuota

A non-preemptible workload (its `priorityClassName` has `value >= 100`) can only run **within**
the queue's `quota`, never on borrowed over-quota capacity - even if the cluster has free GPUs
and the queue `limit` is open.

**Check** - `.details.queueDetails`:
- `allocatedNP (queue)` + `requestedNP (podgroup)` > `deserved`/quota => blocked.
- `kubectl get priorityclass <name> -o jsonpath='{.value}'` >= 100 confirms non-preemptible.

**Fix**
- Raise the queue `quota` for that resource, **or**
- Use a preemptible priorityClass (`value < 100`) - it can then run on over-quota capacity.
