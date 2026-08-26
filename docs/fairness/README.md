# Fairness

KAI Scheduler implements hierarchical fair-share scheduling using multi-level queues to distribute cluster resources equitably across users and projects.

> **Prerequisites**: Familiarity with [Scheduling Queues](../queues/README.md) concepts

### Fair Share Simulator - coming soon

## Table of Contents
- [What is fair-share?](#what-is-fair-share)
- [Resource Allocation](#resource-allocation)
- [Fair Share Calculation](#fair-share-algorithm)
- [Reclaim Strategies](#reclaim-resources)
- [Configuration](#configuration)

## What is fair-share?
Fair-share based allocation of resources is an algorithm or algorithms used to determine how to distribute resources between
consumers with the intent of achieving equal or "fair" distribution of resources.

In KAI-scheduler, we aim to achieve priority based equal distribution of free resources, while maintaining a guarantee of bare-minimum allocation for each queue. Idle resouces are reclaimable by KAI-scheduler as part of the fair-share algorithm, increasing resource usage.

### Philosophy
1. Deserved quota for a queue will always be allocted to it
2. Surplus resources will be allocated based on priority

## Resource Allocation

> Resource Allocation is done on each scheduling cycle


### Fair-Share Algorithm

Resource allocation and fair-share calculation is done on each scheduling cycle.

1. **Top-level distribution**: Total available resources are disributed to top-level queues according with respect to **deserved** quota.
    * **Hierarchical division**: Each queue's resources are further distributed among its child queues
    * **Recursive allocation**: The process continues down the queue hierarchy until all levels are allocated
2. **Over-quota share**: if resources remain unallocated after all deserved quotas are distributed, a fair share of the over quota part is calculated based on **priority** and **weight** attributes. 
    * Queues with the same priority will be reclaimed propotionaly to their weight
3. **Job scheduling**: The scheduler schedules jobs from each queue to utilize their allocated resources, aiming to keep actual usage **as close to the fair share as possible**

> Deserved resouces calculation:
```python
remainingResources = totalResources
for q in queues:
    q.fairShare += min(q.deserved, requested)
    remainingResources -= min(q.deserved, requested)
```
> Fair share calculation:
```python
while remainingResources > 0:
    totalWeights = sum(q.OverQuotaWeight for q in queues)
    for q in queues:
        resourcesToAssign = min(remainingResources * q.OverQuotaWeight / totalWeights, q.RemainingRequest)
        q.fairShare += resourcesToAssign
        remainingResources -= resourcesToAssign
```


## Reclaim Resources
Fair share determines queue scheduling priority and reclaim eligibility:

- **Scheduling Priority**: Queues below fair share are prioritized for receiving more resources
- **Reclaim Eligibility**: Queues can be reclaimed if they are allocated more resources than their fair share
- **Saturation Ratio**: `Allocated / FairShare` used for reclaim decisions. Higher ratio == higher probability of reclaim

KAI scheduler uses two main reclaim strategies:
1. **Fair Share Reclaim** - Workloads from queues with resources below their fair share can evict workloads from queues that have exceeded their fair share.
2. **Quota Reclaim** - Workloads from queues under their quota can evict workloads from queues that have exceeded their quota.

In both strategies, the scheduler ensures that the **relative ordering is preserved**: a queue that had the lowest Saturation ratio in its level before reclamation will still have the lowest ratio afterwards. Likewise, a queue that was below its quota will remain below its quota.
The scheduler will prioritize the first strategy.
> **Note:** because of the hierarchical nature & priority/weight parametes of job queues in KAI, there are scenarios that a queue will have lower resources allocated than its siblings, yet it'll receive no additional resources via reclaim.

Both strategies only ever reclaim over-quota resources — a queue using only its guaranteed quota is normally never a reclaim victim. Enabling the `queuePriorityInQuotaReclaim` plugin argument (see [Configuration](#configuration)) adds a third, opt-in strategy: a queue can reclaim in-quota resources from another queue with strictly lower `Priority`, as long as the reclaimer itself stays within its own deserved quota. See [Scheduling Deep Dive](../scheduling-deep-dive/README.md#the-quota-protection-guarantee) for details.

## Configuration

### Reclaim Sensitivity
Adjust reclaim aggressiveness using `reclaimerUtilizationMultiplier`:

```yaml
pluginArguments:
  proportion:
    reclaimerUtilizationMultiplier: "1.2"  # 20% more conservative
```

| Value | Behavior |
|-------|----------|
| `1.0` | Standard comparison (default) |
| `> 1.0` | More conservative reclaim |
| `< 1.0` | Not allowed (prevents infinite cycles) |

### Priority-Based In-Quota Reclaim

Opt in to letting a strictly higher priority queue reclaim in-quota resources from a strictly lower priority queue, using `queuePriorityInQuotaReclaim`. This is a `proportion` plugin argument, set on the `SchedulingShard` this applies to (see [Scheduler Config Customization](../operator/scheduler-config-customization.md)):

```yaml
# SchedulingShard
spec:
  plugins:
    proportion:
      arguments:
        queuePriorityInQuotaReclaim: "true"
```

```yaml
# Helm values — templated into the default SchedulingShard's spec.plugins
scheduler:
  plugins:
    proportion:
      arguments:
        queuePriorityInQuotaReclaim: "true"
```

| Value | Behavior |
|-------|----------|
| `false` (default) | Only over-quota resources are ever reclaimed — the quota protection guarantee holds for every queue |
| `true` | A queue can additionally reclaim in-quota resources from any strictly lower priority queue, provided the reclaimer stays within its own deserved quota |

This is alpha functionality; enable it per scheduling shard only when queue `Priority` should outrank quota protection for lower-priority queues.

## See Also

- [Scheduling Deep Dive](../scheduling-deep-dive/README.md) — Visual guide to how fair-share allocation, reclaim, and preemption interact
