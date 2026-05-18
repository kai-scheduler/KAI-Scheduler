# Action-Level Scheduling Error Reporting

## Overview

Today, when a job fails to schedule in the **allocate** action, we produce detailed errors that explain exactly why: per-node resource fit failures, predicate violations, queue capacity limits, and gang scheduling constraints. These errors are published as Kubernetes events on pods and podgroups, and as structured `SchedulingCondition` entries in the podgroup status.

However, when a job fails in **reclaim**, **preempt**, or **consolidation**, nothing is reported. The user sees only the allocation-level error and has no visibility into whether the scheduler attempted these advanced actions or why they failed. This makes it difficult for users to understand what is preventing their workload from running and what actions they could take (e.g., increase queue quota, lower other workload priorities, adjust min-runtime settings).

## Motivation

Consider a user whose non-preemptible job is pending. The allocation error says "not enough GPUs." The user expects the scheduler to reclaim resources from other queues, but nothing happens. Without action-level errors, the user has no way to know that:
- Reclaim was attempted but the queue is already at fair share
- Preemption was attempted but all lower-priority jobs are protected by min-runtime
- Consolidation was disabled at the cluster level

These are all fixable situations, but only if the user (or admin) knows what the actual blocker is.

### Silent Failure Points Today

| Action | Decision Point | Plugin | What Happens | User Visibility |
|--------|---------------|--------|-------------|-----------------|
| **Reclaim** | `CanReclaimResources` returns false | proportion/reclaimable | Job skipped | None |
| **Reclaim** | All victims filtered by `ReclaimVictimFilter` | minruntime | No eligible victims | None |
| **Reclaim** | `FeasibleNodesForJob` returns empty | - | No nodes with GPUs | None |
| **Reclaim** | Solver finds no solution | - | Resource math doesn't work | None |
| **Reclaim** | `ReclaimScenarioValidatorFn` rejects | proportion, minruntime | Scenario violates constraints | None |
| **Preempt** | `IsNonPreemptibleJobOverQueueQuota` fails | proportion | Would exceed quota | None |
| **Preempt** | No same-queue lower-priority victims | - | Filter eliminates all candidates | None |
| **Preempt** | All victims protected by min-runtime | minruntime | Filter eliminates all candidates | None |
| **Preempt** | Solver finds no solution | - | Resource math doesn't work | None |
| **Consolidation** | Disabled (maxPreemptees=0) | - | Action skips entirely | None |
| **Consolidation** | Not enough GPUs cluster-wide | - | Early capacity check fails | None |
| **Consolidation** | No preemptible victims | - | Filter eliminates all candidates | None |

## Design Goals

1. **Report why jobs fail in reclaim, preempt, and consolidation** - covering the key decision points listed above
2. **Minimal performance overhead** on the scheduling hot path - avoid string formatting during scheduling, defer to publishing time
3. **Leverage existing infrastructure** - reuse `JobFitError`, events, and `SchedulingCondition` mechanisms
4. **Incremental coverage** - error reporting can be added to actions and plugins gradually; not every decision point needs coverage in the initial implementation

## Design Non-Goals

1. Reporting every individual filter decision for every victim candidate (too verbose, too costly)
2. Per-task (pod-level) errors for advanced actions - action errors are job-level by nature

## Detailed Design

The design has three layers:

1. **`FilterResult`** - a new return type for plugin callbacks that carries rejection reasons with lazy formatting
2. **`LazyJobFitError`** - a `JobFitError` implementation that defers string formatting to publish time
3. **Error recording at action decision points** - actions consume `FilterResult`s from plugins and record `LazyJobFitError`s on the job

### 1. FilterResult - Structured Plugin Callback Return Type

Today, `CanReclaimResourcesFn`, `VictimFilterFn`, and `ScenarioValidatorFn` return `bool`. This means the caller (action or session) has no idea _why_ a plugin rejected something. We change these callbacks to return a `FilterResult` struct that carries the rejection reason **without eagerly formatting strings**:

```go
// pkg/scheduler/api/types.go

// FilterResult carries a plugin's decision and the data needed to explain it.
// String formatting is deferred until Message() is called (at publish time).
type FilterResult struct {
    Passed     bool
    ReasonCode v2alpha2.UnschedulableReason
    msgFormat  string
    msgArgs    []any
}

// Message returns the human-readable explanation. Formatting happens here,
// not at construction time. For static messages (no args), returns the format
// string directly with zero cost.
func (r *FilterResult) Message() string {
    if r.Passed || r.msgFormat == "" {
        return ""
    }
    if len(r.msgArgs) == 0 {
        return r.msgFormat
    }
    return fmt.Sprintf(r.msgFormat, r.msgArgs...)
}

// Convenience constructors

func Pass() FilterResult {
    return FilterResult{Passed: true}
}

func Reject(reason v2alpha2.UnschedulableReason, msgFormat string, args ...any) FilterResult {
    return FilterResult{
        Passed:     false,
        ReasonCode: reason,
        msgFormat:  msgFormat,
        msgArgs:    args,
    }
}
```

Updated callback signatures:

```go
// pkg/scheduler/api/types.go

// Before:
type CanReclaimResourcesFn func(pendingJob *podgroup_info.PodGroupInfo) bool
type VictimFilterFn func(pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool
type ScenarioValidatorFn func(scenario ScenarioInfo) bool

// After:
type CanReclaimResourcesFn func(pendingJob *podgroup_info.PodGroupInfo) FilterResult
type VictimFilterFn func(pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) FilterResult
type ScenarioValidatorFn func(scenario ScenarioInfo) FilterResult
```

### 2. Session Invocation Changes

The session methods that chain plugin callbacks are updated to propagate `FilterResult`:

```go
// pkg/scheduler/framework/session_plugins.go

func (ssn *Session) CanReclaimResources(reclaimer *podgroup_info.PodGroupInfo) FilterResult {
    for _, canReclaimFn := range ssn.CanReclaimResourcesFns {
        return canReclaimFn(reclaimer)
    }
    return api.Reject(v2alpha2.ReclaimNoSolutionFound, "no CanReclaimResources plugin registered")
}

// AND-chain: returns first failing result with its reason
func (ssn *Session) ReclaimVictimFilter(reclaimer, victim *podgroup_info.PodGroupInfo) FilterResult {
    for _, rf := range ssn.ReclaimVictimFilterFns {
        result := rf(reclaimer, victim)
        if !result.Passed {
            return result
        }
    }
    return api.Pass()
}

func (ssn *Session) ReclaimScenarioValidatorFn(scenario api.ScenarioInfo) FilterResult {
    for _, rf := range ssn.ReclaimScenarioValidatorFns {
        result := rf(scenario)
        if !result.Passed {
            return result
        }
    }
    return api.Pass()
}

// Same pattern for PreemptVictimFilter, PreemptScenarioValidator
```

### 3. Plugin Implementation Changes

All plugins that implement these callbacks are updated to return `FilterResult`. Since we control all plugins in the repo, this is a straightforward change.

#### proportion/reclaimable plugin

```go
// pkg/scheduler/plugins/proportion/reclaimable/reclaimable.go

func (r *Reclaimable) CanReclaimResources(
    queues map[common_info.QueueID]*rs.QueueAttributes,
    reclaimer *ReclaimerInfo,
) FilterResult {
    reclaimerQueue := queues[reclaimer.Queue]
    requestedResources := utils.QuantifyVector(reclaimer.RequiredResources, reclaimer.VectorMap)

    allocatedResources := reclaimerQueue.GetAllocatedShare()
    allocatedResources.Add(requestedResources)
    if !allocatedResources.LessEqual(reclaimerQueue.GetFairShare()) {
        return api.Reject(
            v2alpha2.ReclaimQueueAtFairShare,
            "queue %s allocated resources would exceed fair share",
            reclaimerQueue.Name,
        )
    }

    if !reclaimer.IsPreemptable {
        allocatedNonPreemptible := reclaimerQueue.GetAllocatedNonPreemptible()
        allocatedNonPreemptible.Add(requestedResources)
        if !allocatedNonPreemptible.LessEqual(reclaimerQueue.GetDeservedShare()) {
            return api.Reject(
                v2alpha2.ReclaimQueueAtFairShare,
                "queue %s non-preemptible allocation would exceed deserved share",
                reclaimerQueue.Name,
            )
        }
    }

    return api.Pass()
}
```

#### minruntime plugin

```go
// pkg/scheduler/plugins/minruntime/minruntime.go

func (mr *minruntimePlugin) reclaimFilterFn(
    pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo,
) FilterResult {
    if victim.IsElastic() {
        return api.Pass()
    }
    if mr.isReclaimMinRuntimeProtected(pendingJob, victim) {
        return api.Reject(
            v2alpha2.ReclaimNoEligibleVictims,
            "victim %s/%s protected by reclaim min-runtime",
            victim.Namespace, victim.Name,
        )
    }
    return api.Pass()
}

func (mr *minruntimePlugin) preemptFilterFn(
    pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo,
) FilterResult {
    if victim.IsElastic() {
        return api.Pass()
    }
    if mr.isPreemptMinRuntimeProtected(pendingJob, victim) {
        return api.Reject(
            v2alpha2.PreemptNoEligibleVictims,
            "victim %s/%s protected by preempt min-runtime",
            victim.Namespace, victim.Name,
        )
    }
    return api.Pass()
}

func (mr *minruntimePlugin) reclaimScenarioValidatorFn(scenario api.ScenarioInfo) FilterResult {
    reclaimer := scenario.GetPreemptor()
    for _, victimInfo := range scenario.GetVictims() {
        if !victimInfo.Job.IsElastic() {
            continue
        }
        if !mr.isReclaimMinRuntimeProtected(reclaimer, victimInfo.Job) {
            continue
        }
        if !validVictimForMinAvailable(victimInfo) {
            return api.Reject(
                v2alpha2.ReclaimNoSolutionFound,
                "elastic victim %s/%s would drop below minAvailable during min-runtime protection",
                victimInfo.Job.Namespace, victimInfo.Job.Name,
            )
        }
    }
    return api.Pass()
}

// Same pattern for preemptScenarioValidatorFn
```

### 4. Action Error Reasons (New Constants)

```go
// pkg/apis/scheduling/v2alpha2/podgroup_types.go

const (
    // Reclaim action reasons
    ReclaimQueueAtFairShare  UnschedulableReason = "ReclaimQueueAtFairShare"
    ReclaimNoEligibleVictims UnschedulableReason = "ReclaimNoEligibleVictims"
    ReclaimNoFeasibleNodes   UnschedulableReason = "ReclaimNoFeasibleNodes"
    ReclaimNoSolutionFound   UnschedulableReason = "ReclaimNoSolutionFound"

    // Preempt action reasons
    PreemptOverQueueQuota    UnschedulableReason = "PreemptOverQueueQuota"
    PreemptNoEligibleVictims UnschedulableReason = "PreemptNoEligibleVictims"
    PreemptNoFeasibleNodes   UnschedulableReason = "PreemptNoFeasibleNodes"
    PreemptNoSolutionFound   UnschedulableReason = "PreemptNoSolutionFound"

    // Consolidation action reasons
    ConsolidationDisabled         UnschedulableReason = "ConsolidationDisabled"
    ConsolidationInsufficientGPUs UnschedulableReason = "ConsolidationInsufficientGPUs"
    ConsolidationNoSolutionFound  UnschedulableReason = "ConsolidationNoSolutionFound"
)
```

### 5. LazyJobFitError - Job-Level Error Type

`LazyJobFitError` implements the `JobFitError` interface and defers string formatting to publish time. Actions create these from `FilterResult`s returned by plugins, or directly for action-level decisions.

```go
// pkg/scheduler/api/common_info/lazy_job_fit_error.go

type LazyJobFitError struct {
    reason    enginev2alpha2.UnschedulableReason
    msgFormat string
    msgArgs   []any
    cached    string
    once      sync.Once
}

func NewLazyJobFitError(
    reason enginev2alpha2.UnschedulableReason,
    msgFormat string,
    args ...any,
) *LazyJobFitError {
    return &LazyJobFitError{
        reason:    reason,
        msgFormat: msgFormat,
        msgArgs:   args,
    }
}

// NewLazyJobFitErrorFromFilterResult converts a plugin FilterResult into a
// job-level error. The FilterResult's lazy args are transferred, not formatted.
func NewLazyJobFitErrorFromFilterResult(result api.FilterResult) *LazyJobFitError {
    return &LazyJobFitError{
        reason:    result.ReasonCode,
        msgFormat: result.MsgFormat(),
        msgArgs:   result.MsgArgs(),
    }
}

func (e *LazyJobFitError) message() string {
    e.once.Do(func() {
        if len(e.msgArgs) == 0 {
            e.cached = e.msgFormat
        } else {
            e.cached = fmt.Sprintf(e.msgFormat, e.msgArgs...)
        }
    })
    return e.cached
}

func (e *LazyJobFitError) Reason() enginev2alpha2.UnschedulableReason {
    return e.reason
}

func (e *LazyJobFitError) DetailedMessage() string { return e.message() }
func (e *LazyJobFitError) Messages() []string      { return []string{e.message()} }

func (e *LazyJobFitError) ToUnschedulableExplanation() enginev2alpha2.UnschedulableExplanation {
    return enginev2alpha2.UnschedulableExplanation{
        Reason:  e.reason,
        Message: e.message(),
    }
}
```

### 6. Error Recording at Action Decision Points

Actions consume `FilterResult`s from plugin callbacks and record `LazyJobFitError`s on the job. For action-level decisions (no plugin involved), actions create `LazyJobFitError`s directly.

#### Reclaim Action

```go
// pkg/scheduler/actions/reclaim/reclaim.go

func (ra *reclaimAction) Execute(ssn *framework.Session) {
    // ...
    for !jobsOrderByQueues.IsEmpty() {
        job := jobsOrderByQueues.PopNextJob()

        // Plugin callback - now returns FilterResult with reason
        result := ssn.CanReclaimResources(job)
        if !result.Passed {
            job.AddJobFitError(common_info.NewLazyJobFitErrorFromFilterResult(result))
            continue
        }

        // Action-level decision - create error directly
        if ssn.UseSchedulingSignatures() {
            easier, otherJob := smallestFailedJobs.IsEasierToSchedule(job)
            if !easier {
                job.AddJobFitError(common_info.NewLazyJobFitError(
                    v2alpha2.ReclaimNoSolutionFound,
                    "Reclaim: skipped after considering equivalent job %s/%s",
                    otherJob.Namespace, otherJob.Name,
                ))
                continue
            }
        }

        succeeded, statement, reclaimeeTasksNames := ra.attemptToReclaimForSpecificJob(ssn, job)
        if succeeded {
            // ...commit...
        } else {
            // Error already recorded by attemptToReclaimForSpecificJob
            smallestFailedJobs.UpdateRepresentative(job)
        }
    }
}
```

The victim queue builder aggregates filter rejections and records an error if no victims pass:

```go
func getOrderedVictimsQueue(ssn *framework.Session, reclaimer *podgroup_info.PodGroupInfo) solvers.GenerateVictimsQueue {
    return func() *utils.JobsOrderByQueues {
        rejectionCounts := map[v2alpha2.UnschedulableReason]int{}
        totalCandidates := 0

        jobs := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{}
        for _, job := range ssn.ClusterInfo.PodGroupInfos {
            if job.Queue == reclaimer.Queue {
                continue
            }
            totalCandidates++
            result := ssn.ReclaimVictimFilter(reclaimer, job)
            if !result.Passed {
                rejectionCounts[result.ReasonCode]++
                continue
            }
            jobs[job.UID] = job
        }

        if len(jobs) == 0 && totalCandidates > 0 {
            reclaimer.AddJobFitError(common_info.NewLazyJobFitError(
                v2alpha2.ReclaimNoEligibleVictims,
                "Reclaim: all %d cross-queue candidates filtered (%s)",
                totalCandidates, formatRejectionCounts(rejectionCounts),
            ))
        }

        jobsOrderedByQueue := utils.NewJobsOrderByQueues(ssn, ...)
        jobsOrderedByQueue.InitializeWithJobs(jobs)
        return &jobsOrderedByQueue
    }
}

// formatRejectionCounts produces e.g. "3 by MinRuntimeProtected, 2 by QueueOverFairShare"
func formatRejectionCounts(counts map[v2alpha2.UnschedulableReason]int) string {
    // ...
}
```

#### Preempt Action

```go
// pkg/scheduler/actions/preempt/preempt.go

func attemptToPreemptForPreemptor(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) (...) {
    // Existing SchedulableResult-based check (already has reason/message)
    preemptorTasks := podgroup_info.GetTasksToAllocate(preemptor, ...)
    if result := ssn.IsNonPreemptibleJobOverQueueQuotaFn(preemptor, preemptorTasks); !result.IsSchedulable {
        preemptor.AddJobFitError(common_info.NewLazyJobFitError(
            v2alpha2.PreemptOverQueueQuota,
            "Preempt: %s", result.Message,
        ))
        return false, nil, nil
    }
    // ...victim queue building with filter aggregation (same pattern as reclaim)...
}
```

The preempt victim filter (`buildFilterFuncForPreempt`) has action-level checks (priority, queue, preemptibility) in addition to plugin filters. These can also produce specific reasons:

```go
func buildFilterFuncForPreempt(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) func(*podgroup_info.PodGroupInfo) FilterResult {
    return func(job *podgroup_info.PodGroupInfo) FilterResult {
        if !job.IsPreemptibleJob() {
            return api.Reject(v2alpha2.PreemptNoEligibleVictims, "victim %s/%s is not preemptible", job.Namespace, job.Name)
        }
        if job.Priority >= preemptor.Priority {
            return api.Reject(v2alpha2.PreemptNoEligibleVictims, "victim %s/%s priority %d >= preemptor priority %d", job.Namespace, job.Name, job.Priority, preemptor.Priority)
        }
        if job.Queue != preemptor.Queue {
            return api.Reject(v2alpha2.PreemptNoEligibleVictims, "victim %s/%s is in different queue", job.Namespace, job.Name)
        }
        if preemptor.UID == job.UID {
            return api.Pass() // skip self silently
        }
        if job.GetActiveAllocatedTasksCount() == 0 {
            return api.Reject(v2alpha2.PreemptNoEligibleVictims, "victim %s/%s has no active allocated tasks", job.Namespace, job.Name)
        }
        return ssn.PreemptVictimFilter(preemptor, job)
    }
}
```

#### Consolidation Action

```go
// pkg/scheduler/actions/consolidation/consolidation.go

func (alloc *consolidationAction) Execute(ssn *framework.Session) {
    if ssn.GetMaxNumberConsolidationPreemptees() == 0 {
        // Record on all pending jobs that consolidation is disabled
        // (or skip - this is a cluster-level config, not per-job)
        return
    }
    // ...
}

func attemptToConsolidateForPreemptor(ssn *framework.Session, job *podgroup_info.PodGroupInfo) (...) {
    if !utils.IsEnoughGPUsAllocatableForJob(job, ssn, false) {
        job.AddJobFitError(common_info.NewLazyJobFitError(
            v2alpha2.ConsolidationInsufficientGPUs,
            "Consolidation: not enough allocatable GPUs in the cluster",
        ))
        return false, nil
    }
    // ...
}
```

### 7. Solver Changes

The solver's `SolutionValidator` is a `ScenarioValidatorFn`, which now returns `FilterResult`. The solver can propagate the last rejection reason back to the action:

```go
// pkg/scheduler/actions/common/solvers/by_pod_solver.go

// SolutionValidator updated to match ScenarioValidatorFn signature
type SolutionValidator func(scenario api.ScenarioInfo) api.FilterResult

func (s *byPodSolver) handleScenarioSolution(...) *solutionResult {
    // ...
    if s.solutionValidator != nil {
        result := s.solutionValidator(scenario)
        if !result.Passed {
            statement.Discard()
            return &solutionResult{
                solved:       false,
                filterResult: &result, // propagate rejection reason
            }
        }
    }
    // ...
}
```

The `solutionResult` struct gains an optional `filterResult` field. The action-level code (`attemptToReclaimForSpecificJob`, etc.) can check this after the solver returns false and record the validator's reason on the job.

### 8. Publishing - No Changes Required

The existing publishing machinery handles action errors automatically because they're standard `JobFitError` entries:

1. **Session close** (`framework/session.go:375`): Iterates all jobs and calls `RecordJobStatusEvent`
2. **RecordJobStatusEvent** (`status_updater.go:199`): Checks if job has pending tasks, calls `recordUnschedulablePodsEvents` and `recordUnschedulablePodGroup`
3. **recordUnschedulablePodGroup** (`status_updater.go:396`): Converts all `JobFitErrors` to message string and `UnschedulableExplanation` entries
4. **Pod events** (`status_updater.go:350-372`): Falls back to `JobFitErrors` if no task-specific errors exist

Action errors appear:
- In the pod's Unschedulable event
- In the podgroup's Unschedulable event
- In the podgroup's `SchedulingCondition.Reasons[]` array (as structured `UnschedulableExplanation` entries)

### 9. Example Output

After this change, a pending job's podgroup status would look like:

```yaml
status:
  schedulingConditions:
    - type: UnschedulableOnNodePool
      nodePool: default
      reason: Unschedulable
      message: |
        Unable to schedule podgroup.
        Not enough resources for the workload: node-group(s) didn't have enough resources: GPUs.
        Reclaim: queue team-a allocated resources would exceed fair share.
        Preempt: all 5 in-queue candidates filtered (3 by MinRuntimeProtected, 2 not preemptible).
      reasons:
        - reason: "Not enough resources for the workload"
          message: "node-group(s) didn't have enough resources: GPUs"
        - reason: "ReclaimQueueAtFairShare"
          message: "queue team-a allocated resources would exceed fair share"
        - reason: "PreemptNoEligibleVictims"
          message: "all 5 in-queue candidates filtered (3 by MinRuntimeProtected, 2 not preemptible)"
```

## Affected Code - Full Change List

### New files
| File | Content |
|------|---------|
| `pkg/scheduler/api/filter_result.go` | `FilterResult` type, `Pass()`, `Reject()` constructors |
| `pkg/scheduler/api/common_info/lazy_job_fit_error.go` | `LazyJobFitError` type implementing `JobFitError` |
| `pkg/apis/scheduling/v2alpha2/action_reasons.go` | New `UnschedulableReason` constants |

### Modified - API types
| File | Change |
|------|--------|
| `pkg/scheduler/api/types.go` | `CanReclaimResourcesFn`, `VictimFilterFn`, `ScenarioValidatorFn` return `FilterResult` instead of `bool` |

### Modified - Session plugin invocation
| File | Change |
|------|--------|
| `pkg/scheduler/framework/session_plugins.go` | `CanReclaimResources`, `ReclaimVictimFilter`, `PreemptVictimFilter`, `ReclaimScenarioValidatorFn`, `PreemptScenarioValidator` return `FilterResult` |

### Modified - Plugins
| File | Change |
|------|--------|
| `pkg/scheduler/plugins/proportion/proportion.go` | `CanReclaimResourcesFn` returns `FilterResult` |
| `pkg/scheduler/plugins/proportion/reclaimable/reclaimable.go` | `CanReclaimResources`, `Reclaimable` return `FilterResult` |
| `pkg/scheduler/plugins/minruntime/minruntime.go` | `reclaimFilterFn`, `preemptFilterFn`, `reclaimScenarioValidatorFn`, `preemptScenarioValidatorFn` return `FilterResult` |

### Modified - Actions
| File | Change |
|------|--------|
| `pkg/scheduler/actions/reclaim/reclaim.go` | Record errors at `CanReclaimResources`, signature skip, no victims, no solution |
| `pkg/scheduler/actions/preempt/preempt.go` | Record errors at over-quota, signature skip, no victims, no solution |
| `pkg/scheduler/actions/consolidation/consolidation.go` | Record errors at not-enough-GPUs, signature skip, no solution |

### Modified - Solver
| File | Change |
|------|--------|
| `pkg/scheduler/actions/common/solvers/by_pod_solver.go` | `SolutionValidator` returns `FilterResult`; `solutionResult` gains `filterResult` field |

## Performance Considerations

### Cost Model

| Operation | When | Cost | Mitigation |
|-----------|------|------|------------|
| Return `FilterResult` from plugin | During scheduling | Stack-allocated struct, no formatting | Comparable to returning bool + reason enum |
| Create `LazyJobFitError` | During scheduling | One small heap alloc, no formatting | Only created at action decision points, not per-victim |
| Aggregate rejection counts | During victim filtering | Map increment per rejected victim | Map is small (bounded by number of distinct reason codes) |
| Format message string | At publish time (session close) | One `fmt.Sprintf` per error | Lazy - only formats if actually read |

### FilterResult is Cheap

`FilterResult` is a value type with 4 fields (bool, string, string, []any). Returning it from a plugin callback is comparable to returning a bool - the compiler can optimize the common `Pass()` case. For rejections, the `msgArgs` slice is the only heap allocation, and only when the message has format args. Static messages like `"victim is not preemptible"` have zero heap allocation.

### Worst Case

N pending jobs, each fails all actions. Each accumulates ~3-4 action errors. N * 4 `LazyJobFitError` objects (~64 bytes each), N * 4 format calls at publish time. For N=1000: ~256KB allocations, ~4000 format calls. Well within acceptable limits.

### Alternative: Toggle Flag

A scheduler config flag could disable action error collection:

```yaml
actionErrorReporting: true  # default: true
```

We recommend against adding this unless profiling shows a real problem, given the cost model above.

## Future Enhancements

### Solver Diagnostic Mode

Add an optional diagnostic mode to the solver that tracks why each scenario failed:
- Which scenario filters rejected it (NodeAffinities, TopologyAwareIdleGpus, IdleGpus)
- Whether the allocation simulation succeeded but the validator rejected it
- Resource deltas showing how close the solver got to a solution

This would be gated behind the existing `detailedFitErrors` flag or a new `detailedActionErrors` flag.

### Detailed Message Mode

When `detailedFitErrors` is enabled, expand aggregated filter rejections into individual victim-level messages:

```
"Reclaim: no eligible victims. victim ns/job-a protected by min-runtime until 2024-01-15T10:30:00Z,
 victim ns/job-b protected by min-runtime until 2024-01-15T10:35:00Z, ..."
```

vs the default aggregated form:

```
"Reclaim: all 5 cross-queue candidates filtered (3 by MinRuntimeProtected, 2 by QueueOverFairShare)"
```

## Implementation Plan

### Phase 1: Core types
1. Add `FilterResult` type in `pkg/scheduler/api/`
2. Add `LazyJobFitError` type in `pkg/scheduler/api/common_info/`
3. Add action-specific `UnschedulableReason` constants in `pkg/apis/scheduling/v2alpha2/`
4. Unit tests for `FilterResult` and `LazyJobFitError`

### Phase 2: Plugin API migration
1. Change `CanReclaimResourcesFn`, `VictimFilterFn`, `ScenarioValidatorFn` signatures to return `FilterResult`
2. Update session invocation methods in `session_plugins.go`
3. Update proportion/reclaimable plugin
4. Update minruntime plugin
5. Update solver's `SolutionValidator` type
6. Unit tests for updated plugins

### Phase 3: Reclaim action errors
1. Record errors from `CanReclaimResources` FilterResult
2. Aggregate victim filter rejections in `getOrderedVictimsQueue`
3. Record errors for no feasible nodes, no solution, scheduling signature skip
4. Unit tests

### Phase 4: Preempt action errors
1. Record errors from over-quota check
2. Convert `buildFilterFuncForPreempt` to return `FilterResult` with specific reasons
3. Aggregate victim filter rejections
4. Record errors for no solution, scheduling signature skip
5. Unit tests

### Phase 5: Consolidation action errors
1. Record errors for disabled, not-enough-GPUs, no solution, scheduling signature skip
2. Unit tests

### Phase 6: Integration testing
1. E2E tests verifying error messages appear in podgroup status and pod events
2. Test that errors from multiple actions accumulate correctly
3. Test that errors are cleared when a job is scheduled in a subsequent cycle

## Resolved Design Decisions

1. **Scheduling signature optimization**: When a job is skipped because a similar job already failed, record a message like `"Reclaim: skipped after considering equivalent job ns/name"`. This tells the user their job was considered and points to which other job determined the outcome.

2. **Message verbosity**: Minimal for Phase 1 (aggregated counts). In a later iteration, gated behind the existing `detailedFitErrors` flag, expand to per-victim detail.

3. **Plugin API changes**: Plugin callbacks (`CanReclaimResourcesFn`, `VictimFilterFn`, `ScenarioValidatorFn`) change from `bool` to `FilterResult`. All plugins are in-repo and updated together. `FilterResult` uses lazy string formatting to avoid performance overhead on the hot path.

## Open Questions

1. **Error deduplication across cycles**: If a job stays pending across multiple scheduling cycles, the same action errors will be regenerated each cycle. The existing `shouldUpdateCondition` check in the status updater prevents redundant API updates if the message hasn't changed. Is this sufficient, or do we need additional deduplication?

2. **Preempt action-level filters**: The `buildFilterFuncForPreempt` function has hardcoded checks (priority, queue, preemptibility) that are not plugin callbacks. Should these return `FilterResult` too (making the filter function signature `func(*PodGroupInfo) FilterResult`), or is this over-engineering for action-internal logic?
