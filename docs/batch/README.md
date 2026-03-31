# Batch and Gang Scheduling

## Overview

KAI Scheduler provides sophisticated workload scheduling with support for both independent scheduling and gang scheduling. The scheduler automatically detects the workload type and applies the appropriate scheduling strategy through the PodGrouper component, which creates PodGroup custom resources to coordinate pod scheduling.

## Definitions

### Independent Scheduling

Independent scheduling allows pods within a workload to be scheduled independently. Each pod is scheduled as resources become available, without waiting for other pods in the same workload. This is the default behavior for standard Kubernetes Jobs where individual pods can make progress independently.

In KAI Scheduler, independently-scheduled workloads are created with `minMember=1` in their PodGroup, meaning only one pod needs to be schedulable for the workload to start.

### Gang Scheduling

Gang scheduling ensures that either all pods in a workload are scheduled together, or none are scheduled until sufficient resources become available. This "all-or-nothing" approach prevents resource deadlocks and ensures distributed workloads can start simultaneously.

Gang scheduling is essential for:
- Distributed machine learning training (PyTorch, TensorFlow, MPI)
- Parallel computing workloads that require inter-pod communication
- Applications where partial scheduling would waste resources or cause deadlocks

In KAI Scheduler, gang-scheduled workloads have `minMember` set to the total number of required replicas, ensuring all pods are scheduled atomically.

## How It Works

The PodGrouper component automatically creates PodGroup custom resources for incoming workloads. Each workload type has a specialized plugin that determines the appropriate grouping logic:

- **Standard Jobs**: Create PodGroups with `minMember=1` (independent scheduling)
- **Distributed Training Jobs**: Create PodGroups with `minMember=<total replicas>` (gang scheduling)
- **JobSets**: Create one or multiple PodGroups depending on startup policy

For technical details on the PodGrouper architecture and plugin system, see [Pod Grouper Technical Details](../developer/pod-grouper.md).

### Configuring Workloads for KAI Scheduler

To use KAI scheduler with your workloads, configure the following fields in your workload specifications:

| Field | Location | Value | Description |
|-------|----------|-------|-------------|
| `kai.scheduler/queue` | `metadata.labels` | Queue name (e.g., `default-queue`) | Assigns workload to a KAI queue |
| `schedulerName` | Pod template spec | `kai-scheduler` | Routes pods to KAI scheduler |

**Note:** For workloads with multiple pod templates (e.g., Ray head and workers, Spark driver and executors), you must set `schedulerName: kai-scheduler` in each pod template spec.

## Supported Workload Types

### Quick Reference

| Workload Type | Scheduling | Operator Required | Example |
|---------------|-----------|-------------------|---------|
| [Standard Kubernetes Job](#standard-kubernetes-job) | Independent | None (native K8s) | [job.yaml](examples/job.yaml) |
| [PyTorchJob](#pytorchjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [pytorchjob.yaml](examples/pytorchjob.yaml) |
| [MPIJob](#mpijob-kubeflow-training-operator) | Gang | Kubeflow Training Operator / MPI Operator | [mpijob.yaml](examples/mpijob.yaml) |
| [TFJob](#tfjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [tfjob.yaml](examples/tfjob.yaml) |
| [XGBoostJob](#xgboostjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [xgboostjob.yaml](examples/xgboostjob.yaml) |
| [JAXJob](#jaxjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [jaxjob.yaml](examples/jaxjob.yaml) |
| [RayJob](#rayjob-kuberay-operator) | Gang | KubeRay Operator | [rayjob.yaml](examples/rayjob.yaml) |
| [RayCluster](#raycluster-kuberay-operator) | Gang | KubeRay Operator | [raycluster.yaml](examples/raycluster.yaml) |
| [JobSet](#jobset-kubernetes-sig) | Gang | JobSet Controller | [jobset.yaml](examples/jobset.yaml) |
| [SparkApplication](#sparkapplication-spark-operator) | Gang | Spark Operator | [sparkapplication.yaml](examples/sparkapplication.yaml) |

### Standard Kubernetes Job

Standard Kubernetes Jobs run batch workloads where pods can be scheduled independently.

- **Scheduling Behavior:** Independent scheduling (pods scheduled independently)
- **External Requirements:** None (native Kubernetes resource)
- **Example:** [examples/job.yaml](examples/job.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/job.yaml`

### PyTorchJob (Kubeflow Training Operator)

PyTorchJob enables distributed PyTorch training across multiple GPUs and nodes using the Kubeflow Training Operator.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/) - See [installation guide](https://github.com/kubeflow/training-operator#installation)
- **Example:** [examples/pytorchjob.yaml](examples/pytorchjob.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/pytorchjob.yaml`

### MPIJob (Kubeflow Training Operator)

MPIJob enables distributed training and HPC workloads using the Message Passing Interface (MPI) protocol.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/mpi/) - See [installation guide](https://github.com/kubeflow/training-operator#installation)
- **Example:** [examples/mpijob.yaml](examples/mpijob.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/mpijob.yaml`

### TFJob (Kubeflow Training Operator)

TFJob enables distributed TensorFlow training across multiple nodes.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/tftraining/) - See [installation guide](https://github.com/kubeflow/training-operator#installation)
- **Example:** [examples/tfjob.yaml](examples/tfjob.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/tfjob.yaml`

### XGBoostJob (Kubeflow Training Operator)

XGBoostJob enables distributed XGBoost training for gradient boosting workloads.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/xgboost/) - See [installation guide](https://github.com/kubeflow/training-operator#installation)
- **Example:** [examples/xgboostjob.yaml](examples/xgboostjob.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/xgboostjob.yaml`

### JAXJob (Kubeflow Training Operator)

JAXJob enables distributed JAX training workloads using JAX's native distributed capabilities.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/) - See [installation guide](https://github.com/kubeflow/training-operator#installation)
- **Example:** [examples/jaxjob.yaml](examples/jaxjob.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/jaxjob.yaml`

### RayJob (KubeRay Operator)

RayJob enables distributed computing and machine learning workloads using the Ray framework.

- **Scheduling Behavior:** Gang scheduling (all pods in the Ray cluster scheduled together)
- **External Requirements:** Requires [KubeRay Operator](https://docs.ray.io/en/latest/cluster/kubernetes/index.html) - See [installation guide](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/kuberay-operator-installation.html)
- **Example:** [examples/rayjob.yaml](examples/rayjob.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/rayjob.yaml`

### RayCluster (KubeRay Operator)

RayCluster enables long-running Ray clusters for distributed computing and machine learning workloads.

- **Scheduling Behavior:** Gang scheduling (all pods in the Ray cluster scheduled together)
- **External Requirements:** Requires [KubeRay Operator](https://docs.ray.io/en/latest/cluster/kubernetes/index.html) - See installation instructions above
- **Example:** [examples/raycluster.yaml](examples/raycluster.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/raycluster.yaml`

### JobSet (Kubernetes SIG)

JobSet manages a group of Jobs as a single unit, enabling complex multi-job workflows.

- **Scheduling Behavior:** Gang scheduling per replicatedJob or for all jobs (depends on startup policy)
- **External Requirements:** Requires [JobSet Controller](https://github.com/kubernetes-sigs/jobset) - See [installation guide](https://github.com/kubernetes-sigs/jobset#installation)
- **Example:** [examples/jobset.yaml](examples/jobset.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/jobset.yaml`

**Note:** With `startupPolicyOrder: AnyOrder`, KAI creates one PodGroup for all jobs together. If you use `startupPolicyOrder: InOrder`, KAI creates separate PodGroups to avoid sequencing deadlocks.

### SparkApplication (Spark Operator)

SparkApplication enables running Apache Spark workloads on Kubernetes.

- **Scheduling Behavior:** Gang scheduling (driver and executors scheduled together)
- **External Requirements:** Requires [Spark Operator](https://github.com/GoogleCloudPlatform/spark-on-k8s-operator) - See [installation guide](https://github.com/GoogleCloudPlatform/spark-on-k8s-operator#installation)
- **Example:** [examples/sparkapplication.yaml](examples/sparkapplication.yaml)
- **Apply:** `kubectl apply -f docs/batch/examples/sparkapplication.yaml`

**Note:** You must create a `spark` service account with appropriate RBAC permissions before running Spark applications.

## Topology-Aware Scheduling

For distributed workloads, you can optionally specify topology constraints to control pod placement across racks, zones, or other hierarchical domains. This is particularly useful for workloads that require low-latency communication between pods or need to avoid network bottlenecks.

Add topology annotations to your workload metadata:

```yaml
metadata:
  annotations:
    kai.scheduler/topology: "cluster-topology"
    kai.scheduler/topology-preferred-placement: "topology.kubernetes.io/rack"
```

Available placement modes:
- **Required placement**: Strictly enforces placement within specified topology domain
- **Preferred placement**: Attempts to place pods together but falls back to higher-level domains if needed

See [Topology-Aware Scheduling](../topology/README.md) for comprehensive documentation on topology configuration and scheduling strategies.

## Additional Resources

- [Pod Grouper Technical Details](../developer/pod-grouper.md) - Deep dive into PodGrouper architecture and plugin system
- [Topology-Aware Scheduling](../topology/README.md) - Configure topology-aware scheduling for distributed workloads
- [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/) - Official documentation for distributed training jobs
- [KubeRay Documentation](https://docs.ray.io/en/latest/cluster/kubernetes/index.html) - Ray on Kubernetes guide
- [JobSet Documentation](https://github.com/kubernetes-sigs/jobset) - Kubernetes JobSet API reference
- [Spark Operator Documentation](https://github.com/GoogleCloudPlatform/spark-on-k8s-operator) - Spark on Kubernetes operator
