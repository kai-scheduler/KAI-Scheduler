# Scheduling Queues

Scheduling queues are the core resource management primitive in KAI Scheduler, providing hierarchical resource allocation with quota guarantees and priority-based distribution.

Only leaf queues (queues with no children) can be used for scheduling jobs. Parent queues serve as organizational units for resource distribution among their child queues.

## Table of Contents
- [Queue Attributes](#queue-attributes)
- [API Reference](#api-reference)
- [Resource Configuration](#resource-configuration)
- [Queue Status](#queue-status)
- [Examples](#examples)

## Queue Attributes

| Attribute | Description | Units |
|-----------|-------------|-------|
| **Quota** | Guaranteed resource allocation | CPU: millicores, Memory: MB, GPU: units |
| **Over-Quota Priority** | Resource allocation order when exceeding quota | Integer (higher = first) |
| **Over-Quota Weight** | Resource distribution weight within priority level | Integer |
| **Limit** | Hard cap on resource consumption | Same as quota |

## API Reference

### Queue Specification
```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: example-queue
spec:
  displayName: "Example Queue"           # Optional: logging purposes
  parentQueue: "parent-queue"            # Optional: hierarchical structure
  priority: 100                          # Optional: allocation precedence
  resources:
    cpu: ResourceQuota
    memory: ResourceQuota
    gpu: ResourceQuota
```

### Resource Quota Structure
```yaml
resources:
  cpu:
    quota: 2000                          # 2 CPU cores guaranteed
    overQuotaWeight: 1                   # Distribution weight
    limit: 4000                          # Max 4 CPU cores
  memory:
    quota: 4096                          # 4GB guaranteed
    overQuotaWeight: 1
    limit: 8192                          # Max 8GB
  gpu:
    quota: 2                             # 2 GPUs guaranteed
    overQuotaWeight: 1
    limit: 4                             # Max 4 GPUs
```

## Resource Configuration

### Special Values
| Field | Value | Behavior |
|-------|-------|----------|
| `quota` | `-1` | Unlimited quota |
| `quota` | `0` or unset | No guaranteed resources (default) |
| `limit` | `-1` | No limit |
| `limit` | `0` or unset | No additional resources allowed (default) |

### Resource Units
- **CPU**: Millicores (1000 = 1 CPU core)
- **Memory**: Megabytes (MB = 10⁶ bytes)
- **GPU**: Units (1 = full GPU device)

## Queue Status

The queue controller reports consumption in `Queue.status`:

| Field | Covers |
|-------|--------|
| `allocated` | Running pods, plus pending pods already scheduled to a node |
| `allocatedNonPreemptible` | The same, restricted to non-preemptible pods |
| `requested` | Running and pending pods |

A queue's numbers are the sum over its own pod groups and its child queues. `PodGroup.status.resourcesStatus` carries the same numbers for a single pod group.

### How allocated and requested are counted

Per pod, the controller sums:

- every regular container's `resources.requests`
- every native sidecar's `resources.requests`, meaning an init container with `restartPolicy: Always` ([KEP-753](https://github.com/kubernetes/enhancements/issues/753)). A sidecar runs alongside the regular containers for the whole life of the pod, so its request is held the whole time
- `spec.overhead`, the per-pod cost a RuntimeClass adds for sandboxed runtimes such as Kata or gVisor

GPU-sharing requests (the `gpu-fraction` and `gpu-memory` annotations) and DRA claims are added on their own paths.

One thing is deliberately left out today:

- **The peak of a non-restartable init container.** The scheduler reserves `max(regular + sidecars, init peak) + overhead`, so a pod whose init container is larger than its steady state holds more than the status reports, for as long as that init container runs. This is tracked in [#1880](https://github.com/NVIDIA/KAI-Scheduler/issues/1880), which is about aligning the status with the resources the scheduler reserves.

Two GPU details follow the scheduler on purpose:

- **A GPU set in `spec.overhead`** is not counted: the scheduler adds only the CPU and memory half of the overhead.
- **A GPU on a pod whose GPU is rebuilt from an annotation** (`gpu-fraction`, `gpu-memory`, or a legacy MIG annotation) is not counted from the containers, since the annotation decides and the scheduler discards the container request. A regular container's or a native sidecar's GPU on any other pod is counted, matching the scheduler. A name counts as a GPU here if it ends in `/gpu` or is a MIG device, the rule `kai_queue_allocated_gpus` already applies.

## Examples

### Basic Queue
```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: research-team
spec:
  displayName: "Research Team"
  resources:
    cpu:
      quota: 1000
      limit: 2000
    gpu:
      quota: 1
      limit: 2
```

### Hierarchical Queue 
```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: ml-team
spec:
  displayName: "ML Team"
  parentQueue: "research-team"
  priority: 200
  resources:
    cpu:
      quota: 500
      overQuotaWeight: 2
    gpu:
      quota: 1
      overQuotaWeight: 1
```

### Unlimited Queue - default value is -1
```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: burst-queue
spec:
  resources:
    cpu:
      quota: -1                          # Unlimited quota
      limit: -1                          # No limit
    gpu:
      quota: 0                           # No guarantee
      limit: -1                          # No limit
```

## See Also

- [Scheduling Deep Dive](../scheduling-deep-dive/README.md) — How queues, priority, fairness, preemption, and reclaim interact
