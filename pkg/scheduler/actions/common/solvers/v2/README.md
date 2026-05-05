# solvers/v2

Refactored solver stack for reclaim/preempt/consolidation actions.

Three interfaces, one driver:
- `Generator` yields candidate scenarios (pending tasks + victim set).
- `Simulator` evicts the victims and tries to virtually allocate the
  pending tasks; returns a `SimulationResult` plus a live `Statement`.
- `Validator` runs action-specific policy checks on the simulation
  result.
- `Solve(g, sim, val)` loops until a feasible+valid scenario is found.

Scenarios are emitted in non-decreasing disruption order, so the first
valid result is the least-disruptive solution found.

## Migration status

The refactor is feature-complete:
- Solve-loop spine, `sessionSimulator`, `accumulatingGenerator`,
  `LegacyValidator` are in place.
- `JobSolver.Solve` runs a single full-gang solve through `v2.Solve` —
  no per-task probing, no binary search.
- `byPodSolver` is deleted; its per-node iteration logic moved into
  the generator's emission step.

Open follow-ups (not blocking):
- **Native action validators.** Today every action wraps its
  `func(api.ScenarioInfo) bool` validator via `LegacyValidator`, which
  rebuilds a `BaseScenario` from `Scenario.Candidates`. Once each
  action exposes a native `v2.Validator`, the `Candidates` field and
  `LegacyValidator` adapter both go away. The plugin contract
  (`ssn.AddReclaimScenarioValidatorFn`, etc.) needs to migrate too.
- **`solvers/v2/` → `solvers/` rename.** Mechanical move. Best done
  together with the validator migration to avoid touching every import
  twice.

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

Solve takes the first emission whose simulation is feasible AND passes
the validator, so the least-disruptive solution wins.
