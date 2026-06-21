# QueueDoesNotExist

The `kai.scheduler/queue` label points at a `Queue` that doesn't exist (or whose `parentQueue`
doesn't). KAI admits the pod but can never place it.

**Check**
- `kubectl get pod <pod> -o jsonpath='{.metadata.labels.kai\.scheduler/queue}'` - the label.
- `kubectl get queues` - does it (and its `parentQueue`) exist?
- Message names **`default-queue`**? Then the pod has **no** queue label at all - KAI defaulted it.

**Fix**
- Wrong/missing label -> set `kai.scheduler/queue` to an existing queue.
- Queue genuinely missing -> create it (and its parent). An e2e install ships no default queues.
