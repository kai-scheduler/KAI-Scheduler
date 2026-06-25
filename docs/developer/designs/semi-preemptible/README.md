# Semi-Preemptible Mode

## Overview

In `v0.10` we separated Priority and Preemption to allow users to control the two parameters independently, where Preemption has 2 modes (values) - **preemptible** and **non-preemptible**.

We want to add a new 3rd mode, named **semi-preemptible**, where the podgroup is non-preemptible up to its **minimum required shape** — `minMember` pods at each leaf PodSet and `minSubGroup` child subgroups at each intermediate node — and anything beyond that minimum is "elastic" and preemptible. Elasticity therefore applies at **every level of the subgroup tree**, not just to pods.

## Goals / Non-Goals

**Goals**
- Add a third preemptibility mode, `semi-preemptible`, on top of the **existing APIs** (the `preemptibility` field/label, `minMember`, `minSubGroup`) — no new API fields.
- Keep a job's **minimum required shape** non-preemptible and in-quota; allow anything beyond it to run elastically (over-quota, reclaimed first).
- Apply the core/elastic split at **every level** of the subgroup tree, and compose cleanly with [segmented subgroups](../segmented-subgroups/README.md).
- Change nothing for existing workloads — the mode is strictly opt-in.

**Non-Goals**
- Solving queue **quota scale-down** in general for KAI Scheduler. If a queue's deserved quota drops below a running job's core allocation, the queue stays over-quota until the job releases resources on its own — exactly as a `non-preemptible` job behaves today. No new mitigation is introduced (see [Quota Scale-Down](#quota-scale-down)).
- The `minNonPreemptible` field that would decouple the scheduling minimum from the non-preemptible threshold (see [Future Work](#future-work-minnonpreemptible-field)).

## Use Cases

A workload with `minReplicas` such as Inference and Elastic Distributed Training can request to be non-preemptible up to its `minReplicas`, with any pods above that count being preemptible. This allows running a critical workload with some assured resources and some on-demand, availability-based resources.

## Usage

Semi-preemptible reuses the **existing preemptibility API** introduced in [priority/preemptibility separation](../priority-preemptibility-separation/README.md). It is an **opt-in** feature — when `preemptibility` is omitted, behavior is unchanged.

**On the PodGroup spec** — a single elastic group (3 core pods, bursts beyond):
```yaml
apiVersion: scheduling.kai.nvidia.com/v2alpha2
kind: PodGroup
metadata:
  name: elastic-inference
spec:
  preemptibility: "semi-preemptible"
  minMember: 3            # 3 core (non-preemptible) pods; pods above 3 are elastic
  priorityClassName: "inference"
  # ... rest of podgroup spec
```

**On a workload (label)** — the PodGrouper propagates it to the PodGroup:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: elastic-inference
spec:
  template:
    metadata:
      labels:
        kai.scheduler/preemptibility: "semi-preemptible"
```

For multi-level trees, `minSubGroup` makes whole subgroups core vs. elastic — see [Interaction with Segmented Subgroups](#interaction-with-segmented-subgroups).

## Quota Requirements

The "core" pods (up to `minMember` per leaf PodSet) must be in-quota when allocated. Any "extra" pods can be allocated over-quota. All pods must respect the Limit setting for the job's queue.

From this:
- A semi-preemptible podgroup where the total pod count equals `minMember` == non-preemptible podgroup
- A semi-preemptible podgroup where `minMember` is 0 == preemptible podgroup

### Quota Scale-Down

If a queue's deserved quota drops below a semi-preemptible job's running **core** allocation, the queue becomes persistently over-quota: reclaim evicts the job's elastic pods/subgroups first, but the core remains (protected by the existing `minAvailable` / `GetNumActiveAllocatedTasks()` eviction guard) until the job ends or scales down on its own. This is **accepted behavior** — identical to how a fully `non-preemptible` job already behaves when its queue is scaled down — and is an explicit [non-goal](#goals--non-goals): no special mitigation is introduced for semi-preemptible jobs.

## Subgroups and Multi-Level Trees

The core/elastic split is defined by the **minimum of each node** in the subgroup tree, not by pods alone:

- a **leaf PodSet** with `minMember = m` keeps `m` pods core; pods beyond `m` are elastic;
- an **intermediate SubGroupSet** with `minSubGroup = k` keeps its `k` highest-priority child subgroups core; additional scheduled subgroups are elastic and reclaimed **as a whole** (subgroups stay atomic — never split).

Subgroups inherit the mode from the root: a `semi-preemptible` PodGroup makes the whole tree semi-preemptible, and each node's minimum sets its own core/elastic boundary.

**Non-preemptible (core) resources = the tree's minimal satisfying set**, computed recursively: at each SubGroupSet descend into the `minSubGroup` highest-priority children (all of them if `minSubGroup` is unset); at each leaf take `minMember` pods × the per-pod request. This is the same set the allocator builds in its gang phase, so quota and scheduling agree on what "core" means. Where scheduled count equals the minimum, that node is fully non-preemptible; `minMember == 0` / no minimum ⇒ fully preemptible at that node.

The scheduler already gates this way: allocation (`collectTasksFromSubGroupSet`) schedules the `minSubGroup` core children then bursts extras opportunistically, and eviction (`eviction_info.go`) protects exactly the core children (`GetMinMembersToSatisfy()`) and reclaims surplus whole. An elastic subgroup is deployed only if its pods can be gang-scheduled — otherwise it stays unsatisfied.

## Immutability Constraint

A validation webhook must **reject increases** to `minMember` or `minSubGroup` on a running semi-preemptible PodGroup (the root spec and every SubGroup entry). Raising either would reclassify already-running over-quota elastic pods/subgroups as core, silently growing the minimal satisfying set and breaking quota invariants without a rescheduling cycle. Decreasing is allowed — it can only widen the elastic tier.

## Interaction with Segmented Subgroups

[Segmented subgroups](../segmented-subgroups/README.md) auto-builds a fixed shape: a workload's replicas are split into `N` fixed-size **segments**, each emitted as a leaf PodSet with `minAvailable = segmentSize`, under a parent SubGroupSet that may carry a `minSubGroup`. Segmentation forces this shape so each segment lands in its own topology domain (e.g. a rack).

Subgroup-level elasticity is what makes semi-preemptible **meaningful** here. Each segment is fully gang (`minAvailable == segmentSize`), so it has no pod-level surplus — under a pod-only model every segment would be core and the job would collapse to plain non-preemptible. With the tree-level model the parent's `minSubGroup` decides how many **segments** are core; the rest are elastic and reclaimed a **whole segment at a time**, so the forced topology shape is never torn apart.

The elastic tier exists only when `minSubGroup < #segments`. Example — `minSubGroup: 2` over 4 segments: 2 segments core (in-quota), 2 elastic (over-quota, reclaimed first):

```yaml
spec:
  preemptibility: "semi-preemptible"
  minSubGroup: 2          # 2 of 4 segments are core; the rest are elastic
  subGroups:
    - name: segment-0     # core
      minMember: 8        # fully gang: no pod-level elasticity inside a segment
    - name: segment-1     # core
      minMember: 8
    - name: segment-2     # elastic — evicted as a whole segment
      minMember: 8
    - name: segment-3     # elastic — evicted as a whole segment
      minMember: 8
```

If `minSubGroup` equals the segment count, no node has surplus and the job is effectively non-preemptible despite the setting — lower `minSubGroup` to create an elastic segment tier.

## Simulation Considerations

Victim selection considers only the **surplus** of each node: the "extra" (`n - minMember`) pods at a leaf PodSet, and the extra (`scheduled - minSubGroup`) child subgroups at an intermediate node (evicted whole). This is applied independently per node; no cross-subgroup victim selection is needed. This approach may miss some solutions when checking all orderings, but the added complexity is not justified for the MVP.

This implies that pods are treated equally within the same subgroup for eviction, prompting the user to use the subgroup API to specify any ordering or hierarchy for pod eviction.

## Future Work: `minNonPreemptible` field

This design uses `minMember` as the non-preemptible threshold. A future `minNonPreemptible` field (pod-level only, no subgroup analog) would decouple the scheduling minimum from the non-preemptible threshold — e.g. `minMember=4, minNonPreemptible=2` (needs 4 pods to start, but only 2 are non-preemptible). It introduces a third pod tier — "required for scheduling but elastic for preemption" — between core and extra-elastic, requiring explicit ordering or labeling to identify which pods fall into each tier, plus a new API field, validation (`minNonPreemptible ≤ minMember`), quota accounting decoupled from `minMember`, and matching webhook/solver/status updates.
