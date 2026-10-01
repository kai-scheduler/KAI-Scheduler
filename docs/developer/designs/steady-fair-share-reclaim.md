# Steady Fair-Share Reclaim

This feature is implemented as alpha, behind a `proportion` plugin argument.

## 1. Background

The `proportion` plugin divides capacity between queues every session (`resource_division.go`): each queue first gets its deserved quota, then what remains is shared by over-quota weight. The result, the queue's **fair share**, is capped by demand: a queue never gets more than it requests, and what it does not request goes to the queues that do. While the cluster has free capacity, every queue's fair share therefore equals its demand.

Reclaim (`pkg/scheduler/plugins/proportion/reclaimable/`) uses that fair share:

- A reclaimer may reclaim only if its queue's allocation plus the job fits the queue's fair share (`CanReclaimResources`).
- `MaintainFairShareStrategy` takes victims from a queue that is above its fair share.
- `GuaranteeDeservedQuotaStrategy` lets a reclaimer within its quota take from a queue above its quota.

## 2. Problem Statement

A cluster can have free GPUs that a pending job cannot use. Examples: free GPUs scattered across nodes (no node has the 8 GPUs a pod asks for), free GPUs outside the job's required topology domain, or nodes its affinity excludes. The scheduler counts them in the division, so every queue's fair share still equals its demand and no queue is above it. Reclaim finds no victims, and the pending job waits, possibly for days, while other queues hold far more than their weighted share of the cluster.

Consolidation can make room by moving jobs, but not always: the jobs on the fragmented nodes may have nowhere to move to, multi-node and topology-constrained jobs are hard to repack, `maxNumberConsolidationPreemptees` bounds it, and some clusters disable consolidation because moved jobs restart.

Quotas do not help clusters that rely on over-quota weights alone. A common setup gives the workload queues quota 0 and over-quota weights, so that a queue with unlimited quota can reclaim everything when needed (`GuaranteeDeservedQuotaStrategy` lets it take from any queue above its quota of 0). Giving those queues quotas would protect their in-quota jobs from that queue as well.

Example: 300 GPUs, three queues A, B, C with quota 0 and over-quota weight 1. B is idle. A runs 180 GPUs. C runs 90 and has a pending job of two 8-GPU pods; the 30 free GPUs are fragmented. Fair shares are A 180 and C 106 (their demands), so no queue is above its fair share and C's job waits. By weight, C is entitled to 100 and A to 100: A holds 80 more than its weighted share.

## 3. Steady Fair Share

The **steady fair share** of a queue is its fair share if every queue with an over-quota weight requested the whole cluster. It is computed with the same division code as the fair share, run a second time on copies of the queues with a different demand:

- A leaf queue with an over-quota weight and a finite quota requests the whole cluster, and so do its ancestors.
- Every other queue keeps its real demand. A queue without a weight therefore keeps what it uses up to its quota, and its unused quota goes to the weighted queues. A queue with unlimited quota keeps its demand; requesting the whole cluster would give it the whole cluster.

It does not depend on how much the weighted queues currently request, which makes it a stable reference for fairness between them. This is the distinction Hadoop YARN's fair scheduler draws between a queue's *steady* fair share (by weight, over all queues) and its *instantaneous* fair share (over active queues only). Like the fair share, it is computed per resource, level by level, honours queue priority, limits and the `kValue` usage weighting, and is capped at the queue's limit.

In the example, the steady fair shares are A 100, B 100 and C 100.

## 4. Reclaim Rules

A new path, added to the existing ones, lets a queue below its steady fair share reclaim from queues above theirs. It applies to jobs that request GPUs (whole, fractional or MIG) and compares queues in GPUs. Let *keep* be what a queue may hold under this path: the larger of its quota and its steady fair share, capped at its limit (as `GetAllocatableShare` is for the fair share).

1. **Reclaimer.** The job requests GPUs, and its queue holds fewer GPUs than its *keep*.
2. **Victims.** Victims come from queues that hold more GPUs than their *keep*. Within a queue they are taken in the usual victim order.
3. **Floor.** A victim queue is never taken below its *keep*. A victim whose first eviction batch (a whole gang, or an elastic job's surplus pods) alone is larger than its queue's surplus is left out by the victim filter, so the search moves on to the queue's smaller jobs, even ones of higher priority. The total is checked again when the scenario is validated.
4. **Overshoot.** A job larger than its queue's gap may take its queue above its *keep* when it remains less saturated than every queue it reclaims from. This is the existing saturation rule (`relcaimerSaturationMultiplier`), computed with steady fair shares: `(allocated + job) / steady × multiplier < remaining / steady`.

In the example, C (90 of 100) may reclaim A's lowest-priority jobs as long as A keeps at least 100. C ends at 106, above its 100: allowed, because C's 1.06 stays below A's 1.64. C cannot reclaim again on this path once it is at or above its 100.

**GPUs only.** A steady fair share is a fixed split rather than capped by demand, so comparing every resource would let a plentiful one decide: a queue above its split of CPU, which a GPU-heavy queue often is, would lose GPU jobs, a queue above its split of memory could not reclaim GPUs, and a CPU-only job could evict GPU jobs. CPU-only jobs keep today's behaviour.

## 5. Relationship with the Existing Strategies

The path is additive. A reclaim scenario is accepted when the existing path accepts it, exactly as today, or when the new path accepts it:

- **The existing path is unchanged and only as reachable as before.** It applies when the existing entry condition holds: allocation plus the job within the fair share. A reclaimer admitted only by rule 1 gets neither the existing strategies nor their victims, which it would not have had before.
- **The new path has its own entry condition (rule 1).** Its victims, floor and saturation check use steady fair shares.
- **The floor binds only the new path.** A victim accepted by a quota-based strategy (`GuaranteeDeservedQuotaStrategy`, `InQuotaQueuePriorityStrategy`) is not subject to it, so a queue with unlimited quota can still clear the cluster.

Replacing the fair share with the steady fair share in the existing path was considered and rejected. With three equal-weight queues and one idle, the two busy queues today balance the idle queue's share between them (150 and 150 of 300). Under replacement both would be above their steady fair share (100), and neither could reclaim from the other.

## 6. No Reclaim Loops

A queue loses GPUs on this path only down to its *keep*, so it never ends below its steady fair share, and rule 1 never lets it reclaim back on this path. A reclaimer's overshoot is bounded by the saturation rule: after any reclaim that takes the reclaimer above its steady fair share, it is less saturated than every queue it reclaimed from, so none of them can reclaim from it on this path. Every reclaim accepted on this path lowers `Σ allocated² / steady` over the queues involved, so such reclaims cannot cycle. On the existing path, the reclaimer's overshoot is reclaimable only when it also puts the reclaimer above its fair share, exactly as today.

What is reclaimed must also reach the reclaimer rather than the victim queue's re-created pods, see §8.

## 7. Hierarchy Scoping

The steady fair share is divided level by level, like the fair share. The path compares a reclaimer and a victim at the children of their lowest common ancestor (`getLeveledQueues`), as the existing strategies do. Rule 1 is checked both on the reclaimer's own queue and on its ancestor at that level, and rules 2 and 3 on the victim's ancestor at that level. The saturation check walks the reclaimer's ancestors as it does today.

## 8. Queue Order

Freed GPUs must go to the reclaimer, not back to the victim queue's re-created pods, or the eviction was wasted and the two queues could trade jobs (the same concern as in the priority-based in-quota reclaim design). The existing order does not guarantee it: by share of the cluster, a victim queue with a smaller weight can come first. When the feature is enabled and both queues' candidate jobs request GPUs, the queue order first puts a queue whose candidate job may reclaim on this path (rule 1) before a queue whose candidate job may not, then applies the existing order.

After a reclaim the reclaimer's job is pending again in the next session, its queue still below its steady fair share, while the victim queue is at or above its own. A queue within its quota with its candidate GPU job is below its *keep*, so the step never reorders such queues relative to each other, and the priority-based in-quota ordering keeps working. Queues whose candidate job requests no GPUs are not reordered at all.

## 9. Configuration

A `proportion` plugin argument, following `queuePriorityInQuotaReclaim`:

- Argument name: `steadyFairShareReclaim` (bool, default `false`).
- Set per scheduling shard via `SchedulingShardSpec.Plugins["proportion"].Arguments`; no CRD change. Plugin arguments replace the plugin's default arguments, which include the `kValue` derived from `spec.kValue`.
- The saturation rule's `relcaimerSaturationMultiplier` applies to both paths.

The steady fair share is computed only when the argument is set, and logged per queue at verbosity 3 next to the division result.

## 10. Consolidation

No change. Consolidation does not use the `proportion` reclaim strategies, and it runs before reclaim: fragmentation it can repack is fixed by moving jobs, without evictions. This path covers what consolidation cannot move and clusters that disable it.

## 11. Edge Cases & Risks

- **Idle weighted queues count.** Their weight is part of every steady fair share, so a queue running on an idle queue's share is reclaimable by any queue below its steady fair share whose job cannot be placed. This is the intent, and it adds evictions while some queues are idle. Counting only queues with work would bring back the problem: it counts the fragmented free GPUs again (a queue holding 140 of 300, with another at 90 and 70 GPUs fragmented, would be entitled to 150 and untouchable).
- **Queue priority.** As in the fair share, higher-priority queues take what is above the quotas first. With a weighted queue of higher priority at the same level, a lower-priority queue's steady fair share is its quota, so it is reclaimable down to its quota by higher-priority queues, as its fair share already allows when they have demand.
- **Surplus smaller than every job.** If no victim batch fits a queue's surplus, nothing is reclaimed from it; the floor wins over the pending job.
- **Search coverage.** The reclaim search tries victims in order and node subsets of them, not every combination, so it can miss an unusual packing of smaller victims, as for the existing strategies.
- **Gangs and elastic jobs.** Victims are evicted in the same batches as today (whole gangs, elastic surplus first); the filter checks the first batch and the scenario validation the total.
- **Non-preemptible jobs** are never victims. A queue above its steady fair share only through them gives nothing.
- **Unlimited quota.** A queue with unlimited quota is never above what it may keep, so it is never a victim on this path; it may reclaim on it, but `GuaranteeDeservedQuotaStrategy` already lets it take more.
- **Unchanged protections.** Min-runtime, preemption delay and the non-preemptible quota check apply unchanged.
- **Cost.** One more division per session, only when enabled. The victim filter computes a victim's first eviction batch only when the reclaimer is on this path and its other checks pass.

## 12. Alternatives

- **Quotas for the weighted queues.** These would protect in-quota jobs from a queue with unlimited quota too (see §2).
- **Replacing the fair share in reclaim.** This loses balancing of idle shares between busy queues (see §5).
- **A fair share over active queues only.** It still counts the fragmented free GPUs (see §11).
- **Comparing every resource.** CPU or memory would decide GPU reclaim (see §4).
- **Balancing: letting any queue reclaim from a more saturated one,** above its steady fair share or not. Busy queues would even out, but the entry condition would depend on every other queue, and the queue order would need to sort by saturation, which changes allocation order. It can follow as its own option.
- **Several arguments for these choices.** One alpha argument keeps the configuration and test matrix small; splitting it later is compatible, removing arguments is not.
- **Making the division aware of unusable capacity** (fragmentation, topology). This needs a per-job notion of usable capacity, which the division does not have.
