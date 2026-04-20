# Segment-Based Topology Placement

Distributed workloads often use hierarchical communication patterns. For example, a 16-worker training job may be organized as 4 tensor-parallel groups of 4 workers, where intra-group communication is frequent and benefits from tight locality (NVLink/NVSwitch on the same rack), while inter-group communication is less frequent and tolerates a larger blast radius.

Segment-based topology placement lets users express this with a few annotations on a workload's PodTemplate, without manually defining identical SubGroups in a `PodGroup` spec. The PodGrouper splits the replicas into fixed-size **segments** and creates a SubGroup for each segment, applying the requested topology constraint to every segment.

This page is a user guide. For the full design rationale see the [design document](../developer/designs/segmented-subgroups/README.md). For the underlying SubGroup mechanism see [Multi-Level Topology Aware Scheduling](multilevel.md).

---

## Annotations

Segment annotations are placed on the **PodTemplate** of the replica that should be segmented (e.g. the `Worker` template of a `PyTorchJob`, or the `workerTemplate` of a `LeaderWorkerSet`).


| Annotation                                           | Required                 | Description                                                                                |
| ---------------------------------------------------- | ------------------------ | ------------------------------------------------------------------------------------------ |
| `kai.scheduler/topology`                             | Yes                      | Name of the `Topology` resource to use. May also be set at the workload level.             |
| `kai.scheduler/segment-size`                         | Yes                      | Number of pods per segment. Must be greater than 1 and not greater than the replica count. |
| `kai.scheduler/segment-topology-required-placement`  | One of these is required | Topology level that all pods within a segment must share (hard constraint).                |
| `kai.scheduler/segment-topology-preferred-placement` |                          | Topology level that pods within a segment should share when possible (soft constraint).    |


Notes:

- The `kai.scheduler/topology` annotation may be set on the workload root or on the PodTemplate. When both are set, the PodTemplate annotation wins.
- If `kai.scheduler/topology` cannot be resolved, segment annotations are ignored.
- For LeaderWorkerSet, `segment-size` may also be set via `spec.leaderWorkerTemplate.subGroupPolicy.subGroupSize`. The annotation takes effect only when that field is not set.

---

## How Pods Are Assigned to Segments

Segment assignment is deterministic from a per-pod **index label** that the workload controller writes onto each pod (e.g. `training.kubeflow.org/replica-index` for Kubeflow jobs). For each pod, the PodGrouper reads this label and computes:

```
segment_index = floor(replica_index / segment-size)
```

So with `segment-size = 4`, pods with index 0-3 land in segment 0, pods 4-7 in segment 1, and so on. Different workload kinds use different default index labels — see the next section. LeaderWorkerSet uses a slight variation to account for the leader pod (see the [LeaderWorkerSet example](#example-leaderworkerset--leaderworker-policy) below).

---

## Supported Workloads


| Workload Kind           | Default Index Label                        | Notes                                                                                                                                              |
| ----------------------- | ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PyTorchJob` (Kubeflow) | `training.kubeflow.org/replica-index`      | Segmentation applies to the `Worker` replica only.                                                                                                 |
| `LeaderWorkerSet`       | `leaderworkerset.sigs.k8s.io/worker-index` | The leader pod has index 0; workers start at 1. Segmentation honours `subGroupPolicyType` (`LeaderWorker` or `LeaderExcluded`).                     |


Segmentation for other workload kinds is not currently wired up. For those workloads, use a hand-written `PodGroup` with explicit subgroups as described in [Multi-Level Topology Aware Scheduling](multilevel.md).

---

## How Segmentation Maps to SubGroups

Given a replica with `R` pods and `segment-size = S`, the PodGrouper:

1. Creates `ceil(R / S)` segment SubGroups.
2. Sets `MinMember = S` on segments that fall within the replica's `MinMember` (mandatory segments). The last segment may be smaller if `R` is not divisible by `S`.
3. Sets `MinMember = 0` on segments beyond the replica's `MinMember` (elastic segments — they do not block allocation).
4. Applies the segment topology constraint to every segment SubGroup.
5. Routes each pod into its segment SubGroup using the index-based mapping described above.

Each segment SubGroup is independently scheduled into a domain that satisfies its topology constraint. Different segments may land in different domains.

---

## Example: PyTorchJob — Single-Level (Rack)

A PyTorchJob with 16 workers, split into 4 segments of 4 workers, each segment co-located on the same rack.

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: pytorch-tp4
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        spec:
          containers:
            - name: pytorch
              image: pytorch/pytorch:latest
    Worker:
      replicas: 16
      template:
        metadata:
          annotations:
            kai.scheduler/topology: "cluster-topology"
            kai.scheduler/segment-size: "4"
            kai.scheduler/segment-topology-required-placement: "cloud.provider.com/topology-rack"
        spec:
          containers:
            - name: pytorch
              image: pytorch/pytorch:latest
```

Result: workers 0-3 are placed on one rack, 4-7 on a (possibly different) rack, and so on. Different segments may land on different racks.

---

## Example: PyTorchJob — Multi-Level (Zone + Rack)

The whole job is constrained to a single zone, while each segment of 4 workers is co-located on a single rack inside that zone.

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: pytorch-tp4-zoned
  annotations:
    kai.scheduler/topology: "cluster-topology"
    kai.scheduler/topology-required-placement: "topology.kubernetes.io/zone"
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        spec:
          containers:
            - name: pytorch
              image: pytorch/pytorch:latest
    Worker:
      replicas: 16
      template:
        metadata:
          annotations:
            kai.scheduler/segment-size: "4"
            kai.scheduler/segment-topology-required-placement: "cloud.provider.com/topology-rack"
        spec:
          containers:
            - name: pytorch
              image: pytorch/pytorch:latest
```

The workload-level annotation pins the entire job to one zone; the PodTemplate-level segment annotation enforces rack co-location within each group of 4 workers.

---

## Example: LeaderWorkerSet — LeaderWorker Policy

A LeaderWorkerSet with 1 leader and 4 workers per group, split into segments of 2. With the default `LeaderWorker` policy, the leader is grouped together with the workers in the first segment.

```yaml
apiVersion: leaderworkerset.x-k8s.io/v1
kind: LeaderWorkerSet
metadata:
  name: lws-segmented
spec:
  replicas: 1
  leaderWorkerTemplate:
    size: 5
    subGroupPolicy:
      subGroupSize: 2
      subGroupPolicyType: LeaderWorker
    leaderTemplate:
      spec:
        containers:
          - name: leader
            image: my/image:latest
    workerTemplate:
      metadata:
        annotations:
          kai.scheduler/topology: "cluster-topology"
          kai.scheduler/segment-topology-required-placement: "cloud.provider.com/topology-rack"
      spec:
        containers:
          - name: worker
            image: my/image:latest
```

For `LeaderExcluded` policy the leader sits in its own SubGroup outside the segments. `LeaderExcluded` is only supported when `(size - 1)` is divisible by `subGroupSize`.

---

## Elasticity

Segments interact naturally with elastic replicas:

- A replica with `MinMember = 12`, `replicas = 20`, `segment-size = 4` produces:
  - Segments 0-2 (pods 0-11): `MinMember = 4` each, mandatory.
  - Segments 3-4 (pods 12-19): `MinMember = 0` each, elastic — these segments can be skipped if no suitable domain is available.

This lets a job start as soon as its mandatory segments fit, while still attempting to place the elastic segments under the same constraint.

---

## Limitations and Notes

- **Pod index is not the distributed-training rank.** Segments are assigned at scheduling time using the static pod-index label set by the workload controller. Frameworks that reassign ranks dynamically at runtime (for example, `torchrun` with elastic membership) may end up with rank assignments that do not match the segment layout. Segment-based co-location still applies, but users should be aware that the rank a process sees inside the container is independent of which segment its pod belongs to. For frameworks where rank tracks pod index, a process can compute its segment as `segment_index = pod_index / segment_size`.
- **`segment-size` must be > 1 and ≤ replica count.** Other values are rejected.
- **`LeaderExcluded` requires divisibility.** For LeaderWorkerSet, `LeaderExcluded` requires `(size - 1)` to be divisible by `subGroupSize`.
- **Topology required.** If `kai.scheduler/topology` cannot be resolved (neither on the workload nor on the PodTemplate), segment annotations are silently ignored.
- **Limited workload coverage.** Today only `PyTorchJob` (Worker replica) and `LeaderWorkerSet` honour these annotations. For other workloads, define segments explicitly via the `PodGroup` API — see [Multi-Level Topology Aware Scheduling](multilevel.md).

