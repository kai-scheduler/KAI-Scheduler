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

## Status Conditions

The KAI operator reports installation health on `Config.status.conditions`. The most relevant condition types are:

- `Deployed`: the operator created the Kubernetes resources for KAI services.
- `Available`: the KAI service controllers report availability.
- `DependenciesFulfilled`: external dependencies required by the selected configuration are present and healthy.
- `Ready`: the KAI services are available.

When `global.gpuSharingMode` is `NvFractions`, the KAI operator also checks the cluster-scoped `GpuSharingConfig` resource from the GPU-sharing operator, as running fractional GPU pods depends on it.

### Installing the GPU-sharing operator

The GPU-sharing operator is required only for `NvFractions`; every other `gpuSharingMode` runs without it. It also raises the cluster's requirements: [NVIDIA GPU Operator](https://github.com/NVIDIA/gpu-operator) **v26.7.1 or newer** and an **`r615` or newer driver (CUDA 13.4)** on the GPU nodes. `r615` is not the GPU Operator default, so it must be requested explicitly via `driver.version`; on an older driver the `GpuSharingConfig` `Ready` condition reports `GPUDriverVersionUnsupported` and the node-level daemons are not rolled out. See the [gpu-sharing prerequisites](https://github.com/kai-scheduler/gpu-sharing#prerequisites) for the full list, including containerd 2.0+ with NRI enabled.

Set `global.nvFractions.set=true` to install the [gpu-sharing](https://github.com/kai-scheduler/gpu-sharing) operator as a subchart and select the `NvFractions` mode in one step:

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace --set global.nvFractions.set=true
```

Leaving `global.gpuSharingMode` empty is enough — `nvFractions.set` selects `NvFractions`. Setting it to any other mode at the same time fails the install at render time. To run `NvFractions` against a GPU-sharing operator installed out-of-band, leave `global.nvFractions.set` at its `false` default and set `global.gpuSharingMode: NvFractions` instead.

KAI's `global.imagePullSecrets`, `global.nodeSelector`, `global.affinity` and `global.tolerations` are **not** propagated to the subchart, because the gpu-sharing chart reads those keys at its own top level rather than under `global`. Set them explicitly under the `gpu-sharing:` key — relevant for private-registry and air-gapped installs:

```yaml
global:
  nvFractions:
    set: true
gpu-sharing:
  imagePullSecrets:
    - name: my-registry-secret
  nodeSelector:
    nvidia.com/gpu.present: "true"
```

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
