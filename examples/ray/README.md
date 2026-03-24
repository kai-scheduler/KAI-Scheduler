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

To use KAI scheduler with your Ray workloads, you need to configure the pod templates in your RayJob or RayCluster specifications.

### Required Configuration

1. **Queue Label**: Add `kai.scheduler/queue` label on the RayJob or RayCluster metadata to specify the scheduling queue
2. **Scheduler Name**: Set `schedulerName: kai-scheduler` in all pod template specs (head group and worker groups)

### RayJob Example

```yaml
apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
  labels:
    kai.scheduler/queue: "default-queue"          # KAI queue name
spec:
  entrypoint: python /home/ray/samples/sample_code.py
  rayClusterSpec:
    rayVersion: '2.54.0'
    # Head group configuration
    headGroupSpec:
      rayStartParams: {}
      template:
        spec:
          schedulerName: kai-scheduler               # Use KAI scheduler
          containers:
          - name: ray-head
            image: rayproject/ray:2.54.0
            resources:
              limits:
                cpu: "1"
                nvidia.com/gpu: "1"                  # Optional: GPU resources
              requests:
                cpu: "200m"
    # Worker group configuration
    workerGroupSpecs:
    - replicas: 2
      minReplicas: 1
      maxReplicas: 5
      groupName: gpu-workers
      rayStartParams: {}
      template:
        spec:
          schedulerName: kai-scheduler               # Use KAI scheduler
          containers:
          - name: ray-worker
            image: rayproject/ray:2.54.0
            resources:
              limits:
                cpu: "1"
                nvidia.com/gpu: "1"                  # GPU resources per worker
              requests:
                cpu: "200m"
```

### RayCluster Example

```yaml
apiVersion: ray.io/v1
kind: RayCluster
metadata:
  name: raycluster-sample
  labels:
    kai.scheduler/queue: "default-queue"          # KAI queue name
spec:
  rayVersion: '2.54.0'
  headGroupSpec:
    rayStartParams:
      dashboard-host: '0.0.0.0'
    template:
      spec:
        schedulerName: kai-scheduler
        containers:
        - name: ray-head
          image: rayproject/ray:2.54.0
          resources:
            limits:
              cpu: "2"
              memory: "4Gi"
            requests:
              cpu: "1"
              memory: "2Gi"
  workerGroupSpecs:
  - replicas: 3
    groupName: gpu-workers
    rayStartParams: {}
    template:
      spec:
        schedulerName: kai-scheduler
        containers:
        - name: ray-worker
          image: rayproject/ray:2.54.0
          resources:
            limits:
              nvidia.com/gpu: "1"
            requests:
              cpu: "500m"
              memory: "1Gi"
```

## Configuration Summary

| Field | Location | Value | Description |
|-------|----------|-------|-------------|
| `kai.scheduler/queue` | `metadata.labels` (on RayJob/RayCluster) | Queue name (e.g., `default-queue`) | Assigns workload to a KAI queue |
| `schedulerName` | `spec.template.spec` (on each pod template) | `kai-scheduler` | Routes pods to KAI scheduler |

## Topology-Aware Scheduling (Optional)

To enable topology-aware scheduling for Ray subgroups, add topology annotations on the pod templates:

- `spec.headGroupSpec.template.metadata.annotations`
- `spec.workerGroupSpecs[*].template.metadata.annotations`

Supported annotations:

- `kai.scheduler/topology`
- `kai.scheduler/topology-required-placement`
- `kai.scheduler/topology-preferred-placement`

Example:

```yaml
apiVersion: ray.io/v1
kind: RayCluster
metadata:
  name: raycluster-topology
  labels:
    kai.scheduler/queue: "default-queue"
spec:
  rayVersion: '2.54.0'
  headGroupSpec:
    template:
      metadata:
        annotations:
          kai.scheduler/topology: "cluster-topology"
          kai.scheduler/topology-required-placement: "rack"
          kai.scheduler/topology-preferred-placement: "node"
      spec:
        schedulerName: kai-scheduler
        containers:
        - name: ray-head
          image: rayproject/ray:2.54.0
  workerGroupSpecs:
  - groupName: gpu-workers
    replicas: 1
    minReplicas: 1
    maxReplicas: 1
    template:
      metadata:
        annotations:
          kai.scheduler/topology: "cluster-topology"
          kai.scheduler/topology-required-placement: "zone"
          kai.scheduler/topology-preferred-placement: "rack"
      spec:
        schedulerName: kai-scheduler
        containers:
        - name: ray-worker
          image: rayproject/ray:2.54.0
  - groupName: best-effort-workers
    replicas: 1
    minReplicas: 1
    maxReplicas: 1
    template:
      spec:
        schedulerName: kai-scheduler
        containers:
        - name: ray-worker
          image: rayproject/ray:2.54.0
```

Notes:

- If `kai.scheduler/topology` is omitted for a subgroup, that subgroup has no topology constraint.
- `kai.scheduler/topology` is required when `topology-required-placement` or `topology-preferred-placement` is set.
