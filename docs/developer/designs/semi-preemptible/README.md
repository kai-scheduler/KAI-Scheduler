# Semi-Preemptible Mode

## Overview

In v0.10 we separated Priority and Preemption to allow users to control the two parameters independently, where Preemption has 2 modes (values) - **preemptible** and **non-preemptible**.

We want to add a new 3rd mode, named **semi-preemptible**, where the podgroup will be non-preemptible up to the `minMember` count of each leaf PodSet, and any extra pods are "elastic" pods and preemptible.

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

### Scope: semi-elasticity is a pod-level concept

Semi-elasticity (the core/elastic split) applies **exclusively to pods**. Subgroups and groups are atomic scheduling units — they are either fully scheduled or not. There is no "semi-elastic subgroup" concept. A user who defines fine-grained subgroups intends them to be scheduled as a whole.

### Inheritance

Subgroups inherit the preemption mode from the root PodGroup. If the PodGroup is **semi-preemptible**, all subgroups are **semi-preemptible**.

### `minMember` vs. `minSubGroups`

The core/elastic split is determined **only at nodes that have `minMember` set** (leaf PodSets). Intermediate nodes that use `minSubGroups` define a scheduling gate (how many child subgroups must be satisfied) but do not themselves define a non-preemptible pod threshold.

Since pods are always attached to leaf PodSets and never to intermediate SubGroupSets, this is a natural boundary: `minMember` is always a leaf-level concept.

**Non-preemptible resource count** = sum of (`minMember × pod resource request`) across all scheduled leaf PodSets.

### Behavior when `minSubGroups < scheduled children`

When a parent node requires fewer children than are actually scheduled (i.e., some children are "extra" from the scheduling perspective), each child's core/elastic split is still determined independently by that child's own `minMember`. The "extra-ness" is a scheduling-gate concept handled by the existing elastic subgroup scheduling; it does not override the per-subgroup semi-preemptible semantics.

## Immutability Constraint

A validation webhook must **prevent increases** to `minMember` and `minSubGroups` on a semi-preemptible PodGroup after creation. This applies to the root PodGroup spec and to all SubGroup entries within it.

**Rationale:** once a semi-preemptible job is running, some pods may be over-quota (the elastic ones). Increasing `minMember` or `minSubGroups` would silently reclassify those over-quota pods as "core" non-preemptible pods, violating quota invariants without a rescheduling cycle.

Decreasing these fields is allowed — it can only widen the elastic tier.

## Simulation Considerations

In simulations, only the "extra" (`n - minMember`) pods per leaf PodSet are considered as possible victims. This is applied independently per PodSet; no cross-subgroup victim selection is needed. This approach may miss some solutions when checking all $\binom{n}{m}$ orderings, but the added complexity is not justified for the MVP.

## Implementation Notes

- **Over-quota checks**: core pods (up to `minMember` per leaf) must be in-quota; extra pods may be over-quota
- **Podgroup and queue status**: count only `minMember` resources per leaf PodSet toward the non-preemptible totals. The pod ordering plugin determines which specific pods are "core" vs. "extra"
- **Solver simulation**: represent the "extra" pods of a semi-preemptible job as a fully-preemptible representative job

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
