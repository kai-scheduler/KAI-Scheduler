---
name: kai-explain-queue-fairshare
description: Use when someone asks what a KAI-Scheduler queue is entitled to right now and why - deserved quota vs fair share of surplus capacity, whether the queue is borrowing or lending, why a peer queue gets more GPUs, or whether what it holds is exposed to reclaim. Reads the Queue CRD and the scheduler's own resource-division result.
license: MIT
compatibility: Requires kubectl. Fair share is read from the scheduler's metrics endpoint or its log.
metadata:
  author: KAI Scheduler maintainers
  version: "1.0"
---

# KAI: what is my queue entitled to, and why?

A queue's standing is three numbers, not one:

- **deserved** - `spec.resources.<r>.quota`. Guaranteed, handed out before any surplus.
- **fair share** - deserved plus this queue's slice of the capacity left over, capped by
  `spec.resources.<r>.limit`. Recomputed from scratch every scheduling cycle.
- **allocated** - what the queue holds right now (`status.allocated`).

Allocation is gated on fair share, so `allocated` against `fairShare` is the borrow/lend position
and `fairShare` against `deserved` is the size of the surplus slice. Walk the steps in order.

## 1. Rule out the queues that have no answer - stop if either holds

```bash
kubectl get queue <queue> -o json | jq '{parent: .spec.parentQueue, children: .status.childQueues}'
```

- **`childQueues` is non-empty -> not a leaf queue.** Jobs are never scheduled from it. The
  scheduler skips any job whose queue is not a leaf, silently, with no verdict written to the
  PodGroup, so a workload labelled `kai.scheduler/queue: <this queue>` sits Pending forever with
  nothing to read. Say that, and point at the children - they are where an allocation can live.
  A non-leaf queue does still get a fair share, but it is a *pool* its children divide (step 5),
  not something it can hold.
- **queue does not exist** -> workloads naming it fail with `QueueDoesNotExist`. Nothing to explain
  until it and its parent are created.

## 2. Read the entitlement from the Queue CRD

```bash
kubectl get queue <queue> -o json \
  | jq '{priority: .spec.priority, parent: .spec.parentQueue, resources: .spec.resources,
         allocated: .status.allocated, allocatedNP: .status.allocatedNonPreemptible,
         requested: .status.requested}'
```

Queues are cluster-scoped. Spec and status use different units, do not compare them raw: spec GPU
is in fractions (`0.7` is 70% of one GPU), CPU in millicpus, memory in megabytes.

- `quota` - deserved. Unset is `0`, which means *no guarantee*, not "unlimited". `-1` is unlimited.
- `overQuotaWeight` - the queue's weight when surplus is divided. Only bites above quota.
- `limit` - hard cap. Fair share never exceeds it whatever the weights say. `-1` is unlimited.
- `priority` - surplus is divided by priority tier, highest first. Unset is `100`.

## 3. Read the fair share - it is computed, and it lives somewhere else

The Queue CRD has no `fairShare` field, and no single endpoint carries all three numbers: deserved
and allocated are published by the **queue controller** (`queue_deserved_gpus`,
`queue_allocated_gpus`, `queue_quota_cpu_cores`, `queue_allocated_memory_bytes`), fair share only by
the **scheduler** (`queue_fair_share_gpu` in devices, `queue_fair_share_cpu_cores`,
`queue_fair_share_memory_gb`). Both default to the `kai` metric namespace, configurable per
component with `--metrics-namespace`. The queue controller's numbers are copied straight off the
CRD, so step 2 already has them - the one thing worth scraping for is fair share:

```bash
# 8080 is the scheduler's default --listen-address
kubectl -n <kai scheduler namespace> port-forward deploy/<scheduler> 8080:8080 &
curl -s localhost:8080/metrics | grep '^kai_queue_fair_share'
```

Otherwise the scheduler log at default verbosity (`-v=3`) carries the whole division, which is
strictly more than any endpoint exposes:

```
Resource division result for queue <$QUEUE>: Queue Priority: <N>,
GPU: deserved: <..>, requested: <..>, maxAllowed: <..>, allocated: <..>, historicalUsage: <..>, fairShare: <..>
```

Re-logged every cycle (default 1s), so a recent tail is current. `maxAllowed` is the effective
`limit` and `historicalUsage` is the input step 5 needs; neither is on the CRD.

Do not read `queue_gpu_usage` as "what the queue is using now". It is the *historical* usage from
the usage database, its units depend on the UsageDB configuration, and it is absent when usage
tracking is off. Current allocation is `status.allocated`.

## 4. State the standing

Per resource, compare:

- `allocated < deserved` -> **lending**. Under its guarantee; the difference is being borrowed by
  other queues and comes back when this queue asks for it (via reclaim).
- `deserved <= allocated <= fairShare` -> **borrowing within its share**. Legitimate, and the
  normal steady state for a queue with quota 0.
- `allocated > fairShare` -> **over fair share**. Only reachable when fair share shrank after the
  allocation, i.e. a peer queue submitted. Exposed to reclaim now, go to step 6.
- `requested > fairShare` -> the queue is *asking* for more than it is entitled to and the excess
  will not be allocated. This is the usual answer to "there are free GPUs, why is my job pending".

## 5. Explain the why - the surplus slice, in order

Fair share is built in this order each cycle. Report the step that binds, not all of them:

1. **Deserved first.** Every queue takes `min(quota, requested)` before anyone sees surplus.
2. **Then surplus, by priority tier.** A higher `priority` tier is satisfied in full before a lower
   tier gets anything at all, whatever the lower tier's weights are. Check tiers before blaming
   weights.
3. **Within a tier, by weight, penalised by history.** A queue's slice is proportional to
   `max(0, nWeight + k * (nWeight - nUsage))`, where `nWeight` is its `overQuotaWeight` normalised
   across the tier's still-unsatisfied queues and `nUsage` its normalised historical usage. Two
   queues on equal weight do **not** get equal surplus if one has been consuming more - the heavier
   user's slice shrinks. `k` is the proportion plugin's `kValue` argument, default `1.0`; at `k=0`
   history is ignored and it is pure weight. `historicalUsage` comes from the log line in step 3.
4. **Capped by limit.** `maxAllowed` truncates whatever the previous steps produced.
5. **Top-down.** A parent's fair share is the pool its children divide, so a child can be starved by
   an ancestor's cap however the siblings are weighted. Walk up `spec.parentQueue` when the local
   numbers do not explain the result.

For a peer comparison, name the single step that differs:

> Equal deserved (16 GPUs each), but team-b's over-quota weight is 4 against your 1, so it takes
> 80% of the free GPUs.

or, when the tiers differ:

> team-b is priority 200 and you are 100, so it is satisfied in full before any surplus reaches you.

## 6. Flag reclaim exposure

Everything the queue holds **above its deserved quota is borrowed, and reclaimable**. When another
queue submits and drops under its own guarantee, reclaim evicts the borrower.

- `allocated - deserved` is the exposure. Report it whenever it is positive, even when the queue is
  comfortably inside its fair share - fair share moves the moment a peer submits.
- `status.allocatedNonPreemptible` is **not** exposed. Non-preemptible workloads are gated to
  deserved at admission (`NonPreemptibleOverQuota`) and are never reclaimed. Subtract it for the
  genuinely at-risk amount.
- `spec.reclaimMinRuntime` buys a grace period before a job in this queue can be reclaimed. Unset
  means it can go on the next cycle.
- Queue `priority` is consulted last for reclaim, so a high-priority queue borrows more safely. Pod
  `priorityClassName` does not help here - it only orders preemption inside the queue itself.

## When not to use

- One named workload is Pending and the user wants its blocker -> `kai-pending`.
- Changing the numbers rather than explaining them -> Queue spec knobs are an admin action,
  `docs/queues/README.md`; the model is in `docs/scheduling-deep-dive/`.

## RBAC

Queues are cluster-scoped: reading them needs `get`/`list` on `queues.scheduling.run.ai`. Fair share
needs read access in the scheduler's namespace (its metrics port or its log). Lacking either, say
which number you could not read rather than deriving it. The division depends on cluster capacity
and on every peer queue, so it cannot be recomputed from one queue's CRD.
