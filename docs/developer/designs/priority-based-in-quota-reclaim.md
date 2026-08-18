# Priority-Based In-Quota Reclaim

*Status: Draft*

## Table of Contents
1. Background
2. Problem Statement
3. Goals / Non-Goals
4. Fairshare Model Validation
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

Today, reclaim eligibility in the `proportion` plugin
(`pkg/scheduler/plugins/proportion/reclaimable/`) is governed by two strategies:

- `MaintainFairShareStrategy` — reclaimable if the victim queue is currently
  allocated above its allocatable fair share.
- `GuaranteeDeservedQuotaStrategy` — reclaimable if the reclaimer stays within
  its own deserved quota **and** the victim queue is currently above its
  deserved quota.

Both require the victim queue to be over-quota in some sense. In-quota
(`Allocated <= Deserved`) allocations are never reclaimable — this is
documented as the "Quota Protection Guarantee"
(`docs/scheduling-deep-dive/README.md`).

Queue `Priority` (`pkg/apis/scheduling/v2/queue_types.go`) exists today but
only affects (a) how over-quota surplus is divided among queues
(`resource_division.go`) and (b) the iteration order used to pick which
queue's job to try first for allocation and last for reclaim victim
selection (`queue_order.go`). It has no role in reclaim *eligibility*.

## 2. Problem Statement

Operators want higher-priority queues to be able to reclaim resources from
lower-priority queues even when the victim is within its deserved quota,
as long as:

1. The victim's workload is preemptible.
2. The reclaimer stays within *its own* deserved quota after reclaiming
   (this path never lets a reclaimer go over-quota using in-quota victims).
3. The victim queue's priority is *strictly* lower than the reclaimer's.

The existing behavior — an in-quota queue reclaiming from an over-quota
queue regardless of priority — already exists via
`GuaranteeDeservedQuotaStrategy` and is unaffected.

This must be opt-in (per scheduling shard), since it weakens the quota
protection guarantee for low-priority queues.

## 3. Goals / Non-Goals

### Goals
- Let a queue reclaim in-quota, preemptible allocations from strictly
  lower-priority queues, bounded by the reclaimer's own deserved quota.
- Apply to the `reclaim` action. Apply to `consolidation` only in the sense
  described in §6 (no code change there).
- Opt-in per scheduling shard, default off.
- No change to how `Deserved` / `FairShare` are computed.

### Non-Goals
- No protected floor for the victim (e.g., "never drain a queue below X% of
  its deserved quota"). Full drain of a lower-priority queue's in-quota
  allocation is accepted as intentional — priority governs access even
  within quota. A floor could be added later if operators need it.
- No change to consolidation's victim-selection logic.
- No change to `MaintainFairShareStrategy` / `GuaranteeDeservedQuotaStrategy`
  — this is purely additive.

## 4. Fairshare Model Validation

Validated: the current fairshare model requires **no changes**.

- `Deserved` and `FairShare` per queue are computed exactly as today
  (`resource_division.go`); this proposal doesn't touch that code path.
- Queue `Priority` is already carried on `rs.QueueAttributes` (set from
  `queue.Priority` in `proportion.go`), and `Deserved` is already exposed
  via `QueueAttributes.GetDeservedShare()` /
  `AllocatedPlusResourcesLessEqualDeserved()`. Both pieces of state the new
  rule needs already exist.
- The change is confined to reclaim *eligibility*: a third
  `ReclaimStrategy` that compares `Priority` and checks
  `ReclaimerFitsDeservedQuota` (already used by
  `GuaranteeDeservedQuotaStrategy`) — it does not consult or require the
  victim to be over its deserved quota.

This confirms the request in the feature description: no changes to how
fairshare/deserved are calculated, only to which strategy makes a victim
eligible.

Note: `docs/developer/designs/priority-based-fair-share.md` is a separate,
older draft that takes a materially different approach — it changes how
`FairShare` itself is computed (processing queues in priority buckets, so a
lower-priority queue's *deserved* allocation can shrink before it's even
allocated). That proposal is more invasive and changes the fairshare model.
This design deliberately avoids that: `Deserved`/`FairShare` stay as
computed today, and only the reclaim eligibility predicate changes. See §9.

## 5. Proposal

### 5.1 Rule

A candidate victim task is reclaimable via the new path when, for the
(leveled) reclaimer queue and victim queue pair:

```
reclaimerQueue.Priority > victimQueue.Priority   (strict)
AND
reclaimer's queue stays within its own Deserved quota after taking the resource
AND
victim task is preemptible (already enforced upstream by both actions)
```

No requirement that the victim queue be over its own deserved quota — that
is exactly the behavior change from today.

### 5.2 Hierarchy Scoping

Open question raised during design: when queues sit in different branches
of a hierarchy, whose priority should be compared — the leaf queue's own
priority, or something scoped to a common ancestor?

Resolved by reuse, not by adding new logic. The existing reclaim path
already needs to compare a reclaimer and a reclaimee that may live in
different branches, and already solves this via
`Reclaimable.getLeveledQueues` (`reclaimable.go`): it walks both queues'
ancestor paths and returns the pair of queues at the point they diverge
from their lowest common ancestor (LCA). `FilterVictim` and
`reclaimResourcesFromReclaimees` already operate on this leveled pair for
quota comparisons — this is effectively "option 2" (descend from the LCA
until one side's priority differs) from the two options considered:

1. Compare only the leaf queues' own priority, ignoring hierarchy.
2. Find the LCA, then compare the priority of the two branches one level
   below it (i.e., compare the child-of-LCA ancestors that contain each
   queue).

Recommendation: reuse `getLeveledQueues` and compare `Priority` on the
already-leveled pair (option 2). Rationale:
- It is what the rest of the reclaim path already does for quota/fairshare
  comparisons (`FitsReclaimStrategy` is called with leveled queues), so
  behavior stays consistent — a job wouldn't be eligible under the
  fairshare check but ineligible under the priority check due to differing
  hierarchy semantics.
- It matches how over-quota division already treats priority as
  sibling-scoped (`divideOverQuotaResource` buckets by priority among
  queues sharing a parent) — priority is a sibling-relative concept
  elsewhere in the codebase, and LCA-leveling generalizes that to non-sibling,
  same-branch-depth comparisons.
- Option 1 (raw leaf priority) would let a deeply nested low-priority queue
  be reclaimed from by an unrelated, differently-nested queue whose leaf
  priority happens to be higher, even if their common ancestor branch is
  lower priority overall — inconsistent with how priority buckets are
  scoped for over-quota division today.

No new code is needed for this — `getLeveledQueues` already returns the
right pair; the new strategy just reads `.Priority` off it.

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

`FitsReclaimStrategy` gains a parameter for whether the new strategy is
active (threaded from `Reclaimable.priorityInQuotaReclaim`, set at
construction — see §5.4) and includes it in the strategy set additively:

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

This is an additive OR with the existing two strategies — today's
over-quota reclaim behavior is unchanged; the new path only adds
eligibility, never removes it.

`filter_victims.go`'s coarse pool filter (`FilterVictim`) must also be
updated, since `canBeDeservedQuotaReclaimCandidate` currently requires the
victim to have at least one over-deserved resource, which would wrongly
exclude in-quota victims from the candidate pool before the strategy check
ever runs:

```go
if !strategies.ReclaimerFitsDeservedQuota(reclaimer.RequiredResources, reclaimer.VectorMap, reclaimerQueue) {
    return strategies.FitsMaintainFairShare(reclaimeeQueue, reclaimeeQueue.GetAllocatedShare())
}
if r.priorityInQuotaReclaim && reclaimerQueue.Priority > reclaimeeQueue.Priority {
    return true
}
return canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue)
```

`reclaimingQueuesRemainWithinBoundaries` (the cross-sibling saturation
guard) is left untouched and applies uniformly after any strategy
validates a victim, same as today — victims reclaimed via the new priority
path are still subject to it.

### 5.4 Configuration

Follows the existing pattern used for `kValue` /
`relcaimerSaturationMultiplier` (`proportion.go`): a new `proportion`
plugin argument.

- Argument name: `queuePriorityInQuotaReclaim` (bool, default `false`).
- Parsed in `proportion.go` alongside existing arguments, passed into
  `reclaimable.New(saturationMultiplier, priorityInQuotaReclaim)`.
- Set per scheduling shard via
  `SchedulingShardSpec.Plugins["proportion"].Arguments`
  (`pkg/apis/kai/v1/schedulingshard_types.go`) — no CRD schema change
  needed, consistent with `docs/developer/designs/scheduler-config-customization.md`.

## 6. Consolidation

No code change. Consolidation (`pkg/scheduler/actions/consolidation/`)
doesn't call into the `proportion` reclaim strategies at all — its victim
filter only checks `IsPreemptibleJob()` and active-task count, because its
scenario validator (`allPodsReallocated`) requires every evicted victim to
be reallocated somewhere; it repacks, it doesn't permanently transfer
resources across queues. The quota-protection concern this feature relaxes
doesn't apply to consolidation the same way, so today's behavior already
covers "applicable to consolidation" without change.

## 7. Edge Cases & Risks

- **Equal priority**: no reclaim via this path (`>` is strict). Existing
  strategies still apply unchanged.
- **Full drain of a low-priority queue**: since there's no protected floor
  (Non-Goal), a lower-priority queue's entire in-quota allocation can be
  reclaimed if enough higher-priority demand exists across multiple
  reclaim rounds. This is the intended effect of the feature (priority
  governs access even within quota) but should be called out to operators
  enabling the flag.
- **Saturation guard doesn't protect the victim**: `reclaimingQueuesRemainWithinBoundaries`
  compares the *reclaiming* queue's saturation against its own siblings —
  it doesn't rate-limit how much a single victim queue can be drained. No
  change proposed here; flagged as a known limitation, consistent with
  today's guard scope.
- **Non-preemptible reclaimers**: `CanReclaimResources`'s existing
  additional check (`AllocatedNonPreemptible+Requested <= Deserved`) is
  unaffected — it runs before strategy selection.
- **Interaction with `GuaranteeDeservedQuotaStrategy`**: unchanged; if a
  lower-priority queue is over-quota, a higher-priority in-quota reclaimer
  can still reclaim it via the existing strategy regardless of this flag.

## 8. Testing Strategy

- Unit tests in `strategies/strategies_test.go` for `QueuePriorityStrategy`
  (priority equal/lower/higher, reclaimer over/under its own deserved).
- Unit tests in `filter_victims_test.go` for the new coarse-filter branch.
- Integration tests (`actions/reclaim`) with the flag on/off: verify a
  higher-priority in-quota job can evict a lower-priority in-quota
  preemptible job only when enabled, and that reclaim stops once the
  reclaimer would exceed its own deserved quota.
- Regression: existing reclaim/proportion test suites must pass unchanged
  with the flag left at its default (`false`).

## 9. Alternatives Considered

- **Change `FairShare` computation instead** (per-priority-bucket
  allocation, as in `priority-based-fair-share.md`): rejected for this
  feature. It changes the fairshare model itself (goes against the
  requirement to leave it untouched), affects allocation ordering and
  every consumer of `FairShare`/`Deserved`, and is a much larger surface
  than an additive reclaim strategy. That draft addresses a related but
  distinct problem (weight-0 queues never getting over-quota share); it
  isn't a substitute for or superseded by this design.
- **Priority as a full replacement for the two existing strategies**:
  rejected — additive OR keeps the change low-risk and fully backward
  compatible when disabled.

---

*End of document*
