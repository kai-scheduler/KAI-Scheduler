# Stale Grace Period

When a gang-scheduled workload loses some of its pods (due to failures, node issues, etc.) but still has enough running pods to make progress, KAI Scheduler may evict the remaining pods to free resources for other workloads. In production clusters this can be disruptive: a partially-failed gang can hang around consuming resources indefinitely.

Stale grace period gives a stale workload a waiting window during which it will not be evicted. While it waits, the pods continue running and the cluster can stabilize. If enough pods fail during the window, the workload can restart; if not, eviction proceeds normally once the window expires.

## When is a podgroup stale?

A podgroup becomes stale when it has running pods but the gang condition is no longer fully met - i.e. some pods have failed or are otherwise not running. The scheduler detects this state, records a timestamp, and begins a stale grace period countdown.

## API

You can configure the grace period via two methods:

### 1. PodGroup spec

Set the grace period on a workload that creates a PodGroup:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
spec:
  stalenessGracePeriod: 1m
```

### 2. Annotation

Set the annotation `kai.scheduler/staleness-grace-period` on the PodGroup, a parent resource, or individual pods:

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    kai.scheduler/staleness-grace-period: "1m"
```

The value is a Go duration (`"30s"`, `"5m"`, `"1h"`). Invalid values cause the field to be ignored. Negative values disable stale eviction for the podgroup entirely. Missing field uses the scheduler's global default (configurable via `globalDefaultStalenessGracePeriod` in the scheduler config, default is 60s).

## Behavior

A stale podgroup within its grace period:

- Keeps running - pods are not evicted during the window
- Counts toward the scheduler's staleness tracking but does not trigger eviction
- Can be restarted by the podgrouper if enough pods fail during the window
- Resets its staleness counter when pods succeed (becomes non-stale)

After the grace period expires:

- The stalegangeviction action evicts all running pods in the stale gang
- The podgroup's `last-eviction-timestamp` is updated
- The podgroup becomes eligible for rescheduling

## Example

See [examples/staleness-grace-period](../../examples/staleness-grace-period) for a runnable scenario: a 2-pod gang where 1 pod remains running but the group is no longer gang-satisfied. With a 30 second grace period, the scheduler waits before evicting the stale pods.
