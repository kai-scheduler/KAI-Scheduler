# OverLimit

The queue has a hard `limit` and admitting this workload would exceed it. The `limit` is a
ceiling even preemptible/over-quota work cannot cross.

**Check** - `.details.queueDetails` in the verdict:
- `allocated (queue)` + `requested (podgroup)` > `limit (queue)` => blocked.
- `limit` low (e.g. `gpu: 0`) -> nothing of that resource is ever allowed.
- `limit` already full -> the queue's other workloads hold it; yours waits (normal "GPU busy").

**Fix**
- Wait (contention frees as workloads finish), or lower the request, or raise
  `spec.resources.<r>.limit` on the queue.
