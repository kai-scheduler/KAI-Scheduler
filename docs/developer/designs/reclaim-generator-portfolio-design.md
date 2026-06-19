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
  - [Search Result Contract](#search-result-contract)
  - [Generator Abstraction](#generator-abstraction)
  - [Plugin Registration and Ordering](#plugin-registration-and-ordering)
  - [Configuration](#configuration)
  - [Driver Loop and Budget](#driver-loop-and-budget)
  - [Initial Shipped Plugin Policy](#initial-shipped-plugin-policy)
  - [Future Enhancements](#future-enhancements)
    - [Scenario Deduplication Cache](#scenario-deduplication-cache)
    - [Smart Generator Selection](#smart-generator-selection)
    - [Generator Checkpointing Across Scheduling Sessions](#generator-checkpointing-across-scheduling-sessions)
    - [Possible Future Generators](#possible-future-generators)
  - [Approximation Contract](#approximation-contract)
  - [Explainability](#explainability)
  - [Integration Posture](#integration-posture)
  - [Scale-Test Walkthrough](#scale-test-walkthrough)
  - [Relationship to Necessary-Condition Checks](#relationship-to-necessary-condition-checks)
- [Monitoring](#monitoring)
- [Test Plan](#test-plan)
- [Rollout Criteria](#rollout-criteria)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

This proposal replaces unbounded reclaim scenario enumeration with a bounded, plugin-registered generator portfolio. Generators propose concrete victim scenarios best-first, while the existing simulator and post-simulation validator remain the authority for accepted solutions. The first policy runs a cheap node-local generator before the existing multi-node gang generator, all under configurable time budgets. This intentionally bounds scheduler time rather than trying to prove every negative reclaim case, because victim selection is a knapsack related problem.

## Motivation

The current reclaim path can spend unbounded synchronous scheduler time trying to prove that no valid victim set exists. The scale-test failure is a negative case: the pending job is unschedulable by construction, but reclaim drains a wide scenario search before giving up. The bounded generator portfolio makes that failure mode bounded and observable while preserving the safety property that any accepted reclaim solution is fully simulated and validator-approved.

### Goals

- Bound pathological reclaim, preempt, and consolidation scenario search by time budget.
- Keep every accepted solution validator-approved; never accept a scenario from a shortcut alone.
- Preserve the #1537 gang/topology correctness fix by keeping whole victim gangs intact.
- Restore fast common-case behavior with a narrow `NodeLocalGreedy` generator before wider search.
- Let future case-specific generators register through normal scheduler plugin enablement and ordering without changing the shared solver driver.
- Expose the new search-budget configuration knobs as alpha/experimental.
- Add production metrics that show budget use, generator work, scenario outcomes, and reduced-budget jobs.

### Non-Goals

- Do not prove complete unschedulability for reclaim. General victim selection is a hard combinatorial problem.
- Do not expose victim-count, node-count, victim-by-node, or scenario-count work-unit budgets.
- Do not introduce a runtime generator-selection policy in Phase 1; generator enablement and default ordering remain controlled by normal scheduler plugin configuration.
- Do not move heavy search off the synchronous scheduling path in Phase 1.
- Do not include replay, benchmark, or debug-only metric schemas in the production metric contract.

## Proposal

Move scenario generation behind a bounded generator portfolio owned by the shared `JobSolver` path used by reclaim, preempt, and consolidation. Each applicable generator yields `ByNodeScenario` candidates incrementally. The driver simulates candidates through the existing solver, validates accepted solutions with the existing post-simulation validator, and stops when a solution is found, all generators are exhausted, or the effective time budget expires.

The initial portfolio is:

| Plugin order | Generator | Purpose |
| --- | --- | --- |
| 1 | `NodeLocalGreedy` | Restore the cheap pre-#1537 node-local scenario shape for common cases and the scale-test failure. |
| 2 | `MultiNodeGang` | Wrap today's wide accumulated scenario builder while preserving the #1537 whole-gang behavior. |

The negative result is intentionally approximate when produced by the bounded portfolio. If the budget expires or the registered generators do not cover the shape, the scheduler may report no solution even if a solution exists. This is acceptable only because accepted positives remain fully simulated and validator-approved, and because the behavior is explicitly bounded and observable.

### Limitations/Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| False negative when a valid scenario exists after the budget expires. | Report deadline/generator exhaustion through metrics; keep budget knobs alpha/experimental while tuning defaults; add future generators based on observed misses. |
| A job can consume most of the action budget before later jobs run. | Support `maxJobSearchDuration` and optional best-effort `minJobSearchDuration` with default `0s`; it does not reserve action budget upfront, but gives reached jobs a minimal chance when action budget remains. |
| Generator ordering can hide useful later generators. | Derive ordering from normal scheduler plugin order and document that plugin enablement/order controls generator selection. |
| `MultiNodeGang` changes could regress #1537 gang/topology correctness. | Wrap the existing builder/emitter and keep #1537 coverage; topology-specific generators may also preserve the same correctness case later. |
| Budget metrics can grow unbounded as cumulative counters/histograms. | Document that Prometheus `_sum` and `_count` series are cumulative and should be queried with `rate()` or `increase()`. |
| Experimental knobs become an accidental compatibility contract. | Mark only the new time-budget configuration args alpha/experimental in the design and implementation docs. |

## Design Details

### Terminology

- **Probe size (`probeSize`)**: number of pending tasks from the job that `searchMaxSolvableK` asks the solver to place in the current probe.
- **Node-prefix size (`nodePrefixSize`)**: number of ordered candidate victim nodes included by an emitter in one candidate scenario.
- **Scenario**: one concrete victim set to evict or reclaim before trying to place the current `probeSize`.
- **Simulation**: the virtual allocation attempt after applying one scenario.
- **Generator**: a component that proposes scenarios. It does not simulate them.
- **Generator attempt**: one generator's complete search for a pending job across the partial gang sizes tried by `searchMaxSolvableK`, followed by the full-job probe if the partial search succeeds.
- **Deadline / budget**: time limits for action, job, and generator search.
- **Reduced budget**: a job received less reclaim-search time than configured because the action-level budget was already depleted.

### Shared Invariants

1. Every accepted solution is fully simulated and validator-approved.
2. Bounded-portfolio negative results can be approximate; they are not complete unschedulability proofs.
3. Victim batches remain gang-preserving units, including multi-node jobs from the #1537 fix.
4. `Solve` remains all-or-nothing; no partial-probe state is committed unless the full job is solved.
5. The post-simulation validator remains the final authority for consolidation, proportion, and other post-eviction side effects.

### Mechanism

`solvePartialJob` keeps its simulation and validation skeleton. The scenario source changes from a single exhaustive emitter to an ordered portfolio of generators. The portfolio owns generator iteration, action filtering, budget checks, and stop reasons.

Generator iteration happens at pending-job granularity, not at `probeSize` granularity. For one pending job, the driver selects the first applicable generator and lets that generator attempt own the complete `searchMaxSolvableK` progression: exponential probes, binary-search probes, and the full-job probe when the partial search succeeds. Only after that generator attempt exhausts candidates or reaches its generator deadline does the driver start over with the next applicable generator. The driver must not restart the whole generator portfolio for every partial gang size, because that discards the intended progression from smaller successful probes to larger probes and can let an earlier generator repeatedly consume the shared job/action budget before a later generator gets its own search.

When the driver reaches the effective deadline without a validated solution, the action reports "no solution" as an incomplete result. The result reason must distinguish at least deadline exhaustion, generator exhaustion, no applicable generator, and not-attempted jobs so metrics and reduced-budget messages are accurate.

### Search Result Contract

The driver returns a structured result, not just `nil`. The result reason is the single source of truth for metrics, reduced-budget messages, and rollout criteria:

| Result reason | Meaning | Counts as entered search? |
| --- | --- | --- |
| `solved` | a fully simulated, validator-approved scenario was found | yes |
| `deadline_exhausted` | the action or job deadline expired before a solution was found | yes, if at least one candidate was requested or the job received a search attempt |
| `generators_exhausted` | all applicable generators were exhausted or reached their own generator deadline before a solution was found | yes |
| `no_generator` | no registered generator applies to this action/job shape | no |
| `not_attempted` | the job was skipped before any generator attempt, usually because no search budget remained | no |

Generator deadlines bound one generator attempt, not the whole job search. A generator's `maxGeneratorSearchDuration` covers all partial gang probes it owns for that pending job. When that generator attempt reaches its deadline, the portfolio moves to the next applicable generator and restarts the pending-job partial-gang search with that generator. The whole-job result is `generators_exhausted` only after all applicable generators have either exhausted their candidates or reached their generator deadline. User-facing messages may still say "no valid reclaim scenario was found," but metrics should use the concrete result reason rather than a vague `no_solution_found` value.

### Generator Abstraction

```go
// A ScenarioGenerator proposes concrete candidate victim sets, best-first and
// cheaply pre-filtered. It performs no simulation; it only decides which victim
// sets are worth trying.
type ScenarioGenerator interface {
    Name() string
    Next() *scenario.ByNodeScenario // nil when exhausted
}
```

A generator attempt is built per pending job from a shared solve context: pending job, recorded victims, feasible nodes, victim queue, gang constraints, topology constraints, and action type. Within that attempt, the same generator family owns every `probeSize` tried for the job. The current probe still supplies a partial pending job when deriving candidate scenarios, and smaller successful probes still update the recorded-victim state used by later probes. Changing `probeSize` must not switch to the next generator or restart the full portfolio.

For Phase 1, `Next()` intentionally stays simple and does not accept a deadline or context. The generator contract is that `Next()` is cheap and incremental: it may compute or return the next candidate, but it must not run simulation, scan an unbounded search space, or perform expensive blocking work before returning. The initial `NodeLocalGreedy` and `MultiNodeGang` generators must be structured around this contract. If a future generator needs expensive candidate construction, the interface must be revisited so candidate generation can receive a deadline/context or otherwise poll the search budget internally.

Generators are responsible for scenario quality, not just scenario enumeration. They should use cheap necessary-condition checks before emitting a candidate, because every emitted scenario can trigger a full simulation and validator pass. Low-quality candidates waste the bounded search budget and can hide useful later generators behind avoidable simulation work.

Accumulated scenario filters are the Phase 1 mechanism for these cheap validity checks. A generator that uses them must build the filter input incrementally: one accumulated filter stream may append potential victims, but it must not retract victims or restart from a different accumulated base. This preserves the assumptions used by accumulated filters and by cursor-aware inputs such as PR #1614's monotonic `AccumulatedScenarioInput` path. The monotonicity requirement applies to each accumulated-filter stream, not to every independent candidate returned by `Next()`. A generator may still emit independent candidate scenarios after deriving them from a monotonic accumulated base, as `NodeLocalGreedy` does for node-local branches. A future generator that needs to restart from a different base should express that through a richer generator result, a reset/full-scan input, or a separate stream abstraction rather than making a reset implicit in `Next()`.

### Plugin Registration and Ordering

`NewJobsSolver`, `solvePartialJob`, and scenario generation are shared by reclaim, preempt, and consolidation. The proposal adds a session extension point that lets plugins register generators for one or more action types:

```go
type ScenarioGeneratorFactory func(ctx *solvers.SolveContext) solvers.ScenarioGenerator

func (ssn *Session) AddScenarioGenerator(
    f ScenarioGeneratorFactory,
    applies ...framework.ActionType,
)
```

Generator order is derived from scheduler plugin execution order. `OpenSession` calls plugin `OnSessionOpen` hooks in configured plugin order, and each hook appends its generators by calling `AddScenarioGenerator`. If a plugin registers multiple generators, their relative order is the order of its `AddScenarioGenerator` calls. `JobSolver` filters registered generators by the current action and tries them in registration order at generator-attempt granularity: each generator gets the complete partial-gang search for the pending job before the next generator starts.

Generator selection is controlled by normal scheduler plugin enablement and plugin order; this is not an alpha mechanism. The new time-budget knobs are alpha/experimental controls for KAI development, support, and experiments while defaults are tuned.

### Configuration

Budget configuration is exposed through `SchedulingShard.spec.scenarioSearchBudgets`. The block is alpha/experimental even though the field name does not include an alpha prefix. Helm should mirror the same shape under `scheduler.scenarioSearchBudgets` and render it into the default `SchedulingShard`. Generator enablement and ordering stay under normal `spec.plugins` configuration.

Example:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
spec:
  scenarioSearchBudgets:
    maxActionSearchDuration:
      default: "5m"
      reclaim: "5m"
      preempt: "5m"
      consolidation: "5m"
    maxJobSearchDuration: "4m"
    minJobSearchDuration: "0s"
    maxGeneratorSearchDuration:
      default: "2m"
      NodeLocalGreedy: "30s"
      MultiNodeGang: "2m"
```

Initial defaults when the block is omitted:

| Field | Default | Meaning |
| --- | --- | --- |
| `maxActionSearchDuration.default` | `"5m"` | fallback action budget |
| `maxActionSearchDuration.reclaim` | `"5m"` | reclaim-specific action budget |
| `maxActionSearchDuration.preempt` | `"5m"` | preempt-specific action budget |
| `maxActionSearchDuration.consolidation` | `"5m"` | consolidation-specific action budget |
| `maxJobSearchDuration` | `"4m"` | per-job budget cap |
| `minJobSearchDuration` | `"0s"` | disabled best-effort job floor |
| `maxGeneratorSearchDuration.default` | `"2m"` | fallback generator budget |
| `maxGeneratorSearchDuration.NodeLocalGreedy` | `"30s"` | narrow generator budget |
| `maxGeneratorSearchDuration.MultiNodeGang` | `"2m"` | wide generator budget |

All values are strings parsed with Go's standard `time.ParseDuration`, which supports units such as `ms`, `s`, `m`, and `h`. Values must parse as non-negative durations. For max budgets, `0s` means unlimited and is used for legacy-equivalent support configurations. For `minJobSearchDuration`, `0s` disables the best-effort floor. If configured, `minJobSearchDuration` must be lower than `maxJobSearchDuration` unless `maxJobSearchDuration` is unlimited. Unknown action or generator names should be rejected during configuration validation so misspelled knobs do not silently fail.

### Driver Loop and Budget

```go
budget := newSearchBudget(actionDeadline, jobDeadline, generatorDeadlines)
generators := applicableGenerators(ssn, action, pendingJob)

for _, generatorFactory := range generators {
    generatorBudget := budget.beginGenerator(generatorFactory.Name())
    result := searchMaxSolvableKWithGenerator(
        ssn, pendingJob, state, feasibleNodeMap, generatorFactory, generatorBudget,
    )
    if result.reason == solved || result.reason == deadline_exhausted {
        return result
    }
}

return searchResult{reason: generators_exhausted} // approximate no solution
```

The budget model is time-only: every exposed budget is a wall-clock duration.

| Budget | Configuration key | Contract |
| --- | --- | --- |
| Action deadline | `maxActionSearchDuration` | the action stops scenario search after this time and moves on |
| Job deadline | `maxJobSearchDuration` | one pending job cannot consume the whole action indefinitely |
| Minimum job attempt | `minJobSearchDuration` | optional best-effort floor for jobs reached while action budget remains |
| Generator deadline | `maxGeneratorSearchDuration` | one generator cannot consume the whole job indefinitely |

The effective deadline for any candidate is normally the minimum remaining time across action, current job, and current generator. `minJobSearchDuration` is an alpha/experimental budget knob. It is an optional best-effort floor, disabled by default, and must be lower than `maxJobSearchDuration` when configured. It does not reserve action budget upfront and does not guarantee every pending job receives that amount of time. Instead, when a job is reached and action budget remains, the scheduler should give it a minimal search attempt up to the remaining action budget. If the remaining action budget is less than `minJobSearchDuration`, the job may still run with the remaining time and is marked `reduced_budget=true`. If no action budget remains, the job is `not_attempted`.

Internal work-unit budgets such as victim-count, node-count, victim-by-node products, and per-generator scenario caps are not exposed. Budgets are enforced at candidate boundaries: before asking the active generator for the next scenario, before accepting that candidate for simulation, and before starting the simulation. Generators must yield candidates incrementally so the driver can check the effective deadline between candidates. One `Next()` call or simulation may finish after the deadline if it started just before the deadline, but the loop must not request or start another candidate after the effective deadline has expired.

### Initial Shipped Plugin Policy

| Plugin order | Generator | Restores / covers | Width |
| --- | --- | --- | --- |
| 1 | `NodeLocalGreedy` | builds an accumulated, filter-validated base, then emits the same node-local scenario shape used by pre-#1537 `solveOnPotentialNodes`: recorded victims plus one candidate node's victims | narrow |
| 2 | `MultiNodeGang` | today's `PodAccumulatedScenarioBuilder` plus `subScenarioEmitter`, using accumulated filters before emission and time-limited by the effective deadline while preserving #1537 gang/topology correctness | wide |
| later | plugin hook | new case-specific generators | case-specific |

`NodeLocalGreedy` is expected to handle the common single-pod-per-node reclaimee case and the known scale-test failure. `MultiNodeGang` remains necessary for true gangs that need several nodes freed simultaneously. A topology-specific generator may later preserve the same correctness case more directly, but #1537 regression coverage remains required either way.

### Future Enhancements

#### Scenario Deduplication Cache

The generator portfolio needs a scenario deduplication mechanism so generators cannot repeatedly simulate equivalent victim sets. This is necessary both within one generator and across different generators: `NodeLocalGreedy`, `MultiNodeGang`, and future case-specific generators may overlap when they discover the same victim set through different heuristics.

The exact deduplication design should be specified and implemented separately. That follow-up design must define:

- whether the cache is new or wraps an existing helper;
- the canonical scenario fingerprint, including victim tasks/jobs and node assignment data that affects simulation;
- how equivalent victim sets emitted in different orders map to the same fingerprint;
- where deduplication metrics are recorded, including the `duplicate` state in `scenario_search_scenarios_total`;
- how long the cache lives, expected to be per job/probe rather than global scheduler state.

#### Smart Generator Selection

Phase 1 drains applicable generators in normal scheduler plugin order. A later generator-selection policy may choose, skip, or reorder registered generators per job based on job shape and, if useful, cluster state. It must preserve the generator-attempt boundary unless a separate design explains how per-probe switching preserves search continuity and budget fairness. That future policy should be designed from replay and production evidence showing which generator families solve which workload shapes; it should not be implicit in the Phase 1 plugin-registration mechanism.

#### Generator Checkpointing Across Scheduling Sessions

A future portfolio can persist per-job generator progress across scheduling sessions so a job that exhausts its current budget can resume near the last tried scenario instead of restarting from the first generator candidate every session. The checkpoint should record enough state to resume safely, such as job/probe identity, generator name, generator cursor or last scenario fingerprint, budget stop reason, and an input fingerprint covering pending tasks, recorded victims, feasible nodes, plugin order, generator configuration, and relevant cluster state. If any fingerprint input changes, the checkpoint must be discarded and the next session should restart from the beginning rather than reuse stale generator state.

#### Possible Future Generators

| Generator option | Covers | Notes |
| --- | --- | --- |
| `AggressiveOneShot` | cases where the narrow path failed but one large direct scenario may solve quickly | bounded by generator time budget |
| `TopologyFirst` | topology-required gangs vs. fully packed topology domains | enumerate viable topology domains, then derive the minimum victim set per domain |
| `FullNodeFirst` | whole-node workloads where each pending task needs an entire node | derive scenarios around freeing complete nodes before generic multi-node search |
| `DisruptionBounded` | user-understandable disruption limits, for example avoiding victim sets larger than 2x or 3x the pending job size | implemented as generator policy, not generic solver work-unit budget |

### Approximation Contract

- Incomplete by design: may report no solution when one exists.
- Never wrong-positive: accepted solutions are fully simulated and validator-approved.
- Quality-gated: generators use cheap validity checks before emission so bounded time is spent mostly on plausible scenarios.
- Gang-preserving: `MultiNodeGang` uses #1537 batches; `NodeLocalGreedy` pulls whole victim-job representatives.
- Reduced-budget reporting: only jobs that actually received reduced budget get the user-visible message.

For reduced-budget jobs, the `ScenarioSearchUnresolved` detail should say that the scheduler could not find a valid reclaim scenario within the remaining configured search time. Jobs that received their full configured search budget should not get this wording.

### Explainability

Bounded scenario search outcomes must be visible to job submitters without changing the meaning of Kubernetes `Unschedulable`. The allocate action can continue to set the existing `Unschedulable` condition and events for ordinary allocation failures. An unresolved scenario-search attempt is a separate scheduler outcome and must not overload that signal, because other Kubernetes components, including autoscaling integrations, already use `Unschedulable` semantics.

When reclaim, preempt, or consolidation reaches a bounded-search terminal result for a pending job, the scheduler should set a dedicated `ScenarioSearchUnresolved` condition on both the `PodGroup` and its pending Pods. The `PodGroup` condition is the authoritative job-level explanation. Pod conditions provide a direct answer for users who inspect only the submitted Pods. The condition should be emitted once for the job scheduling outcome, not once per generator, probe, or scenario. `Unresolved` is intentionally broader than `exhausted`: it covers jobs where all configured generator attempts were drained, jobs where the time budget expired before a complete answer, jobs skipped because no budget remained, and jobs with no applicable generator.

User-facing condition messages should describe the scheduling outcome, not the internal generator mechanics:

| Search result | `ScenarioSearchUnresolved` message |
| --- | --- |
| `deadline_exhausted` | `KAI could not find a valid reclaim scenario within the configured search budget for this scheduling attempt. The job remains pending and may be retried in a later scheduling cycle.` |
| `generators_exhausted` | `KAI tried the configured scenario-search policy and found no valid reclaim scenario for this scheduling attempt. The job remains pending and may be retried in a later scheduling cycle.` |
| `not_attempted` | `KAI did not attempt scenario search for this job in this scheduling cycle because the configured search budget was already exhausted.` |
| `no_generator` | `KAI did not attempt scenario search for this job because no configured scenario generator applies to this action.` |

If `reduced_budget=true`, the message should say that the scheduler could not find a valid scenario within the remaining configured search time because the action search budget was partly consumed by earlier jobs. This wording must only be used for jobs that actually received a reduced budget.

Kubernetes Events may repeat the same human-readable message on the `PodGroup` and Pods, but they should use `ScenarioSearchUnresolved` as the event reason rather than `Unschedulable`. Events should not include generator names, probe sizes, scenario counts, or elapsed durations. Those details belong in metrics, logs, replay output, or future debug tooling.

### Integration Posture

Wrap rather than rewrite. `NodeLocalGreedy` restores deleted narrow logic, and `MultiNodeGang` wraps the existing builder/emitter under the configured deadline. `byPodSolver.solve` and the validator remain unchanged. The generator portfolio is the normal path, with sensible default budgets. Operators or support workflows that need legacy-equivalent behavior can configure the portfolio to use only the current emitter and unlimited budgets.

### Scale-Test Walkthrough

For the known distributed unschedulable fixture, `NodeLocalGreedy` owns the whole partial-gang search for the job first. Its `probeSize=1,2,4,8,9` probes solve cheaply, accumulating recorded victims. At `probeSize=10`, `NodeLocalGreedy` tries the pre-#1537 scenario shape: "recorded 9 nodes plus one remaining candidate node as the 10th" for each candidate node. Every candidate fails because no 10th node exists. Reclaim moves to `MultiNodeGang` only after the `NodeLocalGreedy` attempt exhausts or reaches its generator deadline. `MultiNodeGang` then starts its own partial-gang search from the pending job shape, time-limited by its generator deadline and the remaining job/action time.

### Relationship to Necessary-Condition Checks

Necessary-condition checks remain complementary. They may certify some negative cases cheaply with conservative checks, such as aggregate capacity ceilings. The bounded generator portfolio handles coupled cases that cannot be soundly pre-decided by bounding constructive search instead of proving complete unschedulability. Both mechanisms should share cheap capacity and packing estimators where practical.

## Monitoring

Production metrics to add:

- `scenario_search_jobs_total{action,result,reduced_budget}`: count jobs considered by bounded search, including jobs skipped before their first generator attempt. `result` values: `solved`, `deadline_exhausted`, `generators_exhausted`, `no_generator`, `not_attempted`.
- `scenario_search_action_budget_configured_seconds{action}`: configured action budget.
- `scenario_search_job_budget_configured_seconds`: configured per-job budget.
- `scenario_search_generator_budget_configured_seconds{generator}`: configured generator budget.
- `scenario_search_action_budget_exhausted_total{action}`: count action-level budget exhaustion.
- `scenario_search_duration_seconds{action,generator,result}`: Prometheus histogram of elapsed generator-search duration. `result` uses the same result-reason values as `scenario_search_jobs_total` when the attempt maps to a whole-job outcome.
- `scenario_search_scenarios_total{action,generator,state}`: count scenarios by `state`. `state` values: `emitted`, `simulated`, `validator_rejected`.

The `scenario_search_duration_seconds` histogram `_sum` and `_count` series are cumulative and are expected to grow after each scheduling session. Dashboards should use `rate()` or `increase()`. The histogram `_count` is the per-generator attempt count. Sum by `action` to get total generator-search time spent by an action.

Example queries:

- Average generator-attempt duration over 5 minutes: `rate(scenario_search_duration_seconds_sum[5m]) / rate(scenario_search_duration_seconds_count[5m])`.
- Total generator-search time spent per action over 5 minutes: `sum by (action) (increase(scenario_search_duration_seconds_sum[5m]))`.

Replay and benchmark-only instrumentation can be added later for generator discovery, but it is not part of the Phase 1 production metric contract.

The Phase 1 production metrics do not export per-job generator attribution such as "generator X solved job Y". That tuple is high-cardinality and does not fit the production metric contract. If needed for generator validation, it should be collected through bounded logs, replay output, benchmark instrumentation, or a separate observability design.

## Test Plan

- Unit-test portfolio ordering, applicable-action filtering, stop reasons, and deadline handling.
- Unit-test that one generator owns the complete partial-gang search for a pending job before the next generator is tried.
- Unit-test `NodeLocalGreedy` candidate construction, pre-#1537 node-local scenario shape, and whole victim-job handling.
- Unit-test `MultiNodeGang` as a wrapper over the existing builder/emitter.
- Keep existing reclaim, preempt, and consolidation solver tests passing with the default portfolio configuration and with the legacy-equivalent configuration of current emitter plus unlimited budgets.
- Preserve existing #1537 gang/topology regression coverage.
- Preserve or add topology coverage so bounded search does not lose cases that motivated wide search.
- Test that bounded-search terminal results set `ScenarioSearchUnresolved` on the `PodGroup` and pending Pods without changing the allocate action's existing `Unschedulable` behavior.
- Replay the failing scale snapshot and verify reclaim exits quickly.
- Benchmark `BenchmarkReclaimUnschedulableDistributedJob_100Node`, `AntiAffinity100Node`, and `Topology100Node`, and use width-decomposition instrumentation to show simulations avoided, generator coverage, and deadline behavior.

## Rollout Criteria

Initial implementation criteria:

- Default generator order is safe.
- All new budget configuration knobs are marked alpha/experimental.
- Production metrics are emitted with the labels defined in this design.
- Snapshot replay leaves reclaim quickly under configured budgets.
- Existing reclaim, preempt, consolidation, gang, and topology tests pass.

Default tuning criteria:

- Defaults are tuned against the 1000-node large-job benchmark and representative production-like snapshots.
- `ScenarioSearchUnresolved` conditions and events are accurate, and reduced-budget wording is only emitted for reduced-budget jobs.
- Metrics show `deadline_exhausted`, `generators_exhausted`, `no_generator`, and `not_attempted` outcomes clearly by action and, where applicable, generator.
- Legacy-equivalent configuration is documented as current emitter plus unlimited budgets; bounded-search misses are terminal for the current scheduling attempt.

Long-term criteria:

- Generator registration and plugin-order behavior remain normal scheduler plugin behavior, or are replaced by adaptive scheduler policy.
- Operational dashboards and alerts use the production metric contract.
- False-negative behavior is understood and accepted for supported workloads.

## Alternatives

| Alternative | Reason not selected |
| --- | --- |
| Complete unschedulability prover for reclaim | Victim selection is knapsack-shaped; a complete cheap proof would require solving the hard search problem. |
| Necessary-condition oracle only | Useful for covered negative causes, but cannot handle coupled proportion, victim, and topology cases generally. It remains complementary. |
| Keep exhaustive emitter with constant-factor improvements only | Reduces constants but does not bound worst-case synchronous scheduler time. |
| Expose work-unit budgets | Hard to explain and tune operationally; time budgets are clearer for alpha users and support. |
| Async/off-path search in Phase 1 | Valuable later, but too invasive for the first bounded-search change. |
