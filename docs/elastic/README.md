# Elastic Workloads
Elastic workloads specify minimum gang thresholds and maximum capacity. KAI Scheduler supports elasticity at both levels of a PodGroup hierarchy:
- `minMember` controls how many pods are required in a flat PodGroup or leaf SubGroup.
- `minSubGroup` controls how many direct child SubGroups are required in a hierarchical PodGroup or mid-level SubGroup.

When resources are limited, KAI Scheduler schedules the required pods or SubGroups first and treats capacity above those thresholds as elastic. If the running workload falls below its required threshold, the gang is evicted.
KAI Scheduler intelligently manages pod roles—prioritizing eviction of non-leader pods when possible.

For example, a PodGroup with four replica SubGroups can set `minSubGroup: 3` so the workload starts once any three replicas satisfy their own `minMember` thresholds. The fourth replica remains elastic and can be scheduled later when resources are available.

#### Prerequisites
This requires the [training-operator](https://github.com/kubeflow/trainer) to be installed in the cluster.

### Elastic Pytorch
To submit an elastic pytorch job, run this command:
```
kubectl apply -f pytorch-elastic.yaml
```
It will create a PytorchJob with a minimum of 1 worker, and will be able to start running as soon as there are enough resource in the cluster for the one pod.
And, if additional resources are available, the workload will be able to add 2 additional workers.
If resources are requested by more prioritized workload, KAI Scheduler will be able to evict only part of its pods and the workload will continue running.

## Semi-Preemptible Workloads

A semi-preemptible workload combines an elastic gang with preemption guarantees: the "core" of the
workload — up to `minMember` pods per leaf, or `minSubGroup` child SubGroups — is **non-preemptible** and
must stay in-quota, while any pods beyond that threshold are **elastic** and may be scheduled over-quota and
preempted first when higher-priority workloads need the resources.

Set it with the `kai.scheduler/preemptibility: semi-preemptible` label on the workload. It is most useful for
genuinely elastic workloads — inference, or elastic training where `minReplicas < replicas` — and for
hand-authored `minSubGroup` SubGroup trees.

### Elastic Semi-Preemptible PyTorch
```
kubectl apply -f semi-preemptible-pytorch.yaml
```
This creates a PyTorchJob whose single `minReplicas` worker is non-preemptible and kept in-quota, while the
additional elastic workers can run over-quota and are reclaimed first under contention.

> **Not compatible with automatic segmentation.** Semi-preemptible and the `kai.scheduler/segment-size`
> annotation are mutually exclusive. Automatic segmentation produces a fully-gang tree with no elastic
> surplus, so semi-preemptible has no effect and the workload behaves as non-preemptible. When both are
> requested, the podgrouper still creates the PodGroup but emits a `PodGrouperWarning` event on the pod. Use
> semi-preemptible with elastic (`minReplicas`) workloads or hand-authored SubGroup trees instead.
