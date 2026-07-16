# Semi-Preemptible Mode

## Overview

In v0.10 we separated Priority and Preemption to allow users to control the two parameters independently, where Preemption has 2 modes (values) - **preemptible** and **non-preemptible**.

We want to add a new 3rd mode, named **semi-preemptible**, where the podgroup will be non-preemptible up to the `min-members`, and any extra pods are "elastic" pods and preemptible.

## Use Cases

This value means a workload with `minReplicas` such as Inference and Elastic Distributed Training can request to be non-preemptible up to its `minReplicas` and then other pods above `min-replicas` are preemptible. This allows to run critical workload with some assured resources and some on-demand and availability based.

## Quota Requirements

The `min-replicas` must be in-quota when allocated. Any "extra" pods can be allocated over-quota. All the pods must respect the Limit setting for the job's queue.
With this requirements, we can see that: 
- A semi-preemptible podgroup where the amount of pods equal to minMember == non-preemptible podgroup
- A semi-preemptible podgroup where minMember is set to 0 == preemptible podgroup

## Subgroups

Subgroups inherit the preemption mode from the podGroup - For example, if the podgroup is **semi-preemptible**, then all the subgroups are **semi-preemptible**.

Under the current implementation, the `minMember` behavior is relevant only for the leaf subgroups and the top podgroup (calculated based on the pod sum). If the top `minMember` equals to the sum of the leaf `minMembers`, all the requirements will remain satisfied under a **semi-preemptible** mode.

For podgroups without any subgroups, we are still dependant on the pod ordering plugin in the scheduler, just like before.

## Simulation Considerations

In simulations, I would consider "possible victims" only the last `n-m` ("the extra") pods. This approach might miss some solutions, but I don't think that checking all $\binom{n}{m}$ options is important enough to make the code less readable and more complicated.

## Implementation Notes

- **Over-quota checks are different**: base in quota, extra can be over-quota
- **For podgroup and queue statuses**: consider only the `min-member` resources for the non-preemptible counting. Like the scheduler, they should know the pod order to know which pods are considered "core", and which pods are "extra".
- "Fully preemptible" representative job for solver simulations containing the "extra" pods of a semi-preemptible job.

## Non-Preemptible Accounting API (Option A)

### Problem

Two components compute "non-preemptible allocated" resources independently, and for
semi-preemptible jobs they diverge:

- The **scheduler** (proportion plugin) counts the **core only** — the minimal-satisfying set,
  via `GetCoreTasks` (`pkg/scheduler/api/podgroup_info/core_info.go`). This is the number that
  drives real quota and reclaim decisions.
- The **podgroupcontroller** counts **all allocated pods** (a flat sum) in `getStatusWithMetadata`
  (`pkg/podgroupcontroller/controllers/patcher/pod_group.go`) and publishes it to
  `PodGroup.Status.ResourcesStatus.AllocatedNonPreemptible`, which the queuecontroller rolls up into
  `Queue.Status.AllocatedNonPreemptible`.

For a semi-preemptible job the controller therefore reports **core + elastic burst** as
non-preemptible. Example: `minSubGroup: 2` over 4 fully-gang subgroups (`minMember: 8`) → the
scheduler counts **16** core pods while the controller publishes **32** — a 2× overstatement. The
value is *observational only* (the scheduler re-seeds core from live job state every session and
never reads the published status back), so scheduling stays correct; but the queue's reported
protected usage is wrong for users, dashboards, and external consumers, and can "flip" between
reconciles because core membership was ranked by live allocation ratio.

### Chosen approach

The **scheduler is the single source of truth** for the core amount. It already computes the core
resource vector per job (`coreResourceQuantities` in the proportion plugin). It publishes that
amount at the **PodGroup level**, and the podgroupcontroller **consumes** it instead of re-deriving
which pods are "core". This removes the divergence and the flip by construction, and stays fully
**Pod-agnostic** — no pod-level labels or markers (an anti-pattern for this feature).

### API shape

A new **scheduler-owned** status block on the PodGroup (in
`pkg/apis/scheduling/v2alpha2/podgroup_types.go`). It is a separate block — not a new field on the
controller-owned `ResourcesStatus` — so each struct keeps a single writer:

```go
// PodGroupSchedulingState carries the scheduler's authoritative accounting for this pod group.
// It is populated exclusively by the scheduler; all other controllers MUST treat it as read-only.
// This is the single source of truth for resource classifications that depend on scheduling order
// (e.g. which portion of a semi-preemptible job's allocation is "core").
type PodGroupSchedulingState struct {
	// NonPreemptibleAllocated is the portion of this pod group's current allocation that the
	// scheduler protects from preemption/reclaim (the "core", i.e. the minimal-satisfying set).
	//   - preemptible      pod group -> empty
	//   - non-preemptible  pod group -> equals total allocation
	//   - semi-preemptible pod group -> the core only (excludes elastic burst)
	// Expressed in the same units as ResourcesStatus (GPU fractions, CPU millicpus, Memory MB).
	// +optional
	NonPreemptibleAllocated v1.ResourceList `json:"nonPreemptibleAllocated,omitempty"`
}

type PodGroupStatus struct {
	// ... existing: Conditions, SchedulingConditions, ResourcesStatus ...

	// SchedulingState is the scheduler's authoritative view of this pod group. Read-only for
	// non-scheduler components.
	// +optional
	SchedulingState *PodGroupSchedulingState `json:"schedulingState,omitempty"`
}
```

`ResourcesStatus` (`Allocated`, `AllocatedNonPreemptible`, `Requested`) is unchanged in shape. The
field is `+optional`/`omitempty` and PodGroup is a single-version alpha type (`v2alpha2`, storage
version), so the change is additive and needs no conversion webhook.

### Data flow

```
scheduler (proportion.coreResourceQuantities)
    → convert rs.ResourceQuantities → v1.ResourceList
    → publish PodGroup.Status.SchedulingState.NonPreemptibleAllocated   [scheduler-owned]
                                   │
                                   ▼
podgroupcontroller.getStatusWithMetadata
    → PodGroup.Status.ResourcesStatus.AllocatedNonPreemptible           [controller-owned]
                                   │
                                   ▼
queuecontroller.sumPodGroupsResources
    → Queue.Status.AllocatedNonPreemptible                              [unchanged rollup]
```

Controller consumption changes its *source*, not its output field:

```go
if state := originalStatus.SchedulingState; state != nil && state.NonPreemptibleAllocated != nil {
	updatedStatus.ResourcesStatus.AllocatedNonPreemptible = state.NonPreemptibleAllocated
} else if !metaData.Preemptible {
	updatedStatus.ResourcesStatus.AllocatedNonPreemptible = metaData.Allocated // legacy fallback
}
```

### Why a nested block (designed for expansion)

`schedulingState` is a namespace for scheduler-authoritative accounting so future needs land without
further top-level status churn. Illustrative future fields (not built now):

```go
// ElasticAllocated        v1.ResourceList // surplus beyond core (reclaimed first)
// MinRequirementSatisfied *bool           // job has reached its minimal shape
// CoreSubGroups           []string        // which subgroups the scheduler counts as core (debug/observability)
// LastEvaluatedSession    string          // scheduler session id/generation for staleness detection
```

Intended invariant once `ElasticAllocated` exists:
`NonPreemptibleAllocated + ElasticAllocated == ResourcesStatus.Allocated`.

### Notes / trade-offs

- **Fidelity**: the scheduler's quota vector tracks CPU/Memory/GPU only, whereas the controller's
  `Allocated` is a full `v1.ResourceList` (may include extended resources). `NonPreemptibleAllocated`
  reflects the scheduler's quota dimensions — acceptable, since non-preemptible quota is
  CPU/Mem/GPU-based. For this reason the field is set for semi-preemptible jobs first; for other
  modes the controller keeps its existing fallback until the producer is broadened.
- **Ownership**: the scheduler and controller already patch `PodGroup.Status` on disjoint paths
  (`schedulingConditions` / annotations vs `resourcesStatus`); adding scheduler-written
  `schedulingState` introduces no new write-conflict class.
- **Staleness**: the value refreshes each scheduler session and only changes when allocation changes
  (which is scheduler-driven). `LastEvaluatedSession` is the future hook if staleness detection is
  ever needed.
- **Defense-in-depth**: Option A removes the need for both sides to recompute core independently. If
  they ever must, core ordering should be deterministic *and reproducible* — rank by static priority
  then name rather than by live allocation ratio (the root cause of the flip).

