# solvers

Solver stack for reclaim/preempt/consolidation actions.

Three interfaces, one driver:
- `Generator` yields candidate scenarios (pending tasks + victim set).
- `Simulator` evicts the victims and tries to virtually allocate the
  pending tasks; returns a `SimulationResult` plus a live `Statement`.
- `Validator` runs action-specific policy checks on the simulation
  result.
- `Solve(g, sim, val)` loops until a feasible+valid scenario is found.

Scenarios are emitted in non-decreasing disruption order, so the first
valid result is the least-disruptive solution found.

`JobSolver` is the per-action wrapper: it builds the production
generator (`accumulatingGenerator`) and simulator (`sessionSimulator`),
runs `Solve`, and adapts the result to the action's `(bool,
*Statement, []string)` return shape.

## Validators

Each action passes a `Validator` to `NewJobsSolver`:
- **Consolidation** uses a native `Validator` that checks
  `len(r.Preempted) == 0` directly on `SimulationResult`.
- **Preempt** and **reclaim** wrap their plugin-registered
  `func(api.ScenarioInfo) bool` validator with `LegacyValidator`,
  which rebuilds a `BaseScenario` from `Scenario.Candidates` so the
  legacy plugin contract still applies. Native validators that read
  `SimulationResult` directly would let the `Candidates` field and
  `LegacyValidator` adapter both go away — that requires migrating the
  plugin registration API (`ssn.AddReclaimScenarioValidatorFn`, etc.)
  and is left as a follow-up.

## Emission strategy

The accumulating generator emits, per accumulation step, a layered
sequence of victim subsets:

1. **Per-node** of the latest victim job's host nodes — the cheapest
   single-node fix.
2. **Pairs** of (one prior host node + one latest host node) — solves
   two-node gang preemptors without dragging unrelated accumulated
   victims along.
3. **Full-set** — recorded ∪ every accumulated potential. Required for
   gangs spanning more than two host nodes.

`Solve` takes the first emission whose simulation is feasible AND
passes the validator, so the least-disruptive solution wins.
