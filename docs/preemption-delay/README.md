# Preemption Delay

When a pending workload cannot fit in the cluster, KAI Scheduler may evict lower-priority or over-quota workloads to make room for it. In autoscaled clusters this races the cluster autoscaler: workloads get evicted even when a new node would arrive within minutes.

Preemption delay gives a pending workload a waiting window during which it does not evict anyone. While it waits, it remains a pending unschedulable pod, so the cluster autoscaler reacts to it and can provision capacity. If enough capacity appears, the workload schedules without disrupting others; if not, evictions proceed normally once the window expires.

## API

Set the delay with an annotation on the workload's pods (or on the workload object itself):

```yaml
metadata:
  annotations:
    kai.scheduler/preemption-delay: "5m"
```

The value is a Go duration (`"30s"`, `"5m"`, `"1h"`). Invalid values, including unit-less numbers, are ignored with a warning. Workloads without the annotation behave exactly as before.

Workloads that create PodGroups directly can set the equivalent spec field:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
spec:
  preemptionDelay: 5m
```

## Behavior

A workload within its delay window:
- Does not trigger evictions through preemption, reclaim or consolidation.
- Schedules normally into free capacity - the delay never slows plain allocation.
- Can still be evicted itself once running; the delay is not eviction protection. Use [preemptibility](../priority/README.md) for that.

The window is measured from the workload's creation, and restarts if the workload is evicted and becomes pending again - each placement attempt gets a fresh autoscaler window.

While a workload waits, its PodGroup status explains it:

```
$ kubectl get podgroup <name> -o jsonpath='{.status.schedulingConditions[*].message}'
Workload is within its preemption delay window (5m0s) and may not trigger evictions before 2026-07-12T10:33:43Z. ...
```

## Example

See [examples/preemption-delay](../../examples/preemption-delay) for a runnable scenario: a queue at capacity, a running low-priority workload, and a higher-priority pod with a 2-minute delay that waits out its window before preempting.
