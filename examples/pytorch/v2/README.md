# TrainJob (v2) with KAI Scheduler

This example demonstrates how to run distributed PyTorch training using the [Kubeflow Trainer v2](https://www.kubeflow.org/docs/components/trainer/) `TrainJob` API with KAI Scheduler.

## Prerequisites

Install Kubeflow Trainer v2:

```bash
helm install kubeflow-trainer oci://ghcr.io/kubeflow/charts/kubeflow-trainer \
    --namespace kubeflow-trainer-system \
    --create-namespace \
    --version 2.1.0
```

Verify the installation:

```bash
kubectl get crd trainjobs.trainer.kubeflow.org
kubectl get crd clustertrainingruntimes.trainer.kubeflow.org
```

## Setup

### 1. Create the ClusterTrainingRuntime

The `ClusterTrainingRuntime` defines a reusable template for PyTorch distributed training with KAI Scheduler. Apply it first:

```bash
kubectl apply -f cluster-training-runtime.yaml
```

Key configuration for KAI Scheduler:
- `schedulerName: kai-scheduler` in the pod spec ensures pods are scheduled by KAI

### 2. Submit the TrainJob

```bash
kubectl apply -f trainjob-simple.yaml
```

Key configuration for KAI Scheduler:
- `kai.scheduler/queue: default-queue` label assigns the job to a queue
- `runtimeRef.name: torch-distributed-kai` references the runtime with KAI scheduler configured

## How It Works

1. The `TrainJob` references a `ClusterTrainingRuntime` that defines the pod template with `schedulerName: kai-scheduler`
2. KAI's **podgrouper** detects the TrainJob and creates a PodGroup for gang scheduling
3. All training pods are scheduled together as a unit by KAI Scheduler

## Check Status

```bash
# View TrainJob status
kubectl get trainjobs

# View pods created by the TrainJob
kubectl get pods -l trainer.kubeflow.org/job-name=pytorch-simple

# View the PodGroup created by KAI
kubectl get podgroups
```

## Files

| File | Description |
|------|-------------|
| [trainjob-simple.yaml](trainjob-simple.yaml) | Simple TrainJob example with 2 nodes |
| [cluster-training-runtime.yaml](cluster-training-runtime.yaml) | ClusterTrainingRuntime configured for KAI Scheduler |

## Additional Resources

- [Kubeflow Trainer PyTorch Guide](https://www.kubeflow.org/docs/components/trainer/user-guides/pytorch/)
- [Kubeflow Trainer Runtime Guide](https://www.kubeflow.org/docs/components/trainer/operator-guides/runtime/)
- [Migrating from v1 to v2](https://www.kubeflow.org/docs/components/trainer/operator-guides/migration/)
