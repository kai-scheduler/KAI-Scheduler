# KAI Operator

The KAI Operator manages KAI-scheduler services through Kubernetes Custom Resource Definitions (CRDs). It provides declarative configuration and status monitoring for KAI components.

## Overview

The operator uses two main CRDs:

- **`config.kai.scheduler`** - Deploys and configures core KAI services
- **`schedulingshard.kai.scheduler`** - Creates partitioned scheduler instances for specific node groups

## Architecture

### Controllers
- **Config Controller** - Manages main KAI configuration and service deployment
- **SchedulingShard Controller** - Handles cluster partitioning and shard-specific deployments

### Operands
Each KAI service is managed by a dedicated operand:
- **Admission Webhooks** - Pod validation and mutation
- **Scheduler** - Pod scheduling decisions
- **Queue Controller** - Queue management
- **Pod Group Controller** - Pod grouping functionality
- **Node Scale Adjuster** - Node scaling operations

### CRDs

#### Config CRD (`config.kai.scheduler`)

The Config CRD is a cluster-scoped resource that defines the overall KAI installation:

```yaml
apiVersion: kai.scheduler/v1
kind: Config
metadata:
  name: kai-config
spec:
  namespace: kai-system
  global:
    replicaCount: 2
    schedulerName: kai-scheduler
    nodePoolLabelKey: kai.scheduler/node-pool
```

#### SchedulingShard CRD (`schedulingshard.kai.scheduler`)

The SchedulingShard CRD enables cluster partitioning for distributed scheduling:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: default
---
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: gpu-shard
spec:
  partitionLabelValue: gpu-nodes
  placementStrategy:
    gpu: binpack
    cpu: spread
  queueDepthPerAction:
    preempt: 10
    reclaim: 5
  minRuntime:
    preemptMinRuntime: "5m"
    reclaimMinRuntime: "2m"
```

- [Scheduling Shards](./scheduling-shards.md) - Advanced cluster partitioning
