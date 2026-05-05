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

Phases 0–5 of the refactor are landed:
- Solve-loop spine, `sessionSimulator`, `accumulatingGenerator`,
  `LegacyValidator` are in place.
- `JobSolver.Solve` runs a single full-gang solve through `v2.Solve` —
  no per-task probing, no binary search.
- The accumulating generator emits per-node subsets followed by a
  full-accumulated-set fallback per accumulation step.
- `byPodSolver` is deleted; its emission logic moved into the generator.

Open follow-ups (not blocking):
- **Native action validators.** Today every action wraps its
  `func(api.ScenarioInfo) bool` validator via `LegacyValidator`, which
  rebuilds a `BaseScenario` from `Scenario.Candidates`. Once each
  action exposes a native `v2.Validator`, the `Candidates` field and
  `LegacyValidator` adapter both go away. The plugin contract
  (`ssn.AddReclaimScenarioValidatorFn`, etc.) needs to migrate too.
- **Tighter generator emissions for gang preemptors.**
  `TestHandleScatteredNodesForGangPreempt` now expects
  `NumberOfPipelineActions: 3` (was 2). Phase 4 collapses the partial-K
  gang loop, so a gang preemptor with cross-node node-affinity falls
  through the per-node emissions and is solved by the full-set
  fallback. That set includes accumulated victims that aren't strictly
  needed (a node-2 victim accumulated before the relevant node-1 and
  node-3 victims), so one extra task gets pipelined as part of
  re-allocation. Correctness preserved; solution-quality regression
  vs. legacy. To restore the tighter set, the generator needs to
  explore subsets of accumulated victims rather than emitting only the
  full set as fallback.
- **`solvers/v2/` → `solvers/` rename.** Mechanical move. Best done
  together with the validator migration to avoid touching every import
  twice.

## Known follow-up

`TestHandleScatteredNodesForGangPreempt` now expects
`NumberOfPipelineActions: 3` (was 2). Phase 4 collapses the partial-K
gang loop, so a gang preemptor with cross-node node-affinity falls
through the per-node emissions and is solved by the full-set fallback.
That set includes accumulated victims that aren't strictly needed for
the placement (a node-2 victim accumulated before the relevant node-1
and node-3 victims), so one extra task gets pipelined as part of
re-allocation.

This is correctness-preserving but a solution-quality regression
relative to legacy. To restore the tighter set, the generator needs to
explore subsets of accumulated victims rather than emitting only the
full set as fallback. Consider doing this when `byPodSolver` is removed
in Phase 5/7.
