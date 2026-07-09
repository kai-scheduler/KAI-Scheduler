# Aggressor-Side Preemption Control: `preemptionPolicy` and Preemption Delay

*Status: Draft — decision points for design review*

Related issues: [#1584](https://github.com/kai-scheduler/KAI-Scheduler/issues/1584) (design), [#1032](https://github.com/kai-scheduler/KAI-Scheduler/issues/1032) (original ask), [#1832](https://github.com/kai-scheduler/KAI-Scheduler/issues/1832) (preemption delay)

## Background

KAI ignores the native `PriorityClass.preemptionPolicy` field — priority class resolution reads only `Value` (`getPodGroupPriority`, `pkg/scheduler/cache/cluster_info/cluster_info.go`). Workloads whose class specifies `Never` trigger preemption anyway.

```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: high-priority-nonpreempting
value: 125
preemptionPolicy: Never   # PreemptLowerPriority (default) | Never
description: "Schedules ahead of lower priorities, but never triggers eviction."
```

Victim-side control (can a running workload be evicted) already exists: `PodGroup.Spec.Preemptibility` and the `kai.scheduler/preemptibility` label ([priority-preemptibility-separation](../priority-preemptibility-separation/README.md)). The missing half is aggressor-side: may a *pending* workload trigger evictions.

## Motivation

1. **Never-evicting workloads** — workloads of any priority that run only in free capacity and never kill running work; e.g. inference that should schedule first but not disrupt training. Priority and eviction-triggering are coupled today; this is not expressible.
2. **Autoscaler race (#1832)** — evictions fire even when a new node would arrive within minutes. Delaying eviction-triggering lets scale-up win and saves restarted work.

## One Mechanism

#1584 and #1832 are the same knob with different duration — *the earliest time a pending podgroup may trigger evictions*:

| Setting | Meaning |
|---|---|
| `PreemptLowerPriority` (k8s default) | immediately (delay 0) |
| Preemption delay (#1832) | after pending N seconds |
| `Never` | never (delay ∞) |

Both resolve to one value on `PodGroupInfo` and one prefilter in the eviction-triggering actions. When both are set, the explicit duration annotation overrides the class policy (D5).

## Semantic Model

| Concern | Mechanism |
|---|---|
| May this workload evict others to get scheduled? (aggressor) | `preemptionPolicy` / delay — this design |
| May this workload be evicted once running? (victim) | `Preemptibility` — existing |

Orthogonal: `Never` does **not** imply non-preemptible, and vice versa.

## Design Summary

- **Resolution**: once per PodGroup, `Spec.PriorityClassName` → `PriorityClass.PreemptionPolicy`. Missing class or field → `PreemptLowerPriority`.
- **Enforcement**: skip the podgroup as eviction trigger in the in-scope actions, before victim-scenario generation. Surface the skip through the podgroup's existing unschedulable-explanation status (updated on change, not re-emitted every cycle). No other scheduler change: allocation, queue ordering, and fair-share division are untouched.
- **Scope**: preempt, reclaim, and consolidation. Not stale-gang eviction (no aggressor).
- **Accounting**: pending demand is counted in queue `Request` as usual, `Never` or not. This lets a persistently pending `Never` job inflate its queue's fair share — an accepted distortion, detailed in [Fair-Share Inflation](#fair-share-inflation-accepted-distortion) below.
- **`Never` consequences (documented, inherent)**: may starve even in-quota — it waits for natural churn and cannot force its queue's deserved quota back from over-quota users in other queues. Choosing a finite delay instead of `Never` bounds this. Over-quota allocation into free capacity works normally (its counted demand earns surplus like any other).

## Fair-Share Inflation (Accepted Distortion)

Fair-share division caps each queue's share by its aggregated demand (`Request`), and pending pods count toward it unconditionally. A `Never` job that pends for a long time (it cannot force placement) keeps its demand in that division while never enforcing it. Under contention — when aggregate demand exceeds capacity, so surplus division is zero-sum — this shifts fair share from queues with enforceable demand to the `Never` job's queue:

- **Other queues' shares shrink**, which hits them twice: their reclaim budget is smaller (`CanReclaimResources` requires `Allocated + required ≤ FairShare`), and they are reclaimable deeper — `MaintainFairShareStrategy` allows shaving a victim queue down to its fair share, which is now lower.
- **The `Never` job's queue-mates** pass reclaim eligibility more easily, on the strength of demand that will never fight.
- **The `Never` job's queue is shielded**: its running over-quota pods stop being reclaim victims once its allocation is within the inflated fair share.

Concrete example — 12 GPUs, three queues with deserved 0 and equal over-quota weights (pure surplus division). Queue A: idle, pending `Never` job of 6. Queue B: running 8. Queue C: running 4, pending normal job of 2. Counting the `Never` demand, the surplus divides 4/4/4 — C's pending job fails reclaim eligibility (`Allocated 4 + required 2 > FairShare 4`) and pends indefinitely, although B runs 8 GPUs against a share of 4. Excluding it, shares are 0/6/6 — C's job is eligible (4+2 ≤ 6) and reclaims 2 GPUs from B (shaved to 6, exactly its share). A's ballast flips C's legitimate reclaim from allowed to blocked.

**Why this is accepted**: the identical inflation is already achievable today with any unschedulable pending pod — `updateQueuesResourceUsageForPendingJob` counts all pending demand with no schedulability check. `Never` adds a member to an existing exposure class, not a new class; the effect exists only under contention and is bounded by the `Never` job's requested size.

## Decision Points

### Proposed decisions — confirm or object

| # | Decision | Proposed |
|---|---|---|
| D1 | Relationship to #1832 | One mechanism, delay 0 / N / ∞; designed and reviewed together |
| D2 | **Reclaim in scope from day one** | Yes. Most evictions in multi-tenant clusters are reclaim; a preempt-only phase is near-valueless. With D4 (count demand normally), reclaim scope adds no accounting work — the change stays a prefilter |
| D3 | Consolidation in scope | Yes — a `Never` workload should not disrupt others in any way, and consolidation-evicted victims still restart and lose state even though they are re-placed with their resources in the same cycle. (The narrower "don't take others' resources" reading would exclude consolidation — noted for discussion) |
| D4 | Accounting | Count all pending demand normally, including `Never`, accepting the fair-share inflation described in [Fair-Share Inflation](#fair-share-inflation-accepted-distortion) — the identical exploit already exists via unschedulable pending pods, so excluding only `Never` demand buys little and costs a change in the scheduler's most correctness-sensitive accounting |
| D5 | Source precedence | The explicit preemption-duration annotation overrides the class `preemptionPolicy` — it is the wider API (expresses any duration, including 0 and ∞) and the more specific one (per-workload vs. per-class). Alternative considered: most-restrictive wins |
| D6 | Rollout safety | No global disable flag — per-class opt-in suffices: behavior changes only for workloads whose priority class explicitly sets `Never` (all shipped classes default to `PreemptLowerPriority`) |
