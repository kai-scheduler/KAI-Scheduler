# Quick Start Setup

## Scheduling queues
A queue is an object which represents a job queue in the cluster. Queues are an essential scheduling primitive, and can reflect different scheduling guarantees, such as resource quota and priority. 
Queues are typically assigned to different consumers in the cluster (users, groups, or initiatives). A workload must belong to a queue in order to be scheduled.
KAI Scheduler supports multi-level hierarchical scheduling queues.

### Default Queue on Fresh Install

After installing KAI Scheduler, a default queue hierarchy is automatically created:
* `default-parent-queue` – Top-level (parent) queue. By default, this queue has no reserved resource quotas, allowing governance of resource distribution for its leaf queues.
* `default-queue` – Leaf (child) queue under the `default-parent-queue` top-level queue. Workloads should reference this queue.

The expected default queues are in [default-queues.yaml](default-queues.yaml).

No manual queue setup is required. Both queues will exist immediately after installation, allowing you to start submitting workloads right away.
To customize scheduling, you can create additional queues or modify existing ones to set quotas, priorities, and hierarchies.

### Creating Additional Queues

To add custom queues, apply your queue configuration:
```
kubectl apply -f queues.yaml
```
For detailed configuration options, refer to the [Scheduling Queues](../queues/README.md) documentation.

Pods can now be assigned to the new queue and submitted to the cluster for scheduling.

### Assigning Pods to Queues
To schedule a pod using KAI Scheduler, ensure the following:
1. Specify the queue name using the `kai.scheduler/queue: default-queue` label on the pod/workload.
2. Set the scheduler name in the pod specification as `kai-scheduler`
This ensures the pod is placed in the correct scheduling queue and managed by KAI Scheduler.

### ⚠️ Workload namespaces
When submitting workloads, make sure to use a dedicated namespace. Do not use the `kai-scheduler` namespace for workload submission.

### Submitting Example Pods
#### CPU-Only Pods
To submit a very simple pod that requests CPU and memory resources, use the following command:
```
kubectl apply -f pods/cpu-only-pod.yaml
```

#### GPU Pods
Before you run the below, make sure the [NVIDIA GPU-Operator](https://github.com/NVIDIA/gpu-operator) is installed in the cluster.

To submit a pod that requests a GPU resource, use the following command:
```
kubectl apply -f pods/gpu-pod.yaml
```
