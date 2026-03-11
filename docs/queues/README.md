# Scheduling Queues

Scheduling queues are the core resource management primitive in KAI Scheduler, providing hierarchical resource allocation with quota guarantees and priority-based distribution.

Only leaf queues (queues with no children) can be used for scheduling jobs. Parent queues serve as organizational units for resource distribution among their child queues.

## Table of Contents
- [Queue Attributes](#queue-attributes)
- [API Reference](#api-reference)
- [Resource Configuration](#resource-configuration)
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
