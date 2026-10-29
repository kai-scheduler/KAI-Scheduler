# Core SubGroups: Marking SubGroups as Core

## Overview

[Semi-preemptible](../semi-preemptible/README.md) podgroups keep a job's **core** (its minimal satisfying set) non-preemptible and in-quota, and treat everything beyond it as elastic. Today the core is **derived**: at every `SubGroupSet` the scheduler picks `minSubGroup` children, ranked *satisfied first, then by name*. The user has no way to say *which* children are core.

This design adds an optional `core` marker to the `SubGroup` spec. A marked subgroup is always part of its parent's core. Unmarked subgroups keep today's behavior, so the field is strictly opt-in.

## Motivation

### Current state

`minSubGroup: 1` over `{decode, prefill}` means "any 1 of 2". Core goes to the first *satisfied* member by name, so `decode` always wins when both are running, and `decode`/`prefill` are interchangeable from the API's point of view.

This causes three problems:

1. **The user cannot express intent.** `SubGroup` carries only `name`, `minMember`, `minSubGroup`, `parent` and `topologyConstraint`. When the roles are not symmetric (prefill is the part that must always exist), there is no way to say so.
2. **Renaming changes protection.** Name is the tie-break, so renaming a subgroup silently changes which one is protected.
3. **The protected charge can flip.** If the siblings cost different amounts (for example 1 GPU vs 8), the quota charged as core changes when the satisfaction order changes, with no admission check. The code already notes this as an alpha limitation (`coreRequiredQuota`).

### Desired state

The user marks the subgroup(s) that must be core:

```yaml
spec:
  preemptibility: "semi-preemptible"
  minSubGroup: 1
  subGroups:
    - name: prefill
      core: true          # always the protected slot
      minMember: 2
    - name: decode
      minMember: 4        # elastic: bursts over-quota, reclaimed as a whole
```

## Goals / Non-Goals

**Goals**
- Let the user designate which children of a `SubGroupSet` hold the core slots.
- Make the core set **deterministic and rename-proof** for the marked slots, so their charge cannot flip.
- Keep the semantics: core is the tree's *minimal satisfying set*. Marking chooses **which** members are in it, never **how large** it is.
- No change for any PodGroup that does not use the field.

**Non-Goals**
- An integer rank / priority per subgroup (see [Alternatives](#alternatives-considered)).
- A third tier of "required but elastic", or protecting everything inside a marked subgroup (only its min shape is core).
- Fixing ungated promotion of a larger pod into a core slot at a leaf (issue 2 in the [open-issues one-pager](../semi-preemptible/open-issues-one-pager.html)). Marks give marked slots a fixed identity but do not gate leaf-level promotion; that needs a separate design.
- Segmented subgroups (`kai.scheduler/segment-size`), same as the semi-preemptible HLD.
- The `minNonPreemptible` future-work field.

## API Changes

### SubGroup

```go
type SubGroup struct {
    // ...existing fields...

    // Core marks this SubGroup as part of its parent's core (minimal satisfying set).
    // Only meaningful when the PodGroup is semi-preemptible.
    // The number of core SubGroups under one parent must not exceed that parent's effective minSubGroup.
    // +kubebuilder:validation:Optional
    Core bool `json:"core,omitempty"`
}
```

- Unset or `false` means today's behavior.
- The field lives on `SubGroup` only. The root is always core, and the root's children are governed by the root's `minSubGroup`.
- The API type ships through `github.com/kai-scheduler/api` and is then repinned in `go.mod` (currently `v0.1.4`), followed by `make manifests` to regenerate the CRD.

### PodGrouper

`SubGroupMetadata` (`pkg/podgrouper/podgroup/metadata.go`) gains `Core bool`, copied into `SubGroup.Core` where `handler.go` builds the PodGroup. Hand-authored PodGroups work on day one. Workload-submitted jobs need per-plugin sourcing, otherwise users of grouped workloads (for example prefill/decode on Grove) cannot reach the field:

1. Grove: a PodClique annotation.
2. Others (Karta mapping and similar): follow-up per plugin.

## Semantics

Let a `SubGroupSet` have children `C`, effective minimum `m` (`minSubGroup`, or `|C|` when unset), and `c` marked children.

**Required set R** = the `c` marked children, plus the first `m - c` unmarked children by today's rule (satisfied first, then name). `|R| = m` always, so the protected charge never exceeds `m` slots.

- **Core**: members of R are core and non-preemptible; the remaining children are elastic and reclaimed as a whole.
- **Min satisfied**: all marked children are satisfied **and** at least `m` children are satisfied. Count alone is no longer enough, identity matters.
- **Recursion**: a marked child that is itself a `SubGroupSet` contributes its own required set (its `minSubGroup` children, marks first). A marked leaf contributes its `minMember` pods. Everything beyond the min shape of a marked subgroup is still elastic.
- **Gang phase** (allocation): collect all unsatisfied marked children first, then unsatisfied unmarked children up to `max(0, m - c - satisfiedUnmarked)`. All of it is charged as core. The elastic phase runs only after min is satisfied and charges 0.
- **Eviction**: a satisfied child outside R is surplus even when the count would otherwise be at the minimum, so an unmarked satisfied sibling can be reclaimed while a marked one is missing. Members of R are never victims.
- **Starting the job**: if a marked subgroup cannot be scheduled, the job does not start, even if unmarked subgroups would fit. "Core" means required.

### Example

`minSubGroup: 1`, `prefill {minMember: 2, core: true}`, `decode {minMember: 4}`:

| State | Today (name rule) | With `core` |
|---|---|---|
| Both pending | Gang phase starts `decode` (first by name) and charges it as core. | Gang phase starts `prefill` and charges it as core. `decode` bursts later at zero charge. |
| `prefill` dies, `decode` runs | Min is satisfied by count, so `decode` becomes core and the charge flips. | Min is not satisfied. `decode` is outside R, so it is surplus and uncharged. `prefill` is rescheduled through the gang phase at core price. |

### Nested example

```yaml
spec:
  preemptibility: "semi-preemptible"
  minSubGroup: 2
  subGroups:
    - name: serving                  # core, its own required set applies below
      core: true
      minSubGroup: 1
    - name: serving-prefill
      parent: serving
      core: true
      minMember: 2
    - name: serving-decode
      parent: serving
      minMember: 4                   # elastic inside a core subgroup
    - name: batch-a
      minMember: 8                   # fills the second root slot
    - name: batch-b
      minMember: 8                   # elastic
```

`serving` and `serving/prefill` are core. `batch-a` takes the remaining slot by the existing rule; `batch-b` and `serving/decode` are elastic.

## Validation

In the PodGroup webhook (`pkg/apis/scheduling/v2alpha2/podgroup_webhook.go`, next to `validateSubGroupMinFields`):

| Rule | Behavior |
|---|---|
| Marked children under a parent exceed that parent's effective `minSubGroup` | **Reject.** Marks must fit inside the minimum, they cannot grow it. |
| `core` is added, removed or moved on a semi-preemptible PodGroup | **Reject.** It changes the protected set without admission, the same class as the existing `minMember`/`minSubGroup` increase rule. |
| `core: true` on a PodGroup whose `preemptibility` is `preemptible` or `non-preemptible` | **Warn and ignore.** The field has no meaning there, and ignoring keeps the mode switchable. |
| Redundant marks (parent `minSubGroup` unset, so all children are required) | **Accept.** |

## Scheduler Changes

| Component | Change |
|---|---|
| `subgroup_info` (`factory.go`, `subgroupset.go`) | Carry the flag from `FromPodGroup`. Make `SubGroupSet` the single source of truth: add `GetRequiredMembers()` and make `IsMinRequirementSatisfied` / `IsReadyForScheduling` identity-aware. |
| `podgroup_info/core_info.go` | `partitionCoreMembers` takes marked children first, then the existing ordering (`coreMemberLess`) for the remaining `m - c` slots. |
| `podgroup_info/allocation_info.go` | Gang phase follows the rule above. |
| `podgroup_info/eviction_info.go` | Phase 2 and `hasElasticSurplus` treat satisfied members outside R as surplus. |
| `proportion`, `capacity_policy` | No code change expected: they read `GetCoreTasks` and `IsMinRequirementSatisfied`. Verify with tests. |
| `status.schedulingState.corePods` | Published set follows R. No new status fields. |

## Backward Compatibility

- Field unset everywhere: behavior is identical to today. The new code paths only branch when at least one child is marked.
- Old schedulers ignore the field (unknown to them); rolling the API first, then the scheduler, then the PodGrouper is safe. A PodGroup with marks processed by an old scheduler simply falls back to the name rule.
- `v2alpha2` only; no conversion needed.

## Alternatives Considered

| Option | Why not |
|---|---|
| **Integer rank** per subgroup | More general, but it weakens "required" (what does rank 3 of `minSubGroup: 2` mean?), removes the marked-count rule, and needs the same scheduler changes. |
| **Nesting only** (express choice by structure, no new field) | Forces users to restructure trees and loses topology/min semantics attached to the existing shape. |
| **Reject asymmetric trees** at admission | Does not give users what they want; it just removes the use case. |
| **Name it `required`** instead of `core` | Matches `minSubGroup`'s vocabulary and says what the scheduler enforces. `core` is preferred because it is the term already used in the design, status (`corePods`) and docs; kept as an open question. |
| **Pin core to the published `CorePods`** | Makes results depend on prior status writes rather than on the spec; already deferred in the semi-preemptible HLD. Marks make this unnecessary for marked slots. |

## Open Questions

1. **Min shape vs everything.** We propose that marking protects only the subgroup's min shape. Protecting "everything inside" adds a third tier (required, protected, elastic) and breaks "core equals the minimal satisfying set".
2. **Name:** `core` vs `required` (and no `is` prefix, per Kubernetes API style).
3. **Immutability.** Reject all changes on semi-preemptible groups, or allow un-marking (which only widens the elastic tier, like decreasing `minMember`)?
4. **Reopens the "no new API fields" goal** of the semi-preemptible HLD. This needs agreement from its reviewers.
5. **Scope of the first PR:** API + webhook only, or API + scheduler together?

## Development Phases

1. **P1 - API and webhook** in `kai-scheduler/api`, plus webhook validation and CRD.
2. **P2 - Scheduler semantics**: `subgroup_info`, `core_info`, `allocation_info`, `eviction_info`; repin `go.mod`.
3. **P3 - PodGrouper plumbing**, Grove first.
4. **P4 - Docs and example** (`examples/semi-preemptible/`, `docs/elastic/README.md`).

## Test Plan

- **Unit**: `SubGroupSet` required set and satisfaction (marks, redundant marks, nested); `partitionCoreMembers`; gang phase collection order; eviction surplus; webhook rules (table-driven).
- **Property**: eviction never returns a member of R, and the gang phase always covers the marked children.
- **Integration** (`pkg/scheduler/actions/integration_tests/preempt/preempt_semi_preemptible_test.go`): reclaim on the prefill/decode example, including "prefill dies, decode runs".
- **E2E** (`test/e2e/suites/preempt/preempt_semi_preemptible_specs.go`): one spec where the marked subgroup is not the first by name and still holds the protected slot after a reclaim.
