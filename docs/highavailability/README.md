# High Availability

High availability ensures services continue operating even if some instances fail, preventing downtime and improving fault tolerance.

## Leader Election in Kubernetes

In a distributed system with multiple replicas, some operations must be performed by only one instance at a time. Kubernetes uses leader election to automatically choose one pod as the leader while others stay in standby mode.

## Leader Election Support in KAI Services

KAI services integrate Kubernetes leader election to allow multiple replicas to run safely. With leader election enabled, only one instance actively performs operations that require a single active leader at any moment, while others can take over if the leader fails.

Leader election for KAI services is disabled by default. Enable it during installation using:

```
--set "global.leaderElection=true"
```

## Increasing the number of running replicas

After enabling leader election, scale all KAI service replicas in the `kai-scheduler` namespace with:

```
kubectl scale deployment --all --replicas=2 -n kai-scheduler
```

## Pod Anti Affinity

In order to spread replicas of each individual service across cluster nodes, set the following value:
```
--set global.requireDefaultPodAntiAffinityTerm=true
```
This will add a required podAntiAffinityTerm to all deployments, requiring the scheduler to not place multiple pods of the same service on the same node.
