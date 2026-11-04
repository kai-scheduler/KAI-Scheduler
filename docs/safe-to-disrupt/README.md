# Safe to Disrupt

Some evictions are not driven by fairness or priority. Today that is the consolidation action: when a pending workload does not fit on any node, consolidation may move running preemptible workloads to other nodes to make room for it. Moving a workload evicts its pods: they are deleted, and their controller is expected to recreate them so the scheduler can place them on the new node.

Workloads that do not survive a restart can opt out of these disruptions. Examples:
- Bare pods, which no controller recreates.
- Workloads whose controller treats a deleted pod as a failure, such as training jobs whose replicas use `restartPolicy: Never`.
- Long-running jobs that checkpoint rarely.

## API

Set the annotation on the workload's pods (or on the workload object itself):

```yaml
metadata:
  annotations:
    kai.scheduler/safe-to-disrupt: "false"
```

The value is a boolean (`"true"` or `"false"`). Invalid values are ignored with a warning. When both the workload object and its pods carry the annotation, the workload object's value wins, as with `kai.scheduler/preemption-delay`. Workloads without the annotation behave exactly as before.

The pod grouper copies the value to the workload's PodGroup as the same annotation, which is what the scheduler reads. Workloads that create PodGroups directly set the annotation on the PodGroup:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  annotations:
    kai.scheduler/safe-to-disrupt: "false"
```

## Behavior

A workload with `kai.scheduler/safe-to-disrupt: "false"`:
- Is never moved by the consolidation action.
- Can still be evicted by reclaim and preempt, which follow fairness and priority. It is not eviction protection; use [preemptibility](../priority/README.md) for that.
- Keeps its preemptibility, quota accounting and fair share.
- Is scheduled normally, and while pending can still trigger consolidation of other workloads.

Only preemptible workloads are moved by consolidation, so the setting has no effect on non-preemptible or semi-preemptible workloads.

To apply the setting to a class of workloads, for example every pod of a priority class or queue, add the annotation with an admission policy. Scheduler plugins can also restrict which workloads consolidation moves by registering `Session.AddConsolidationVictimFilterFn` (see [Plugin Framework](../developer/plugin-framework.md)).

## Example

A training job that must not be disrupted by consolidation:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: long-training
  labels:
    kai.scheduler/queue: default-queue
  annotations:
    kai.scheduler/safe-to-disrupt: "false"
spec:
  template:
    spec:
      schedulerName: kai-scheduler
      restartPolicy: Never
      containers:
      - name: main
        image: ubuntu
        args: ["sleep", "infinity"]
        resources:
          limits:
            nvidia.com/gpu: 1
```
