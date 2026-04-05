# Batch Job Examples

## Min Member Override

By default, batch jobs have a `minMember` of 1, meaning pods are scheduled independently. Use the `kai.scheduler/min-member` annotation to require a minimum number of pods to be scheduled together (gang scheduling).

```bash
kubectl apply -f batch-job-min-member.yaml
```

This creates a job with `parallelism: 6` but requires at least 2 pods to be schedulable before any pod starts running. The annotation value must be a positive integer.
