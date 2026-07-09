# Semi-Preemptible Workloads

A **semi-preemptible** workload keeps its minimal required shape non-preemptible and in-quota, while
everything above that minimum runs elastically (allocated over-quota, reclaimed/preempted first). See
the [Elastic Workloads guide](../../docs/elastic/README.md#semi-preemptible-workloads) for the full
model and semantics.

## Examples

- [`podgroup-elastic.yaml`](podgroup-elastic.yaml) — a semi-preemptible PodGroup with a single elastic
  group: `minMember: 3` core pods, extra pods elastic.
- [`podgroup-subgroups.yaml`](podgroup-subgroups.yaml) — a hand-authored multi-subgroup PodGroup with
  `minSubGroup: 2` over 4 fully-gang replica subgroups: 2 core replicas, 2 elastic (reclaimed a whole
  replica at a time).
- [`pytorch-elastic-semi-preemptible.yaml`](pytorch-elastic-semi-preemptible.yaml) — an elastic
  PyTorchJob marked semi-preemptible via the `kai.scheduler/preemptibility` label
  (`minReplicas < replicas`). Requires the training-operator.

## Apply

```bash
kubectl apply -f podgroup-elastic.yaml
```

> **Note:** Semi-preemptible is not compatible with automatic segmentation
> (`kai.scheduler/segment-size`). Combining them still creates the PodGroup but emits a
> `PodGrouperWarning` event, and the workload behaves as non-preemptible. Use hand-authored
> `minSubGroup` trees for subgroup-level elasticity.
