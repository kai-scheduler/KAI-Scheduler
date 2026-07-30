<!--
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# In-Place Pod Resize Accounting and Queue Admission

*Status: Draft*

Related issues: [#1906](https://github.com/kai-scheduler/KAI-Scheduler/issues/1906), [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872)

## Background

Kubernetes 1.35 supports stable in-place Pod resize via the `pods/resize`
subresource (KEP-1287). CPU and memory can change without restarting the Pod.
A running Pod therefore has three concurrent resource layers per container:

| Layer | Field | Meaning |
| --- | --- | --- |
| Desired | `spec.containers[*].resources` | What the user requested |
| Allocated | `status.containerStatuses[*].allocatedResources` | What the kubelet admitted |
| Actuated | `status.containerStatuses[*].resources` | What the container runtime enforces |

Upstream Kubernetes computes an effective request per container and resource
before aggregating into a Pod total:

```text
Normal, Deferred, or InProgress:  effective = max(spec, allocated, actuated)
Infeasible:                        effective = max(allocated, actuated)
```

The spec is excluded for `Infeasible` because it can never be realized on the
current node. The max is per-container, not per-Pod, because the kubelet applies each
container's resize independently.

Pod conditions `PodResizePending/Deferred`, `PodResizePending/Infeasible`, and
`PodResizeInProgress` report progress. Each condition carries `observedGeneration`
identifying the Pod generation for which the kubelet made the decision.

## Current KAI Gaps

**Spec-only accounting.** `getPodResourceRequest`
(`pkg/scheduler/api/pod_info/pod_info.go`) reads only `spec.containers[*].resources`.
This causes phantom node capacity during a pending downsize, permanent
over-accounting after an infeasible upsize, and incorrect `AllocatedNotPreemptible`
in both cases. The same spec-only path feeds node fit, queue allocation,
fair-share, reclamation, victim selection, and status reporting.

**Resize bypasses queue policy.** KAI enforces queue quota and limits at
scheduling time. Once a Pod is running, a user can increase its CPU or memory
through `pods/resize` without triggering a new scheduling decision. A workload
can therefore silently exceed its queue quota or limit, and a non-preemptible
workload can acquire more than its queue's deserved share — undermining the
guarantees KAI made at scheduling time.

## Goals

- Use the Kubernetes effective Pod request as KAI's default resource vector.
- Enforce queue quota and limit for resize.

## Non-Goals

- Guaranteeing that a deferred resize will eventually complete.

## Design

### Central effective-request helper

One function replaces all three current call sites:

- `getPodResourceRequest` in `pkg/scheduler/api/pod_info/pod_info.go`
- `resourcehelper.PodRequests` in `pkg/scheduler/plugins/numa/requests.go`
- `calculatedAllocatedResources` in `pkg/podgroupcontroller/controllers/metadata/pod.go`

It computes the effective request using all three resource layers (desired,
allocated, actuated) and converts the result into KAI's resource
representation, preserving existing GPU accounting.

**Generation-aware infeasible handling.** Upstream `IsPodResizeInfeasible`
does not check `observedGeneration`. KAI will treat an infeasible condition as
current only when:

```text
condition.reason == Infeasible &&
condition.observedGeneration == pod.metadata.generation
```

A stale condition from generation N must not exclude the new spec in generation
N+1. During admission, any inherited infeasible condition belongs to the old
generation and is ignored for the proposed calculation.

The effective vector replaces the spec vector everywhere: node fit, queue
allocation, `AllocatedNotPreemptible`, fair-share, reclamation, victim
selection, and allocated-resource reporting. The raw spec is read only when the
documented meaning is user intent (validating a proposed target, diagnosing an
infeasible resize).

### Resize admission

`pods/resize` is a distinct subresource and does not route through the
existing `/validate--v1-pod` endpoint. A new webhook path
(`/validate--v1-pod-resize`) is registered for it. This ensures both old and
proposed Pods are always available and keeps resize validation separate from
general pod validation.

Admission steps:

1. Skip Pods not scheduled by KAI.
2. Resolve PodGroup, leaf queue, ancestors, and preemptibility.
3. Compute old effective request (old Pod) and proposed effective request (new
   spec + existing status, ignoring any inherited infeasible condition).
4. `delta = max(proposed effective − old effective, 0)`
5. Check the hierarchy:
   ```text
   Allocated + delta ≤ limit            (all workloads)
   AllocatedNotPreemptible + delta ≤ quota  (non-preemptible)
   ```
6. If admitted, immediately update `Queue.status.allocated` by the delta,
   using the Queue's `resourceVersion` as an optimistic concurrency token.
   Retry on conflict with fresh queue status.

Pure downsizes are always admitted; they do not release capacity until the
effective request falls. Dry-run requests are validated but do not update
Queue status.

The queue controller's reconciler remains the source of truth: it
periodically recomputes `Allocated` from actual pod effective requests and
corrects any drift. Because an upsize makes the pod's effective request rise
immediately, the reconciler converges to the same value the webhook wrote.

This mirrors the upstream ResourceQuota pattern, where the admission controller
writes to `ResourceQuota.status.used` immediately on admission and the
ResourceQuota controller reconciles it periodically from actual usage.

### Violations

A queue can end up over its quota or limit in several ways: a resize admitted
before this feature was deployed, a disabled or unavailable webhook, or a race
where the queue controller reconciles from a stale pod cache and overwrites the
webhook's Queue status update before the pod informer delivers the resize event.

The queue controller detects a violation when it reconciles `Allocated` from
actual pod effective requests and finds `Allocated > limit` or, for
non-preemptible workloads, `AllocatedNotPreemptible > quota`. When this
happens:

- The violation is recorded as a condition and metric on the Queue.
- The scheduler treats the queue as over-capacity and blocks further allocation
  until the queue returns to compliance.
- Further resize upsizes that would deepen the violation are rejected.
- Downsizes and Pod termination are always allowed, as they move the queue
  toward compliance.
- No capacity is manufactured by ignoring the resize — the full effective
  request continues to count against the queue.

Whether to proactively evict a preemptible pod to restore compliance, or
block new allocations and wait for natural compliance, is covered in decision
point 1.

## Rollout

Ship accounting first — it is independently safe and fixes real scheduler
bugs. Strict admission is feature-gated and requires the updated webhook and
RBAC write access to Queue status before enabling enforcement.

## Decision Points

### 1. Violation remediation

Because admission is not hermetic, a resize may slip through and put a queue
over its limit or quota. When the scheduler detects this, it must decide how
to restore the queue's guarantees. Remediation applies to the queue as a whole
since the contributing resize cannot always be identified. In all cases the
violation is surfaced as a condition and metric on the Queue.

#### 1a. Limit violation (`Allocated > limit`)

| Option | Behaviour | Trade-off |
| --- | --- | --- |
| Block and wait | No new allocations until compliance is restored through natural downsizes or terminations | Least disruptive; matches upstream ResourceQuota; violation may persist indefinitely |
| Evict a preemptible pod | Proactively evict a preemptible pod to restore compliance | Faster recovery; punishes a bystander for a system race |
| Evict a non-preemptible pod (fallback) | If no preemptible pod is available, evict a non-preemptible pod | Immediate recovery; breaks the non-preemptible guarantee |

#### 1b. Quota violation (`AllocatedNotPreemptible > quota`)

Evicting a preemptible pod does not help — it reduces `Allocated` but not
`AllocatedNotPreemptible`.

| Option | Behaviour | Trade-off |
| --- | --- | --- |
| Block and wait | Block further non-preemptible allocations and wait for natural compliance | Safe; violation may persist indefinitely |
| Evict a non-preemptible pod | Evict a non-preemptible pod to restore compliance | Immediate recovery; breaks the non-preemptible guarantee |

#### 1c. Victim selection order

When eviction is chosen, should pods that have upsized since initial scheduling
be preferred as victims? Preferring upsized pods is more principled — they are
the likely source of the violation — but requires comparing the current
effective request against what was recorded at scheduling time, which is not
currently tracked. Resize conditions can serve as a proxy but only while the
condition is still present.

## Follow-Up: Deferred Resize Preemption (#1872)

This must follow accounting and strict admission. Without them, KAI could
evict workloads to help a resize that never passed quota checks.

KAI will attempt to free node capacity for a deferred resize only when:

- the Pod has a current-generation `PodResizePending/Deferred` condition;
- the desired request exceeds `max(allocated, actuated)` for at least one resource;
- `Queue.status.allocated` already includes the resize delta, confirming the
  webhook admitted it and the queue has the entitlement; and
- no eviction attempt for the same generation is in progress.

The action computes `resize delta = target − max(allocated, actuated)` and
`node shortfall = max(node used − node allocatable, 0)`, both per resource.
Victim selection is restricted to the current node and follows existing KAI
policy: lower-priority preemptible same-queue pods for preemption, over-quota
preemptible cross-queue pods for reclamation. Non-preemptible and in-quota
victims are protected. Reclaim entitlement is evaluated against the
pre-resize allocation baseline, not the post-resize effective allocation.

Before committing evictions, KAI revalidates the generation, condition, and
node. It evicts only enough victims to cover the node shortfall and does not
mark the resize complete — the kubelet decides that.

This is best-effort. There may be no eligible victims, or kubelet constraints
(topology, memory manager) may block the resize even after capacity is freed.
