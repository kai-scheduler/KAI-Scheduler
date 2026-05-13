# External PodGroup Example: RayJob + batch/Job

This example demonstrates how multiple independent workloads can be grouped
into a single externally-created PodGroup for atomic scheduling.

**Use case**: A post-training pipeline where a RayJob runs distributed training
and a batch/Job runs evaluation. Both must be scheduled together or not at all.

## Resources

1. `podgroup.yaml` — The externally-created PodGroup (created first)
2. `rayjob.yaml` — A RayJob whose pods reference the external PodGroup
3. `batch-job.yaml` — A batch/Job whose pods reference the external PodGroup

## Usage

```bash
# 1. Create the PodGroup first
kubectl apply -f podgroup.yaml

# 2. Create the workloads — their pods will join the existing PodGroup
kubectl apply -f rayjob.yaml
kubectl apply -f batch-job.yaml

# 3. Watch scheduling events on the PodGroup
kubectl describe podgroup post-training-pipeline -n default
```

## Important notes

- The PodGroup must exist before the workloads are created.
- All pods must have the `pod-group-name: <podgroup-name>` annotation.
- All pods must use `schedulerName: kai-scheduler`.
- The PodGroup's `spec.queue` must reference an existing KAI queue.
- `spec.minMember` should reflect the total number of pods across all workloads
  that must be schedulable before any pod is scheduled.
- Without an `ownerReference`, the PodGroup is not garbage collected automatically.
  Delete it manually when the pipeline completes.
