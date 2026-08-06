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

> **Note:** With automatic segmentation (`kai.scheduler/segment-size`), semi-preemptible only has an
> effect if the emitted tree has surplus — a LeaderWorkerSet segmented tree is fully gang and behaves
> as non-preemptible, while a `PyTorchJob` with `minReplicas` keeps its trailing segments elastic.
> Use hand-authored `minSubGroup` trees to control subgroup-level elasticity explicitly.
