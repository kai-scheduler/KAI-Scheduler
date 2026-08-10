<!--
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# Deferred Resize Eviction

*Status: Proposed*

Related issue: [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872).
Builds on [in-place pod resize accounting](README.md) (goal 3 there, out of scope).

## Problem

When the kubelet cannot enact an in-place resize because the node lacks
capacity, it marks the Pod `PodResizePending` with reason `Deferred` and
retries whenever node capacity changes. KAI never frees capacity on that
node, so a deferred resize can starve indefinitely even when the node runs
lower-priority or reclaimable workloads that a *new* pod of the same size
would be allowed to displace.

## What the accounting already gives us

Effective-request accounting charges a Deferred resize at
`max(spec, actual)` — the target size — on both the node and the queue:

- **The demand is already reserved.** The node reports `Used > Allocatable`
  for the deferred delta, so `allocate` never places new pods into the gap,
  and the queue's `Allocated` already carries the target. Once victims are
  evicted, the freed capacity cannot be raced away by other KAI workloads —
  this is the thrash guard: evict → kubelet enacts → accounting unchanged.
- **The demand is node-local and scalar.** The pod is bound; nothing needs
  placement. Enactment only needs, per resized resource, that the requests
  remaining on the node after victims terminate fit allocatable. No
  predicates, bin-packing, or gang placement are involved, so the scenario
  simulation machinery (built to answer "will the pending job *fit
  somewhere* after these evictions") is unnecessary; reusing it would mean
  re-placing a bound pod through statements, which the framework does not
  model.

## Design

A new action, `resizeeviction`, disabled by default, running after
`preempt` in the cycle (default priority 150).

### Detection

A task has a deferred resize when its Pod carries
`PodResizePending=True` with reason `Deferred` and the condition's
`observedGeneration` is current (stale conditions from a previous resize
generation are ignored). The informer transform already retains this
condition and the resize-relevant container statuses.

The deferred delta is, per resource, `spec request − actual request`
(clamped at zero) summed over regular containers — CPU and memory only,
matching what in-place resize supports.

### Node shortfall

For deferred task `p` on node `N`:

```text
shortfall(p) = (Used − Releasing − Σ delta(q) for other deferred q on N − Allocatable)
               clamped ≥ 0, masked to resources where delta(p) > 0
```

`Used − Releasing` is what remains after already-evicted pods terminate.
Other deferred targets are excluded because the kubelet has not allocated
them — each deferred pod's enactment is judged against actual allocations.
A zero shortfall means the kubelet can already enact (or will, once
in-flight releases finish) and the task is skipped.

### Victim selection

Deferred tasks are handled one at a time, ordered by job priority (then
creation time). For each, victim candidates are jobs with an allocated task
on the node, eligible under **exactly the rules a new pod of that size
would face**, reusing the existing session callbacks:

- **Preempt semantics (same queue):** victim is preemptible, has lower
  priority than the resizing job, and passes `PreemptVictimFilter`.
- **Reclaim semantics (cross queue):** gated on
  `CanReclaimResources(resizing job)` — with the target already charged to
  the queue, this asks "is the queue within fair share *including* the
  resize?", which is the correct incremental question — and the victim
  passes `ReclaimVictimFilter`.

Victims are consumed from the standard ordered victims queue
(`utils.GetVictimsQueue`), evicting via `Statement.Evict` with
`GetTasksToEvict` batches (elastic tasks first, whole gang when at
`minAvailable`), until the shortfall reaches zero. Statement eviction
updates the node's `Releasing` vector live, so the shortfall is recomputed
from session state after every batch. A batch that frees nothing on the
target node skips that job.

Before commit, the accumulated victim set is validated with the same
scenario validators preempt/reclaim use: same-queue victims against
`PreemptScenarioValidator`, cross-queue victims against
`ReclaimScenarioValidatorFn` (proportion fairness — with a zero additional
request, since the target is pre-charged — and min-runtime protection). If
the shortfall cannot be zeroed or a validator rejects, the statement is
discarded and nothing is evicted — all-or-nothing per resizing pod.

KAI's job ends at freeing capacity: the kubelet re-evaluates deferred
resizes on its own when pods terminate; no re-trigger is needed.

### Configuration

The action is registered but not part of the default actions list.
Operator users enable it per shard:

```yaml
spec:
  actions:
    resizeeviction:
      enabled: true   # default false; priority defaults to 150
```

Raw-config users append `resizeeviction` to the `actions` string.

## Decisions

| Topic | Decision |
| --- | --- |
| Mechanism | New action; do not extend preempt/reclaim (their pipelines assume a pending job with tasks to place) |
| Demand model | Node-local scalar shortfall from live node vectors; no virtual pending task, no scenario simulation |
| Victim rules | Reuse preempt (same-queue) and reclaim (cross-queue) filters + scenario validators verbatim |
| Reclaimer demand for fairness | Zero additional request — the target is already charged to the queue by effective accounting |
| Thrash guard | Effective accounting keeps the target reserved on node and queue across cycles; eviction is all-or-nothing per pod |
| Default | Off; per-shard opt-in via the actions map |

## Known limitations

- Elastic victim jobs surrender tasks in `GetTasksToEvict` order, which may
  pick a task on a different node; such batches are skipped rather than
  retargeted, so an eligible elastic victim on the target node may be
  missed.
- Victim search is greedy in victim-queue order; it does not search for the
  minimal victim set the way scenario generators do. For a scalar
  node-local demand the ordering already dominates the outcome.
- KAI's request-based arithmetic approximates the kubelet's allocated-based
  admission; a discrepancy (e.g. pods without requests) can leave a resize
  deferred after eviction. The action re-evaluates every cycle from fresh
  state, and repeated eviction is bounded by victims actually present on
  the node.
