# Priority-Based Fair-Share Distribution

*Status: Draft*

## Table of Contents
1. Background
2. Problem Statement
3. Goals / Non-Goals
4. Proposal
   1. User-Visible Changes
   2. Algorithm Details
   3. Configuration
5. Migration & Compatibility
6. Examples
7. Testing Strategy
8. Risks & Mitigations

---

## 1. Background

Kai-Scheduler relies on the **Proportion** plugin (`pkg/scheduler/plugins/proportion`) to divide cluster capacity between queues.  
The current algorithm first allocates each queue its *Deserved* quota and then distributes the remaining capacity (the *over-quota* bucket) **proportionally to `OverQuotaWeight`**.  
Queues with `OverQuotaWeight = 0` never receive additional capacity beyond their *Deserved* quota, even when they have pending demand.

For preemptive workloads we often want **high-priority queues to reclaim resources from lower-priority queues regardless of `OverQuotaWeight`.**  

## 2. Problem Statement

*Desired behaviour* (when the feature is enabled):

1. Queues are ordered by descending **`QueuePriority`** (same value already used by the scheduler).
2. Starting from the highest-priority queue, allocate up to its *requested* resources **even if this means lower-priority queues receive less than their `Deserved` share**.
3. Once all high-priority queues are satisfied *or* cluster capacity is exhausted, allocate any remaining capacity to lower-priority queues using the existing `OverQuotaWeight` logic.

This behaviour must be opt-in to maintain backward compatibility.

## 3. Goals / Non-Goals

### Goals
* Provide a simple switch to enable the priority-first fair-share distribution.
* Minimise changes to existing reclaim code paths.
* Preserve all other fairness semantics (hierarchical fairness, non-preemptible quotas, GPU sharing, etc.).

### Non-Goals
* Redesign the reclaim algorithm itself.  
  Only the **calculation of `FairShare`** is updated; reclaim validation continues to rely on `FairShare` as before.
* Change default behaviour – the scheduler must remain backward compatible until users enable the flag.

## 4. Proposal

### 4.1 User-Visible Changes

Add a **boolean flag** (default `false`) that can be specified in the configuration ConfigMap:

* **Scheduler ConfigMap** – add optional field `priorityBasedFairShare` under the Proportion plugin configuration block (mirrors the CLI flag).

### 4.2 Algorithm Details (when flag = true)

The scheduler processes **priority buckets** one after another, from highest to lowest.  
Within a bucket the original proportion semantics remain intact; the only addition is a third pass that gives pending demand of weight-0 queues a chance to receive capacity if resources are still available.

```
Input:
  Q  – set of active queues with attributes {priority, deserved, weight, requested}
  C  – cluster total capacity
  PB – Q grouped by QueuePriority (desc)

Remaining = C
for each priorityBucket in PB {            # Highest priority first
  if Remaining == 0:
      break

  ----------------------------- Phase 1: Deserved Quota -----------------------------
  for q in priorityBucket {
      grant = min(q.deserved, Remaining, q.request)
      FairShare(q) = grant
      Remaining    -= grant
  }
  if Remaining == 0:
      continue

  ----------------------------- Phase 2: Weighted Over-Quota -------------------------
  apply existing over-quota algorithm on queues in bucket that still
  have unmet demand (weights as configured). This consumes from Remaining.
  If Remaining == 0: continue

  ----------------------------- Phase 3: Weight-0 Compensation -----------------------
  # All queues that still request resources are treated as weight = 1
  equally share Remaining according to original proportion rules.
  Remaining may reach 0 here.
}
```

Key points:
* **Inter-bucket priority ordering** – higher-priority buckets are fully processed before lower-priority buckets see any capacity.
* **Intra-bucket fairness** – the two-phase (deserved → weighted over-quota) logic is preserved.
* **Weight-0 Handling** – the new Phase 3 ensures queues without `OverQuotaWeight` can still obtain resources if the bucket still has room.
* Hierarchical fairness, non-preemptible quotas and other caps are applied after `FairShare` is computed exactly as today.

The rest of the reclaim path (`reclaimable.Reclaimable` etc.) remains unchanged – it simply references the new `FairShare` values.

A reference implementation lives in `pkg/scheduler/plugins/proportion/capacity_policy/priority_fair_share.go` (to be created).

### 4.3 Configuration Wire-Up

```
# pseudo-code
ServerOption {
  PriorityBasedFairShare bool `json:"priorityBasedFairShare,omitempty"`
}
```

* CLI flag sets `ServerOption.PriorityBasedFairShare`.
* `pkg/scheduler/plugins/proportion.New()` receives the option via its config struct.
* At plugin initialisation choose between `defaultPolicy` and `priorityBasedPolicy`.

## 5. Migration & Compatibility

* **Default = disabled** – current behaviour unchanged.
* Feature can be toggled at runtime by restarting the scheduler with the CLI flag or by updating the SchedulerConfig.
* No CRD version bumps required because the new field is optional and omitempty.

## 6. Examples

### Example 1 – Capacity Exhausted at Top Priority

Cluster capacity: **100 CPU**

| Queue | Priority | Deserved | OverQuotaWeight | Requested | Notes |
|-------|----------|----------|-----------------|-----------|-------|
| A     | 100      | 20       | 0               | 70        | weight-0, high demand |
| B     | 100      | 30       | 2               | 40        | over-quota weight 2 |
| C     | 50       | 30       | 1               | 50        | lower priority |

Step-by-step allocation:

1. **Priority bucket 100** (Remaining = 100)
   * Phase 1 – Deserved: A +20, B +30 ⇒ Remaining = 50
   * Phase 2 – Weighted: only B (weight 2) eligible ⇒ B gets min(10, 50) = 10 ⇒ Remaining = 40
   * Phase 3 – Weight-0 compensation: A still needs 50 ⇒ A gets min(50, 40) = 40 ⇒ Remaining = 0
2. **Priority bucket 50** is skipped – no remaining capacity.

**Final FairShare**: A = 60, B = 40, C = 0.

### Example 2 – Spare Capacity Reaches Lower Priority

Cluster capacity: **120 CPU**

| Queue | Priority | Deserved | OverQuotaWeight | Requested |
|-------|----------|----------|-----------------|-----------|
| A     | 100      | 20       | 0               | 70        |
| B     | 100      | 30       | 2               | 40        |
| C     | 50       | 30       | 1               | 50        |

Allocation:

1. **Priority 100** (Remaining = 120)
   * Phase 1: A +20, B +30 ⇒ Remaining = 70
   * Phase 2: B eligible (weight 2) ⇒ B +10 ⇒ Remaining = 60
   * Phase 3: A eligible ⇒ A +50 ⇒ Remaining = 10
2. **Priority 50** C gets the remaining 10 which doesn't cover its 30 deserved GPUs.

**FairShare**: A = 70, B = 40, C = 10

### Example 3 – Capacity Spills to Next Priority

Cluster capacity: **200 CPU**

| Queue | Priority | Deserved | OverQuotaWeight | Requested |
|-------|----------|----------|-----------------|-----------|
| A     | 100      | 20       | 0               | 70        |
| B     | 100      | 30       | 2               | 50        |
| C     | 50       | 30       | 1               | 90        |

1. **Priority 100** (Remaining = 200)
   * Phase 1: A +20, B +30 ⇒ Remaining = 150
   * Phase 2: B +20 ⇒ Remaining = 130
   * Phase 3: A still needs 50 ⇒ A +50 ⇒ Remaining = 80
2. **Priority 50** (Remaining = 80)
   * Phase 1: C +30 ⇒ Remaining = 50
   * Phase 2: C weight 1 ⇒ C +50 ⇒ Remaining = 0

**FairShare**: A = 70, B = 50, C = 80 (Requests 90).

These scenarios illustrate:
* Weight-0 queues at a high priority can reclaim capacity before lower-priority queues see any resources.
* Lower-priority queues still receive capacity when higher-priority demand is satisfied.

## 7. Testing Strategy

1. **Unit Tests** –
   * New tests exercising the allocation algorithm with various combinations of priority, weight, request and quota.
2. **Integration Tests** –
   * Existing proportion plugin scenarios cloned with the flag enabled to ensure no regressions.
3. **E2E** –
   * Deploy multi-queue workloads verifying that high-priority queues obtain capacity even when their `OverQuotaWeight` is 0.

## 8. Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Starvation of low-priority queues | Low-priority queues may never reach `Deserved` quota | Leave feature opt-in; cluster admins must size `Deserved` appropriately |
| Increased scheduling churn | More aggressive resource reclamation | Leave feature opt-in; monitor with existing metrics |

---

*End of document* 
