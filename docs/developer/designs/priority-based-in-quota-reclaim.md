# Priority-Based In-Quota Reclaim

This feature will be implemented as alpha for v0.18.

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

In this viewpoint, the desired allocation order is:

1. High-priority queue, in quota
2. Low-priority queue, in quota
3. Regular over quota fairness

### 2.1 Is proper quota split enough?

Under ideal circumstances, if the sum of all the quotas would have been lower then the capacity, to satisfy any on the queues quota we would need to preempt only over-quota resources.  The scenario becomes more relevant when the reclaimer has additional constraints (for example, topology requirements) that block the use of other resources (over-quota or free nodes) in the cluster.


## 3. Reclaim Rules

A candidate victim task is reclaimable via the new path when, for the reclaimer queue and victim queue pair:

```
reclaimerQueue.Priority > victimQueue.Priority
AND
reclaimer's queue stays within its own Deserved quota after taking the resource
AND
victim task is preemptible (already enforced upstream by both actions)

```

## 4. No Reclaim Loops

A reclaim of a `InQuotaQueuePriorityStrategy` will happen only for a reclaimer that fits into the quota of the higher priority queue. This means that if a reclaim will happen, the higher queue will be in quota. Because all of the reclaimees of `InQuotaQueuePriorityStrategy` are from a queue with lower priority. The reclaimees won't be allegeable for a "back" reclaim for either `MaintainFairShareStrategy` or `GuaranteeDeservedQuotaStrategy` because the higher priority queue is in quota, and they won't be allegeable for `InQuotaQueuePriorityStrategy` because they come from a queue with lower priority.

## 5. Hierarchy Scoping

When queues sit in different branches of a hierarchy, whose priority should be compared, we will reuse the current logic of finding the lowest common ancestor, then compare the priority of the two branches one level below it (i.e., compare the child-of-LCA ancestors that contain each queue). The existing reclaim path already needs to compare a reclaimer and a reclaimee that may live in different branches, and already solves this via `Reclaimable.getLeveledQueues` (`reclaimable.go`): it walks both queues' ancestor paths and returns the pair of queues at the point they diverge from their lowest common ancestor (LCA). 

## 6. Queue sorting function changes

If the in-quota queue priority strategy is enabled, we must preserve the existing property - the reclaimer is ordered before its reclaimees. Otherwise, during allocation, some victims may be reallocated before the reclaimer gets a chance to use their resources. In that case, the re-reclaimed resources  will not help the reclaimer or the other victims it forces to move, meaning those earlier victims should not have been preempted in the first place.

The proportion plugin’s current queue order is:
1. Prefer queues not over fair share over queues over fair share.
2. Prefer queues where allocated + candidate job <= deserved over queues where the candidate would go over deserved.
3. Prefer higher queue priority.
4. Penalize queues that would use a resource with zero allocatable share.
5. Prefer lower dominant resource share after adding the pending job or subtracting victims.
6. Prefer lower current dominant resource share.
7. Prefer smaller allocatable share.
8. Tie-break by earlier queue creation time.

As I said before, the desired allocation priority is: high-priority in-quota, low-priority in-quota, regular over quota fairness. 
The suggested change is:

Apply the change only if the `InQuotaQueuePriorityStrategy` is enabled.
Compare if `allocated + job <= deserved` for both queues.
 - If step one both queues are over their quota, `InQuotaQueuePriorityStrategy` is irrelevant and no change is required.
 - If the higher queue is over-quota, `InQuotaQueuePriorityStrategy` is irrelevant and no change is required.
 - If the higher queue is under quota and the lower queue is over-quota, previous step 1 will not prefer the lower queue and previous step 2 will prefer the higher priority queue.
 - If both queues in quota, check which queue has higher priority (previously step 3) before starvation checks (previously steps 1,2)

## 6. Configuration

Follows the existing pattern used for `kValue` / `relcaimerSaturationMultiplier` (`proportion.go`): a new `proportion` plugin argument, single flag controlling both the reclaim strategy (§5) and the phase-1 fairshare calculation change (§4).

- Argument name: `queuePriorityInQuotaReclaim` (bool, default `false`).
- Set per scheduling shard via `SchedulingShardSpec.Plugins["proportion"].Arguments` (`pkg/apis/kai/v1/schedulingshard_types.go`) — no CRD schema change needed, consistent with `docs/developer/designs/scheduler-config-customization.md`.

## 7. Consolidation

No code change. Consolidation (`pkg/scheduler/actions/consolidation/`) doesn't call into the `proportion` reclaim strategies at all — its victim filter only checks `IsPreemptibleJob()` and active-task count, because its scenario validator (`allPodsReallocated`) requires every evicted victim to be reallocated somewhere; it repacks, it doesn't permanently transfer resources across queues. The quota-protection concern this feature relaxes doesn't apply to consolidation the same way, so today's behavior already covers "applicable to consolidation" without change.

## 7. Edge Cases & Risks
- **Non-preemptible**: The weakness of the new approach lays in non-preemptable workloads. Now there is an incentive for the user to fill its quota with Non-preemtible workloads.
