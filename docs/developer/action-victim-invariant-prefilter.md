# Action Victim-Invariant Pre-Filter Guard

Date: 2026-04-30

## Summary

Add an action-level guard that skips expensive solver-backed work when a pending
job is already blocked by a known pre-solver failure that victim simulation
cannot fix.

The motivating production case is reclaim spending seconds simulating victims
for jobs whose pods reference a missing PVC. Evicting victims cannot create a
PVC, so the action should record the existing scheduling error and move on.

Initial action users:

- `reclaim`
- `preempt`
- `consolidation`

## Problem

Solver-backed actions are useful when changing the victim set can make a job
schedulable. They are not useful when the job is blocked by a condition that is
independent of victims.

Victim-dependent examples:

- not enough idle CPU, memory, GPU, or pod slots;
- affinity or topology conflicts involving existing pods;
- queue or quota pressure that can change after victims are removed.

Victim-invariant examples:

- a pod references a PVC that does not exist;
- a pod references a required ConfigMap that does not exist;
- one task requests more resources than any node can provide.

Today the scheduler can enter the generic reclaim/preempt/consolidation solver
for these victim-invariant cases and only rediscover the failure during
simulated allocation. This is expensive and does not change the outcome.

## Goals

- Skip solver work before victim simulation when a job has a known
  victim-invariant blocker.
- Keep the API narrow and action-oriented.
- Preserve the existing `PrePredicateFn` allocation path as the canonical
  correctness gate.
- Return the same useful scheduling error users would have seen after
  allocation failed.
- Apply the same guard to reclaim, preempt, and consolidation.

## Non-Goals

- Do not create a broad structured pre-predicate result API.
- Do not classify every pre-predicate failure.
- Do not cache failures across scheduler sessions.
- Do not change normal allocation correctness.

## Core Rule

The guard may skip action solver work only when the failure is known to be
victim-invariant:

```text
known victim-invariant failure -> skip action solver work
victim-dependent failure       -> run existing action flow
unknown failure                -> run existing action flow
```

False negatives are acceptable because the existing allocation path still runs
`ssn.PrePredicateFn` and records the normal failure.

False positives are not acceptable because they can skip an action that might
have found a valid victim set.

## Public Scheduler API

Add a narrow plugin-to-session API for victim-invariant pre-predicate blockers.

Location:

```text
pkg/scheduler/api/types.go
```

Proposed types:

```go
type VictimInvariantPrePredicateFailure struct {
	Task *pod_info.PodInfo
	Err  error
}

type VictimInvariantPrePredicateFn func(
	task *pod_info.PodInfo,
	job *podgroup_info.PodGroupInfo,
) *VictimInvariantPrePredicateFailure
```

This exposes only what actions need:

- the blocked task;
- the user-visible error to record.

It intentionally does not expose Kubernetes status codes, all pre-predicate
failures, allowed node sets, or a generic failure taxonomy.

## Session Hooks

Add the function slice and registration/evaluation helpers to the session.

Locations:

```text
pkg/scheduler/framework/session.go
pkg/scheduler/framework/session_plugins.go
```

Proposed hooks:

```go
func (ssn *Session) AddVictimInvariantPrePredicateFn(fn api.VictimInvariantPrePredicateFn)

func (ssn *Session) VictimInvariantPrePredicateFailure(
	task *pod_info.PodInfo,
	job *podgroup_info.PodGroupInfo,
) *api.VictimInvariantPrePredicateFailure
```

The evaluator returns the first non-nil failure from registered plugins. If no
plugin registers a function, or every function returns nil, actions continue
normally.

Registration order is the evaluation order. Other plugins can add their own
victim-invariant blockers later without changing the action code.

## Predicate Plugin Implementation

The predicates plugin registers one `VictimInvariantPrePredicateFn` alongside
its existing `PrePredicateFn`.

Location:

```text
pkg/scheduler/plugins/predicates
```

The new hook checks only the first supported candidate pre-filters:

- `VolumeBinding`
- `ConfigMap`
- `MaxNodePoolResources`

It must not run all pre-filters and then classify their failures. Unknown or
victim-dependent failures should remain invisible to this API.

The hook follows the same status semantics as the existing
`evaluateTaskOnPrePredicate` path:

- `status.IsSkip()` means the predicate is not applicable; it is not a failure;
- skip statuses still update `skipPredicates`;
- `status.AsError() != nil` is the failure signal;
- nil, success, and skip statuses return nil for this optimization.

The normal `PrePredicateFn` path remains unchanged and continues to be used by
`allocateTask`.

For this action guard, a candidate predicate must opt in by returning
`UnschedulableAndUnresolvable` for the specific victim-invariant failure. The
guard should not treat every `UnschedulableAndUnresolvable` from every predicate
as actionable; the predicate still has to be one of the explicitly supported
candidate predicates.

## Initial Classifiers

The first implementation supports only three conservative classifiers.

`VolumeBinding` missing PVC:

- predicate key is `VolumeBinding`;
- status code is `UnschedulableAndUnresolvable`;
- message identifies `persistentvolumeclaim "<name>" not found`.

`ConfigMap` missing required ConfigMap:

- predicate key is `ConfigMap`;
- status code is `UnschedulableAndUnresolvable`;
- message contains `Missing required configmaps:`.

`MaxNodePoolResources` max node size:

- predicate key is `MaxNodePoolResources`;
- status code is `UnschedulableAndUnresolvable`.

As part of this feature, change `ConfigMap` and `MaxNodePoolResources` to return
`UnschedulableAndUnresolvable` for these specific failures. That status means
"not fixable by scheduling or victim simulation in this scheduler snapshot",
not "impossible forever". A later scheduler session can reconsider the job if a
ConfigMap/PVC is created or a larger node is added.

## Error Reporting

The returned `Err` should match the existing pre-predicate error shape:

```text
Scheduling conditions were not met for pod <namespace>/<name>:
<PredicateKey>: <predicate error>.
```

Use the predicate map key in the message. This matters for
`MaxNodePoolResources`, where the session predicate name can differ from the
map key.

Actions should record the failure on the blocked task using the same task fit
error path used by allocation:

```go
fitErrors := common_info.NewFitErrors()
fitErrors.SetError(failure.Err.Error())
job.AddTaskFitErrors(failure.Task, fitErrors)
```

If job-level status reporting requires a job error, the implementation should
mirror the existing `handleFailedTaskAllocation` wording instead of inventing a
new message shape.

## Action Usage

Add a small common helper for actions.

Location:

```text
pkg/scheduler/actions/common/action_eligibility.go
```

Proposed helper:

```go
func VictimInvariantPrePredicateFailureForTasks(
	ssn *framework.Session,
	job *podgroup_info.PodGroupInfo,
	tasks []*pod_info.PodInfo,
) *api.VictimInvariantPrePredicateFailure
```

The helper iterates the tasks the action would try to allocate and returns the
first classified blocker. It receives the task list from the action so it does
not encode action-specific task-selection rules.

One blocked task is enough to skip the job because the job solver has
all-or-nothing semantics for pending tasks.

### Reclaim

In `reclaimAction.Execute`, run the guard after:

- `ssn.CanReclaimResources(job)`;
- the scheduling-signature `IsEasierToSchedule` check.

Run it before:

- `metrics.IncPodgroupsConsideredByAction()`;
- `attemptToReclaimForSpecificJob`.

If a victim-invariant failure is found, record it and `continue`.

Do not call `smallestFailedJobs.UpdateRepresentative(job)` for this skip.

### Preempt

In `preemptAction.Execute`, run the guard after the scheduling-signature check
and before:

- `metrics.IncPodgroupsConsideredByAction()`;
- `attemptToPreemptForPreemptor`.

If a victim-invariant failure is found, record it and `continue`.

Do not call `smallestFailedJobs.UpdateRepresentative(job)` for this skip.

This may change precedence for jobs that also fail preempt-specific quota
checks, because the current quota check lives inside
`attemptToPreemptForPreemptor`. The intended first behavior is to prefer the
more actionable missing-dependency error and skip representative updates for
victim-invariant failures.

### Consolidation

In `consolidationAction.Execute`, run the guard after the scheduling-signature
check and before:

- `metrics.IncPodgroupsConsideredByAction()`;
- `attemptToConsolidateForPreemptor`.

If a victim-invariant failure is found, record it and `continue`.

Do not call `smallestFailedJobs.UpdateRepresentative(job)` for this skip.
Consolidation has a session-wide `smallestFailedJobs` pool, so avoiding false
representative updates is especially important.

## Correctness Notes

- The optimization is session-local. If a missing PVC or ConfigMap is created
  before a later scheduler session, the job can be considered again.
- The guard does not replace normal predicates. It only avoids expensive action
  work before the same failure would be rediscovered later.
- Actions must inspect all tasks they would try to allocate until the first
  blocker. Heterogeneous gang jobs can have only one task with a missing
  dependency, and that is enough to make the job unschedulable in the current
  solver attempt.
- Skips caused by this guard must not update scheduling-signature
  representatives. Missing dependencies do not prove that this job is a useful
  resource-size representative for other jobs.

## Validation

Required tests:

- session registration and first-non-nil evaluation;
- no registered victim-invariant functions returns nil;
- predicate classifier positives for missing PVC, missing ConfigMap, and
  max-node-size;
- predicate classifier negatives for unknown and victim-dependent failures;
- action tests proving reclaim, preempt, and consolidation skip solver work;
- action tests proving the skip does not update smallest-failed-job
  representatives;
- a heterogeneous job test where the blocker is on the second task;
- no-blocker action behavior remains unchanged.

Benchmark:

```bash
go test -run '^$' -bench '^BenchmarkReclaimWithMissingPVCJobs$' -benchtime=1x -count=3 -benchmem ./pkg/scheduler/actions/reclaim
```

Before values captured on 2026-04-30, before adding the victim-invariant guard:

```text
BenchmarkReclaimWithMissingPVCJobs-22    1    8683066674 ns/op    4421050064 B/op    20974821 allocs/op
BenchmarkReclaimWithMissingPVCJobs-22    1    8846954707 ns/op    4420085552 B/op    20972221 allocs/op
BenchmarkReclaimWithMissingPVCJobs-22    1    8930444242 ns/op    4420507840 B/op    20971710 allocs/op
```

Average before value:

```text
8.820 s/op    4.421 GB/op    20.973M allocs/op
```

The benchmark should improve materially in both runtime and memory because the
missing-PVC jobs should skip the solver and avoid the `SubsetNodesFn` path.
