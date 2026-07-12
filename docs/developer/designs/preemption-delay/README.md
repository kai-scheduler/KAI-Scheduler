# Preemption Delay

*Status: Approved — decision points agreed in design review*

Related issues: [#1832](https://github.com/kai-scheduler/KAI-Scheduler/issues/1832) (request), [#1584](https://github.com/kai-scheduler/KAI-Scheduler/issues/1584) / [#1032](https://github.com/kai-scheduler/KAI-Scheduler/issues/1032) (native `preemptionPolicy` — deferred, see below)

## Motivation

In autoscaled clusters, evictions race the cluster autoscaler: KAI preempts or reclaims the moment a pending workload cannot fit, even when a new node would arrive within minutes and no one would have to die. A configurable delay before a workload may trigger evictions lets scale-up win the race, saving restarted work.

## API

A label on the workload's pods, ingested by the podgrouper into the PodGroup (same pattern as `kai.scheduler/preemptibility`):

```yaml
metadata:
  labels:
    kai.scheduler/preemption-delay: "5m"
```

- Value: a non-negative Go duration (`"30s"`, `"5m"`, `"1h"`), matching the `metav1.Duration` format of the existing `preemptMinRuntime`/`reclaimMinRuntime` Queue fields. Missing → 0 (current behavior). Invalid values (including unit-less numbers) fall back to 0 with a log warning.

On the PodGroup itself, a spec field — the podgrouper populates it from the label; workloads that create PodGroups directly set it explicitly (spec wins over label):

```go
type PodGroupSpec struct {
    // PreemptionDelay is the minimal time the podgroup must be pending
    // before it may trigger eviction of other workloads.
    // +optional
    PreemptionDelay *metav1.Duration `json:"preemptionDelay,omitempty"`
}
```

The scheduler reads only the spec field, keeping a single source at scheduling time (same as `Preemptibility` and `PriorityClassName`).
- Aggressor-side only: the delay restricts what the pending workload may do *to others*. It says nothing about the workload's own evictability — that remains `Preemptibility` (victim-side), and the two are orthogonal.

## Semantics

A podgroup whose pending age is below its delay is skipped as an eviction trigger — it does not initiate preempt, reclaim, or consolidation evictions. Everything else is unchanged:

- It allocates normally into free capacity (the delay does not slow plain scheduling).
- It still appears as a pending unschedulable workload, so the cluster autoscaler (and `node-scale-adjuster` for GPU-sharing pods) reacts to it during the window.
- Once the delay expires, the next scheduling cycle treats it as a normal eviction trigger.

**Pending age anchor**: `max(PodGroup.CreationTimestamp, last eviction time)` — creation for new workloads, re-armed when a workload returns to pending after eviction, so each placement attempt gets a fresh autoscaler window. The scheduler stamps a `last-eviction-timestamp` annotation on the podgroup when evicting it, mirroring the existing `kai.scheduler/last-start-timestamp` annotation it stamps on start (survives scheduler restarts; no new status semantics).

**Enforcement point**: a prefilter on the pending podgroup in the preempt, reclaim, and consolidation actions, before victim-scenario generation. The skip is surfaced through the podgroup's existing unschedulable-explanation status (updated on change, not re-emitted every cycle).

## Decided Points

| # | Decision |
|---|---|
| D1 | The delay is the general aggressor-side mechanism: "earliest time a pending podgroup may trigger evictions." Native `preemptionPolicy` (`Never` = ∞) can later resolve onto the same value — see below |
| D2 | Reclaim is in scope from day one — most evictions in multi-tenant clusters are reclaim; a preempt-only delay would miss the motivation |
| D3 | Consolidation is in scope — a delayed workload should not disrupt others in any way during its window; consolidation victims still restart and lose state even though they are re-placed with their resources |
| D4 | Accounting unchanged: the pending demand counts in queue `Request` as usual. The fair-share inflation this implies is bounded by the delay duration (transient, unlike a permanent `Never`) and identical in kind to what any unschedulable pending pod causes today |
| D6 | No global disable flag — the feature is opt-in per workload via the label; missing label means current behavior |

## Deferred: Native `preemptionPolicy`

Support for the k8s `PriorityClass.preemptionPolicy` field (issues #1584, #1032) is deferred until a concrete request appears. The mechanism here is designed to absorb it: the field would resolve to the same per-podgroup trigger-delay value (`Never` = ∞), reusing the prefilter unchanged. The open questions it would reopen — source precedence vs. the label, unbounded-delay fair-share inflation, in-quota starvation of `Never` workloads — are documented in the git history of this design.
