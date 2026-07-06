# Semi-Preemptible Mode

## Overview

In `v0.10` we separated Priority and Preemption to allow users to control the two parameters independently, where Preemption has 2 modes (values) - **preemptible** and **non-preemptible**.

We want to add a new 3rd mode, named **semi-preemptible**, where the podgroup is non-preemptible up to its **minimum required shape** — `minMember` pods at each leaf PodSet and `minSubGroup` child subgroups at each intermediate node — and anything beyond that minimum is "elastic" and preemptible. Elasticity therefore applies at **every level of the subgroup tree**, not just to pods.

## Goals / Non-Goals

**Goals**
- Add a third preemptibility mode, `semi-preemptible`, on top of the **existing APIs** (the `preemptibility` field/label, `minMember`, `minSubGroup`) — no new API fields.
- Keep a job's **minimum required shape** non-preemptible and in-quota; allow anything beyond it to run elastically (over-quota, reclaimed first).
- Apply the core/elastic split at **every level** of the subgroup tree, so whole child subgroups burst elastically in the same manner as surplus pods do at a leaf — driven by `minSubGroup` on **hand-authored** subgroup trees.
- Change nothing for existing workloads — the mode is strictly opt-in.

**Non-Goals**
- **Automated segmented subgroups** (the `kai.scheduler/segment-size` annotation path). Automated segmentation is **out of scope** and mutually exclusive with semi-preemptible; the combination is soft-enforced (see [Automated Segmentation](#automated-segmentation-out-of-scope)). Subgroup-level elasticity is supported only for **hand-authored** subgroup trees.
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

For multi-level trees, `minSubGroup` makes whole subgroups core vs. elastic — see [Subgroups and Multi-Level Trees](#subgroups-and-multi-level-trees).

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

**Example** — a hand-authored PodGroup of 4 fully-gang replica subgroups with `minSubGroup: 2`: 2 replicas are core (in-quota), the other 2 burst elastically (over-quota, reclaimed a whole replica at a time):

```yaml
spec:
  preemptibility: "semi-preemptible"
  minSubGroup: 2          # 2 of 4 replica subgroups are core; the rest are elastic
  subGroups:
    - name: replica-0     # core
      minMember: 8        # fully gang: no pod-level elasticity inside the subgroup
    - name: replica-1     # core
      minMember: 8
    - name: replica-2     # elastic — evicted as a whole subgroup
      minMember: 8
    - name: replica-3     # elastic — evicted as a whole subgroup
      minMember: 8
```

Because each subgroup here is fully gang (`minMember == size`), there is no pod-level surplus inside a subgroup; `minSubGroup` is what creates the elastic tier. If `minSubGroup` equalled the subgroup count, no node would have surplus and the job would be effectively non-preemptible despite the setting.

## Immutability Constraint

A validation webhook must **reject increases** to `minMember` or `minSubGroup` on a running semi-preemptible PodGroup (the root spec and every SubGroup entry). Raising either would reclassify already-running over-quota elastic pods/subgroups as core, silently growing the minimal satisfying set and breaking quota invariants without a rescheduling cycle. Decreasing is allowed — it can only widen the elastic tier.

## Automated Segmentation (Out of Scope)

[Automated segmented subgroups](../segmented-subgroups/README.md) (the `kai.scheduler/segment-size` annotation, wired for PyTorchJob Worker replicas and LeaderWorkerSet) are **out of scope** for semi-preemptible and are **mutually exclusive** with it.

Automated segmentation emits a **fully-gang** tree: every segment leaf gets `minAvailable = segmentSize`, with no `minSubGroup` and no `minMember = 0`. A fully-gang tree has no surplus at any level, so semi-preemptible has nothing to make elastic and silently collapses to plain non-preemptible. Rather than ship a combination that looks meaningful but is inert, the two are kept apart.

This does **not** remove subgroup-level elasticity — it remains fully supported for **hand-authored** subgroup trees (see [Subgroups and Multi-Level Trees](#subgroups-and-multi-level-trees)), where the user sets `minSubGroup` below the number of child subgroups to create an elastic tier. The exclusion applies only to the automated, annotation-driven path.

**Enforcement (soft).** When a workload requests both automated segmentation and `semi-preemptible`, the PodGrouper still creates the PodGroup but emits a `PodGrouperWarning` event on the pod, noting that the two are mutually exclusive and the workload will behave as non-preemptible. Enforcement lives in the PodGrouper rather than an admission webhook because only the grouper sees both the resolved preemptibility and the segmentation decision, and for auto-segmented workloads the user submits the source workload (PyTorchJob/LWS) — never the PodGroup — so a Warning event on the pod is more visible than a PodGroup-webhook rejection. Being non-blocking, it never breaks an existing workload that happens to set both.

## Simulation Considerations

Victim selection considers only the **surplus** of each node: the "extra" (`n - minMember`) pods at a leaf PodSet, and the extra (`scheduled - minSubGroup`) child subgroups at an intermediate node (evicted whole). This is applied independently per node; no cross-subgroup victim selection is needed. This approach may miss some solutions when checking all orderings, but the added complexity is not justified for the MVP.

This implies that pods are treated equally within the same subgroup for eviction, prompting the user to use the subgroup API to specify any ordering or hierarchy for pod eviction (see [Footnote: Eviction Ordering](#footnote-eviction-ordering)).


## Footnote: Eviction Ordering

In a `Semi-Preemptible` PodGroup / SubGroup, pods are NOT "colored out" as preemptible — there is no election of individual pods. All pods within a subgroup are treated equally: victims are drawn from the surplus using the **existing pod eviction ordering** (the same ordering used elsewhere in preemption), which is role-agnostic. It does not know that one pod is a master and another is a worker.

A non-homogeneous subgroup / podgroup with the semi-preemptible attribute might therefore experience reduced service because the "wrong" pods are evicted — the ordering has no notion of the user's intended hierarchy. This is amended by correctly configuring subgroups and grouping similar pods into logical structures.

**Unadvised** — one master pod and three workers mixed in a single leaf PodSet with `minMember: 2`. Any 2 of the 4 pods are kept as core; because eviction ordering does not distinguish roles, the master is not guaranteed to survive, and the job can be left with 2 workers and no master:

```yaml
spec:
  preemptibility: "semi-preemptible"
  minMember: 2            # keeps ANY 2 of {master, worker, worker, worker} — master may be evicted
```

**Aligned with API** — separate master and workers into their own subgroups so eviction can only target the surplus workers, never the master:

```yaml
spec:
  preemptibility: "semi-preemptible"
  minSubGroup: 2          # both subgroups below are core
  subGroups:
    - name: master        # core — never evicted
      minMember: 1
    - name: workers       # 2 workers core; extra workers are elastic
      minMember: 2
```

## Future Work: `minNonPreemptible` field

This design uses `minMember` as the non-preemptible threshold. A future `minNonPreemptible` field (pod-level only, no subgroup analog) would decouple the scheduling minimum from the non-preemptible threshold — e.g. `minMember=4, minNonPreemptible=2` (needs 4 pods to start, but only 2 are non-preemptible). It introduces a third pod tier — "required for scheduling but elastic for preemption" — between core and extra-elastic, requiring explicit ordering or labeling to identify which pods fall into each tier, plus a new API field, validation (`minNonPreemptible ≤ minMember`), quota accounting decoupled from `minMember`, and matching webhook/solver/status updates.