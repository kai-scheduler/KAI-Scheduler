# Batch and Gang Scheduling

## Overview

KAI Scheduler provides sophisticated workload scheduling with support for both independent scheduling and gang scheduling. The scheduler automatically detects the workload type and applies the appropriate scheduling strategy through the PodGrouper component, which creates PodGroup custom resources to coordinate pod scheduling.

## Min Member Override
To require a minimum number of pods to be scheduled together (gang scheduling) for a batch Job or JobSet, use the `kai.scheduler/batch-min-member` annotation on the Job or JobSet resource:
```
kubectl apply -f batch-job-min-member.yaml
```
This will create a job with parallelism of 6, but requires at least 2 pods to be scheduled together before any pod starts running. This is useful for workloads like hyperparameter optimization (HPO) where you want a minimum level of parallelism but don't need all pods running simultaneously.

For JobSets, KAI creates a single PodGroup per JobSet with a parent SubGroup per replicatedJob and a leaf SubGroup per replica. The `kai.scheduler/batch-min-member` annotation behaves at two levels:

- On the **JobSet** resource: overrides the root `minSubGroup` (how many top-level subgroups must be schedulable). If the user didn't set an override, the value will be 1 if the jobset has an "InOrder" policy. Otherwise ("AnyOrder"), the value will be equal to the amount of replicatedJob provided in the jobset. 
- On a **replicatedJob's `template.metadata.annotations`**: overrides the `minMember` of every leaf SubGroup of that replicatedJob. Defaults to `template.spec.parallelism` when absent.

## External PodGroups

KAI also supports PodGroups that are created outside the podgrouper. This is useful when multiple workloads should join the same gang or when an external controller owns the PodGroup lifecycle.

Use the following contract:

- Create the `PodGroup` explicitly.
- Set `pod-group-name` on the pod template metadata to join that PodGroup.
- Set `kai.scheduler/subgroup-name` on the pod template metadata labels when using non-default subgroups.
- Set `kai.scheduler/skip-podgrouper: "true"` on the workload or any readable owner in the owner chain to prevent podgrouper from creating or rewriting PodGroup membership.

Example:

```bash
kubectl apply -f examples/batch/external-podgroup-job.yaml
```

Behavior notes:

- `PodGroup.spec.queue` is authoritative for scheduling.
- If a pod references a PodGroup that does not exist yet, KAI leaves that case unchanged and does not set a new pod condition.
- If a pod references a subgroup that does not exist in the PodGroup, KAI ignores only that pod for scheduling and sets a pod condition explaining the invalid subgroup.

## PyTorchJob
To run in a distributed way across multiple pods, you can use PyTorchJob.


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

> **Note:** All `kubectl apply` and example file paths below are relative to the repository root.

### Quick Reference

| Workload Type | Scheduling | Operator Required | Example |
|---------------|-----------|-------------------|---------|
| [Standard Kubernetes Job](#standard-kubernetes-job) | Independent | None (native K8s) | [job.yaml](../../examples/batch/job.yaml) |
| [PyTorchJob](#pytorchjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [pytorchjob.yaml](../../examples/batch/pytorchjob.yaml) |
| [MPIJob](#mpijob-kubeflow-training-operator) | Gang | Kubeflow Training Operator / MPI Operator | [mpijob.yaml](../../examples/batch/mpijob.yaml) |
| [TFJob](#tfjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [tfjob.yaml](../../examples/batch/tfjob.yaml) |
| [XGBoostJob](#xgboostjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [xgboostjob.yaml](../../examples/batch/xgboostjob.yaml) |
| [JAXJob](#jaxjob-kubeflow-training-operator) | Gang | Kubeflow Training Operator | [jaxjob.yaml](../../examples/batch/jaxjob.yaml) |
| [RayJob](#rayjob-kuberay-operator) | Gang | KubeRay Operator | [rayjob.yaml](../../examples/ray/rayjob.yaml) |
| [RayCluster](#raycluster-kuberay-operator) | Gang | KubeRay Operator | [raycluster.yaml](../../examples/ray/raycluster.yaml) |
| [JobSet](#jobset-kubernetes-sig) | Gang | JobSet Controller | [jobset.yaml](../../examples/batch/jobset.yaml) |
| [TrainJob](#trainjob-kubeflow-trainer-v2) | Gang | Kubeflow Trainer v2 | [jobset.yaml](../../examples/batch/jobset.yaml) *(JobSet reference)* |
| [SparkApplication](#sparkapplication-spark-operator) | Gang | Spark Operator | [sparkapplication.yaml](../../examples/batch/sparkapplication.yaml) |

### Standard Kubernetes Job

Standard Kubernetes Jobs run batch workloads where pods can be scheduled independently.

- **Scheduling Behavior:** Independent scheduling (pods scheduled independently)
- **External Requirements:** None (native Kubernetes resource)
- **Example:** [examples/batch/job.yaml](../../examples/batch/job.yaml)
- **Apply:** `kubectl apply -f examples/batch/job.yaml`

### PyTorchJob (Kubeflow Training Operator)

PyTorchJob enables distributed PyTorch training across multiple GPUs and nodes using the Kubeflow Training Operator.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/) - See [installation guide](https://github.com/kubeflow/training-operator#installation) and [PyTorchJob user guide](https://trainer.kubeflow.org/en/latest/legacy-v1/user-guides/pytorch.html)
- **Example:** [examples/batch/pytorchjob.yaml](../../examples/batch/pytorchjob.yaml)
- **Apply:** `kubectl apply -f examples/batch/pytorchjob.yaml`

### MPIJob (Kubeflow Training Operator)

MPIJob enables distributed training and HPC workloads using the Message Passing Interface (MPI) protocol.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/mpi/) - See [installation guide](https://github.com/kubeflow/training-operator#installation) and [MPIJob user guide](https://trainer.kubeflow.org/en/latest/legacy-v1/user-guides/mpi.html)
- **Example:** [examples/batch/mpijob.yaml](../../examples/batch/mpijob.yaml)
- **Apply:** `kubectl apply -f examples/batch/mpijob.yaml`

### TFJob (Kubeflow Training Operator)

TFJob enables distributed TensorFlow training across multiple nodes.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/tftraining/) - See [installation guide](https://github.com/kubeflow/training-operator#installation) and [TFJob user guide](https://trainer.kubeflow.org/en/latest/legacy-v1/user-guides/tensorflow.html)
- **Example:** [examples/batch/tfjob.yaml](../../examples/batch/tfjob.yaml)
- **Apply:** `kubectl apply -f examples/batch/tfjob.yaml`

### XGBoostJob (Kubeflow Training Operator)

XGBoostJob enables distributed XGBoost training for gradient boosting workloads.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/xgboost/) - See [installation guide](https://github.com/kubeflow/training-operator#installation) and [XGBoostJob user guide](https://trainer.kubeflow.org/en/latest/legacy-v1/user-guides/xgboost.html)
- **Example:** [examples/batch/xgboostjob.yaml](../../examples/batch/xgboostjob.yaml)
- **Apply:** `kubectl apply -f examples/batch/xgboostjob.yaml`

### JAXJob (Kubeflow Training Operator)

JAXJob enables distributed JAX training workloads using JAX's native distributed capabilities.

- **Scheduling Behavior:** Gang scheduling (all pods scheduled together)
- **External Requirements:** Requires [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/training/) - See the [installation guide](https://github.com/kubeflow/training-operator#installation) and [JAXJob user guide](https://trainer.kubeflow.org/en/latest/legacy-v1/user-guides/jax.html)
- **Example:** [examples/batch/jaxjob.yaml](../../examples/batch/jaxjob.yaml)
- **Apply:** `kubectl apply -f examples/batch/jaxjob.yaml`

### RayJob (KubeRay Operator)

RayJob enables distributed computing and machine learning workloads using the Ray framework.

- **Scheduling Behavior:** Gang scheduling (all pods in the Ray cluster scheduled together)
- **External Requirements:** Requires [KubeRay Operator](https://docs.ray.io/en/latest/cluster/kubernetes/index.html) - See [installation guide](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/kuberay-operator-installation.html)
- **Example:** [examples/ray/rayjob.yaml](../../examples/ray/rayjob.yaml)
- **Apply:** `kubectl apply -f examples/ray/rayjob.yaml`
- **Detailed setup:** [KubeRay with KAI Scheduler](../../examples/ray/README.md)

### RayCluster (KubeRay Operator)

RayCluster enables long-running Ray clusters for distributed computing and machine learning workloads.

- **Scheduling Behavior:** Gang scheduling (all pods in the Ray cluster scheduled together)
- **External Requirements:** Requires [KubeRay Operator](https://docs.ray.io/en/latest/cluster/kubernetes/index.html) - See installation instructions above
- **Example:** [examples/ray/raycluster.yaml](../../examples/ray/raycluster.yaml)
- **Apply:** `kubectl apply -f examples/ray/raycluster.yaml`
- **Detailed setup:** [KubeRay with KAI Scheduler](../../examples/ray/README.md)

### JobSet (Kubernetes SIG)

JobSet manages a group of Jobs as a single unit, enabling complex multi-job workflows.

- **Scheduling Behavior:** Gang scheduling per replicatedJob or for all jobs (depends on startup policy)
- **External Requirements:** Requires [JobSet Controller](https://github.com/kubernetes-sigs/jobset) - See [installation guide](https://github.com/kubernetes-sigs/jobset#installation)
- **Example:** [examples/batch/jobset.yaml](../../examples/batch/jobset.yaml)
- **Apply:** `kubectl apply -f examples/batch/jobset.yaml`

**Note:** With `startupPolicyOrder: AnyOrder`, KAI creates one PodGroup for all jobs together. If you use `startupPolicyOrder: InOrder`, KAI creates separate PodGroups to avoid sequencing deadlocks.

### TrainJob (Kubeflow Trainer v2)

TrainJob is the Kubeflow Trainer v2 API for distributed training. KAI Scheduler delegates TrainJob scheduling to the underlying JobSet that Trainer v2 creates, so gang scheduling behavior is inherited from JobSet.

- **Scheduling Behavior:** Gang scheduling (via JobSet delegation)
- **External Requirements:** Requires [Kubeflow Trainer v2](https://github.com/kubeflow/trainer) - See [installation guide](https://github.com/kubeflow/trainer?tab=readme-ov-file#installation)
- **Example:** [examples/batch/jobset.yaml](../../examples/batch/jobset.yaml) (TrainJob wraps a JobSet; use the JobSet example as a reference)

**Note:** KAI Scheduler registers a `skipTopOwnerGrouper` for TrainJob, meaning it skips the TrainJob and applies grouping logic directly to the underlying JobSet resources.

### SparkApplication (Spark Operator)

SparkApplication enables running Apache Spark workloads on Kubernetes.

- **Scheduling Behavior:** Gang scheduling (driver and executors scheduled together)
- **External Requirements:** Requires [Spark Operator](https://github.com/GoogleCloudPlatform/spark-on-k8s-operator) - See [installation guide](https://github.com/GoogleCloudPlatform/spark-on-k8s-operator#installation)
- **Example:** [examples/batch/sparkapplication.yaml](../../examples/batch/sparkapplication.yaml)
- **Apply:** `kubectl apply -f examples/batch/sparkapplication.yaml`

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
