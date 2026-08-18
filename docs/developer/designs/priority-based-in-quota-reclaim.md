# Priority-Based In-Quota Reclaim

*Status: Draft*

## Table of Contents
1. Background
2. Problem Statement
3. Goals / Non-Goals
4. Fairshare Model Calculation Changes
5. Proposal
   1. Rule
   2. Hierarchy Scoping
   3. Algorithm / Code Changes
   4. Configuration
6. Consolidation
7. Edge Cases & Risks
8. Testing Strategy
9. Alternatives Considered

---

## 1. Background

Today, reclaim eligibility in the `proportion` plugin (`pkg/scheduler/plugins/proportion/reclaimable/`) is governed by two strategies:

- `MaintainFairShareStrategy` — reclaimable if the victim queue is currently allocated above its allocatable fair share.
- `GuaranteeDeservedQuotaStrategy` — reclaimable if the reclaimer stays within its own deserved quota **and** the victim queue is currently above its deserved quota.

Both require the victim queue to be over-quota in some sense. In-quota (`Allocated <= Deserved`) allocations are never reclaimable — this is documented as the "Quota Protection Guarantee" (`docs/scheduling-deep-dive/README.md`).

Queue `Priority` (`pkg/apis/scheduling/v2/queue_types.go`) exists today but only affects (a) how over-quota surplus is divided among queues (`resource_division.go`) and (b) the iteration order used to pick which queue's job to try first for allocation and last for reclaim victim selection (`queue_order.go`). This design adds a third, flag-gated use: ordering the deserved-quota computation itself (§4).

## 2. Problem Statement

Operators want higher-priority queues to be able to reclaim resources from lower-priority queues even when the victim is within its deserved quota, as long as:

1. The victim's workload is preemptible.
2. The reclaimer stays within *its own* deserved quota after reclaiming (this path never lets a reclaimer go over-quota using in-quota victims).
3. The victim queue's priority is *strictly* lower than the reclaimer's.

The existing behavior — an in-quota queue reclaiming from an over-quota queue regardless of priority — already exists via `GuaranteeDeservedQuotaStrategy` and is unaffected.

This must be opt-in (per scheduling shard), since it weakens the quota protection guarantee for low-priority queues.

## 3. Goals / Non-Goals

### Goals
- Let a queue reclaim in-quota, preemptible allocations from strictly lower-priority queues, bounded by the reclaimer's own deserved quota.
- Apply to the `reclaim` action. Apply to `consolidation` only in the sense described in §6 (no code change there).
- Opt-in per scheduling shard, default off.
- Make deserved-quota computation (`FairShare`'s phase 1, §4) priority-ordered and capacity-capped, matching the over-quota phase, so the accounting reflects real contention when the flag is on.
- The new reclaim strategy's own eligibility check is based on the raw `Deserved` field, not `FairShare` — see §4 for why.

### Non-Goals
- No protected floor for the victim (e.g., "never drain a queue below X% of its deserved quota"). Full drain of a lower-priority queue's in-quota allocation is accepted as intentional — priority governs access even within quota. A floor could be added later if operators need it.
- No change to consolidation's victim-selection logic.
- No change to `MaintainFairShareStrategy` / `GuaranteeDeservedQuotaStrategy` — this is purely additive.
- No intra-tier fairness weighting for a deserved-quota shortfall (see §7) — over-engineering beyond what was asked.

## 4. Fairshare Model Calculation Changes

`Deserved` is a static, per-queue configured input (`QueueResourceShare.SetQuotaResources`, from the Queue spec) — never computed, never touched by `resource_division.go`. `FairShare` is the derived, computed value, built in two phases:

- **Phase 1 (`setDeservedResource`)** grants every queue `min(Deserved, Requested)` into its `FairShare` accumulator. Today this is a single unordered pass over *all* queues with **no cap against remaining capacity** — every queue gets its full `min(Deserved, Requested)` regardless of `Priority` or of what any other queue receives.
- **Phase 2 (`divideOverQuotaResource`)** distributes whatever capacity is left (`remainingAmount`) among queues bucketed by `Priority`, highest bucket first, each bucket drawing from the same shared pool before the next lower bucket gets a turn (`getQueuesByPriority`).

**Validated gap**: because phase 1 has no capacity cap, if `sum(Deserved)` across queues exceeds real cluster capacity — the exact situation that motivates this feature, since it's what lets a lower-priority queue occupy capacity a higher-priority queue also deserves — phase 1 still credits *every* queue its full deserved amount into `FairShare`, as if there were no contention. Phase 2 already solves this correctly for the over-quota band (priority-ordered, capacity-capped); phase 1 does not do the equivalent for the deserved band. The fairshare model does need a change: phase 1 should be made priority-ordered and capacity-capped, the same way phase 2 already is, gated behind the feature flag so default (disabled) behavior is unchanged:

```go
func setDeservedResource(
    totalResourceAmount float64, queues map[common_info.QueueID]*rs.QueueAttributes,
    resource rs.ResourceName, priorityOrdered bool,
) (remainingAmount float64) {
    remainingAmount = totalResourceAmount
    if !priorityOrdered {
        for _, queue := range queues {
            remainingAmount = grantDeserved(queue, resource, totalResourceAmount, remainingAmount, false)
        }
        return remainingAmount
    }
    queuesByPriority, priorities := getQueuesByPriority(queues) // reuse the existing bucketing helper
    for _, priority := range priorities {
        for _, queue := range queuesByPriority[priority] {
            remainingAmount = grantDeserved(queue, resource, totalResourceAmount, remainingAmount, true)
        }
    }
    return remainingAmount
}

// grantDeserved adds min(Deserved, Requested) to FairShare, capped to what's left when priorityOrdered.
func grantDeserved(queue *rs.QueueAttributes, resource rs.ResourceName, totalResourceAmount, remainingAmount float64, priorityOrdered bool) float64 {
    resourceShare := queue.ResourceShare(resource)
    deserved := resourceShare.Deserved
    if deserved == commonconstants.UnlimitedResourceQuantity {
        deserved = totalResourceAmount
    }
    amount := math.Min(deserved, resourceShare.GetRequestableShare())
    if priorityOrdered {
        amount = math.Max(0, math.Min(amount, remainingAmount))
    }
    queue.AddResourceShare(resource, amount)
    return remainingAmount - amount
}
```

`priorityOrdered` is threaded from the same feature flag as the rest of this design (§5.4). `getQueuesByPriority` already exists in the package for phase 2 and is reused as-is.

The new reclaim strategy's own eligibility check is based on `Deserved`, not `FairShare` (§5.1/§5.3): the strategy operates pairwise, on the specific reclaimer/victim pair, and doesn't need to re-derive cluster-wide contention — that's already the pre-gate's job once phase 1 is ordered. The existing `GuaranteeDeservedQuotaStrategy` follows the same pattern (`ReclaimerFitsDeservedQuota` reads the raw `Deserved` field, not `FairShare`) — the new strategy is consistent with established precedent, not a new convention.

## 5. Proposal

### 5.1 Rule

A candidate victim task is reclaimable via the new path when, for the (leveled) reclaimer queue and victim queue pair:

```
reclaimerQueue.Priority > victimQueue.Priority   (strict)
AND
reclaimer's queue stays within its own Deserved quota after taking the resource
AND
victim task is preemptible (already enforced upstream by both actions)
```

No requirement that the victim queue be over its own deserved quota — that is exactly the behavior change from today. The check is against `Deserved`, not `FairShare` — see §4 for why.

### 5.2 Hierarchy Scoping

Open question raised during design: when queues sit in different branches of a hierarchy, whose priority should be compared — the leaf queue's own priority, or something scoped to a common ancestor?

Resolved by reuse, not by adding new logic. The existing reclaim path already needs to compare a reclaimer and a reclaimee that may live in different branches, and already solves this via `Reclaimable.getLeveledQueues` (`reclaimable.go`): it walks both queues' ancestor paths and returns the pair of queues at the point they diverge from their lowest common ancestor (LCA). `FilterVictim` and `reclaimResourcesFromReclaimees` already operate on this leveled pair for quota comparisons — this is effectively "option 2" (descend from the LCA until one side's priority differs) from the two options considered:

1. Compare only the leaf queues' own priority, ignoring hierarchy.
2. Find the LCA, then compare the priority of the two branches one level below it (i.e., compare the child-of-LCA ancestors that contain each queue).

Recommendation: reuse `getLeveledQueues` and compare `Priority` on the already-leveled pair (option 2). Rationale:
- It is what the rest of the reclaim path already does for quota/fairshare comparisons (`FitsReclaimStrategy` is called with leveled queues), so behavior stays consistent — a job wouldn't be eligible under the fairshare check but ineligible under the priority check due to differing hierarchy semantics.
- It matches how over-quota division already treats priority as sibling-scoped (`divideOverQuotaResource` buckets by priority among queues sharing a parent) — priority is a sibling-relative concept elsewhere in the codebase, and LCA-leveling generalizes that to non-sibling, same-branch-depth comparisons.
- Option 1 (raw leaf priority) would let a deeply nested low-priority queue be reclaimed from by an unrelated, differently-nested queue whose leaf priority happens to be higher, even if their common ancestor branch is lower priority overall — inconsistent with how priority buckets are scoped for over-quota division today.

No new code is needed for this — `getLeveledQueues` already returns the right pair; the new strategy just reads `.Priority` off it.

### 5.3 Algorithm / Code Changes

`pkg/scheduler/plugins/proportion/reclaimable/strategies/strategies.go`:

```go
type QueuePriorityStrategy struct{}

func (qps *QueuePriorityStrategy) Reclaimable(
    reclaimerResources resource_info.ResourceVector,
    vectorMap *resource_info.ResourceVectorMap,
    reclaimerQueue *rs.QueueAttributes,
    reclaimeeQueue *rs.QueueAttributes,
    _ rs.ResourceQuantities,
) bool {
    if reclaimerQueue.Priority <= reclaimeeQueue.Priority {
        return false
    }
    return ReclaimerFitsDeservedQuota(reclaimerResources, vectorMap, reclaimerQueue)
}
```

`FitsReclaimStrategy` gains a parameter for whether the new strategy is active (threaded from `Reclaimable.priorityInQuotaReclaim`, set at construction — see §5.4) and includes it in the strategy set additively:

```go
func FitsReclaimStrategy(..., enableQueuePriorityReclaim bool) bool {
    activeStrategies := baseStrategies // MaintainFairShareStrategy, GuaranteeDeservedQuotaStrategy
    if enableQueuePriorityReclaim {
        activeStrategies = append(activeStrategies, &QueuePriorityStrategy{})
    }
    for _, s := range activeStrategies {
        if s.Reclaimable(...) {
            return true
        }
    }
    return false
}
```

This is an additive OR with the existing two strategies — today's over-quota reclaim behavior is unchanged; the new path only adds eligibility, never removes it.

`filter_victims.go`'s coarse pool filter (`FilterVictim`) must also be updated, since `canBeDeservedQuotaReclaimCandidate` currently requires the victim to have at least one over-deserved resource, which would wrongly exclude in-quota victims from the candidate pool before the strategy check ever runs:

```go
if !strategies.ReclaimerFitsDeservedQuota(reclaimer.RequiredResources, reclaimer.VectorMap, reclaimerQueue) {
    return strategies.FitsMaintainFairShare(reclaimeeQueue, reclaimeeQueue.GetAllocatedShare())
}
if r.priorityInQuotaReclaim && reclaimerQueue.Priority > reclaimeeQueue.Priority {
    return true
}
return canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue)
```

`reclaimingQueuesRemainWithinBoundaries` (the cross-sibling saturation guard) is left untouched and applies uniformly after any strategy validates a victim, same as today — victims reclaimed via the new priority path are still subject to it.

### 5.4 Configuration

Follows the existing pattern used for `kValue` / `relcaimerSaturationMultiplier` (`proportion.go`): a new `proportion` plugin argument, single flag controlling both the reclaim strategy (§5) and the phase-1 fairshare calculation change (§4).

- Argument name: `queuePriorityInQuotaReclaim` (bool, default `false`).
- Parsed in `proportion.go` alongside existing arguments, threaded into both `resource_division.SetResourcesShare` (as `priorityOrdered`, §4) and `reclaimable.New(saturationMultiplier, priorityInQuotaReclaim)` (§5).
- Set per scheduling shard via `SchedulingShardSpec.Plugins["proportion"].Arguments` (`pkg/apis/kai/v1/schedulingshard_types.go`) — no CRD schema change needed, consistent with `docs/developer/designs/scheduler-config-customization.md`.

## 6. Consolidation

No code change. Consolidation (`pkg/scheduler/actions/consolidation/`) doesn't call into the `proportion` reclaim strategies at all — its victim filter only checks `IsPreemptibleJob()` and active-task count, because its scenario validator (`allPodsReallocated`) requires every evicted victim to be reallocated somewhere; it repacks, it doesn't permanently transfer resources across queues. The quota-protection concern this feature relaxes doesn't apply to consolidation the same way, so today's behavior already covers "applicable to consolidation" without change.

## 7. Edge Cases & Risks

- **Equal priority**: no reclaim via this path (`>` is strict). Existing strategies still apply unchanged.
- **Full drain of a low-priority queue**: since there's no protected floor (Non-Goal), a lower-priority queue's entire in-quota allocation can be reclaimed if enough higher-priority demand exists across multiple reclaim rounds. This is the intended effect of the feature (priority governs access even within quota) but should be called out to operators enabling the flag.
- **No intra-tier fairness for phase 1's shortfall**: unlike phase 2 (`divideUpToFairShare`, weighted by `OverQuotaWeight` and usage), the phase-1 change in §4 grants queues within the same priority bucket their deserved amount in a simple iteration order, not weighted. Two same-priority queues competing for the same scarce deserved capacity aren't proportionally balanced by this change. Acceptable for this feature (Non-Goal) but worth flagging for anyone reusing this code path.
- **Saturation guard doesn't protect the victim**: `reclaimingQueuesRemainWithinBoundaries` compares the *reclaiming* queue's saturation against its own siblings — it doesn't rate-limit how much a single victim queue can be drained. No change proposed here; flagged as a known limitation, consistent with today's guard scope.
- **Non-preemptible reclaimers**: `CanReclaimResources`'s existing additional check (`AllocatedNonPreemptible+Requested <= Deserved`) is unaffected — §4 establishes the existing `FairShare` gate needs no change.
- **Interaction with `GuaranteeDeservedQuotaStrategy`**: unchanged; if a lower-priority queue is over-quota, a higher-priority in-quota reclaimer can still reclaim it via the existing strategy regardless of this flag.

## 8. Testing Strategy

- Unit tests in `resource_division_test.go` for the ordered/capped phase 1 (§4): capacity-constrained multi-tier scenarios, flag on vs off, unlimited-deserved edge case, and confirming `FairShare` never exceeds `Deserved` post-change.
- Unit tests in `reclaimable_test.go` confirming `CanReclaimResources` needs no change: a job-sized request within the ordered `FairShare` ceiling passes, one that exceeds it (because a strictly-higher-priority queue's own deserved claim consumes the shared pool first) correctly fails.
- Unit tests in `strategies/strategies_test.go` for `QueuePriorityStrategy` (priority equal/lower/higher, reclaimer over/under its own deserved).
- Unit tests in `filter_victims_test.go` for the new coarse-filter branch.
- Integration tests (`actions/reclaim`) with the flag on/off: verify a higher-priority in-quota job can evict a lower-priority in-quota preemptible job only when enabled, and that reclaim stops once the reclaimer would exceed its own deserved quota.
- Regression: existing reclaim/proportion test suites must pass unchanged with the flag left at its default (`false`).

## 9. Alternatives Considered

- **Change `FairShare` computation instead of adding a strategy, and rely only on the existing `MaintainFairShareStrategy`**: once phase 1 is priority-ordered (§4), a lower-priority victim's `FairShare` can already drop below its actual allocation purely from the accounting change, which would make `MaintainFairShareStrategy` trigger without any new strategy at all. Rejected as the sole mechanism: it bounds the reclaimer only by "under `FairShare`," which includes over-quota entitlement — an already over-its-own-deserved-but-under-`FairShare` reclaimer could exploit it to take in-quota victims, violating requirement 2/3 in §2. The explicit `Deserved`-based `QueuePriorityStrategy` is still required to bound the reclaimer correctly.
- **Full three-phase priority-bucket `FairShare` redesign** (`priority-based-fair-share.md`): rejected for this feature — larger surface, changes weight-0 over-quota compensation semantics unrelated to this problem. §4's phase-1 change is deliberately narrower.
- **Priority as a full replacement for the two existing strategies**: rejected — additive OR keeps the change low-risk and fully backward compatible when disabled.

---

*End of document*
