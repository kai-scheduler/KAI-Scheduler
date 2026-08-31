<!--
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# In-Place Pod Resize Accounting and Queue Admission

*Status: Implemented*

Related issues: [#1906](https://github.com/kai-scheduler/KAI-Scheduler/issues/1906),
[#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872) (deferred resize eviction — separate track)

## Motivation

In-place resize (`pods/resize`) lets a running Pod change CPU/memory without
rescheduling. KAI has three gaps:

1. **Accounting.** KAI charges Pod resources from the spec. While a resize is
   pending, infeasible, or in progress, spec ≠ occupancy. That creates phantom
   capacity on downsize, overcharges on infeasible upsize, and corrupts queue
   allocation, fair share, reclaim, and reporting.
2. **Queue admission.** Resize bypasses scheduling. A Pod can grow past queue
   `limit`, or past deserved `quota` when non-preemptible. The validating
   webhook matches `pods`, not `pods/resize`, so it never sees an old→new
   delta.
3. **Deferred resize eviction.** When the kubelet marks a resize `Deferred`
   for node capacity, KAI does not preempt or reclaim on that node to make
   room. Out of scope here — tracked in
   [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872).

This design covers (1) and (2). Upstream ResourceQuota already admits resize
via status-aware requests and a positive usage delta; KAI needs the same
effective-request model and a hierarchical check.

## Design

### Effective accounting

Use the upstream effective request as KAI's default Pod resource vector. Per
container and resource, before Pod-level aggregation:

```text
normal / Deferred / in progress:
    effective = max(spec request, allocatedResources, status.resources)

Infeasible:
    effective = max(allocatedResources, status.resources)
```

Drive node fit, queue `Allocated` / `AllocatedNotPreemptible`, fair share,
ordering, reclaim, victim selection, and allocated status/metrics from this
vector. Do not plumb a separate desired-resource vector through the
scheduler; read the raw spec only for intent (proposed target, user-facing
desired fields, infeasible diagnostics).

**`Infeasible` detection** delegates to upstream
`resource.IsPodResizeInfeasible`, which keys off the condition reason alone.
The kubelet owns the condition lifecycle — it clears or replaces
`PodResizePending` when a new resize is submitted — so generation tracking is
not needed, and `observedGeneration` is only populated behind the non-GA
`PodObservedGenerationTracking` gate (a guard on it would disable `Infeasible`
handling on gate-off clusters). Upstream semantics are pinned by a
characterization test so a future upstream tightening surfaces as a failure.

The `Deferred` charge at `max(spec, actual)` is load-bearing beyond
accounting: deferred-resize eviction
([#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872),
[#2051](https://github.com/kai-scheduler/KAI-Scheduler/pull/2051)) relies on
the deferred target already being reserved in node and queue accounting, so
capacity freed by evicting victims is not backfilled before the kubelet
enacts the resize. Charging `Deferred` at actual only would reintroduce
eviction thrash.

Implementation: upstream `resource.AggregateContainerRequests` (status
resources enabled), then KAI custom-resource logic.

### Best-effort resize quota admission

Validating webhook on `UPDATE`/`pods/resize` with old and proposed Pods. For
KAI-scheduled Pods:

1. Resolve PodGroup, preemptibility, leaf queue, and ancestors.
2. Compute old and proposed effective requests (inherited `Infeasible` stale).
3. `delta = max(proposed - old, 0)` per resource.
4. Reject if any queue on the path would violate:

```text
all workloads:
    Allocated + delta > limit

non-preemptible:
    AllocatedNotPreemptible + delta > quota (deserved)
```

Usage comes from the best-available snapshot (Queue status / live effective
requests). Downsize always admissible; capacity frees only when effective
usage falls. Limit-only changes with no request growth have zero delta.
Rejections name Pod, PodGroup, limiting queue/ancestor, resource, allocation,
delta, and boundary.

This is **not** atomic across concurrent resizes or against in-flight
scheduler allocations. Existing violations (race, bypass, pre-rollout) keep
full effective accounting, are reported, and block further deepenings via the
webhook and allocate-time capacity checks. No proactive "drain until under
limit" action for now.

### Optional strict upsize block

Config knob, **disabled by default**, rejects request upsizes that could race
past a hard queue bound — even if current usage would still fit:

- **Any** upsize, if the leaf queue or an ancestor has a finite `limit` on that
  resource.
- **Non-preemptible** upsize, if the leaf queue or an ancestor has a finite
  `quota` (deserved) on that resource.

Escape hatch for operators who cannot tolerate best-effort races without a
ledger. Preemptible upsizes on queues that are only bounded by deserved (no
finite limit) remain subject to the best-effort check only — they may go over
deserved by design.

```yaml
spec:
  admission:
    inPlacePodResize:
      validateQuota: true                 # best-effort hierarchical checks
      blockUpsizeOnBoundedQueues: false   # conservative; default off
```

Exact CR field names TBD at implementation.

## Decisions

| Topic | Decision |
| --- | --- |
| Accounting model | Upstream effective request; upstream reason-only `Infeasible` semantics (kubelet owns the condition lifecycle) |
| Resize admit path | Keep native `pods/resize`; validate in webhook (do **not** convert to a KAI-owned scheduling API — breaks VPA / API contract) |
| Non-preemptible growth past quota | Reject at webhook; keep reject at allocate |
| Concurrent-resize / scheduler races | Best effort; optional `blockUpsizeOnBoundedQueues`; reservation ledger only if a concrete issue appears |
| Drain-until-under-limit action | Rejected — poor UX; reclaim stays demand-driven |
| Deferred resize preemption | Separate track: [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872) / [#2051](https://github.com/kai-scheduler/KAI-Scheduler/pull/2051). Depends on `Deferred` charged at max(spec, actual) — the reserved target is its thrash safety |
| Wait for upstream resize gates / KEP-5836 only | Rejected as sole plan — ship Goals accounting + admit now; upstream remains complementary ([kubernetes#131835](https://github.com/kubernetes/kubernetes/issues/131835)) |

## Known gaps

Best-effort admission can race: concurrent upsizes, or a resize vs scheduler
allocate, may double-spend the same remaining headroom.
`blockUpsizeOnBoundedQueues` and allocate/webhook deepenings mitigate today.
Prefer upstream resize gates
([kubernetes#131835](https://github.com/kubernetes/kubernetes/issues/131835))
as the long-term out-of-tree quota hook; add a reservation ledger only if
gates do not land and races hurt in production.

## References

- [KEP-1287: In-place Update of Pod Resources](https://github.com/kubernetes/enhancements/blob/master/keps/sig-node/1287-in-place-update-pod-resources/README.md)
- [Kubernetes 1.35 in-place container resize](https://v1-35.docs.kubernetes.io/docs/tasks/configure-pod-container/resize-container-resources/)
- [Upstream effective Pod request helper](https://github.com/kubernetes/kubernetes/blob/v1.35.4/staging/src/k8s.io/component-helpers/resource/helpers.go)
- [Upstream Pod quota evaluator](https://github.com/kubernetes/kubernetes/blob/v1.35.4/pkg/quota/v1/evaluator/core/pods.go)
- [KEP-5836: Scheduler Preemption for In-Place Pod Resize](https://www.kubernetes.dev/resources/keps/5836)
- [kubernetes#131835: out-of-tree quota vs in-place resize](https://github.com/kubernetes/kubernetes/issues/131835)
