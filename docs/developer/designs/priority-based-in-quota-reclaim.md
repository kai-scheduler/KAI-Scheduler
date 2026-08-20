# Priority-Based In-Quota Reclaim

*Status: Draft*

## 1. Background

Today, reclaim eligibility in the `proportion` plugin (`pkg/scheduler/plugins/proportion/reclaimable/`) is governed by two strategies:

- `MaintainFairShareStrategy` — reclaimable if the victim queue is currently allocated above its allocatable fair share.
- `GuaranteeDeservedQuotaStrategy` — reclaimable if the reclaimer stays within its own deserved quota **and** the victim queue is currently above its deserved quota.

Both require the victim queue to be over-quota in some sense. In-quota (`Allocated <= Deserved`) allocations are never reclaimable — this is documented as the "Quota Protection Guarantee" (`docs/scheduling-deep-dive/README.md`).

Queue `Priority` (`pkg/apis/scheduling/v2/queue_types.go`) exists today but only affects (a) how over-quota surplus is divided among queues (`resource_division.go`) and (b) the iteration order used to pick which queue's job to try first for allocation and last for reclaim victim selection (`queue_order.go`). This design adds a third, flag-gated use: ordering the deserved-quota computation itself.

## 2. Problem Statement

Operators want higher-priority queues to be able to reclaim resources from lower-priority queues even when the victim is within its deserved quota, as long as:

1. The victim's workload is preemptible.
2. The reclaimer stays within *its own* deserved quota after reclaiming (this path never lets a reclaimer go over-quota using in-quota victims).
3. The victim queue's priority is *strictly* lower than the reclaimer's.

This must be opt-in (per scheduling shard), since it weakens the quota protection guarantee for low-priority queues.

These requirements come as a different approach to look at a queue's priority. Until now, a queues quota was stronger then any other parameter. 
This allows the customer to decide if priority is more important to the cluster fairness state rather then quota, which until now, was absolute. 


## 3. Reclaim Rules

A candidate victim task is reclaimable via the new path when, for the (leveled) reclaimer queue and victim queue pair:

```
reclaimerQueue.Priority > victimQueue.Priority
AND
reclaimer's queue stays within its own Deserved quota after taking the resource
AND
victim task is preemptible (already enforced upstream by both actions)

```

## 4. No Reclaim Loops

A reclaim of a `InQuotaQueuePriorityStrategy` will happen only for a reclaimer that fits into the quota of the higher priority queue. This means that if a reclaim will happen, the higher queue will be in quota. Because all of the reclaimees of `InQuotaQueuePriorityStrategy` are from a queue with lower priority. The reclaimees won't be allegeable for a "back" reclaim for either `MaintainFairShareStrategy` or `GuaranteeDeservedQuotaStrategy` because the higher priority queue is in quota, and they won't be allegeable for `InQuotaQueuePriorityStrategy` bevause they come from a queue with lower priority.

## 5. Hierarchy Scoping

When queues sit in different branches of a hierarchy, whose priority should be compared, we will reuse the current logic of finding the lowest common ancestor, then compare the priority of the two branches one level below it (i.e., compare the child-of-LCA ancestors that contain each queue). The existing reclaim path already needs to compare a reclaimer and a reclaimee that may live in different branches, and already solves this via `Reclaimable.getLeveledQueues` (`reclaimable.go`): it walks both queues' ancestor paths and returns the pair of queues at the point they diverge from their lowest common ancestor (LCA). 

### 6. Configuration

Follows the existing pattern used for `kValue` / `relcaimerSaturationMultiplier` (`proportion.go`): a new `proportion` plugin argument, single flag controlling both the reclaim strategy (§5) and the phase-1 fairshare calculation change (§4).

- Argument name: `queuePriorityInQuotaReclaim` (bool, default `false`).
- Parsed in `proportion.go` alongside existing arguments, threaded into both `resource_division.SetResourcesShare` (as `priorityOrdered`, §4) and `reclaimable.New(saturationMultiplier, priorityInQuotaReclaim)` (§5).
- Set per scheduling shard via `SchedulingShardSpec.Plugins["proportion"].Arguments` (`pkg/apis/kai/v1/schedulingshard_types.go`) — no CRD schema change needed, consistent with `docs/developer/designs/scheduler-config-customization.md`.

## 7. Consolidation

No code change. Consolidation (`pkg/scheduler/actions/consolidation/`) doesn't call into the `proportion` reclaim strategies at all — its victim filter only checks `IsPreemptibleJob()` and active-task count, because its scenario validator (`allPodsReallocated`) requires every evicted victim to be reallocated somewhere; it repacks, it doesn't permanently transfer resources across queues. The quota-protection concern this feature relaxes doesn't apply to consolidation the same way, so today's behavior already covers "applicable to consolidation" without change.

## 7. Edge Cases & Risks
- **Non-preemptible**: The weakness of the new approach layes in non-preemptable workloads. Now there is an incentive for the user to fill it's quota with Non-preemtible workloads.

---

1- For queue order/ job order, the 4 stages needs to be reflected - first allocate from high priority queue deserved, the low priority queue deserved, then high priority queue overquota, then the low priority queue overquota.
2- No need for fairshare changes
3- Be more presice in the doc

*End of document*
