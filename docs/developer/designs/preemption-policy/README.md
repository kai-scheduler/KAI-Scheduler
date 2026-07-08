# PriorityClass PreemptionPolicy Support

*Status: Draft*

Related issues: [#1584](https://github.com/kai-scheduler/KAI-Scheduler/issues/1584), [#1032](https://github.com/kai-scheduler/KAI-Scheduler/issues/1032)

## Table of Contents
- [Background](#background)
- [Problem Statement](#problem-statement)
- [Semantic Model: Aggressor vs. Victim](#semantic-model-aggressor-vs-victim)
- [Goals / Non-Goals](#goals--non-goals)
  * [Goals](#goals)
  * [Non-Goals](#non-goals)
- [Proposal](#proposal)
  * [Scope: Which Flows Are Affected](#scope-which-flows-are-affected)
  * [Reclaim: Deferred Decision](#reclaim-deferred-decision)
  * [Policy Resolution](#policy-resolution)
  * [Scheduler Changes](#scheduler-changes)
  * [Validation and Observability](#validation-and-observability)
  * [Backward Compatibility](#backward-compatibility)
- [Starvation Considerations](#starvation-considerations)
- [Interaction with Existing Features](#interaction-with-existing-features)
- [Alternatives Considered](#alternatives-considered)
- [Examples](#examples)
- [Open Questions](#open-questions)

---

## Background

Kubernetes `PriorityClass` objects carry a `preemptionPolicy` field with two values:

- `PreemptLowerPriority` (default): pods of this class may trigger preemption of lower-priority pods when they cannot otherwise schedule.
- `Never`: pods of this class are placed ahead of lower-priority pods in the scheduling queue, but will never trigger eviction of other pods. They wait for resources to free up naturally.

KAI-scheduler currently ignores this field entirely. Priority class resolution reads only the class `Value` (`pkg/scheduler/cache/cluster_info/cluster_info.go`, `getPodGroupPriority`). Users who configure `preemptionPolicy: Never` on a priority class expect the native semantics and are surprised when KAI preempts on behalf of such workloads.

## Problem Statement

Two distinct expectations exist around this field:

1. **Aggressor-side (native semantics)**: a pending workload whose priority class specifies `Never` must not cause eviction of running workloads.
2. **Victim-side (common misreading)**: some users expect `Never` to mean "this workload can never be evicted".

KAI already has a dedicated, explicit victim-side control: the PodGroup `Preemptibility` field and the `kai.scheduler/preemptibility` label (see [priority-preemptibility-separation](../priority-preemptibility-separation/README.md)). The victim side is therefore covered by existing API. What is missing is the aggressor side — and a clear statement of which KAI eviction flows count as "preemption" for the purpose of this field.

## Semantic Model: Aggressor vs. Victim

This design adopts the native Kubernetes semantics: **`preemptionPolicy` is an aggressor-side property only.**

| Concern | Controls | KAI mechanism |
|---|---|---|
| Can this workload evict others to get scheduled? | Aggressor | `PriorityClass.preemptionPolicy` (this design) |
| Can this workload be evicted once running? | Victim | `PodGroup.Spec.Preemptibility` / `kai.scheduler/preemptibility` label |

The two are orthogonal. `preemptionPolicy: Never` does **not** make a workload non-preemptible, and `Preemptibility: non-preemptible` does **not** prevent a workload from preempting others.

## Goals / Non-Goals

### Goals
- Respect `preemptionPolicy: Never` on the preemptor side: a pending podgroup whose resolved priority class specifies `Never` does not trigger priority-based preemption.
- Define, explicitly, which KAI eviction flows are affected and why.
- No behavior change for clusters that do not use `preemptionPolicy: Never`.

### Non-Goals
- Victim-side preemptibility control (already covered by `Preemptibility`).
- Applying the policy to reclaim in P0 (deferred decision; see [Reclaim: Deferred Decision](#reclaim-deferred-decision)).
- Per-pod or per-subgroup aggressor policy within a single podgroup.
- Reserving idle capacity for `Never` workloads that are waiting (matches native kube-scheduler behavior; see [Starvation Considerations](#starvation-considerations)).
- A KAI-specific override field/label for preemption policy (possible follow-up; see [Open Questions](#open-questions)).

## Proposal

### Scope: Which Flows Are Affected

| Flow | Affected by `Never`? | Rationale |
|---|---|---|
| **Preempt** | **Yes** | This is the exact native semantic: eviction of lower-priority workloads in the same queue, triggered by a higher-priority pending workload. A `Never` podgroup is skipped as a preemptor. |
| **Reclaim** | **Deferred — not in P0** | Genuinely contested; see [Reclaim: Deferred Decision](#reclaim-deferred-decision). P0 ships the unambiguous native semantics only. |
| **Consolidation** | No | Consolidation evicts pods only to immediately re-place them within the same scheduling cycle (defragmentation). No workload permanently loses resources, so the `Never` contract — "do not cause others to lose their resources" — is not violated. |
| **StaleGangEviction** | No | Self-eviction of gangs that lost min-member satisfaction. There is no aggressor. |

### Reclaim: Deferred Decision

Whether `Never` should also prevent a podgroup from triggering cross-queue reclaim is left open in this design. P0 excludes it, but this is a deferral, not a rejection. Both positions have substance:

**For applying `Never` to reclaim:**
- The `Never` promise is leaky otherwise. Reclaim victims are selected specifically to place the pending podgroup — from the victim's perspective it is indistinguishable from preemption. An admin who reads `Never` as "my workload never causes evictions" is surprised when it triggers cross-queue reclaim.
- Practical relevance: in many multi-tenant deployments, same-queue priority preemption is the rarest eviction flow (one workload tier per queue is common), and most real evictions come from reclaim. A preempt-only scope risks honoring the field in the flow where it matters least.
- It enables a "fully opportunistic" workload intent — run only on free capacity, displace nothing — at workload granularity.

**Against applying `Never` to reclaim:**
- **Ownership boundary**: the PriorityClass author is not necessarily the queue admin. A pod-level field would decide whether a queue's deserved quota is recoverable, which is a queue-level concern.
- **Demand accounting**: a podgroup skipped in reclaim still contributes demand to its queue's requested share. Either that demand still drives reclaim "for the queue" (defeating the purpose), or it must be excluded from fair-share computation — threading a pod-level exception through the proportion plugin's accounting.
- **Starvation**: an in-quota `Never` workload can starve indefinitely while its queue's deserved quota is occupied over-quota by other queues (see [Starvation Considerations](#starvation-considerations)). This is arguably acceptable when chosen deliberately, but combined with the ownership issue, the choice may not be made by the party that bears the cost.
- **In-queue priority inversion**: lower-priority queue-mates would reclaim past the skipped `Never` job and receive the recovered quota ahead of it.

**Likely shape if support is added later**: a scheduler-level configuration knob (e.g. `preemptionPolicyScope: preempt | preempt-and-reclaim`, default `preempt`), placing the decision with the cluster admin — the correct owner — rather than hard-coding either interpretation. The demand-accounting design would be settled at that point.

**Data that would settle the default**: the relative frequency of preempt-action vs. reclaim-action evictions in real clusters, and whether users of `preemptionPolicy: Never` predominantly intend "don't disrupt priority competition" or "total non-interference". Until then, the fully-opportunistic intent is expressible with existing primitives: a dedicated queue with zero deserved quota and nonzero over-quota weight, holding preemptible workloads — reclaim never fires on its behalf (nothing to reclaim to), allocation is purely over-quota, and the workloads are first to be reclaimed away when owning queues return.

### Policy Resolution

The policy is resolved **once per PodGroup**, from the same source as priority:

```
PodGroup.Spec.PriorityClassName → PriorityClass.PreemptionPolicy
```

- Resolution happens in the scheduler cache alongside `getPodGroupPriority`, and the result is stored on `PodGroupInfo`.
- If the priority class is missing or the field is unset, the policy defaults to `PreemptLowerPriority` (the Kubernetes default).
- No new API surface is introduced: PodGroups already name their priority class, and the podgrouper already derives `priorityClassName` from workload/pod labels for external workloads. `preemptionPolicy` follows automatically from the resolved class.

### Scheduler Changes

1. **Cache**: extend `PodGroupInfo` with the resolved policy, e.g.:

   ```go
   type PodGroupInfo struct {
       // ... existing fields ...
       Priority int32
       // PreemptPolicy is resolved from the podgroup's priority class.
       // Never means this podgroup must not trigger preemption of other workloads.
       PreemptPolicy v1.PreemptionPolicy
   }
   ```

   Populated in `cluster_info` next to `getPodGroupPriority`, which already fetches the `PriorityClass` object via the `DataLister`.

2. **Preempt action**: skip podgroups with `PreemptPolicy == Never` when selecting pending preemptors, before any victim scenario generation (cheap prefilter, analogous to the existing action eligibility checks). Emit a scheduling condition/event on the podgroup explaining that preemption was skipped due to the policy, so "why is my high-priority job pending" is answerable.

3. **No changes in P0** to reclaim, consolidation, stale gang eviction, or allocation order (priority ordering of the pending queue is unaffected — `Never` workloads still schedule ahead of lower-priority ones when resources are free).

### Validation and Observability

- **Admission warning** (not rejection) when a podgroup combines `preemptionPolicy: Never` with `Preemptibility: preemptible` semantics is unusual but legal ("can be evicted, will never evict"). A warning documents intent without blocking valid configurations.
- **Podgrouper**: optionally log when pod specs carry a `preemptionPolicy` that disagrees with the group's resolved policy, so misconfiguration is visible.
- **Docs**: update `docs/priority/README.md` to document the aggressor/victim split and the scope matrix above.

### Backward Compatibility

- All priority classes shipped by KAI (`train`, `build-preemptible`, `build`, `inference`) leave `preemptionPolicy` unset, which defaults to `PreemptLowerPriority`. Existing clusters see no behavior change.
- The change activates only for workloads whose priority class explicitly sets `Never` — a configuration that today produces surprising behavior (unexpected preemption), so honoring it is a fix from the user's perspective.
- No scheduler configuration flag is proposed; the Kubernetes default keeps the feature opt-in per priority class. (Open question below if a kill switch is desired.)

## Starvation Considerations

A `Never` workload cannot fight for resources, so it can wait indefinitely while lower-priority workloads continue to be allocated onto capacity that frees up naturally. This matches native kube-scheduler behavior and is inherent to the semantics of `Never`. How far the starvation extends depends on the reclaim scope decision:

**Under the P0 scope (reclaim unaffected)**, starvation is bounded by quota: a `Never` workload in an under-quota queue still benefits from reclaim performed on behalf of its queue. It waits only for what its queue is not entitled to.

**If `Never` is later applied to reclaim**, starvation can become unbounded even in-quota. Concretely: a 16-GPU cluster, two queues with 8 GPUs deserved each; queue B's elastic preemptible training job has expanded to all 16 GPUs (8 over-quota); queue A submits an 8-pod inference gang (`minAvailable: 8`) using a `Never` class. Allocate finds no free GPUs; reclaim skips the job as a `Never` reclaimer; preempt is same-queue and queue A runs nothing. When B's pods finish and release capacity in dribbles, the gang cannot take partial allocations and B's elastic job re-absorbs each dribble in the same cycle — the gang never accumulates 8 simultaneous free GPUs. A fully in-quota, highest-priority workload pends indefinitely while priority-50 work occupies its queue's quota. Whether this is a defect or the chosen behavior depends entirely on the admin's intent — which is the crux of the deferred decision above.

Existing mechanisms that bound the impact within a scheduling cycle: resources reclaimed or preempted for a pending workload are protected — the preemptor is virtually allocated in the same cycle as victim selection (`TryToVirtuallyAllocatePreemptorAndGetVictims`), and its tasks enter `Pipelined` status, reserving the capacity while victims terminate. No new reservation mechanism is needed for P0.

Holding capacity idle across cycles for a waiting `Never` workload (cross-cycle resource reservation) is explicitly out of scope, and would be a prerequisite for making `Never`-on-reclaim usable for gang workloads.

## Interaction with Existing Features

- **Preemptibility / semi-preemptible**: fully orthogonal. `Never` affects what a podgroup may do while pending; `Preemptibility` affects what may be done to it while running. A semi-preemptible podgroup with a `Never` class schedules its elastic pods only into free capacity.
- **Elastic jobs**: the podgroup-level policy applies to allocation of all pods, including those above `minAvailable`.
- **Priority ordering**: unaffected. `Never` workloads keep their queue position by priority value.
- **Min-runtime**: unrelated; min-runtime protects victims, this design constrains aggressors.

## Alternatives Considered

1. **Map `Never` to victim-side non-preemptibility.** Rejected: duplicates the existing `Preemptibility` API with a second, conflicting source of truth, and breaks the quota model — non-preemptible workloads must be in-quota, and any user-created PriorityClass could otherwise confer reclaim-immunity to over-quota workloads.
2. **Resolve the policy from pod specs with conservative aggregation** (any `Never` pod → the podgroup never preempts). Rejected for P0: inconsistent with how per-pod priority is treated, and adds an aggregation rule with no enforcement point below podgroup granularity. Recorded here as the fallback definition should podgroup-level resolution prove insufficient.
3. **Apply `Never` to reclaim in P0.** Deferred, not rejected — see [Reclaim: Deferred Decision](#reclaim-deferred-decision) for the full trade-off analysis and the likely config-knob shape if added later.
4. **KAI-specific override field/label in P0** (e.g. `kai.scheduler/preemption-policy`). Deferred: no known use case for "same priority class, different aggressor policy". The class-derived value covers the requirement with zero new API.

## Examples

### Example 1: High-priority class that never preempts

```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: inference-no-preempt
value: 125
preemptionPolicy: Never
```

A podgroup using this class:
- Schedules ahead of lower-priority pending workloads when resources are free.
- Never triggers eviction of running workloads via the preempt action.
- Is non-preemptible as a victim (priority 125 ≥ 100 default threshold), unless `Preemptibility` says otherwise.
- Still benefits from reclaim performed on behalf of its under-quota queue (P0 scope; see the deferred reclaim decision).

### Example 2: Never-preempting but evictable

```yaml
apiVersion: scheduling.kai.nvidia.com/v2alpha2
kind: PodGroup
metadata:
  name: opportunistic-batch
spec:
  priorityClassName: inference-no-preempt   # value 125, preemptionPolicy: Never
  preemptibility: preemptible
```

Runs at high priority in free capacity only; can be evicted by preemption or reclaim, and will never evict others. Suitable for opportunistic workloads. Triggers the admission warning described above (unusual but valid).

## Open Questions

1. Should `Never` apply to reclaim? Deferred from P0 — see [Reclaim: Deferred Decision](#reclaim-deferred-decision) for the trade-offs and the data that would settle it.
2. Is a scheduler-level config for the policy's scope needed from the start (e.g. `preemptionPolicyScope: preempt | preempt-and-reclaim`)? It would double as a rollout kill switch and pre-empt question 1 by delegating it to cluster admins.
3. Should the podgrouper warning on pod/group policy disagreement be an event on the podgroup instead of a log line?
4. Is a `kai.scheduler/preemption-policy` override label a committed follow-up or revisit-on-demand?
