# Semi-Preemptible Mode

## Overview

In v0.10 we separated Priority and Preemption to allow users to control the two parameters independently, where Preemption has 2 modes (values) - **preemptible** and **non-preemptible**.

We want to add a new 3rd mode, named **semi-preemptible**, where the podgroup will be non-preemptible up to the `minMember` count of each leaf PodSet, and any extra pods are "elastic" pods and preemptible.

## Use Cases

A workload with `minReplicas` such as Inference and Elastic Distributed Training can request to be non-preemptible up to its `minReplicas`, with any pods above that count being preemptible. This allows running a critical workload with some assured resources and some on-demand, availability-based resources.

## Quota Requirements

The "core" pods (up to `minMember` per leaf PodSet) must be in-quota when allocated. Any "extra" pods can be allocated over-quota. All pods must respect the Limit setting for the job's queue.

From this:
- A semi-preemptible podgroup where the total pod count equals `minMember` == non-preemptible podgroup
- A semi-preemptible podgroup where `minMember` is 0 == preemptible podgroup

## Subgroups and Multi-Level Trees

### Scope: semi-elasticity is a pod-level concept

Semi-elasticity (the core/elastic split) applies **exclusively to pods**. Subgroups and groups are atomic scheduling units — they are either fully scheduled or not. There is no "semi-elastic subgroup" concept. A user who defines fine-grained subgroups intends them to be scheduled as a whole.

### Inheritance

Subgroups inherit the preemption mode from the root PodGroup. If the PodGroup is **semi-preemptible**, all subgroups are **semi-preemptible**.

### `minMember` vs. `minSubGroups`

The core/elastic split is determined **only at nodes that have `minMember` set** (leaf PodSets). Intermediate nodes that use `minSubGroups` define a scheduling gate (how many child subgroups must be satisfied) but do not themselves define a non-preemptible pod threshold.

Since pods are always attached to leaf PodSets and never to intermediate SubGroupSets, this is a natural boundary: `minMember` is always a leaf-level concept.

**Non-preemptible resource count** = sum of (`minMember × pod resource request`) across all scheduled leaf PodSets.

### Behavior when `minSubGroups < scheduled children`

When a parent node requires fewer children than are actually scheduled (i.e., some children are "extra" from the scheduling perspective), each child's core/elastic split is still determined independently by that child's own `minMember`. The "extra-ness" is a scheduling-gate concept handled by the existing elastic subgroup scheduling; it does not override the per-subgroup semi-preemptible semantics.

## Interaction with Segmented Subgroups

Semi-preemptible is **mutually exclusive with automatic segmentation** (the `kai.scheduler/segment-size`
annotation, wired for `PyTorchJob` Worker replicas and `LeaderWorkerSet`). Automatic segmentation emits a
fully-gang tree — every segment leaf gets `MinMember = segmentSize`, no `minSubGroup` and no `MinMember = 0`
— so the job has no elastic surplus. Semi-preemptible therefore has nothing to make preemptible and collapses
to non-preemptible.

This is **soft-enforced in the podgrouper**, not in an admission webhook: only the grouper has both the
segmentation decision and the resolved preemptibility at full fidelity, and for auto-segmented workloads the
user submits the source workload (PyTorchJob/LWS) rather than the PodGroup, so a Warning event on the pod is
more visible than a PodGroup-webhook warning. When a workload is both auto-segmented and semi-preemptible, the
LWS/PyTorch grouper records a warning on the PodGroup metadata (`podgroup.Metadata.Warnings`) and the pod
controller surfaces it as a `PodGrouperWarning` event. The PodGroup is still created unchanged; the warning is
non-blocking.

Hand-authored `minSubGroup` SubGroup trees are **not** affected — those are the intended way to get
subgroup-level elasticity with semi-preemptible, and they do not pass through the segmentation code path.

## Immutability Constraint

A validation webhook must **prevent increases** to `minMember` and `minSubGroups` on a semi-preemptible PodGroup after creation. This applies to the root PodGroup spec and to all SubGroup entries within it.

**Rationale:** once a semi-preemptible job is running, some pods may be over-quota (the elastic ones). Increasing `minMember` or `minSubGroups` would silently reclassify those over-quota pods as "core" non-preemptible pods, violating quota invariants without a rescheduling cycle.

Decreasing these fields is allowed — it can only widen the elastic tier.

## Simulation Considerations

In simulations, only the "extra" (`n - minMember`) pods per leaf PodSet are considered as possible victims. This is applied independently per PodSet; no cross-subgroup victim selection is needed. This approach may miss some solutions when checking all $\binom{n}{m}$ orderings, but the added complexity is not justified for the MVP.

## Implementation Notes

### API

`pkg/apis/scheduling/v2alpha2/podgroup_types.go` — `SemiPreemptible = "semi-preemptible"` added to the `Preemptibility` enum. The kubebuilder validation marker is updated accordingly.

### Victim Selection

`pkg/scheduler/actions/preempt/preempt.go`, `pkg/scheduler/actions/utils/input_jobs.go` — The job-level filter that previously excluded all non-preemptible jobs from victim pools is updated to also admit semi-preemptible jobs. Core pod protection is handled downstream by the existing `minAvailable` guard in `pkg/scheduler/api/podgroup_info/eviction_info.go`:

```go
if ps.GetNumActiveAllocatedTasks() <= int(ps.GetMinAvailable()) {
    return nil  // cannot evict — would violate minAvailable
}
```

No changes to eviction logic are needed; `minAvailable` already maps to `minMember` for leaf PodSets.

### Quota Accounting

`pkg/scheduler/plugins/proportion/capacity_policy/capacity_policy.go` — `IsJobOverQueueCapacity` and `IsNonPreemptibleJobOverQuota` compute a `coreRequiredQuota` for semi-preemptible jobs (sum of resources for at most `minMember` tasks per PodSet from the tasks being allocated) and use it for the non-preemptible quota check. The over-limit (`MaxAllowed`) check still uses the total resource request.

`pkg/scheduler/plugins/proportion/proportion.go` — `allocateHandlerFn` and `deallocateHandlerFn` track `AllocatedNotPreemptible` per-task for semi-preemptible jobs: a task is "core" if the PodSet's allocated count (post-event) is ≤ `minAvailable`. Session initialization (`updateQueuesCurrentResourceUsage`) uses a per-PodSet counter to correctly initialize `AllocatedNotPreemptible` from already-running semi-preemptible jobs.

## Future Work: `minNonPreemptible` field

This design uses `minMember` as the non-preemptible threshold. A future `minNonPreemptible` field (pod-level only, no subgroup analog) would decouple the scheduling minimum from the non-preemptible threshold — allowing e.g. `minMember=4, minNonPreemptible=2` (needs 4 pods to start, but only 2 are non-preemptible).

**Work required:**
1. New API field: `minNonPreemptible *int32` on PodGroupSpec and SubGroup
2. Validation: `minNonPreemptible ≤ minMember`
3. Quota accounting decoupled from `minMember`
4. Webhook: `minNonPreemptible` is also immutable post-creation on semi-preemptible PodGroups
5. Solver/simulation: "core" count = `minNonPreemptible`, not `minMember`
6. Status/queue reporting updated

**Key complexity introduced:** a new middle pod tier — "required for scheduling but elastic for preemption" — between core and extra-elastic. Today pod ordering has two tiers; this adds a third. Explicit ordering or labeling is needed to identify which specific pods fall into each tier.
