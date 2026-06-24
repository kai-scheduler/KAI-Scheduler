# Semi-Preemptible Mode

## Overview

In v0.10 we separated Priority and Preemption to allow users to control the two parameters independently, where Preemption has 2 modes (values) - **preemptible** and **non-preemptible**.

We want to add a new 3rd mode, named **semi-preemptible**, where the podgroup is non-preemptible up to its **minimum required shape** — `minMember` pods at each leaf PodSet and `minSubGroup` child subgroups at each intermediate node — and anything beyond that minimum is "elastic" and preemptible. Elasticity therefore applies at **every level of the subgroup tree**, not just to pods.

## Use Cases

A workload with `minReplicas` such as Inference and Elastic Distributed Training can request to be non-preemptible up to its `minReplicas`, with any pods above that count being preemptible. This allows running a critical workload with some assured resources and some on-demand, availability-based resources.

## Quota Requirements

The "core" pods (up to `minMember` per leaf PodSet) must be in-quota when allocated. Any "extra" pods can be allocated over-quota. All pods must respect the Limit setting for the job's queue.

From this:
- A semi-preemptible podgroup where the total pod count equals `minMember` == non-preemptible podgroup
- A semi-preemptible podgroup where `minMember` is 0 == preemptible podgroup

### Quota Scale-Down

If a queue's deserved quota is reduced below the running **core** (non-preemptible) allocation of a semi-preemptible job, the queue becomes persistently over-quota with no automatic mitigation. Reclaim evicts the job's elastic pods first; the core pods remain (protected by the existing `minAvailable` / `GetNumActiveAllocatedTasks()` eviction guard) and the queue stays over-quota until the job completes or scales down on its own.

This is **accepted behavior** — it is identical to how a fully `non-preemptible` job already behaves when its queue is scaled down today. No special mitigation is introduced for semi-preemptible jobs.

Future options considered and deferred:
- **Conditional non-preemptibility:** make core pods reclaimable once `Deserved < AllocatedNotPreemptible`. Requires a new reclaim path and breaks the "core pods are always safe" guarantee.
- **Queue quota-floor webhook:** extend the existing Queue validating webhook (`pkg/admission/webhook/queuehooks/queue_validator.go`) to reject lowering quota below `Status.AllocatedNonPreemptible`. A guardrail only — eventually-consistent (validates the queue-controller status projection, not the scheduler's live accounting) and introduces GitOps friction.

## Subgroups and Multi-Level Trees

### Scope: semi-elasticity applies at every level of the tree

The core/elastic split is **not** limited to pods. It is defined by the minimum requirement of each node in the subgroup tree:

- A **leaf PodSet** with `minMember = m` keeps `m` pods core (non-preemptible); any pods beyond `m` are elastic.
- An **intermediate SubGroupSet** with `minSubGroup = k` keeps its `k` most-prioritized child subgroups core; any additional scheduled child subgroups are elastic and reclaimed as a whole.

Subgroups remain **atomic** as a scheduling unit: an elastic subgroup is evicted in its entirety (gang), never partially. "Semi-elasticity at the subgroup level" means *which* subgroups are protected, not that a subgroup is itself split.

This matches what the scheduler already does. The allocator's gang phase (`collectTasksFromSubGroupSet` in `allocation_info.go`) schedules the `minSubGroup` highest-priority children first and then bursts extra children opportunistically; eviction (`eviction_info.go`) protects exactly the `minSubGroup` core children (`GetMinMembersToSatisfy()`) and reclaims surplus subgroups whole. As today, an elastic (extra) subgroup is only deployed if its pods can be gang-scheduled — otherwise it simply stays unsatisfied.

### Inheritance

Subgroups inherit the preemption mode from the root PodGroup. If the PodGroup is **semi-preemptible**, the whole tree is **semi-preemptible**, and each node's minimum (`minMember` or `minSubGroup`) defines its own core/elastic boundary.

### Non-preemptible resource count = the minimal satisfying set

The non-preemptible (core) resources of a semi-preemptible job are the resources of the tree's **minimal satisfying set**, computed recursively from the root:

- at each SubGroupSet, descend into the `minSubGroup` most-prioritized children (all of them if `minSubGroup` is unset);
- at each leaf PodSet, take `minMember` pods × the per-pod request.

This is **the same set the allocator computes in its gang phase**, so quota and scheduling agree on what "core" means. It replaces the earlier leaf-only definition (`Σ minMember × request` over all leaf PodSets), which over-counted core resources whenever an intermediate `minSubGroup` made some leaf subgroups elastic.

Degenerate cases follow naturally:
- a leaf where total pods `== minMember`, or a node where scheduled children `== minSubGroup`, is fully non-preemptible at that level;
- the lower a node's `minSubGroup` (or a leaf's `minMember`) relative to what is scheduled, the wider its elastic tier;
- `minMember == 0` / no minimum ⇒ fully preemptible at that node.

## Immutability Constraint

A validation webhook must **prevent increases** to `minMember` and `minSubGroups` on a semi-preemptible PodGroup after creation. This applies to the root PodGroup spec and to all SubGroup entries within it.

**Rationale:** once a semi-preemptible job is running, some of its shape may be over-quota (the elastic pods *and* the elastic subgroups). Increasing `minMember` would reclassify over-quota elastic pods as core; increasing `minSubGroup` would reclassify whole over-quota elastic subgroups as core. Either silently grows the minimal satisfying set and violates quota invariants without a rescheduling cycle — which is why both fields are immutable upward.

Decreasing these fields is allowed — it can only widen the elastic tier (at the pod or subgroup level, respectively).

## Interaction with Segmented Subgroups

The [segmented subgroups](../segmented-subgroups/README.md) feature auto-builds a specific tree shape: a workload's replicas are split into `N` fixed-size **segments**, each emitted as a leaf PodSet with `minAvailable = segmentSize`, nested under a parent SubGroupSet that may carry a `minSubGroup`. Segmentation forces this shape so each segment lands in its own topology domain (e.g. a rack).

Subgroup-level semi-elasticity is **what makes semi-preemptible meaningful for segmented workloads**:

- Each segment is fully gang (`minAvailable == segmentSize`), so there is **no pod-level elastic tier inside a segment**. Under a pod-only model every pod of every segment would be core, and a segmented semi-preemptible job would collapse to plain non-preemptible.
- With the tree-level model, the parent's `minSubGroup` defines how many **segments** are core; extra segments are elastic. A job with `minSubGroup: 2` over 4 segments keeps 2 segments core (in-quota, non-preemptible) and lets the other 2 burst as elastic, over-quota, reclaimed-first.

Because elastic eviction at the subgroup level drops a **whole** segment (a segment is a gang PodSet with no internal surplus), reclaim never tears apart a segment — the forced topology shape is preserved. Segmentation and semi-preemptible therefore compose cleanly: segmentation decides the shape, semi-preemptible decides which parts of that shape are guaranteed.

## Simulation Considerations

Victim selection considers only the **surplus** of each node: the "extra" (`n - minMember`) pods at a leaf PodSet, and the extra (`scheduled - minSubGroup`) child subgroups at an intermediate node (evicted whole). This is applied independently per node; no cross-subgroup victim selection is needed. This approach may miss some solutions when checking all orderings, but the added complexity is not justified for the MVP.

## Implementation Notes

- **Allocation & eviction — already tree-aware.** `allocation_info.go` (`collectTasksFromSubGroupSet`) and `eviction_info.go` (`hasElasticSurplusInSubGroupSet`, `GetMinMembersToSatisfy()`) already gate core vs. elastic at every level of the tree. Admitting semi-preemptible jobs into the victim pools is enough for reclaim/preempt to evict only the surplus (elastic pods and surplus subgroups); no change to allocation or eviction logic is needed.
- **Over-quota checks**: the minimal satisfying set (see above) must be in-quota; anything beyond it may be over-quota.
- **Quota accounting — follow-up gap (impl branch, PR #1713).** The current quota accounting is **leaf-only**: `pkg/scheduler/plugins/proportion/proportion.go` (`isCoreTaskForSemiPreemptible`, `updateQueuesCurrentResourceUsage`) and `pkg/scheduler/plugins/proportion/capacity_policy/capacity_policy.go` (`filterCoreTasksToAllocate`, `isTaskCoreForSemiPreemptibleJob`) classify a pod as core purely by `count ≤ minMember` within its PodSet and ignore `minSubGroup`. This over-counts `AllocatedNotPreemptible` whenever an intermediate `minSubGroup` makes some leaf subgroups elastic (e.g. a `minSubGroup: 2` job over 4 segments counts all 4 segments as core while eviction protects only 2). These must be reworked to use the tree's minimal satisfying set, so quota matches the allocation/eviction semantics.
- **Correctness item to verify during impl**: ensure preempt/reclaim only ever take the elastic-phase victims for semi-preemptible jobs and never fall back to the phase-3 full eviction in `collectTasksToEvictFromSubGroupSet`, so core subgroups/pods are never offered as victims.
- **Solver simulation**: represent the elastic surplus of a semi-preemptible job (extra pods and extra subgroups) as a fully-preemptible representative job.

## Future Work: `minNonPreemptible` field

This design uses `minMember` as the non-preemptible threshold. A future `minNonPreemptible` field (pod-level only, no subgroup analog) would decouple the scheduling minimum from the non-preemptible threshold — allowing e.g. `minMember=4, minNonPreemptible=2` (needs 4 pods to start, but only 2 are non-preemptible).

**Work required:**
1. New API field: `minNonPreemptible *int32` on PodGroupSpec and SubGroup
2. Validation: `minNonPreemptible ≤ minMember`
3. Quota accounting decoupled from `minMember`
4. Webhook: `minNonPreemptible` is also immutable post-creation on semi-preemptible PodGroups
5. Solver/simulation: "core" count = `minNonPreemptible`, not `minMember`
6. Status/queue reporting updated

**Key complexity introduced:** a new middle pod tier — "required for scheduling but elastic for preemption" — between core and extra-elastic. Today pod ordering has two tiers; this adds a third. Explicit ordering or labeling is needed to identify which specific pods fall into each tier.
