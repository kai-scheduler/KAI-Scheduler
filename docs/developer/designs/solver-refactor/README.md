# Solver stack refactor

## Summary

The solver stack used by `reclaim`, `preempt`, and `consolidation` is
rewritten around three small interfaces — `Generator`, `Simulator`,
`Validator` — driven by a single `Solve` function. The old four-layer
stack (`JobSolver` → `PodAccumulatedScenarioBuilder` → `byPodSolver`
→ `Scenario`) is replaced; about 700 lines of legacy code are
deleted, the gang-loop binary search is gone, and the iter-1
lock-in bug is fixed.

PR scope: 12 commits, +949 / −1127 net across 18 files.

## Workload shape

Typical podgroups are distributed training/inference jobs — a
leader plus one or two worker templates, scaling out to hundreds
of pods that are *replicas* of those few templates: identical
resource requests, predicates, and affinity rules. The solver
treats every pod independently today, but template-level
equivalence classes can shrink the effective candidate space
substantially. Predicate evaluation (one victim's predicates ≡
another victim's of the same template) and placement (one pod's
`feasibleNodes` ≡ all its template-siblings') both deduplicate
cleanly along template boundaries; the same insight powers Borg's
equivalence-class scoring caches (Verma et al., EuroSys 2015).
Several of the open follow-ups below become much cheaper under
this assumption.

## Background

The pre-refactor stack ran each `reclaim` / `preempt` /
`consolidation` action through a four-layer pipeline:

```
                  ┌─────────────────────┐
   action  ────►  │      JobSolver      │   gang loop (binary search
                  │       .Solve        │   over k = 1..N), records
                  └────────┬────────────┘   victim prefix per probe
                           │
                           ▼
                  ┌─────────────────────┐
                  │  PodAccumulated     │   monotonic, single-chain
                  │  ScenarioBuilder    │   filter + emit
                  └────────┬────────────┘
                           │
                           ▼
                  ┌─────────────────────┐
                  │     byPodSolver     │   per-node iteration
                  │       .solve        │   + checkpoint/rollback
                  └────────┬────────────┘   + validator call
                           │
                           ▼
                  ┌─────────────────────┐
                  │  ByNodeScenario /   │   carries pending +
                  │  BaseScenario       │   victims + recorded +
                  └─────────────────────┘   per-node bucketing
```

The stack reads as ad-hoc branch-and-bound where the "branches"
(per-`k` probes) shared mutable state with the search driver.
Every level both produced new candidates and committed results
into a parent context — neither backtracking cleanly nor
expressing its own search order independently.

A typical reclaim of an N-task preemptor:

```go
// JobSolver.Solve
maxK := s.searchMaxSolvableK(...)        // O(log N) probes;
                                         // each commits victims
                                         // into state on success
result := s.probeAtK(ssn, &state, ..., N) // final probe with full N

// solvePartialJob (called by every probe)
builder := NewPodAccumulatedScenarioBuilder(ssn, partialJob, recorded, ...)
for s := builder.GetValidScenario(); s != nil; s = builder.GetNextScenario() {
    bp := newByPodSolver(...)
    result := bp.solve(ssn, s)             // per-node iteration
    if result.solved {                     // inside; validator
        return result                       // called from within
    }
}
```

### Layer responsibilities

Each layer carried multiple, cross-cutting concerns:

- **`JobSolver.Solve`** — entry per pending job. Ran the gang
  loop **and** owned the `recordedVictimsTasks` carry-forward
  state across probes.
- **`PodAccumulatedScenarioBuilder`** — stateful, monotonic
  scenario emitter, with filter logic baked into accumulation.
- **`byPodSolver.solveOnPotentialNodes`** — per-scenario inner
  search with checkpoint/rollback against a shared statement.
- **`byPodSolver.handleScenarioSolution`** — ran the
  action-policy validator from inside that inner loop. What the
  validator saw depended on the scenario builder's internal
  accumulation state.

Validation alone was split across three places: cheap filters
pre-sim, predicates mid-sim, and the action validator post-sim.
The action validator received the scenario's *candidate* set
rather than the actual placement — so its semantics depended on
emission order. The `ByNodeScenario` it inspected was a mix of
post-eviction (`Releasing`/`Pipelined`) and pre-eviction
(`Running`) tasks.

## Motivation

### Architecture: layers had grown into each other

Each layer mutated state owned by another (see *Background*), so
testing any one in isolation required reproducing most of the
stack. Adding a new generator strategy or simulator backend was
effectively impossible without refactoring all four layers in
lockstep. The refactor pins each concern to a single layer behind
a narrow interface — see *Goals and non-goals*.

### Bug: iter-1 lock-in for multi-task pending jobs

The legacy solver allocated a multi-pod gang **incrementally**. For a
preemptor with N pending tasks it ran an exponential-doubling +
binary search over `k ∈ [1, N]`, calling `solvePartialJob` at each
probe with the first `k` tasks. Each successful probe **committed**
its victim set into `state.recordedVictimsJobs`, and that prefix was
carried into the next probe.

There was no backtracking. If the cheapest victim set for `k=1`
poisoned the search space for `k=2..N`, the solver gave up even
when a feasible plan existed. This is a textbook
greedy-prefix-commit pathology — the same shape appears in
Volcano's `reclaim` action (priority-queue greedy with no
global-disruption backtracking) and in the k8s default scheduler's
preemption (mitigated there by single-step `reprievePod` rescue).
Globally lock-in-free production schedulers exist (Firmament,
[OSDI 2016](https://www.usenix.org/system/files/conference/osdi16/osdi16-gog.pdf),
re-solves a min-cost flow each cycle), but at substantially higher
engineering cost.

The reproducer in `reclaim_test.go::TestHandleReclaim` test 40
("Reclaim across many single-GPU nodes") exercises exactly this: a
5-task reclaimer needs one victim from each of 5 single-GPU nodes,
but the binary-search prefix structure picks `k=1, 2, 4, 5` and
skips intermediate `k=3`, so the final probe never encounters the
multi-node victim set.

## Desired properties

The properties below are the long-horizon target for the solver
stack. This refactor delivers some outright; others are natural
follow-ups. The *Goals and non-goals* section that follows
crystallizes which is which for the current change.

### Clearly defined, decoupled components

`Generator`, `Simulator`, `Validator` are independently testable
and substitutable. State lives inside per-call `Statement`s, not
in the search driver. Delivered by this refactor.

### Good performance at our target scale

- **1,000s of nodes** and **1,000s of jobs** per scheduling cycle.
- **Distributed jobs have low template variety** (see *Workload
  shape*) — leader plus a small number of worker templates,
  replicated. Algorithms should deduplicate per-template, not
  per-pod.

The cardinality-first generator targets the common case (gangs
spanning 1–2 host nodes) cheaply. The open follow-ups (pre-flight
static feasibility, failure-mode caching) scale with templates
rather than pods.

### Best solutions for reclaim, by multiple criteria

Reclaim quality is multi-dimensional and the criteria don't
combine into one natural ordering:

- **Fairness** — respect queue guarantees and weights.
- **Bin packing** — favor scenarios that consolidate workloads.
- **Topology** — preserve NVLink islands, NUMA locality.
- **Affinities** — honor pod (anti-)affinity and
  taint/toleration constraints.
- **Minimal cluster disruption** — fewer evictions when possible.
- **Priority-aware victim selection** — prefer disrupting
  lower-priority jobs first.

The path forward is a **pluggable scenario cost function** — a
`ScenarioScorer` plugin that actions compose from per-criterion
contributions, replacing the current implicit cardinality-first
ordering. See *Open follow-ups*.

### Adaptable to evolving definitions of "good"

The cost function must evolve as the cluster does:

- **Node or device utilization** as a first-class objective.
- **Device-level topology** — GPU-to-GPU NVLink/PCIe distance,
  not just node-level.
- **Varying dominant resources** — clusters where memory,
  network, or storage become the bottleneck instead of GPU.

The plugin shape (per-criterion scorers composed by the action)
keeps these additive: new criteria don't perturb existing ones.

### Adaptable to job types

Different job classes warrant different scenario-generation
strategies:

- **Strict topology gangs** (e.g., NVLink-island affinity) —
  enumerate viable placement domains first, derive the minimum
  victim set per domain. The default victim-set-first generator
  is inappropriate here; see *Topology-first generation* in
  *Open follow-ups*.
- **Single-task preemptors** — already special-cased to skip the
  pair and full-set tiers (one pod can only land on one node).
- **Elastic / partial-gang jobs** — out of scope for the current
  all-or-nothing simulator, but the `Generator`/`Simulator` split
  doesn't preclude a future elastic simulator alongside the
  current one.

## Goals and non-goals

### Goals

- **Decouple the four layers** behind narrow, single-purpose
  interfaces (`Generator`, `Simulator`, `Validator`).
- **Prevent responsibility leakage** — no layer reads or mutates
  state owned by another. Each `Simulate` call runs as a
  speculative transaction in its own `Statement`.
- **Independent testability** — each layer can be exercised in
  isolation with a fake or stub.
- **Extendability** — alternative generators (topology-first,
  best-first, IMP-style cardinality-`k`), simulators, or validators
  plug in without touching the others.
- **Find good solutions quickly for typical workloads.** Per
  *Workload shape*, gangs typically span a small number of host
  nodes; the cardinality-first generator targets the common cases
  cheaply.
- **Fix iter-1 lock-in** — no greedy commits in the search
  driver; each candidate is evaluated independently.

### Non-goals

- **Exhaustive enumeration of feasible victim subsets.** Finding
  every possible solution is explicitly *not* the target. Gangs
  spanning 3+ host nodes fall through to a full-set fallback that
  may over-evict. Adding cardinality-`k` enumeration is left to
  *Open follow-ups*, gated on a real workload demonstrating need.
- **Globally optimal disruption cost.** Emission order is
  cardinality-first, not cost-optimal. The plugin-driven scoring
  follow-up is the path here if it becomes load-bearing.
- **Replacing the action-policy validator API.** Plugin-registered
  `func(api.ScenarioInfo) bool` validators continue to work via
  `LegacyValidator`. Migrating that contract is separate work.
- **Graceful partial-gang allocation.** The simulator is
  all-or-nothing per scenario: either the full pending set fits on
  top of the candidate victim set, or the scenario is rejected.

## After

```
   action  ────►  ┌─────────────────────┐
                  │      JobSolver      │   thin wrapper: build
                  │       .Solve        │   gen/sim/val, call Solve
                  └────────┬────────────┘
                           │
                           ▼
                  ┌─────────────────────┐
                  │  Solve(g, sim, val) │   for s, ok := g.Next(); ok; … {
                  │                     │     r := sim.Simulate(s)
                  │                     │     if !r.Feasible        { continue }
                  │                     │     if !val.Validate(s,r) { continue }
                  │                     │     return s, r, true
                  │                     │   }
                  └────────┬────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
  │  Generator  │    │  Simulator  │    │  Validator  │
  ├─────────────┤    ├─────────────┤    ├─────────────┤
  │ Next()      │    │ Simulate(s) │    │ Validate(   │
  │  → Scenario │    │  → Result   │    │   s, r) bool│
  │             │    │             │    │             │
  │ accumulating│    │ session     │    │ native +    │
  │ Generator   │    │ Simulator   │    │ Legacy      │
  │ (per-node + │    │ (one        │    │ Validator   │
  │  pairs +    │    │  Statement  │    │ adapter)    │
  │  full-set)  │    │  per call)  │    │             │
  └─────────────┘    └─────────────┘    └─────────────┘
```

This is **best-first search over scenarios**: the generator is the
enumeration source, the simulator is the per-candidate evaluator,
and the validator is a post-evaluation policy filter. Each
`Simulate` call runs as a speculative transaction in its own
`Statement` — committed by the caller on success, discarded on
failure — so the search driver owns no mutable state between
candidates.

### `Scenario`

A flat candidate plan. No "recorded" carry-forward, no per-node
bucketing in the type — those are implementation details of
generators.

```go
type Scenario struct {
    Preemptor  *podgroup_info.PodGroupInfo
    Pending    []*pod_info.PodInfo  // tasks of Preemptor to place
    Victims    []*pod_info.PodInfo  // tasks the simulator should evict
    Candidates []*pod_info.PodInfo  // broader pool used by LegacyValidator;
                                    // empty when the validator reads
                                    // SimulationResult directly
}
```

### `Generator`

Yields candidate scenarios in a layered, cardinality-first order.
The shape is a node-locality-aware variant of **Incremental Minimal
Preemption** (IMP, [arxiv:2411.11560](https://arxiv.org/abs/2411.11560)):
enumerate victim subsets of increasing cardinality until one is
feasible, where cardinality is *host nodes touched*, not victim
tasks.

The production implementation, `accumulatingGenerator`, walks the
victim queue and emits, per accumulation step:

1. **Per-node** — the latest victim job's host nodes, individually.
   Cheapest when one node's victims suffice.
2. **Pairs** — (one prior host node + one latest host node). Solves
   two-node gang preemptors without dragging unrelated accumulated
   victims along.
3. **Full-set** — recorded ∪ every accumulated potential. Required
   for gangs spanning 3+ host nodes; over-evicts there
   (see Open follow-ups).

A cheap stateless prune (`emissionFits`) drops emissions whose
freed-GPU pool cannot possibly host the gang, so most pre-tier
candidates never reach the simulator. Within each tier, host nodes
are visited in lexicographic order.

The ordering is monotone in *cardinality*, not in *absolute
disruption cost*: a per-node emission may touch a high-priority
job while a later pair emission could touch two low-priority ones.
`Solve` takes the first feasible-and-validated emission, so the
cardinality-minimal feasible scenario wins — but no global cost
minimum is guaranteed. Replacing the implicit ordering with an
explicit `ScenarioScorer` plugin (admissible lower bound + post-sim
score) would turn `Solve` into A\* over scenarios; see Open
follow-ups.

### `Simulator`

Runs the expensive work for one scenario: opens a fresh
`Statement`, evicts the victims, calls the existing
`TryToVirtuallyAllocatePreemptorAndGetVictims` primitive, and
returns the result. On feasibility the live `Statement` is handed
back to the caller; on infeasibility it is discarded. Each call is
independent — no checkpoint/rollback dance, no per-call mutation of
shared state.

```go
type SimulationResult struct {
    Feasible  bool
    Placement map[*pod_info.PodInfo]*node_info.NodeInfo
    Preempted []*pod_info.PodInfo  // status=Releasing post-sim
    Pipelined []*pod_info.PodInfo  // status=Pipelined post-sim
    Statement *framework.Statement // non-nil on Feasible
}
```

### `Validator`

```go
type Validator interface {
    Validate(Scenario, SimulationResult) bool
    Name() string
}
```

- **Consolidation** uses a native `Validator` that reads
  `SimulationResult.Preempted` directly:
  `len(r.Preempted) == 0` means every victim was re-homed.
- **Preempt** and **reclaim** wrap their plugin-registered
  `func(api.ScenarioInfo) bool` validator with `LegacyValidator`,
  which rebuilds a `BaseScenario` from `Scenario.Candidates` so the
  plugin contract is unchanged.

## Behavior changes

### Gang scheduling: incremental greedy → full-gang one-shot

For min-member > 1 jobs, the simulator now tries to fit the entire
gang on top of every candidate victim set. There is no per-task
probing.

- **Min-member = 1 jobs**: behavior unchanged.
- **Multi-task jobs**: different victim choices possible. The
  iter-1 lock-in regression test (`TestHandleReclaim` test 40,
  "Reclaim across many single-GPU nodes") starts succeeding.

### Generator emission order

Per-node emissions are tried before pairs, pairs before the
full-set. Within each tier, host nodes are visited in lexicographic
order (legacy used Go map iteration, effectively random). For
single-task preemptors only the per-node tier is emitted — pair
and full-set emissions can't expand the placement options of a
1-task pod. Tests with specific node-pinning expectations may need
expectation updates; none were observed in this PR.

### Scenario validator's view

Plugin-registered validators (preempt, reclaim) keep their legacy
view via `LegacyValidator`, which passes them
`Scenario.Candidates` (the full accumulated pool) wrapped in a
`BaseScenario`. No semantic change.

The native consolidation validator switches from "any task in the
scenario victim list is `Releasing`" to
`len(SimulationResult.Preempted) == 0`. These are equivalent
because the legacy iteration treated `Running` tasks (non-evicted
potentials on other nodes) as a no-op, leaving the check
effectively `any actually-evicted task is Releasing`.

## Risks

### Simulator-call inflation from unfiltered emissions

The generator can emit `O(host_nodes²)` pair candidates per
accumulation step, and each simulator call is expensive (eviction
+ virtual allocation against a fresh `Statement`). Without
filtering, the per-cycle simulation count can dominate the
scheduling budget on large clusters.

Mitigations in place:
- **`emissionFits`** — stateless idle-GPU lower-bound check that
  rejects emissions whose freed-GPU pool cannot possibly host the
  gang, before the simulator sees them.
- **`accumulated_scenario_filters` pipeline** — node-affinity and
  topology-aware idle-GPU filters at the scenario level.
- **Pair tier gated on multi-task gangs** — single-task preemptors
  emit only the per-node tier, avoiding the pair quadratic.

If emissions still outrun the simulation budget on real workloads,
the next mitigations are pre-flight static feasibility and
failure-mode caching (see *Open follow-ups*); both scale
per-template per *Workload shape*.

### One-shot gang vs incremental greedy

The legacy gang loop probed `k = 1, 2, 4, ..., N` and could retain
the largest feasible `k` as a partial allocation. The new
simulator is all-or-nothing: the entire pending set fits or
nothing fits.

- **Strict gang semantics is correct** for min-member=N jobs —
  partial allocations don't help a gang that requires N tasks
  running together.
- **Different victim choices** — full-gang simulation may pick a
  different victim set than the legacy partial-gang fixed-point
  would. The iter-1 lock-in fix relies on this, and is the
  intended outcome.
- **Larger per-call cost, far fewer calls** — each simulation
  places the whole gang, but there's no exponential probing over
  `k` and no per-node inner loop with checkpoint/rollback.
- **No "almost fit" fallback** — if a gang of N can fit N-1 tasks
  but not all N, the new solver returns infeasible where the
  legacy could have reported partial progress on the first `k`
  tasks. Intended for gang jobs; flagged here so the difference
  from legacy behavior is explicit.

### Validator equivalence not pinned by a test

The consolidation validator's switch from "any victim is
`Releasing`" to `len(Preempted) == 0` relies on legacy iteration
treating `Running` tasks as a no-op. The equivalence holds today
but isn't asserted by a dedicated regression test.

## Test impact

All `pkg/scheduler/...` suites are green.

- **TestHandleReclaim** test 40 ("Reclaim across many single-GPU
  nodes") now passes — this is the iter-1 lock-in regression test
  added before the refactor.
- **TestHandleScatteredNodesForGangPreempt** unchanged — keeps
  `NumberOfPipelineActions: 2`. The pair-emission tier finds the
  tight 2-victim set (`{job-1, job-3}`) before the full-set
  fallback would over-evict.
- All other reclaim, preempt, and consolidation suites unchanged.

## Migration path

The refactor landed in seven phases, each shippable on its own:

| Phase | Description | Behavior change |
|-------|-------------|-----------------|
| 0 | Define `Generator` / `Simulator` / `Validator` interfaces, `Solve` driver | none |
| 1 | `sessionSimulator` wrapping the existing eviction + virtual-allocate body | none |
| 2 | `accumulatingGenerator` ported from `PodAccumulatedScenarioBuilder` | none |
| 3 | `JobSolver.Solve` wires through `Solve(gen, sim, val)`; gang loop still runs outside | none |
| 4 | Collapse the gang loop; single full-gang solve | min-member > 1 jobs may pick different victim sets; iter-1 lock-in fixed |
| 5 | Delete `byPodSolver` and helpers (≈ 300 lines) | none |
| 6 | `JobSolver` accepts `Validator` natively; consolidation gets a native validator | none |
| 7 | Merge `solvers/v2/` into `solvers/`; delete `PodAccumulatedScenarioBuilder` | none |

## Open follow-ups

- **Native validators for preempt and reclaim.** Today they wrap
  their plugin-registered `func(api.ScenarioInfo) bool` validator
  with `LegacyValidator`. Native `Validator` implementations that
  read `SimulationResult` directly would let the `Candidates`
  field and the `LegacyValidator` adapter both go away. This
  requires migrating the plugin registration API
  (`ssn.AddReclaimScenarioValidatorFn`, etc.) to a `Validator`-shaped
  contract.
- **Tighter generator emissions for 3+ node gangs.** The current
  tiering (per-node → pairs → full) optimally handles 1- and
  2-node preemptors. Gangs spanning 3+ host nodes fall through to
  the full-set fallback and may over-evict. The natural extension
  is full IMP-style cardinality-`k` enumeration with `emissionFits`
  pruning; bounded `C(m, k)` for small accumulated `m`. Empirically
  the IMP paper reports k≤2 covers the bulk of cases, which matches
  the current tier shape — the gap is real but rare in practice.
- **Explicit scenario scoring / best-first search.** The generator's
  cardinality-first ordering is a fixed approximation. A
  `ScenarioScorer` plugin (admissible lower bound for pre-sim
  pruning, plus post-sim score) would turn `Solve` into A\* over
  scenarios and let actions express disruption cost directly. Same
  pattern as Volcano's `NodeOrderFn` / `TaskOrderFn` plugins, but
  at the scenario layer.
- **Topology-first generation.** For topology-constrained gangs
  (e.g., NVLink-island affinity), the current victim-set-first
  enumeration mostly emits scenarios that don't free a coherent
  placement and is dominated by the full-set fallback. Inverting
  the search — enumerate viable topology domains first, derive the
  minimum victim set per domain — collapses the search space from
  `O(2^running_tasks)` to `O(islands)`.
- **Pre-flight static feasibility.** When a job is unschedulable
  for static reasons (PVC mismatch, taints, missing GPU type), the
  current solver rediscovers it once per emitted scenario.
  Strengthening `feasibleNodes` to include the pending pods'
  static-only predicates short-circuits the entire search before
  the generator is constructed. Per *Workload shape*: this runs
  once per template, not once per pod.
- **Failure-mode caching across scenarios.** The simulator returns
  only `Feasible: false` on rejection. Surfacing *why*
  (`(template, node, predicate)` triples — keyed by template, not
  by pod, per *Workload shape*) would let the driver skip
  scenarios whose freed-node set is a subset of an already-failed
  set. Standard no-good learning from CSP solvers.

## Code anchors

- Production solver: `pkg/scheduler/actions/common/solvers/`
- Action callers: `pkg/scheduler/actions/{reclaim,preempt,consolidation}/`
- Existing scenario types (still used as generator-internal state):
  `pkg/scheduler/actions/common/solvers/scenario/`
- Existing accumulated filters (still used by the generator):
  `pkg/scheduler/actions/common/solvers/accumulated_scenario_filters/`
