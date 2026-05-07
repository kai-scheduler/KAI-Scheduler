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

## Why

### Bug: iter-1 lock-in for multi-task pending jobs

The legacy solver allocated a multi-pod gang **incrementally**. For a
preemptor with N pending tasks it ran an exponential-doubling +
binary search over `k ∈ [1, N]`, calling `solvePartialJob` at each
probe with the first `k` tasks. Each successful probe **committed**
its victim set into `state.recordedVictimsJobs`, and that prefix was
carried into the next probe.

There was no backtracking. If the cheapest victim set for `k=1`
poisoned the search space for `k=2..N`, the solver gave up even
when a feasible plan existed. The reproducer in
`reclaim_test.go::TestHandleReclaim` test 40 ("Reclaim across many
single-GPU nodes") exercises exactly this: a 5-task reclaimer needs
one victim from each of 5 single-GPU nodes, but the binary-search
prefix structure picks `k=1, 2, 4, 5` and skips intermediate `k=3`,
so the final probe never encounters the multi-node victim set.

### Architectural drift

Four interlocking layers had each accumulated cross-cutting
responsibilities:

- `JobSolver.Solve` ran the gang loop **and** owned the
  `recordedVictimsTasks` carry-forward state.
- `PodAccumulatedScenarioBuilder` was a stateful, monotonic
  scenario emitter with filter logic baked in.
- `byPodSolver.solveOnPotentialNodes` ran a *second* search loop
  inside what was nominally per-scenario simulation, with checkpoint
  / rollback against a shared statement.
- `byPodSolver.handleScenarioSolution` ran the action-policy
  validator — but what the validator saw depended on the scenario
  builder's internal accumulation state.

Validation was split across three places: cheap filters pre-sim,
predicates mid-sim, and the action validator post-sim. The action
validator received the scenario's *candidate* set rather than the
actual placement, so its semantics depended on details of the
accumulator's emission order.

## Before

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

The validator received the `ByNodeScenario` accumulated up to that
step, including potentials on nodes that weren't being evicted in
the current per-node attempt. This made `Scenario.GetVictims()` a
mix of post-eviction (`Releasing`/`Pipelined`) and pre-eviction
(`Running`) tasks.

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

Yields candidate scenarios in non-decreasing disruption order. The
production implementation, `accumulatingGenerator`, walks the
victim queue and emits a layered sequence per accumulation step:

1. **Per-node** of the latest victim job's host nodes — the cheapest
   single-node fix.
2. **Pairs** of (one prior host node + one latest host node) — solves
   two-node gang preemptors without dragging unrelated accumulated
   victims along.
3. **Full-set** — recorded ∪ every accumulated potential. Required
   for gangs that span more than two host nodes.

`Solve` takes the first emission whose simulation is feasible and
passes the validator, so the least-disruptive solution wins.

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
- **Multi-task jobs**: different victim choices possible. The reclaim
  case in `reflection-hero/` (the iter-1 lock-in regression test)
  starts succeeding.

### Generator emission order

Per-node emissions are tried before pairs, pairs before the
full-set. Within each tier, host nodes are visited in sorted order
(legacy used Go map iteration, effectively random). Some tests with
specific node-pinning expectations may need expectation updates;
none were observed in this PR.

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
  2-node preemptors. Gangs spanning 3 or more nodes fall through to
  the full-set fallback and may over-evict. Combinatorially
  enumerating larger subsets is the natural extension if a real
  workload demonstrates the regression.

## Code anchors

- Production solver: `pkg/scheduler/actions/common/solvers/`
- Action callers: `pkg/scheduler/actions/{reclaim,preempt,consolidation}/`
- Existing scenario types (still used as generator-internal state):
  `pkg/scheduler/actions/common/solvers/scenario/`
- Existing accumulated filters (still used by the generator):
  `pkg/scheduler/actions/common/solvers/accumulated_scenario_filters/`
