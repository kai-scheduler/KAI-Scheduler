# Grove Integration with KAI Scheduler

## What is Grove?

[Grove](https://github.com/ai-dynamo/grove) is a Kubernetes-native workload orchestrator designed for AI/ML inference workloads. It introduces the concept of **PodCliqueSets** - a higher-level abstraction for managing groups of related pods with topology-aware scheduling capabilities.

Key concepts:
- **PodCliqueSet (PCS)**: A collection of pod groups (cliques) that are deployed and scaled together
- **PodCliqueScalingGroup (PCSG)**: Logical groupings within a PCS that share scaling behavior
- **Clique**: A group of pods with a specific role (e.g., worker, leader, router)
- **Topology Constraints**: Pack pods within specific topology domains (block, rack) for optimal network locality

Grove is particularly useful for disaggregated inference workloads where different components (prefill workers, decode workers, routers) need to be co-located for performance.

## Installation

Install Grove using Helm with topology-aware scheduling enabled:

```sh
helm upgrade -i grove oci://ghcr.io/ai-dynamo/grove/grove-charts:v0.1.0-alpha.5 \
  --set topologyAwareScheduling.enabled=true
```

For more installation options and configuration details, see the [Grove Installation Guide](https://github.com/ai-dynamo/grove/blob/main/docs/installation.md#installation).

## Integrating Grove with KAI Scheduler

To use Grove workloads with KAI Scheduler, you need to configure each clique in your PodCliqueSet with two key settings:

### 1. Set the Scheduler Name

In each clique's `podSpec`, set the `schedulerName` to `kai-scheduler`:

```yaml
cliques:
  - name: my-clique
    spec:
      podSpec:
        schedulerName: kai-scheduler  # Use KAI Scheduler
        containers:
          - name: worker
            # ...
```

### 2. Add the Queue Label

Add the `kai.scheduler/queue` label to each clique to specify which KAI queue the pods should be scheduled to:

```yaml
cliques:
  - name: my-clique
    labels:
      kai.scheduler/queue: default-queue  # Assign to KAI queue
    spec:
      # ...
```

### Complete Example

The [podcliqueset.yaml](./podcliqueset.yaml) file in this directory demonstrates a complete disaggregated inference setup with:
- A PodCliqueSet with topology constraint at the `block` level
- Two PodCliqueScalingGroups (`decoder` and `prefill`) constrained at the `rack` level
- Multiple cliques (workers, leaders, router) all configured for KAI Scheduler

Each clique follows the integration pattern:

```yaml
cliques:
  - name: dworker
    labels:
      kai.scheduler/queue: default-queue  # KAI queue assignment
    spec:
      roleName: dworker
      replicas: 1
      minAvailable: 1
      podSpec:
        schedulerName: kai-scheduler       # KAI scheduler
        containers:
          - name: worker
            image: nginx:alpine-slim
            resources:
              requests:
                memory: 30Mi
```

## Summary of Required Changes

| Setting | Location | Value | Purpose |
|---------|----------|-------|---------|
| `schedulerName` | `cliques[*].spec.podSpec` | `kai-scheduler` | Route pods to KAI Scheduler |
| `kai.scheduler/queue` | `cliques[*].labels` | Queue name (e.g., `default-queue`) | Assign workload to a KAI queue for resource management and fair-sharing |
