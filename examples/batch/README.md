# Batch Job Examples

## Min Member Override

By default, batch jobs have a `minMember` of 1, meaning pods are scheduled independently. Use the `kai.scheduler/min-member` annotation to require a minimum number of pods to be scheduled together (gang scheduling).

```bash
kubectl apply -f batch-job-min-member.yaml
```

This creates a job with `parallelism: 6` but requires at least 2 pods to be schedulable before any pod starts running. The annotation value must be a positive integer.

## External PodGroup

Use `external-podgroup-job.yaml` when the PodGroup is created manually or by another controller and the Job should join it without podgrouper interference.

```bash
kubectl apply -f external-podgroup-job.yaml
```

This example shows:

- An explicit `PodGroup` resource with queue and subgroup definitions.
- `kai.scheduler/skip-podgrouper: "true"` on the Job.
- `pod-group-name` on the pod template annotations.
- `kai.scheduler/subgroup-name` on the pod template labels.
