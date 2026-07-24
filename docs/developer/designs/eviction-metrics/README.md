# Workload-Centric Eviction Metrics

## Overview

This document proposes a redesign of the PodGroup eviction metric exposed by the scheduler. The current counter `kai_pod_group_evicted_pods_total` records eviction events but cannot be queried with standard PromQL to answer the questions operators actually ask. This document specifies the label shape, semantics, and emission rules for the replacement, and adds a sibling event-level counter.

This design is the response to [issue #1573](https://github.com/kai-scheduler/KAI-Scheduler/issues/1573).

## Motivation

The recurring operational question is:

> **How disrupted has *this workload* been recently?**

Where *this workload* is the user-facing object the human reasons about — a JobSet someone submitted, a Coder Workspace a developer is trying to keep alive, an MPIJob mid-run. Users ping operators in those terms ("my workspace got killed again", "this training keeps restarting"), and operators need to answer with a simple PromQL query keyed on the workload object.

### Driving case: multi-rack distributed training JobSets

The specific operational pain that drove this design is multi-rack distributed training. A JobSet such as `multi-rack-train` runs several replicas, each pinned to its own GPU rack. When a single rack gets preempted, consolidated, or reclaimed, the training run as a whole takes a hit, and the operator needs to answer two questions about a named workload object — not about PodGroups, not about pods:

1. How many times was this JobSet disrupted in the last N hours?
2. Which rack (replica) lost pods, and how many?

Both questions are unanswerable today with standard PromQL. Per-rack disruption surfaces today as a set of PodGroup series whose names look like `pg-multi-rack-train-<uuid>-r0` through `-r3`, with no `owner_name` label to aggregate on and no `subgroup` label to break down by rack. Once the JobSet grouper produces a SubGroup hierarchy per replica ([issue #1189](https://github.com/kai-scheduler/KAI-Scheduler/issues/1189)), the natural mapping is: one PodGroup per JobSet, one leaf SubGroup per replica. The eviction metric proposed in this document is designed against that shape — see [Example 4](#example-4-multi-replica-training-jobset-the-driving-case). It also gives an immediate improvement on today's pre-#1189 state, where each replica is its own PodGroup, by introducing `owner_name` aggregation across those replica PGs.

Today the metric records the underlying events but every standard query path is broken:

1. **Single-eviction series are invisible to `rate()` / `increase()`.** Counters in Prometheus only have a defined rate from the second sample onward — a series born at value `N` reports `0` forever. Production data over a 24h window: `count(kai_pod_group_evicted_pods_total)` grew from 91 → 94 series, but `count(rate(...[5m]) > 0)` and `count(increase(...[1h]) > 0)` both stayed at 0. The new evictions are recorded but invisible to any rate-based dashboard or alert.
2. **No owner-workload label.** To roll evictions up to a JobSet or Workspace, callers must `label_replace` with a regex on the PodGroup name (which encodes the topOwner UID). Fragile, and doesn't generalize across workload types.
3. **`uid` label adds churn for no real benefit.** `{namespace, podgroup}` is already unique for a PG's lifetime, and the PG name already encodes the topOwner UID. The `uid` label only disambiguates the niche case of "PG deleted and recreated while topOwner UID stays the same," and every PG recreation today spawns a fresh series, amplifying problem #1.
4. **Counts pods, not eviction events.** With variable gang sizes, "PG evicted twice, gang of 5 each" and "PG evicted once, gang of 10" should both increment the counter by 10, so the "events" question cannot be answered.

The PodGroup is an internal scheduler abstraction; the metric should let operators pivot to the workload view in one `sum by (...)`.

## Goals

- A single PromQL aggregation answers "how disrupted has *workload X* been" without `label_replace` or PG-name regex.
- Both "how many disruption events" and "how many pods evicted" can be answered, and one is not derivable from the other.
- `rate()` / `increase()` produce correct non-zero output starting from the first eviction of any workload.
- A workload that is destroyed and re-submitted with the same name aggregates as one logical workload across its lifetimes.
- Per-subgroup visibility for any workload whose PodGroup carries leaf SubGroups, reconstructable in PromQL without overcounting at higher levels of aggregation.

## Non-Goals

- No new CRDs.
- No change to the eviction *mechanism* — only to the metrics emitted.
- No per-SubGroup eviction-event counter in v1 (deferred — see [Alternatives](#alternatives-considered)).
- No backfill of historical metric data.

## Design

### Metric shape

```
kai_pod_group_evicted_pods_total{
  podgroup, namespace, nodepool, action,
  owner_group, owner_kind, owner_name, subgroup
}
# Counter. Increments by 1 per pod evicted.
# Pre-initialized at 0 when the scheduler first observes the PodGroup.

kai_pod_group_eviction_events_total{                          # NEW
  podgroup, namespace, nodepool, action,
  owner_group, owner_kind, owner_name
}
# Counter. Increments by 1 per scheduling decision that evicts pods from this PG.
# Pre-initialized at 0 when the scheduler first observes the PodGroup.
```

### Labels

| Label | Source | Notes |
|---|---|---|
| `podgroup` | `PodGroup.Name` | Unchanged. |
| `namespace` | `PodGroup.Namespace` | Unchanged. |
| `nodepool` | PodGroup label (existing helper) | Unchanged. |
| `action` | scheduler action type | Unchanged. Values: `preempt`, `reclaim`, `consolidation`, `stalegangeviction`. |
| `owner_group` | `kai.scheduler/top-owner-metadata` annotation on the PodGroup | NEW. The API Group of the top-level workload object (`kubeflow.org`, `jobset.x-k8s.io`, `apps`, …). Disambiguates Kinds shared across operators (e.g., `MPIJob` from kubeflow vs mpi-operator). `""` for core/v1 objects. |
| `owner_kind` | `kai.scheduler/top-owner-metadata` annotation on the PodGroup | NEW. The Kind of the top-level workload object (`JobSet`, `Deployment`, `MPIJob`, …). |
| `owner_name` | `kai.scheduler/top-owner-metadata` annotation on the PodGroup | NEW. The Name of the top-level workload object. Re-submissions of the same name aggregate together. |
| `subgroup` | `kai.scheduler/subgroup-name` label on the evicted pod (pods counter only) | NEW. The leaf SubGroup the evicted pod belongs to. `""` for flat PodGroups. |
| `uid` | — | REMOVED. |

#### Why source `owner_*` from the annotation, not `OwnerReferences`

The podgrouper writes the annotation `kai.scheduler/top-owner-metadata` on every PodGroup. The annotation value is YAML containing `kind`, `name`, `uid`, `group`, `version` of the top-level workload object that drove PodGroup creation.

`PodGroup.OwnerReferences` is not a reliable source because grouper plugins override it for plugin-specific purposes — e.g., the Deployment grouper sets the ownerReference to the Pod itself rather than the Deployment, to keep its per-pod PodGroup naming model coherent. The top-owner annotation always reflects the true workload object regardless of grouper plugin quirks.

For PodGroups without a top-owner annotation, all three labels (`owner_group`, `owner_kind`, `owner_name`) are emitted as `""`.

#### Why `owner_group` but not `owner_version`

The annotation carries the full GVK, but `version` is intentionally not exposed as a label. A given workload object does not change CRD version after creation, so the version label would add cardinality without disambiguating anything in operator queries. `group` is exposed because the same `Kind` name is reused across operators (e.g., `MPIJob` exists under both `kubeflow.org` and `mpi-operator.kubeflow.org`), and operators querying by `owner_kind="MPIJob"` would otherwise silently aggregate across distinct operator universes. If a future query genuinely needs version pivoting, the version field is still present in the annotation and a follow-up can add the label without breaking existing series.

#### Why `subgroup` only on the pods counter

KAI PodGroups can contain a tree of SubGroups (see [`docs/topology/multilevel.md`](../../../topology/multilevel.md)). Pods belong only to **leaf** SubGroups, via the `kai.scheduler/subgroup-name` label.

- The **pods counter** has `subgroup` because each pod belongs to exactly one leaf SubGroup, so per-subgroup counts are exact and `sum by (subgroup)` answers "which role lost the most pods?".
- The **events counter** does **not** have `subgroup` because one scheduling decision can take down a subtree spanning multiple leaf SubGroups (e.g., evicting the gang of a non-leaf SubGroup). If `subgroup` were on the events counter, that single decision would increment multiple series, and `sum by (podgroup)` would report 2 disruption events when the operator-meaningful answer is 1.

A per-subgroup event counter (`kai_pod_group_subgroup_eviction_events_total`) can be added later if needed; it is intentionally out of scope here.

### Emission semantics

#### Per-pod increment for the pods counter

The pods counter increments by **1 per pod evicted**, at the existing eviction emit point in the scheduler's status updater.

#### Per-(decision, PodGroup) increment for the events counter

The events counter increments by **1 per committed eviction decision per affected PodGroup**. A decision is the unit constructed by the scheduler when it identifies victims to evict together: a preemption, a reclaim, a consolidation, or a stale-gang eviction. A single decision can span multiple PodGroups — the by-pod solver collects victim tasks across all `recordedVictimsJobs` for a scenario and passes the flattened set to `EvictAllPreemptees` — so the rule is per (decision, PG) pair, not per decision. A decision touching two PodGroups emits two event increments, one per PG; a decision touching one PG emits one.

The event increment must fire **after** the decision is committed, not when it is constructed. The scheduler accumulates eviction operations in a transactional statement that can be rolled back; only after the statement commits do the underlying evictions actually happen. Emitting earlier would inflate the count with decisions that were rolled back. The shape is:

> 1. Action layer builds the eviction decision (M victim pods, possibly across multiple PodGroups).
> 2. Each victim becomes a queued evict operation inside the statement.
> 3. Statement commits → physical evictions happen → pods counter increments M times (with each pod's increment labeled by its own PG and subgroup).
> 4. After commit succeeds → events counter increments once per distinct PodGroup that had at least one pod committed in this decision.

So a decision evicting M pods across K distinct PodGroups produces M increments to the pods counter and K increments to the events counter. A rolled-back decision produces zero of either.

#### Pre-initialization at 0

When the scheduler first observes a PodGroup (cache event-handler `OnAdd`), both counters are initialized to 0 across all label combinations that can ever fire for that PG. The series is then born at 0 in Prometheus and a first eviction takes it `0 → N`, making `rate()` / `increase()` correct from the first eviction onward.

- **Events counter**: one series per `action` value (subgroup is not a label on this counter).
- **Pods counter**: one series per `(action, leaf-subgroup)` combination from `PodGroup.Spec.SubGroups`, enumerated at `OnAdd` (or `subgroup=""` when Spec has no SubGroups). SubGroups may be added later; `OnUpdate` pre-inits series for newly added leaves.
- **PodGroup delete**: drop all series for that `{namespace, podgroup}` from both counters (`DeletePartialMatch`).

Pre-initialization is essential for the per-subgroup pods counter: without it, a JobSet whose first eviction lands on leaf `r2` would have the `subgroup="r2"` series born at the eviction value, hitting the first-sample-invisible problem the design is meant to fix.

Cardinality of pre-init is bounded by the active PG count × number of `action` values (× number of leaf subgroups per PG, for the pods counter).

## Worked examples

### Example 1: Deployment with independent pods

State: a Deployment `my-app` with 100 pods. The Deployment grouper creates one PodGroup per pod (PodGroup name `pg-<pod-name>-<pod-uid>`). 10 pods are evicted, each in its own scheduling decision (Deployment pods are not gang-scheduled).

Resulting series — all 10 share `owner_kind=Deployment`, `owner_name=my-app`, `subgroup=""`:

| Metric | Per-series value | `sum by (owner_name)` |
|---|---|---|
| `kai_pod_group_evicted_pods_total` | 10 distinct `podgroup` series, each at 1 | **10** |
| `kai_pod_group_eviction_events_total` | 10 distinct `podgroup` series, each at 1 | **10** |

Both equal 10 because each Deployment pod is an independent scheduling unit.

### Example 2: Gang-scheduled JobSet

State: a JobSet `train-x` with one gang of 10 pods. The whole gang is preempted in one scheduling decision.

Single PG (`pg-train-x-<uid>`), `owner_kind=JobSet`, `owner_name=train-x`, `subgroup=""`:

| Metric | Per-series value | `sum by (owner_name)` |
|---|---|---|
| `kai_pod_group_evicted_pods_total` | one series at 10 | **10** |
| `kai_pod_group_eviction_events_total` | one series at 1 | **1** |

The two counters now answer different questions correctly: 10 pods went down (pod churn) in 1 disruption event (workload disruption).

### Example 3: Hierarchical SubGroups (inference pipeline)

State: a PodGroup `infer-y` with leaf SubGroups `prefill` (3 pods), `decode` (4 pods), `preprocessor` (1 pod). The scheduler preempts the whole PG as one decision (because gang requirements fail at the root).

Single PG, `owner_kind=InferenceJob`, `owner_name=infer-y`. Pods counter has three series, one per leaf SubGroup; events counter has one series:

| Metric | Series | Sum |
|---|---|---|
| `kai_pod_group_evicted_pods_total{subgroup="prefill"}` | 3 | |
| `kai_pod_group_evicted_pods_total{subgroup="decode"}` | 4 | |
| `kai_pod_group_evicted_pods_total{subgroup="preprocessor"}` | 1 | |
| `sum by (owner_name) (kai_pod_group_evicted_pods_total)` | | **8 pods** |
| `sum by (owner_name, subgroup) (kai_pod_group_evicted_pods_total)` | | exact role breakdown |
| `kai_pod_group_eviction_events_total` | 1 | **1 event** |

A subsequent decision that only takes down the `prefill` SubGroup increments `subgroup="prefill"` on the pods counter and the single event series on the events counter. No double-count anywhere.

### Example 4: Multi-replica training JobSet (the driving case)

This is the operational case that motivated the redesign. A distributed training JobSet `multi-rack-train` runs four replicas, each pinned to its own GPU rack, with 256 pods per rack (1024 pods total). The metric must answer "how often is this training run disrupted" and "which rack lost pods" in a single PromQL aggregation.

#### After [issue #1189](https://github.com/kai-scheduler/KAI-Scheduler/issues/1189) (target shape)

The JobSet grouper produces **one PodGroup per JobSet** with a SubGroup hierarchy: `PodGroup → ReplicatedJob subgroup → Replica subgroups → pods`. Each pod belongs to a leaf replica SubGroup (`r0`, `r1`, `r2`, `r3`).

Suppose rack `r2` is preempted while the other three keep running. One PodGroup is affected, one scheduling decision evicts 256 pods all belonging to the `r2` leaf SubGroup:

| Metric | Series affected | Sum |
|---|---|---|
| `kai_pod_group_evicted_pods_total{subgroup="r2"}` | 1 series at 256 | **256 pods** |
| `kai_pod_group_evicted_pods_total{subgroup="r0","r1","r3"}` | unaffected (still 0) | 0 |
| `kai_pod_group_eviction_events_total` | 1 series, incremented by 1 | **1 event** |

The operator-facing queries work in one aggregation:

```promql
# "How often was the JobSet disrupted in the last 24h?"
sum by (owner_name) (
  increase(kai_pod_group_eviction_events_total{
    owner_kind="JobSet", owner_name="multi-rack-train"
  }[24h])
)

# "Which rack lost the most pods this hour?"
topk(5,
  sum by (subgroup) (
    increase(kai_pod_group_evicted_pods_total{
      owner_name="multi-rack-train"
    }[1h])
  )
)
```

Both answers are immediate and exact. No `label_replace`, no PG-name regex.

#### Before #1189 (today's pre-existing per-replica PodGroups)

Today's JobSet grouper produces four PodGroups for this JobSet — `pg-multi-rack-train-<uid>-r0` through `-r3` — with no SubGroups inside any of them. The same eviction event (rack `r2` preempted) emits against PodGroup `…-r2` only, with `subgroup=""`:

| Query | Result today | Result post-#1189 |
|---|---|---|
| `sum by (owner_name) (increase(events[1h]))` | 1 event (only `…-r2` PG affected) | 1 event (same) |
| `sum by (owner_name) (increase(pods[1h]))` | 256 pods | 256 pods |
| Per-rack pod breakdown | Read off the `podgroup` label (PG name carries the replica suffix) | Read off the clean `subgroup` label |

The workload-level answer is correct in both worlds; the only thing that improves once #1189 lands is that per-rack breakdown moves from PG-name parsing to a first-class `subgroup` label.

This means the design is forward-compatible with #1189 with no further changes, and gives a usable workload-level eviction view today.

## Example PromQL queries

The shape unlocks workload-centric dashboards with no `label_replace`:

```promql
# "How often was Coder workspace alice-dev disrupted in the last 24h?"
sum by (owner_name) (
  increase(kai_pod_group_eviction_events_total{
    namespace="user-x", owner_kind="Workspace", owner_name="alice-dev"
  }[24h])
)

# "Top 10 most-disrupted JobSets in the last hour"
# owner_group disambiguates Kinds shared across operators (e.g. MPIJob in kubeflow vs mpi-operator).
topk(10,
  sum by (namespace, owner_group, owner_name) (
    increase(kai_pod_group_eviction_events_total{owner_kind="JobSet"}[1h])
  )
)

# "Pods evicted per role of an inference workload, last hour"
sum by (owner_name, subgroup) (
  increase(kai_pod_group_evicted_pods_total{owner_name="infer-y"}[1h])
)

# "Eviction rate per scheduling action"
sum by (action) (rate(kai_pod_group_eviction_events_total[5m]))
```

## Cardinality

Series count per cluster, with `A` = number of eviction `action` values (4: `preempt`, `reclaim`, `consolidation`, `stalegangeviction`), `P` = active PodGroups, `L` = total leaf SubGroups across all PGs:

- `kai_pod_group_eviction_events_total`: `A × P` series.
- `kai_pod_group_evicted_pods_total`: `A × L` series.

`L` is workload-shape-dependent, not bounded by any single ceiling: a flat PG contributes 1 to `L`, a hierarchical PG with `k` leaves contributes `k`. Typical values: flat PGs (the common case today) give `L = P`; multi-replica JobSets post-[#1189](https://github.com/kai-scheduler/KAI-Scheduler/issues/1189) contribute one leaf per replica (single digits to low tens); inference pipelines contribute a handful of role-named leaves.

For a cluster of 100,000 PodGroups (a realistic upper bound — clusters of this size are observed in practice, with most PGs pending), the events counter is ~400k series; the pods counter is ~400k for mostly-flat PG populations and a multiple of that for hierarchical workloads, in proportion to their average leaf count. Pre-initialization adds zero-valued series for PGs that never get evicted; the cost follows the same formulas.

This sits within Prometheus's operational envelope at that cluster size — kube-state-metrics alone is already emitting multi-million-series volumes from per-pod and per-container metrics, so the eviction counters are a small fraction of the total.

The honest trade-off operators should know about:

- **Sample storage is cheap.** Prometheus TSDB uses Gorilla/XOR encoding for sample values; long sequences of identical values (the zero-init case until a first eviction) compress to near 1 bit per sample, so disk and ingest cost is minimal.
- **In-memory head block cost is not free.** Each active series consumes ~3KB of head-block RAM regardless of sample volume. A zero-valued series costs the same memory as a busy one.
- **Managed-Prometheus billing impact.** Grafana Cloud and most managed Prometheus offerings bill by active series count, not sample volume. Pre-initialized zero series therefore add to recurring billed cardinality even when no eviction has ever happened. Operators on managed plans should size accordingly.

We accept this cost because there is no alternative that solves the "first-eviction invisible to `rate()` / `increase()`" problem (the whole motivation for this design) without pre-initialization. Skipping pre-init keeps the metric broken in exactly the way #1573 reports.

For Deployments specifically, the per-pod PG model means `P` scales with pod count, not workload count. Operators querying by `owner_name` recover the workload view via aggregation — dashboards stay tractable.

## Migration / breaking changes

| Change | Type | Impact |
|---|---|---|
| Drop `uid` label | **Breaking** | Existing dashboard panels that filter on `uid="..."` return empty. In practice impact is expected to be near-zero because (a) the metric is broken for rate-based queries today, so consumers using it are limited, and (b) `uid` is an internal value, not a human-typed filter. CHANGELOG entry required. |
| Add `owner_group`, `owner_kind`, `owner_name`, `subgroup` | Additive | Changes series identity, but queries that `sum by (...)` without these labels keep working. |
| Pre-init at 0 on first observe | Additive | New zero-valued series for never-evicted PGs. Cardinality bound = active PGs × actions. |
| New `kai_pod_group_eviction_events_total` | Additive | No impact on existing consumers. |

Mitigation if `uid` removal is a concern: keep `uid` for one release behind a feature flag, then remove. Default to removed unless maintainers request otherwise.

## Open issues / out of scope

These are explicitly not addressed by this design. The first is a follow-up I would expect to open as its own issue; the second is a known edge case the design does not attempt to fix.

- **Externally-created PodGroups have no owner labels.** PodGroups created outside the podgrouper (per the [`external-podgroups`](../external-podgroups/README.md) design) do not carry the `kai.scheduler/top-owner-metadata` annotation. For those PGs, `owner_kind=""` and `owner_name=""`. A follow-up issue should either define a convention for labeling external PGs (e.g., an explicit annotation users can set) or accept empty values as the intended behavior. Out of scope for this PR.
- **Pre-init / first-eviction race within one scrape interval.** Pre-initializing the series at 0 on PodGroup observation only solves `rate()` if at least one Prometheus scrape interval elapses between observation and the first eviction. If a PG is observed and evicted inside the same scrape interval, only the post-eviction value is sampled and `rate()` still cannot derive a rate. PGs typically live for at least minutes, so this is an edge case the design accepts rather than fixes.

## Design decisions

Sub-design choices made inside the proposal, called out so reviewers don't have to reconstruct the reasoning.

### Two counters instead of a single histogram

A histogram of gang sizes per event was considered as a way to collapse "pods evicted" and "events" into one metric. Rejected: Prometheus histograms are expensive in cardinality once multiplied by the label set proposed here, and operators directly want the running total of evicted pods — a histogram does not give that without an extra aggregation step.

### `subgroup` label scoped to the pods counter only

The events counter intentionally omits `subgroup`. Adding it would inflate `sum by (podgroup)` when one decision takes down a subtree spanning multiple leaf SubGroups, and the workload-level question ("how many times was X disrupted") would over-report. Per-subgroup *event* visibility, if ever needed, comes as a separate sibling counter without disturbing this design.

### Leaf-only `subgroup` semantics, no ancestor walk

When a leaf SubGroup is evicted, only the leaf's `(podgroup, subgroup)` series is incremented — no ancestor SubGroup series. Walking ancestors would make `sum by (podgroup)` overcount proportionally to tree depth. The PromQL `sum` aggregation already reconstructs ancestor views from leaf-keyed series with no overcount.

### `uid` removal is a hard break, not a soft deprecation

The `uid` label is dropped rather than kept for one release behind a feature flag. Keeping it would continue to spawn fresh series on every PG recreation and amplify the first-sample-invisible problem that pre-initialization is meant to fix. The migration section above offers a feature-flag transition if maintainers prefer it.

## Implementation

The implementation lives in a follow-up PR; this document covers behavior and contracts only. The work touches three areas:

- **Metrics registration**: update the existing pods counter (label set), add the new events counter, add an initializer that pre-creates series at 0 on PodGroup observation.
- **Eviction emission**: route the pods counter through the existing per-pod emit point with the new labels; emit the events counter once per committed decision, after the scheduler's eviction statement commits.
- **Cache wiring**: invoke the pre-initializer from the PodGroup `OnAdd` and `OnUpdate` handlers (`OnUpdate` only for newly added leaves); invoke series deletion from `OnDelete`.

Test coverage: unit tests for the new label shape, and an integration test that asserts `rate()` and `increase()` return non-zero for a workload that has been evicted exactly once.

A CHANGELOG entry is required for the `uid` removal.
