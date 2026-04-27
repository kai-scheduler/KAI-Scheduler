# Workload Priority
In Kubernetes, assigning different priorities to workloads ensures efficient resource management, minimizes service disruption, and supports better scaling. 
It allows critical applications to receive the resources they need before less important ones, ensuring high availability and compliance with SLAs.
By prioritizing workloads, KAI Scheduler schedules jobs according to their assigned priority. 
When sufficient resources aren't available for a workload, the scheduler can preempt lower-priority workloads to free up resources for higher-priority ones.
This approach ensures that mission-critical services are always prioritized in resource allocation.

## Priority Classes
KAI scheduler deployment comes with several predefined priority classes:
1. `train` (50) - can be used for preemptible training workloads
2. `build-preemptible` (75) - can be used for preemptible build/interactive workloads
3. `build` (100) - can be used for build/interactive workloads (non-preemptible)
4. `inference` (125) - can be used for inference workloads (non-preemptible) 

## API
Some workloads have a predefined priorities. But, workload can have a custom priority using one of the following ways:
1. By setting `priorityClassName` label on the workload instance with the name of the desired priority class.
2. By setting `priorityClassName` label on the workload's pods with the name of the desired priority class.
3. By setting `pod.Spec.PriorityClassName` on the workload's pods with the name of the desired priority class.

## Preemptibility
KAI Scheduler supports any PriorityClass deployed in the cluster. A PriorityClass with a value of 100 or higher is considered as non-preemptible.
Non preemptible can only consume in-quota resources of the scheduling queue, and cannot go over quota. 
Read about [scheduling queues](../queues/README.md) for more details.

## Workload Default Priority
When priorityClass is not provided, KAI Scheduler will use a predefined default priority based on the workload type:
1. Inference for K8s Deployment or Knative Service
2. Build for Kubeflow Notebook
3. Train as the general default
If pods from the same workload have different priorities, the workload's priority is derived from any of its pods.

## Usability
Workload priorities serve three main purposes:
1. The scheduler attempts to schedule higher priority workloads first.
2. In case of insufficient cluster resources, lower priority workloads can be evicted to prioritize higher priority queues.
3. Workloads with `build` or `inference` priorities are not preemptible, hence they can only run within queue quota boundaries.

## Example
To limit queue resources, use the following command:
```
kubectl apply -f example/limited-queue.yaml
```
It will create a `test` queue that has a limit of 1 GPU.

To submit a pod with `train` priority (with a value of 50), use the following command:
```
kubectl apply -f example/train-priority-pod.yaml
```

After `train-pod` is running, submit a pod with `build` priority (with a value of 100), use the following command:
```
kubectl apply -f example/build-priority-pod.yaml
```
Since both pods request 1 GPU, which is the limit of the `test` queue, only one of the pods will be able to run. 
The scheduler will preempt lower priority `train-pod` and schedule higher priority `build-pod` instead.

## See Also

- [Scheduling Deep Dive](../scheduling-deep-dive/README.md) — How workload priority differs from queue priority, and how preemption and reclaim work
