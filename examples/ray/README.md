# KubeRay with KAI Scheduler

This guide explains how to run Ray workloads on Kubernetes using KubeRay with the KAI scheduler for optimized GPU resource allocation.

## Installing KubeRay Operator

Install the KubeRay operator using Helm. For full installation options and detailed documentation, see the [official KubeRay installation guide](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/kuberay-operator-installation.html).

```sh
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update

# Install both CRDs and KubeRay operator v1.5.1
helm install kuberay-operator kuberay/kuberay-operator \
    --namespace ray \
    --create-namespace \
    --version 1.5.1
```

## Configuring Ray Workloads for KAI Scheduler

To use KAI scheduler with your Ray workloads, configure the pod templates in your RayJob or RayCluster specifications.

### Required Configuration

1. **Queue Label**: Add `kai.scheduler/queue` label on the RayJob or RayCluster metadata to specify the scheduling queue
2. **Scheduler Name**: Set `schedulerName: kai-scheduler` in all pod template specs (head group and worker groups)

### Examples

- **RayJob**: [`rayjob.yaml`](rayjob.yaml)
- **RayCluster**: [`raycluster.yaml`](raycluster.yaml)

## Configuration Summary

| Field | Location | Value | Description |
|-------|----------|-------|-------------|
| `kai.scheduler/queue` | `metadata.labels` (on RayJob/RayCluster) | Queue name (e.g., `default-queue`) | Assigns workload to a KAI queue |
| `schedulerName` | Pod template spec (head group and each worker group) | `kai-scheduler` | Routes pods to KAI scheduler |

## Topology-Aware Scheduling (Optional)

To control Ray subgroup placement, add topology annotations to the head and worker pod templates:

- `spec.headGroupSpec.template.metadata.annotations`
- `spec.workerGroupSpecs[*].template.metadata.annotations`

Supported annotations:

- `kai.scheduler/topology`
- `kai.scheduler/topology-required-placement`
- `kai.scheduler/topology-preferred-placement`

Set `kai.scheduler/topology` whenever either placement annotation is used. The following annotations-only patch requires each Ray subgroup to remain within a zone while preferring rack-local placement; add it to the existing head and worker pod templates in [raycluster.yaml](raycluster.yaml):

```yaml
spec:
  headGroupSpec:
    template:
      metadata:
        annotations:
          kai.scheduler/topology: cluster-topology
          kai.scheduler/topology-required-placement: topology.kubernetes.io/zone
          kai.scheduler/topology-preferred-placement: topology.kubernetes.io/rack
  workerGroupSpecs:
  - groupName: gpu-workers
    template:
      metadata:
        annotations:
          kai.scheduler/topology: cluster-topology
          kai.scheduler/topology-required-placement: topology.kubernetes.io/zone
          kai.scheduler/topology-preferred-placement: topology.kubernetes.io/rack
```

See [raycluster.yaml](raycluster.yaml) for a complete RayCluster and [Topology-Aware Scheduling](../../docs/topology/README.md) for topology configuration and scheduling behavior.

For a full overview of gang scheduling behavior and all supported workload types, see the [Batch and Gang Scheduling guide](../../docs/batch/README.md).
