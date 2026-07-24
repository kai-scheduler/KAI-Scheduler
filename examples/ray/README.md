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

- **RayJob**: [`../batch/rayjob.yaml`](../batch/rayjob.yaml)
- **RayCluster**: [`../batch/raycluster.yaml`](../batch/raycluster.yaml)

## Configuration Summary

| Field | Location | Value | Description |
|-------|----------|-------|-------------|
| `kai.scheduler/queue` | `metadata.labels` (on RayJob/RayCluster) | Queue name (e.g., `default-queue`) | Assigns workload to a KAI queue |
| `schedulerName` | Pod template spec (head group and each worker group) | `kai-scheduler` | Routes pods to KAI scheduler |

For a full overview of gang scheduling behavior and all supported workload types, see the [Batch and Gang Scheduling guide](../../docs/batch/README.md).
