# Solver stack refactor

## Summary

The solver stack used by `reclaim`, `preempt`, and `consolidation` is proposed to be rebuilt around three interfaces — `Generator`, `Simulator`, and `Validator` — driven by a single `Solve` function. The current four-layer stack does not separate responsibilities clearly between the layers, making it both inefficient and complicated to understand and modify.
The proposal is to clearly re-define the interfaces with good separation of concerns. In addition to better readability, testability and maintainability, this will allow us to use alternative generators (e.g., topology-first) and validators, allowing for improvements in both performance, usability, and better scenario search, increasing the chances of success for different actions. Eliminating the state shared between the different layers will allow for more concurrency in the implementation.

For the purpose of this document, we will refer to all actions that require scenario generation and simulation as simply "Actions". For the sake of simplicity, we might refer to reclaim/preempt/consolidate scenarios simply as "reclaim scenarios".

In some cases, we will refer to "priority" of jobs in the context of reclaim: this means the global priority of a job (i.e, considering all jobs and queues in the cluster).

## Motivation

### Responsibility leakage between layers

The pre-refactor stack runs each `reclaim` / `preempt` / `consolidation` action through a four-layer pipeline:

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

Every level both produces new candidates and commits results into a parent context — neither backtracking cleanly nor expressing its own search order independently. A few specific consequences:

- **Scenario generation logic is split** - A higher-level scenario generator adds potential victims. The inner `byPodSolver` internally splits this scenario by bucketing victims per node, essentially iterating over sub-scenarios. These are not validated with the regular scenario filters, instead wasting time on full simulations.
- **Inner solver assumes caller implementation** - the inner solver implicitly assumes that each new scenario is an extension of the previous one, with one more reclaimer task. This allows it to iterate on the new victims by bucketing them to nodes, but breaks when trying to implement changes in the larger solver scope.

Testing any one layer in isolation requires reproducing most of the stack, and adding a new generation strategy or simulator backend requires touching all four layers.

### Decoupling unlocks several open follow-ups

A clean interface boundary is the precondition for several long-term enhancements that cannot be staged independently today:

- pluggable scenario search & scoring
- concurrent scenario generation / filtering
- adaptive scenario generation

Those will be explored in *Open follow-ups*; the refactor itself doesn't deliver them, but they will be used to guide the design.

## Usage stories

### Reclaim across a few host nodes (common case)

A 4-GPU training job queues against a cluster where several lower-priority jobs hold GPUs. The action wants the cheapest viable victim set, evicting as few victims as possible, as low priority as possible.

ToDo: detail a case that requires sub-scenario search

### Topology-constrained gang on topology domains

A distributed inference job requires accelerators with compatible topology constraints (e.g., eight GPUs in the same low-latency domain). Most freed-victim sets across the cluster do not free a coherent domain, so a victim-set-first generator emits mostly infeasible candidates and falls back to a full-set evict-everything candidate that over-evicts.

This is the main argument for well-scoped, adaptive scenario generation: an alternative generator can detect a topology-constrained workload, enumerates viable topology domains first and derives the minimum victim set per domain. It needs to plug into the same `Solve` driver alongside the same simulator and validator — no changes to those layers. Different job types warrant fundamentally different search orders, and the architecture should make swapping enumeration strategies cheap and safe. See *Topology-first generation* in *Open follow-ups*.

## Typical workload shape

Generically solving every possible workload in reasonable time is impractical. Luckily, we can make some reasonable assumptions that will make the task easier.

Typical podgroups are usually:
- Single pod workloads, which are the easiest
- Distributed training, inference, or data processing jobs — a leader plus several (typically one, sometimes a few, generally less than 10) worker templates, ranging from very few to thousands of pods that are *replicas* of those few templates: identical resource requests, predicates, and affinity rules. The solver treats every pod independently today, but template-level equivalence classes can shrink the effective candidate space substantially.

## Desired properties

The properties below are (some of the) desired properties that we want to achieve. 

### Clearly defined, decoupled components

`Generator`, `Simulator`, `Validator` are independently testable and substitutable. They should have minimal assumptions on each other's internal implementation, and share only the necessary state between them. This will allow us to test and modify them independently.

### Good performance at the target scale

- **1,000s of nodes** and **1,000s of jobs** per scheduling cycle.
- **Distributed jobs have low template variety** (see *Typical workload shape*) — leader plus a small number of worker templates, replicated. This assumption is reasonable for common usage patterns and allows us to implement optimizations.
- **Jobs with topology requirements** is a common use case that requires its own optimizations
- Busy, multi-tenant, highly utilized clusters that serve dozens of teams

### Best solutions for reclaim, by multiple criteria

Reclaim quality is multi-dimensional and the criteria don't combine into one natural ordering:

- **Fairness** — respect queue guarantees and weights.
- **Bin packing** — favor scenarios that consolidate workloads.
- **Topology** — respect network topology requirements for workloads. When possible, place workloads according to their preferred topology.
- **Affinities** — honor pod (anti-)affinity and taint/toleration constraints and preferences.
- **Minimal cluster disruption** — fewer evictions when possible.
- **Priority-aware victim selection** — prefer disrupting lower-priority jobs first.
- **Performance at scale** - simulating every possible scenario is not practical in real world large-scale clusters. Sparse scenario evaluation might be necessary.

Different use cases and different setups might weigh these criteria differently. Implementing a **pluggable scenario cost function** will allow greater flexibility in different use cases.

### Adaptable to job types

While out of scope for the initial refactor, it's worth considering that different job classes warrant different scenario-generation strategies. The refactor should take into account that scenario generation could be **adaptive** to job type and cluster state.

- **Strict topology gangs** — enumerate viable placement domains first, derive the minimum victim set per domain. The default victim-set-first generator could be suboptimal here, both from performance and for finding the optimal solution.
- **Single-task reclaimers** — one pod can only land on one node, so a single-pod reclaimer requires single-node sub-scenario evaluation. This can be generalized further: each reclaimer set of pods has a theoretical minimum and maximum number of nodes that need to be evaluated.

## Goals and non-goals

### Goals

- **Decouple the four layers** behind well-defined, single-purpose interfaces (`Generator`, `Simulator`, `Validator`).
- **Prevent responsibility leakage** — no layer reads or mutates state owned by another.
- **Independent testability** — each layer can be exercised in isolation with a fake or stub.
- **Extendability** — alternative generators, simulators, or validators plug in without touching the others. 
- **Performance** - initial refactor should not introduce major performance regressions, while allowing for performance improvements down the road.

### Non-goals

- **Exhaustive enumeration of feasible victim subsets.** Finding the optimum out of every possible solution is explicitly *not* the target. Some gang shapes (e.g., very picky node-selectors) may fall back to a coarser candidate that over-evicts; tightening this is left to *Open follow-ups*, gated on a real workload demonstrating need.

## Proposed initial architecture

```
   action  ────►  ┌─────────────────────┐
                  │      JobSolver      │
                  │       .Solve        │
                  └────────┬────────────┘
                           │
                           ▼
                  ┌─────────────────────┐
                  │  Solve(g, sim, val) │   
                  │                     │   
                  │                     │   
                  │                     │   
                  │                     │   
                  │                     │   
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
  │ pluggable   │    │ transaction │    │ plugin      │
  │ enumeration │    │ per call    │    │ policy      │
  │ strategy    │    │             │    │ enforcement │
  └─────────────┘    └─────────────┘    └─────────────┘
```

The scenario generator is responsible for scenario search strategy and initial filtering. Once a scenario is deemed feasible by all pre-filters, the Simulator simulates it, taking into account every constraint: job allocation order, kubernetes predicates, node scoring, etc. Given a successful simulation, the validator is a post-evaluation policy filter that enforces plugin-specific policies, such as fairness in the proportion plugin.

TODO: scenario scoring as p0?

### `Scenario`

A flat candidate plan. No "recorded" carry-forward, no per-node bucketing in the type — those become implementation details of specific generators.

```go
type Scenario struct {
    Preemptor *podgroup_info.PodGroupInfo
    Pending   []*pod_info.PodInfo  // tasks of Preemptor to be allocated
    Victims   []*pod_info.PodInfo  // candidate victims for the scenario
}
```

### `Generator`

Yields candidate scenarios. Pre-filters unfeasible scenarios before they are simulated.

Today, the scenario generator attempts to greedily find the "cheapest" scenario - meaning, the scenario that involves the least prioritized jobs in the cluster, by adding candidate victims from a global job queue.

A cheap stateless prune (e.g., a freed-resource lower-bound check) should drop emissions whose freed-resource pool cannot possibly host the gang, so most pre-tier candidates never reach the simulator.

ToDo: Adaptive scenario generation, scenario scoring

### `Simulator`

Simulator validates the proposed scenario by simulating the evicted and expected allocation, using the session state and the statement mechanism. Simulator takes into account all relevant scheduling constraints, including job ordering, predicates, fine-grained resources (DRA, fractions), plugins etc. Scenarios that fail to allocate the pending job are discarded. Simulations are orders of magnitude more expensive computationally than scenario generation and filtering, and they cannot be performed concurrently due to modifying session state, so a good implementation will avoid simulating unfeasible scenarios as much as possible.

```go
type SimulationResult struct {
    Feasible  bool
    Placement map[*pod_info.PodInfo]*node_info.NodeInfo
    Preempted []*pod_info.PodInfo  // tasks evicted post-sim
    Pipelined []*pod_info.PodInfo  // tasks pipelined post-sim
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

Validators allow plugins to implement policies on acceptable scenarios: for example, the Proportion plugin allows only reclaims that fit fairness limitations.

## Risks

### Simulator-call inflation from unfiltered emissions

Some job properties are not taken into account by the scenario generator - for example, volume placement. Some jobs risk passing all scenario pre-filters but failing in the simulation stage - this is the case today as it will be after the refactor. However, any changes to scenario generation that can generate more scenarios than today, risk inflating the number of simulations the solver has to run, potentially degrading performance significantly. 

This could be mitigated in a number of ways:

- Setting actual time limits on solver runs per job, to avoid scheduler hang
- Using sparse scenario generation (skipping some feasible scenarios) in cases where scenario filters do not filter out any scenario

A worst-case test should be added to our benchmarks to keep track on this behavior.

### One-shot gang vs incremental greedy

The legacy gang loop probed `k = 1, 2, 4, ..., N` and could retain the largest feasible `k` as a partial allocation. The new simulator is all-or-nothing.

- **Strict gang semantics is correct** for min-member=N jobs — partial allocations don't help a gang that requires N tasks running together.
- **Different victim choices** — full-gang simulation may pick a different victim set than the legacy partial-gang fixed-point would. 

TODO: find an actual scenario where that's the case

This can potentially be mitigated by the scenario generator searching more sub-scenarios instead of just incrementing.

## Implementation plan

TODO

## Open questions

- **Pluggable scenario scoring API.** A future `ScenarioScorer` would allow us to explore scenarios concurrently and pick the best one. Same pattern as `NodeOrderFn`, but at the scenario layer. This needs to be POC'd to assess impact on performance.

## Open follow-ups

- **"Sub-scenario" generation.** Today, after a scenario passes the pre-filters, the `byPodSolver` evaluates it by iterating over the potential nodes involved in the scenario: these are essentially "sub-scenarios", a set of less-disruptive subset scenarios generated from a bigger one. While the current implementation is crude and makes assumptions on the implementation of the bigger solver (attempting to solve a reclaimer pod-by-pod), it might make sense in some cases to attempt a less disruptive sub-scenario before evaluating the full one.
- **Topology-first generation** Is probably the first use case to benefit from improvements to scenario generation.

## Code anchors

The code areas affected by this work:

- Production solver: `pkg/scheduler/actions/common/solvers/`
- Action callers: `pkg/scheduler/actions/{reclaim,preempt,consolidation}/`
- Existing scenario types (would become generator-internal state):
  `pkg/scheduler/actions/common/solvers/scenario/`
- Existing accumulated filters (would be reused by a default
  generator): `pkg/scheduler/actions/common/solvers/accumulated_scenario_filters/`
