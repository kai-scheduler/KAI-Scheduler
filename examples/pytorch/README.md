# PyTorchJob with KAI Scheduler

This example demonstrates how to run distributed PyTorch training jobs using the [Kubeflow Training Operator](https://www.kubeflow.org/docs/components/trainer/) with KAI Scheduler.

## Prerequisites

### Kubeflow Training Operator Versions

Kubeflow provides two versions of the Training Operator with different CRDs and APIs, both are supported by KAI. v1 (legacy) uses`PyTorchJob`, `TFJob`, `MPIJob` and `XGBoostJob`, with v2 using `TrainJob`.

For migration guidance, see the [Kubeflow Trainer v2 Migration Guide](https://www.kubeflow.org/docs/components/trainer/operator-guides/migration/).

### Install the Kubeflow Training Operator

#### Option 1: Training Operator v1 (Recommended for Production)

Install the legacy Training Operator which provides `PyTorchJob`, `TFJob`, and other framework-specific CRDs:

```bash
# Using kubectl (standalone deployment)
kubectl apply -k "github.com/kubeflow/training-operator.git/manifests/overlays/standalone?ref=v1.8.1"
```

Verify the installation:

```bash
kubectl get crd pytorchjobs.kubeflow.org
kubectl get pods -n kubeflow
```

Wait for the operator to be ready before submitting jobs:

```bash
kubectl wait --for=condition=ready pod -l control-plane=kubeflow-training-operator -n kubeflow --timeout=120s
```

#### Option 2: Kubeflow Trainer v2 (Alpha)

Install the new unified Trainer which provides `TrainJob` CRD:

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

Wait for the operator to be ready before submitting jobs:

```bash
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=kubeflow-trainer -n kubeflow-trainer-system --timeout=120s
```

For detailed installation options, see:
- [Training Operator v1 Installation](https://www.kubeflow.org/docs/components/trainer/legacy-v1/installation/)
- [Kubeflow Trainer v2 Installation](https://www.kubeflow.org/docs/components/trainer/operator-guides/installation/)

## Configuring PyTorchJob for KAI Scheduler

To schedule PyTorchJob workloads with KAI Scheduler, you need to configure two things in your PyTorchJob YAML:

### 1. Queue Assignment Label

Add the `kai.scheduler/queue` label to the PyTorchJob metadata to assign the job to a scheduling queue:

```yaml
metadata:
  name: pytorch-simple
  labels:
    kai.scheduler/queue: default-queue
```

This label tells KAI Scheduler which queue should manage the job. The queue determines scheduling priority, resource quotas, and fairshare allocation.

### 2. Scheduler Name in Pod Templates

Set `schedulerName: kai-scheduler` in the pod template spec for **each replica type** (Master, Worker, etc.):

```yaml
spec:
  pytorchReplicaSpecs:
    Master:
      template:
        spec:
          schedulerName: kai-scheduler
          # ... containers
    Worker:
      template:
        spec:
          schedulerName: kai-scheduler
          # ... containers
```

This ensures that all pods created by the PyTorchJob are scheduled by KAI Scheduler rather than the default Kubernetes scheduler.

## How It Works

When you submit a PyTorchJob with KAI Scheduler:

1. The **podgrouper** component detects the PyTorchJob and automatically creates a PodGroup resource that groups all pods (Master and Workers) together.
2. KAI Scheduler treats all pods in the PodGroup as a single scheduling unit, ensuring gang scheduling - all pods are scheduled together or none are scheduled.
3. The job is scheduled according to the queue's priority and available resources.

## Running the Example

Submit the example PyTorchJob:

```bash
kubectl apply -f pytorch-simple.yaml
```

Check the job status:

```bash
kubectl get pytorchjobs
kubectl get pods -l training.kubeflow.org/job-name=pytorch-simple
```

## Examples

| Directory | Description |
|-----------|-------------|
| [V1/](V1/) | PyTorchJob examples using Training Operator v1 |
| [v2/](v2/) | TrainJob examples using Kubeflow Trainer v2 |

## Using TrainJob (v2) with KAI Scheduler

For Kubeflow Trainer v2 examples using `TrainJob`, see the [v2/](v2/) directory which includes:
- A `TrainJob` configured with KAI queue label
- A `ClusterTrainingRuntime` with `schedulerName: kai-scheduler`

The key difference with v2 is that the scheduler is configured in the `ClusterTrainingRuntime` or `TrainingRuntime` rather than directly in the job spec.

## Additional Resources

- [Kubeflow PyTorchJob Documentation (v1)](https://www.kubeflow.org/docs/components/trainer/legacy-v1/user-guides/pytorch/)
- [Kubeflow Trainer PyTorch Guide (v2)](https://www.kubeflow.org/docs/components/trainer/user-guides/pytorch/)
- [Kubeflow Trainer v2 Migration Guide](https://www.kubeflow.org/docs/components/trainer/operator-guides/migration/)
- [KAI Scheduler Queues](../../docs/queues/README.md)
