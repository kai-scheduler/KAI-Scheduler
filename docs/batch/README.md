# Batch Scheduling
KAI Scheduler supports scheduling different types of workloads. Some workloads are scheduled as individual pods, while others require gang scheduling, meaning either all pods are scheduled together or none are scheduled until resources become available.

## BatchJob
To run a simple batch job with multiple pods that will be scheduled separately, run the following command:
```
kubectl apply -f batch-job.yaml
```
This will create 2 pods that will be scheduled separately. Both pods will either run at the same time or sequentially, depending on the available resources in the cluster.


## Min Member Override
To require a minimum number of pods to be scheduled together (gang scheduling) for a batch Job or JobSet, use the `kai.scheduler/batch-min-member` annotation on the Job or JobSet resource:
```
kubectl apply -f batch-job-min-member.yaml
```
This will create a job with parallelism of 6, but requires at least 2 pods to be scheduled together before any pod starts running. This is useful for workloads like hyperparameter optimization (HPO) where you want a minimum level of parallelism but don't need all pods running simultaneously.

For JobSets, the annotation overrides the calculated minAvailable for all PodGroups created by the JobSet.

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

### Prerequisites
This requires the [kubeflow-training-operator-v1](https://www.kubeflow.org/docs/components/trainer/legacy-v1/) to be installed in the cluster.

### Instructions
Apply the following command to create a sample PyTorchJob with a master pod and two worker pods:
```
kubectl apply -f pytorch-job.yaml
```
Since gang scheduling is used, all 3 pods will be scheduled together, or none will be scheduled until resources become available in the cluster. 
