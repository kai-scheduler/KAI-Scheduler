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
- [Resource sizing](./resource-sizing.md) - Requests, limits, and VPA guidance for large clusters

## Status Conditions

The KAI operator reports installation health on `Config.status.conditions`. The most relevant condition types are:

- `Deployed`: the operator created the Kubernetes resources for KAI services.
- `Available`: the KAI service controllers report availability.
- `DependenciesFulfilled`: external dependencies required by the selected configuration are present and healthy.
- `Ready`: the KAI services are available.

When `global.gpuSharingMode` is `NvFractions`, the KAI operator also checks the cluster-scoped `GpuSharingConfig` resource from the GPU-sharing operator, as running fractional GPU pods depends on it.

If all KAI services are deployed and available, but the GPU-sharing operator reports a dependency failure, the KAI `Config` can look like this:

```yaml
apiVersion: kai.scheduler/v1
kind: Config
metadata:
  name: kai-config
spec:
  global:
    gpuSharingMode: NvFractions
status:
  conditions:
    - type: Deployed
      status: "True"
      reason: deployed
      message: Resources deployed
    - type: Available
      status: "True"
      reason: available
      message: System available
    - type: DependenciesFulfilled
      status: "False"
      reason: dependencies_missing
      message: Gpu Sharing is not ready. Ready=False, reason=GPUOperatorNotReady, message=ClusterPolicy is not ready
    - type: Ready
      status: "True"
      reason: ready
      message: System is ready
```

In this state the KAI pods are healthy, but NvFractions should not be considered fully operational until `DependenciesFulfilled=True`. The dependency message carries the GPU-sharing operator's config-level status reason and message so the failing external dependency is visible directly from the KAI `Config`.

## Logging

By default all KAI services use development-mode logging with colored, human-readable console
output. This is optimized for reading logs directly from pods via `kubectl logs`.

### Production Mode (JSON Logging)

For log aggregation platforms where single-line structured logs and
parseable log levels are needed, enable JSON logging via the Helm value:

```bash
helm install kai-scheduler kai-scheduler --set global.jsonLog=true
```
