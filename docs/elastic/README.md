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

A **semi-preemptible** workload keeps its **minimal required shape** non-preemptible and in-quota, while everything above that minimum runs **elastically** — allocated over-quota and reclaimed/preempted first. This lets inference and elastic-training workloads guarantee a minimum while bursting opportunistically when spare capacity exists.

The core (non-preemptible) set is the tree's minimal satisfying set, computed recursively:
- at a leaf PodSet, the `minMember` highest-priority pods are core; pods beyond `minMember` are elastic;
- at a mid-level SubGroupSet, the `minSubGroup` highest-priority child subgroups are core; additional scheduled subgroups are elastic and reclaimed **as a whole subgroup** (never split).

Two edge cases follow directly: a semi-preemptible PodGroup whose total pod count equals `minMember` behaves like a **non-preemptible** job (no elastic tier), and one with `minMember: 0` behaves like a fully **preemptible** job.

### Enabling semi-preemptible

Set the `preemptibility` field on the PodGroup, or the `kai.scheduler/preemptibility` label on the workload (the PodGrouper propagates it to the PodGroup).

**Elastic single group** (3 core pods, bursts beyond) — see [`semi-preemptible/podgroup-elastic.yaml`](../../examples/semi-preemptible/podgroup-elastic.yaml):
```yaml
apiVersion: scheduling.kai.nvidia.com/v2alpha2
kind: PodGroup
spec:
  preemptibility: "semi-preemptible"
  minMember: 3            # 3 core (non-preemptible) pods; pods above 3 are elastic
```

**On a workload (label)**:
```yaml
metadata:
  labels:
    kai.scheduler/preemptibility: "semi-preemptible"
```

**Hand-authored multi-subgroup tree** (2 of 4 replica subgroups core, the rest elastic) — see [`semi-preemptible/podgroup-subgroups.yaml`](../../examples/semi-preemptible/podgroup-subgroups.yaml):
```yaml
spec:
  preemptibility: "semi-preemptible"
  minSubGroup: 2          # 2 of 4 replica subgroups are core; the rest are elastic
  subGroups:
    - name: replica-0     # core
      minMember: 8
    - name: replica-1     # core
      minMember: 8
    - name: replica-2     # elastic — evicted as a whole subgroup
      minMember: 8
    - name: replica-3     # elastic — evicted as a whole subgroup
      minMember: 8
```

The valid, supported cases are elastic workloads (`minReplicas < replicas`) and hand-authored `minSubGroup` trees. Increasing `minMember` or `minSubGroup` on a running semi-preemptible PodGroup is rejected by the admission webhook (it would reclassify running elastic pods/subgroups as core); decreasing is allowed.

### Not compatible with automatic segmentation

Semi-preemptible is **not compatible with automatic segmentation** (the `kai.scheduler/segment-size` annotation). An auto-segmented tree is fully gang and has no elastic surplus, so semi-preemptible is inert. When a workload requests both, the PodGrouper still creates the PodGroup but emits a `PodGrouperWarning` event on the pod, and the workload behaves as non-preemptible. Hand-authored `minSubGroup` trees are the supported way to get subgroup-level elasticity.
