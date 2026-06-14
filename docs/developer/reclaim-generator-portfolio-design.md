# Reclaim Bounded Scenario Generator Portfolio

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [Limitations/Risks and Mitigations](#limitationsrisks-and-mitigations)
- [Design Details](#design-details)
  - [Terminology](#terminology)
  - [Shared Invariants](#shared-invariants)
  - [Mechanism](#mechanism)
  - [Generator Abstraction](#generator-abstraction)
  - [Plugin Registration and Ordering](#plugin-registration-and-ordering)
  - [Driver Loop and Budget](#driver-loop-and-budget)
  - [Initial Shipped Plugin Policy](#initial-shipped-plugin-policy)
  - [Possible Future Generators](#possible-future-generators)
  - [Approximation Contract](#approximation-contract)
  - [Integration Posture](#integration-posture)
  - [Scale-Test Walkthrough](#scale-test-walkthrough)
  - [Relationship to Necessary-Condition Checks](#relationship-to-necessary-condition-checks)
- [Monitoring](#monitoring)
- [Test Plan](#test-plan)
- [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

This proposal replaces unbounded reclaim scenario enumeration with a bounded, plugin-registered generator portfolio. Generators propose concrete victim scenarios best-first, while the existing simulator and post-simulation validator remain the authority for accepted solutions. The first policy runs a cheap node-local generator before the existing multi-node gang generator, all under alpha wall-clock budgets. This intentionally bounds scheduler time rather than trying to prove every negative reclaim case, because victim selection is a knapsack-shaped combinatorial search.

## Motivation

The current reclaim path can spend unbounded synchronous scheduler time trying to prove that no valid victim set exists. The scale-test failure is a negative case: the pending job is unschedulable by construction, but reclaim drains a wide scenario search before giving up. The bounded generator portfolio makes that failure mode bounded and observable while preserving the safety property that any accepted reclaim solution is fully simulated and validator-approved.

### Goals

- Bound pathological reclaim, preempt, and consolidation scenario search by wall-clock time.
- Keep every accepted solution validator-approved; never accept a scenario from a shortcut alone.
- Preserve the #1537 gang/topology correctness fix by keeping whole victim gangs intact.
- Restore fast common-case behavior with a narrow `NodeLocalGreedy` generator before wider search.
- Let future case-specific generators register through plugins without changing the shared solver driver.
- Expose all new search-budget and generator-selection controls as alpha/experimental only.
- Add production metrics that show budget use, generator work, scenario outcomes, and reduced-budget jobs.

### Non-Goals

- Do not prove complete unschedulability for reclaim. General victim selection is a hard combinatorial problem.
- Do not expose victim-count, node-count, victim-by-node, or scenario-count work-unit budgets.
- Do not make the generator plugin API or per-generator args a stable user-facing contract in Phase 1.
- Do not move heavy search off the synchronous scheduling path in Phase 1.
- Do not include replay, benchmark, or debug-only metric schemas in the production metric contract.

## Proposal

Move scenario generation behind a bounded generator portfolio owned by the shared `JobSolver` path used by reclaim, preempt, and consolidation. Each applicable generator yields `ByNodeScenario` candidates incrementally. The driver deduplicates scenarios, simulates candidates through the existing solver, validates accepted solutions with the existing post-simulation validator, and stops when a solution is found, all generators are exhausted, or the effective time budget expires.

The initial portfolio is:

| Plugin order | Generator | Purpose |
| --- | --- | --- |
| 1 | `NodeLocalGreedy` | Restore the cheap pre-#1537 node-local scenario shape for common cases and the scale-test failure. |
| 2 | `MultiNodeGang` | Wrap today's wide accumulated scenario builder while preserving the #1537 whole-gang behavior. |

The negative result is intentionally approximate when produced by the bounded portfolio. If the budget expires or the registered generators do not cover the shape, the scheduler may report no solution even if a solution exists. This is acceptable only because accepted positives remain fully simulated and validator-approved, and because the feature is explicitly bounded and observable.

### Limitations/Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| False negative when a valid scenario exists after the budget expires. | Report deadline/generator exhaustion through metrics; keep the feature alpha; add future generators based on observed misses. |
| A job can consume most of the action budget before later jobs run. | Support `maxJobSearchMillis` and optional `minJobSearchMillis` with default `0`; mark jobs reduced-budget only when they receive less configured time. |
| Generator ordering can hide useful later generators. | Derive ordering from plugin registration order and expose it as alpha/experimental policy. |
| `MultiNodeGang` changes could regress #1537 gang/topology correctness. | Wrap the existing builder/emitter and keep #1537 coverage; topology-specific generators may also preserve the same correctness case later. |
| Budget metrics can grow unbounded as cumulative counters/histograms. | Document that Prometheus `_sum` and `_count` series are cumulative and should be queried with `rate()` or `increase()`. |
| Experimental knobs become an accidental compatibility contract. | Mark all new generator-selection and time-budget args alpha/experimental in the design and implementation docs. |

## Design Details

### Terminology

- **Probe size (`probeSize`)**: number of pending tasks from the job that `searchMaxSolvableK` asks the solver to place in the current probe.
- **Node-prefix size (`nodePrefixSize`)**: number of ordered candidate victim nodes included by an emitter in one candidate scenario.
- **Scenario**: one concrete victim set to evict or reclaim before trying to place the current `probeSize`.
- **Simulation**: the virtual allocation attempt after applying one scenario.
- **Generator**: a component that proposes scenarios. It does not simulate them.
- **Deadline / budget**: wall-clock time limits for action, job, and generator search.
- **Reduced budget**: a job received less reclaim-search time than configured because the action-level budget was already depleted.

### Shared Invariants

1. Every accepted solution is fully simulated and validator-approved.
2. Bounded-portfolio negative results can be approximate; they are not complete unschedulability proofs.
3. Victim batches remain gang-preserving units, including multi-node jobs from the #1537 fix.
4. `Solve` remains all-or-nothing; no partial-probe state is committed unless the full job is solved.
5. The post-simulation validator remains the final authority for consolidation, proportion, and other post-eviction side effects.

### Mechanism

`solvePartialJob` keeps its skeleton. The scenario source changes from a single exhaustive emitter to an ordered portfolio of generators. The portfolio owns generator iteration, action filtering, budget checks, and stop reasons. The existing scenario simulation cache remains in the driver to avoid repeated virtual allocations for duplicate victim sets.

When the driver reaches the effective deadline without a validated solution, the action reports "no solution" as an incomplete result. The result reason must distinguish at least deadline exhaustion, generator exhaustion, no applicable generator, and not-attempted jobs so metrics and reduced-budget messages are accurate.

### Generator Abstraction

```go
// A ScenarioGenerator proposes concrete candidate victim sets, best-first, cheaply.
// It performs no simulation; it only decides which victim sets are worth trying.
type ScenarioGenerator interface {
    Name() string
    Next() *scenario.ByNodeScenario // nil when exhausted
}
```

A generator is built per probe from a shared solve context: partial pending job, recorded victims, feasible nodes, victim queue, gang constraints, topology constraints, and action type.

### Plugin Registration and Ordering

`NewJobsSolver`, `solvePartialJob`, and scenario generation are shared by reclaim, preempt, and consolidation. The proposal adds a session extension point that lets plugins register generators for one or more action types:

```go
type ScenarioGeneratorFactory func(ctx *solvers.SolveContext) solvers.ScenarioGenerator

func (ssn *Session) AddScenarioGenerator(
    f ScenarioGeneratorFactory,
    applies ...framework.ActionType,
)
```

Generator order is derived from scheduler plugin execution order. `OpenSession` calls plugin `OnSessionOpen` hooks in configured plugin order, and each hook appends its generators by calling `AddScenarioGenerator`. If a plugin registers multiple generators, their relative order is the order of its `AddScenarioGenerator` calls. `JobSolver.solvePartialJob` filters registered generators by the current action and drains them in registration order.

The first version does not promise a stable user-facing API for choosing scenario builders or tuning search effort. Builder selection and all new search-budget knobs are alpha/experimental controls for KAI development, support, and experiments.

### Driver Loop and Budget

```go
budget := newSearchBudget(actionDeadline, jobDeadline, generatorDeadlines)
portfolio := newScenarioPortfolio(ssn, partialPendingJob, state, feasibleNodeMap, budget)
cache := newScenarioSimulationCache()

for sc := portfolio.Next(); sc != nil; sc = portfolio.Next() {
    if !cache.shouldSimulate(sc) {
        continue
    }
    result := scenarioSolver.solve(ssn, sc)
    if result.solved {
        return result
    }
}

return nil // deadline/generators exhausted: approximate no solution
```

The budget model is time-only:

| Budget | Unit | Where set | Contract |
| --- | --- | --- | --- |
| Action deadline | wall-clock time, for example `maxActionSearchMillis` | alpha action / solver config | the action stops scenario search after this time and moves on |
| Job deadline | wall-clock time, for example `maxJobSearchMillis` | alpha action / solver config | one pending job cannot consume the whole action indefinitely |
| Minimum job attempt | wall-clock time, for example `minJobSearchMillis`, default `0` | alpha action / solver config | optional floor for jobs that would otherwise receive no reclaim attempt |
| Generator deadline | wall-clock time, for example `maxGeneratorSearchMillis` | alpha generator config | one generator cannot consume the whole job indefinitely |

The effective deadline for any candidate is normally the minimum remaining time across action, current job, and current generator. `minJobSearchMillis` is an optional fairness floor, disabled by default. When set above `0`, the action should preserve or allocate that much time for pending jobs that have not yet received a reclaim attempt. If the action budget is too small to satisfy every floor, any job that receives less than its configured floor is marked as reduced-budget.

Internal work-unit budgets such as victim-count, node-count, victim-by-node products, and per-generator scenario caps are not exposed. Generators must yield candidates incrementally so the driver can check the effective deadline between candidates. One simulation may finish after the deadline if it started just before the deadline; the loop must not start another candidate after the effective deadline has expired.

### Initial Shipped Plugin Policy

| Plugin order | Generator | Restores / covers | Width |
| --- | --- | --- | --- |
| 1 | `NodeLocalGreedy` | recorded victims plus one candidate node's victims, candidate nodes best-fit ordered; restores the pre-#1537 `solveOnPotentialNodes` shape | narrow |
| 2 | `MultiNodeGang` | today's `PodAccumulatedScenarioBuilder` plus `subScenarioEmitter`, time-limited by the effective deadline while preserving #1537 gang/topology correctness | wide |
| later | plugin hook | new case-specific generators | case-specific |

`NodeLocalGreedy` is expected to handle the common single-pod-per-node reclaimee case and the known scale-test failure. `MultiNodeGang` remains necessary for true gangs that need several nodes freed simultaneously. A topology-specific generator may later preserve the same correctness case more directly, but #1537 regression coverage remains required either way.

### Possible Future Generators

| Generator option | Covers | Notes |
| --- | --- | --- |
| `AggressiveOneShot` | cases where the narrow path failed but one large direct scenario may solve quickly | bounded by generator time budget |
| `TopologyFirst` | topology-required gangs vs. fully packed topology domains | enumerate viable topology domains, then derive the minimum victim set per domain |
| `FullNodeFirst` | whole-node workloads where each pending task needs an entire node | derive scenarios around freeing complete nodes before generic multi-node search |
| `DisruptionBounded` | user-understandable disruption limits, for example avoiding victim sets larger than 2x or 3x the pending job size | implemented as generator policy, not generic solver work-unit budget |

### Approximation Contract

- Incomplete by design: may report no solution when one exists.
- Never wrong-positive: accepted solutions are fully simulated and validator-approved.
- Gang-preserving: `MultiNodeGang` uses #1537 batches; `NodeLocalGreedy` pulls whole victim-job representatives.
- Reduced-budget reporting: only jobs that actually received reduced budget get the user-visible message.

For reduced-budget jobs, the user-visible unschedulable detail should say that the scheduler could not find a valid reclaim scenario within the remaining configured search time. Jobs that received their full configured search budget should not get this wording.

### Integration Posture

Wrap rather than rewrite. `NodeLocalGreedy` restores deleted narrow logic, and `MultiNodeGang` wraps the existing builder/emitter under a shared deadline. `byPodSolver.solve`, the fingerprint cache, and the validator remain unchanged. Ship behind a feature flag with today's uncapped emitter as a fallback path until snapshot replay and benchmark variants prove the bounded path is safe enough.

### Scale-Test Walkthrough

For the known distributed unschedulable fixture, `probeSize=1,2,4,8,9` solve cheaply through `NodeLocalGreedy`, accumulating recorded victims. At `probeSize=10`, `NodeLocalGreedy` tries "recorded 9 nodes plus each remaining candidate node as the 10th" in best-fit order. Every candidate fails because no 10th node exists. Reclaim reports unschedulable when the effective deadline is reached or generators are exhausted, instead of draining the wide multi-node search. `MultiNodeGang` remains time-limited by its generator deadline and the remaining job/action time.

### Relationship to Necessary-Condition Checks

Necessary-condition checks remain complementary. They may certify some negative cases cheaply with conservative checks, such as aggregate capacity ceilings. The bounded generator portfolio handles coupled cases that cannot be soundly pre-decided by bounding constructive search instead of proving complete unschedulability. Both mechanisms should share cheap capacity and packing estimators where practical.

## Monitoring

Production metrics to add:

- `scenario_search_jobs_total{action,result,reduced_budget}`: count jobs that entered bounded search. `result` values: `solved`, `no_solution_found`, `deadline_exhausted`, `not_attempted`, `no_generator`.
- `scenario_search_action_budget_configured_seconds{action}`: configured wall-clock budget for the action.
- `scenario_search_job_budget_configured_seconds`: configured per-job wall-clock budget.
- `scenario_search_generator_budget_configured_seconds{generator}`: configured wall-clock budget for each generator.
- `scenario_search_action_budget_exhausted_total{action}`: count action-level budget exhaustion.
- `scenario_search_duration_seconds{action,generator,result}`: Prometheus histogram of elapsed wall time for generator search attempts.
- `scenario_search_scenarios_total{action,generator,state}`: count scenarios by `state`. `state` values: `emitted`, `simulated`, `duplicate`, `validator_rejected`.

The `scenario_search_duration_seconds` histogram `_sum` and `_count` series are cumulative and are expected to grow after each scheduling session. Dashboards should use `rate()` or `increase()`. The histogram `_count` is the per-generator attempt count. Sum by `action` to get total generator-search time spent by an action.

Example queries:

- Average generator-attempt duration over 5 minutes: `rate(scenario_search_duration_seconds_sum[5m]) / rate(scenario_search_duration_seconds_count[5m])`.
- Total generator-search time spent per action over 5 minutes: `sum by (action) (increase(scenario_search_duration_seconds_sum[5m]))`.

Replay and benchmark-only instrumentation can be added later for generator discovery, but it is not part of the Phase 1 production metric contract.

## Test Plan

- Unit-test portfolio ordering, applicable-action filtering, stop reasons, and deadline handling.
- Unit-test `NodeLocalGreedy` candidate construction, best-fit node ordering, dedupe behavior, and whole victim-job handling.
- Unit-test `MultiNodeGang` as a wrapper over the existing builder/emitter.
- Keep existing reclaim, preempt, and consolidation solver tests passing with the feature enabled and disabled.
- Preserve existing #1537 gang/topology regression coverage.
- Preserve or add topology coverage so bounded search does not lose cases that motivated wide search.
- Replay the failing scale snapshot and verify reclaim exits quickly.
- Benchmark `BenchmarkReclaimUnschedulableDistributedJob_100Node`, `AntiAffinity100Node`, and `Topology100Node`, and use width-decomposition instrumentation to show simulations avoided, generator coverage, and deadline behavior.

## Graduation Criteria

Alpha entry criteria:

- Feature flag exists and defaults are safe for experiments.
- All new configuration knobs are marked alpha/experimental.
- Production metrics are emitted with the labels defined in this design.
- Snapshot replay leaves reclaim quickly under configured budgets.
- Existing reclaim, preempt, consolidation, gang, and topology tests pass.

Beta or default-on criteria:

- Defaults are tuned against the 500-node snapshot and representative production-like snapshots.
- Reduced-budget user messages are accurate and only emitted for reduced-budget jobs.
- Metrics show deadline exhaustion and generator misses are understandable by action and generator.
- The uncapped fallback has a documented removal decision.

Stable criteria:

- Generator registration API and config surface are either declared stable or replaced by adaptive scheduler policy.
- Operational dashboards and alerts use the production metric contract.
- False-negative behavior is understood and accepted for supported workloads.

## Implementation History

- 2026-06-13: Initial standalone design.

## Alternatives

| Alternative | Reason not selected |
| --- | --- |
| Complete unschedulability prover for reclaim | Victim selection is knapsack-shaped; a complete cheap proof would require solving the hard search problem. |
| Necessary-condition oracle only | Useful for covered negative causes, but cannot handle coupled proportion, victim, and topology cases generally. It remains complementary. |
| Keep exhaustive emitter with dedupe/cache improvements only | Reduces constants but does not bound worst-case synchronous scheduler time. |
| Expose work-unit budgets | Hard to explain and tune operationally; wall-clock budgets are clearer for alpha users and support. |
| Async/off-path search in Phase 1 | Valuable later, but too invasive for the first bounded-search change. |
