# Deferred Session Action Commit

Related issue: [#2319](https://github.com/kai-scheduler/KAI-Scheduler/issues/2319).

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [Expected behavior](#expected-behavior)
  - [Limitations and risks](#limitations-and-risks)
- [Design Details](#design-details)
  - [Session lifecycle](#session-lifecycle)
  - [Journal and normalization](#journal-and-normalization)
    - [Baseline capture](#baseline-capture)
  - [External effects and ordering](#external-effects-and-ordering)
  - [Failures, shutdown, and restart](#failures-shutdown-and-restart)
  - [Related scenario-validation work](#related-scenario-validation-work)
  - [Implementation scope](#implementation-scope)
  - [Monitoring](#monitoring)
  - [Test plan](#test-plan)
  - [Readiness criteria](#readiness-criteria)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

The scheduler will plan all actions in one in-memory session, then build a final plan and apply it to Kubernetes at session end. If a later action evicts a Pod allocated or pipelined in that session, both operations are canceled before reaching Kubernetes. A limited number of workers run the remaining BindRequests and evictions in parallel and publish accepted BindRequests to the informer store. The scheduler goroutine builds the final plan and applies worker results to session state.

## Motivation

On main, Allocate waits for each BindRequest Create before scheduling the next Pod. Creating the remaining BindRequests in parallel removes that one-at-a-time API wait. Dropping operations replaced by later actions avoids unnecessary API calls.

Waiting until session end also lets the scheduler see all action decisions before changing Kubernetes. It can cancel or combine operations and control their order, dependencies, and Events. Today actions commit as they finish, so later actions cannot change decisions already sent to the cluster.

For example, fixing [issue #2274](https://github.com/kai-scheduler/KAI-Scheduler/issues/2274) can allow reclaim to evict a Pod placed only in session memory by an earlier action. Waiting until session end lets the placement and eviction cancel before either reaches Kubernetes.

### Goals

- Create the remaining BindRequests in parallel instead of one at a time, and run independent evictions in parallel.
- Avoid Kubernetes writes for canceled or replaced operations. Build one plan for the session that can cancel, combine, and order operations after all actions finish.
- Cancel an allocation or pipeline if a later action evicts the same pending Pod in that session. The pair must not create a BindRequest, deletion, scheduler Event, Pod condition, or PodGroup status change. Keep evictions of Pods already active at session open.
- Let later actions see placements and evictions in session memory. Keep changes to `Session`, plugins, and `NodeInfo` on the scheduler goroutine. Workers publish accepted BindRequests through the thread-safe informer store.
- Avoid duplicate BindRequests during retries, publish successful requests to the scheduler cache, and require Binder to check the target Pod UID.

### Non-Goals

- Making all Kubernetes BindRequests, Pod deletions, Events, and status changes succeed or fail together.
- Rebinding an already bound Pod to a different node or GPU placement under the same UID.
- Canceling API effects from a previous session, another scheduler, or a controller.
- Parallel placement or scenario simulation.
- Changing queue fairness or scenario-validation job selection in this change.

## Proposal

Actions still run in configured order. Successful statements still update session state and plugin resource accounting immediately. Instead of calling Kubernetes, they add records to a session journal: a list of planned operations. These records are copies that do not change when session state changes.

Every action and planning hook must use this path. After the last action and any plugin planning work finish, the scheduler combines the records into a final plan. This step is called normalization. Only operations left in that plan are sent to workers. Their Kubernetes status changes and Events are reported after the final plan is known.

### Expected behavior

| Session start | Actions within session | End-of-session effect |
| --- | --- | --- |
| Pod Pending, no BindRequest | Allocate, then later eviction of same UID | Neither bind nor eviction; original Pod remains Pending; no scheduler-created placement/eviction Event |
| Pod Pending, no BindRequest | Pipeline, then later eviction of same UID | Neither pipeline Event nor deletion; original Pod remains Pending |
| Pod Pending, no BindRequest | Allocate or pipeline, no later eviction | Allocate creates a BindRequest; pipeline emits a pipeline Event |
| Pod already active | Later eviction | Delete original UID and report real eviction |
| Pod already active | Evict, then restore its same placement before dispatch | No delete; preserve original Pod |

Cancellation uses the Pod UID and its state at session open, not just namespace/name. It applies only to Pods that were Pending without a BindRequest at session open. Existing BindRequests still follow the current Binder and failed-request cleanup paths. The final plan has at most one placement or deletion decision per Pod UID. Labels and Events follow only a decision kept in that plan.

### Limitations and risks

- All actions and pre-close planning hooks must finish before any planned Kubernetes change is sent. An early placement must wait for the rest of planning, including slow reclaim or preemption simulations, before Binder can process it. Evictions must also wait. This delay is an accepted drawback: Kubernetes work can no longer run alongside planning. Parallel dispatch may improve total throughput, but cannot remove the initial wait.
- The final plan is kept only in memory. A crash before dispatch loses it; a crash during dispatch can leave some operations completed and others unfinished. The next snapshot and existing stale cleanup recover the current Kubernetes state.
- API acceptance does not mean a Pod has bound or a deletion has completed. Pipelines kept in the final plan continue to wait for capacity in later sessions.
- Parallel API calls can finish out of order. Enforce any real dependencies, but do not promise that all operations for a gang succeed or become visible together.
- A deferred queue can grow with the number of planned tasks. Measure peak entries and memory on scale workloads.
- Other controllers may produce Kubernetes Events for the same Pod. Only scheduler operations canceled before dispatch are guaranteed not to emit Events.

## Design Details

### Session lifecycle

```text
Open session and record original Pod identities before plugin hooks
  -> run configured actions in order; accept successful in-memory statements
  -> finish pre-close planning hooks (including background-pod restoration)
  -> build final plan from journal; restore state for canceled operations
  -> prepare API request copies that will not change
  -> dispatch remaining operations; workers publish accepted BindRequests
  -> wait for all workers; apply results on scheduler goroutine
  -> close plugin state; record final PodGroup/Pod statuses; release session
```

`Statement.Discard` and rollback still undo simulation operations that have not been accepted. A statement's operations can be accepted only once. All action loops continue using task, node, and plugin state in session memory. The session starts API work only after the final plan is ready.

Session closing needs a planning phase before plugins release their state. `backgroundpods.OnSessionClose` currently restores background Pods and commits remaining evictions. Move that planning work into the pre-close phase so it can add operations while plugin resource accounting is still available. `OnSessionClose` then releases state. Stale gang eviction must also use an in-memory statement and add its operations to the journal instead of calling `Session.Evict` directly. Check other direct cache writes and plugin hooks before making this the only commit path.

### Journal and normalization

Each accepted statement adds ordered records with the action that created them, Pod namespace/name/UID, original state identity, operation kind, placement or eviction details, and a sequence number. Workers must not receive the session's mutable `PodInfo` or a live `Statement`. Their placement data must include GPU, NUMA, DRA, labels, annotations, and node selection after plugin changes.

`Statement.Evict`, `Statement.Allocate`, `Statement.Pipeline`, and restore operations check journal rules against the current in-memory state. They reject duplicate evictions, conflicting operations, and attempts to bind an already active Pod again under the same UID. These checks happen before changing state or recording an operation. Existing scheduling paths remain responsible for resource-fit checks; statements do not repeat them. Actions handle journal errors during planning. Building the final plan combines valid operations; it must not hide invalid plans by dropping individual Pods or ending the session.

The scheduler goroutine combines records for each UID using that Pod's state at session open:

1. Ignore operations undone within their statement.
2. Keep each accepted in-memory change visible to later actions, but combine its Kubernetes operations at session end.
3. Cancel an Allocate/Pipeline followed by Evict when the Pod was Pending at session open and no prior external placement existed.
4. Preserve an eviction of a Pod active at session open unless it was restored without changing its original placement. A changed placement of an already bound Pod cannot become a new BindRequest for that UID.

   Existing reclaim/consolidation simulations may pipeline an evicted active Pod elsewhere to model its future replacement. Keep that simulation and the eviction of the original UID. Do not create a new BindRequest or pipeline Event for that UID.
5. Restore canceled pending tasks before recording job status: remove their in-memory node resource usage, clear placement, and return them to their original Pending state. The Allocate and Deallocate plugin callbacks have already canceled out each other's resource changes; do not call Deallocate again. Check that final node and queue accounting matches the opening state plus the operations kept in the final plan.

Evicting a Pod newly allocated or pipelined in session memory cancels its placement; it does not free resources in Kubernetes. Existing placement logic must still distinguish capacity free now, including capacity returned by a canceled in-memory allocation, from capacity expected to become free after a real eviction. Work that needs a real eviction remains pipelined and never creates a BindRequest in this session.

An accepted eviction immediately marks the Pod Releasing, so later actions cannot select it as a victim again. Keep only one eviction per UID, with its action's reason and the job requesting resources (the preemptor). If an eviction is undone and a later action evicts the restored Pod, keep only the later eviction. A canceled pair emits no Events or status updates from either operation.

#### Baseline capture

Before plugin hooks, record only each Pod UID's opening status and whether it has a BindRequest. Check both task references and raw snapshot-map entries, including failed requests. Keep opening job-start timestamps separately. These records contain no Kubernetes objects; do not deep-copy every Pod at session opening.

Capture a baseline—a separate copy of the original state—only for Pods that planning may change. Keep it separate from accepted operations and retain it when operations are replaced, rolled back, or restored through accepted Unevict. Include status, node, virtual-status marker, fractional GPU groups, NUMA placement, DRA claim allocations, extended resource claim UID/allocation, accepted GPU requirement/resource vector, and received resource type. Deep-copy nested placement values and copy them again when restoring state. The baseline is not an action to execute.

Capture only once per UID, before pre-predicate/predicate callbacks, NUMA evaluation, fractional GPU selection, or direct Statement changes, including background-pod eviction during opening. Capturing at acceptance or only in `Statement.Allocate` is too late. Opening NUMA hooks load actual placement from saved records. Keep that original placement separate from changes made during planning, without changing hook order or resource accounting. Never build a baseline from later in-memory planning state.

Each operation's undo record holds the state just before that operation, not the session baseline or an accepted-operation copy. Eviction undo needs only node, status, virtual marker, GPU groups, NUMA placement, and DRA claims. Workers must continue to use separate copies that session changes cannot affect. Accepted allocation snapshots must still give annotation callbacks access to the full `PodInfo`; changing what those callbacks can read is a separate change.

### External effects and ordering

After the final plan is ready, a limited worker pool makes API calls using request copies that will not change. Workers publish accepted BindRequests directly to the thread-safe informer store. Each worker inserts a deep copy immediately after a successful Create or a matching `AlreadyExists` result. This keeps `origin/main` publication behavior, without another reservation layer or regular Get calls to refresh state.

Workers do not call session methods or emit Events. BindRequest Creates and Pod Deletes share one worker limit: `max(1, ceil(k8sClientQPS))`. Each client still applies its own rate limiter. The journal holds the operations waiting to run. Only one operation per UID can reach workers, so independent Pods can run in parallel. An allocation that needs a real eviction to finish remains pipelined and is not bound at this point.

BindRequest workers use a fixed name for each Pod. Make at most five attempts for temporary API errors, with a 20 ms delay before the first retry. On `AlreadyExists`, use a direct Get and check that the existing request matches the planned request. Cancellation stops queued and in-flight attempts. Before binding or updating Pod status, Binder checks that the fetched Pod UID matches the BindRequest's Pod owner-reference UID.

Eviction workers must check the target UID and use a Kubernetes Delete UID precondition. Never delete a same-name replacement. An optional Pod-condition patch for an eviction kept in the plan also needs a UID check enforced by the API server. Refactor `SchedulerCache.Evict` so the worker reports the Delete result instead of starting another untracked goroutine. Report eviction status and Events only for a confirmed successful Delete request, not for a canceled operation or failed precondition.

After all workers finish, the scheduler goroutine reads results, updates task state, and reports Events and status changes for the remaining operations. `StatusUpdater.PreBind`, Pod-label patches, `Scheduled`, `Pipelined`, and eviction reporting move to this step. Guard Pod-label and status writes by UID so they cannot affect a same-name replacement. Run `RecordJobStatusEvent` only after building the final plan and handling results; otherwise a canceled in-memory change could appear as a PodGroup status or Event. Workers must not change plugin resource accounting. Direct store insertion keeps the existing race with newer informer watch updates; this change does not add checks to prevent overwriting newer entries.

### Failures, shutdown, and restart

- A canceled operation sends no API request, so there is no Kubernetes change to undo.
- A failed BindRequest Create keeps its in-memory gang slot for the rest of the session. The next snapshot restores the view of actual Kubernetes state.
- A failed Delete, or one whose result is unknown, must not be treated as freed capacity. Report failure and check again next session. Work that needs that capacity remains pipelined.
- Retry temporary Delete errors with the same five-attempt limit and 20 ms initial delay. If the Delete result is unknown, use a direct Get and UID comparison to check whether the original Pod still exists, is NotFound, or has been replaced. Never delete by name after a UID mismatch.
- Losing scheduler leadership cancels queued and in-flight work. Wait for workers before releasing session state. Do not undo successful API operations already accepted by Kubernetes.
- A canceled pending Pod must not receive delayed status updates from this session. Check queued status work by UID against the final plan.

### Related scenario-validation work

The separate scenario-validation fix can reduce simulation work by ordering only the job requesting resources and the proposed victim jobs. It must keep the same ordering rules for those jobs and use the full queue when a plugin requires it. The session journal allows an earlier in-memory placement to be canceled if that Pod becomes a later victim. Add the filtering patch after session cancellation tests pass.

### Implementation scope

- `framework.Statement`: check journal rules, add valid operations as record copies that will not change, and keep rollback behavior for statements not yet accepted.
- `framework.Session` and scheduler run loop: manage the journal, pre-close planning, final plan, limited worker pool, and waiting for workers before applying results.
- Allocate/Reclaim/Preempt/Consolidation/StaleGangEviction: accept in-memory statements without calling Kubernetes; move stale gang eviction to this path.
- `backgroundpods` and session lifecycle: separate final planning from releasing plugin state.
- `cache` and `status_updater`: separate API calls, informer publication, status/Event reporting, and UID-safe eviction. Move `PreBind` to final processing of remaining binds.
- No CRD or user-facing configuration change is proposed. BindRequest Creates and Pod Deletes share a worker limit based on QPS.

### Monitoring

Update existing eviction counters only after confirmed successful Deletes. Count each Pod and keep its subgroup labels. Count a batch once for each original accepted statement / PodGroup / eviction action combination that has at least one successful Delete. Do not count canceled or failed evictions, or Deletes whose result is unknown.

Record operation counts by kind and action: planned, canceled, kept in the final plan, dispatched, succeeded, and failed. Also record queue depth, dispatch time, time waiting for workers, API retry/throttle rates, BindRequest and eviction latency, informer publication failures, and unexpected same-UID conflicts. Do not use Pod UID as a metric label. Keep structured logs for each session with action sequence, Pod UID, the reason an operation was kept or canceled, and API result. Compare `Scheduled`, `Pipelined`, and `Evict` Event counts with the final plan in tests and during rollout.

### Test plan

1. Unit tests for adding statements to the journal, filtering undone operations, combining records by UID, plugin-state consistency, and final job status. Verify invalid journal transitions, duplicate evictions, conflicting operations, and same-UID rebinding fail at the statement call without changing state or recording an operation. Cover capture before preparation and opening hooks, keeping baselines through rollback/replacement/Unevict, deep copies of nested GPU/NUMA/DRA values, and canceled job-start timestamps. Verify existing hook order and accounting remain unchanged.
2. Session tests for Allocate -> Reclaim/Preempt/Consolidation cancellation, pipeline -> eviction cancellation, active-Pod eviction, same-placement Unevict, stale gang eviction, and background-pod restoration.
3. Fake-client tests checking zero Create/Delete/Event/status calls for canceled pairs and exactly one UID-safe effect for each operation kept in the plan. Include same-name replacement and unknown API results.
4. Envtest with a real API server and Binder: no BindRequest or Pod deletion for a canceled pending Pod; remaining BindRequests are visible before the next session; interrupted or partly failed dispatch recovers on the next session.
5. With the separate issue #2274 fix, run the reclaim regression and E2E workload: reclaim progresses without deleting or emitting scheduler Events for a canceled virtual Pod. Also test a real allocation canceled by later reclaim.
6. Compare the current and proposed versions using the same scale workload and configuration, one run at a time. Measure session planning time, wait from placement decision to dispatch, time to first BindRequest, commit time, time waiting for workers, overall fill time, API/Binder load, p50/p95/p99 Create/Delete latency, peak journal memory, and scheduling results. Benchmark session opening alone, planning that uses few or all Pods, and repeated replacement of accepted operations across Pod sizes, including GPU/NUMA/DRA. Measure memory allocations and retained memory before claiming that fewer copies improve performance. Measure scenario-validation CPU separately when adding the related fix.

### Readiness criteria

1. **Initial version:** journal covers all default actions and background-pod planning; canceled pending Pods cause no Kubernetes changes; focused unit and envtests pass.
2. **Correctness with related changes:** reclaim E2E passes with the separate scenario-validation fix; UID-safe retry and failure paths are tested; no regressions in gang, elastic, GPU-sharing, NUMA, and DRA paths.
3. **Ready for production:** comparable scale runs show acceptable overall cost, API/Binder health is stable, journal memory does not grow without a limit in tested workloads, and monitoring separates canceled work from failed API work.

## Alternatives

- **Create BindRequests during Allocate:** could overlap API calls with later planning, but BindRequests and pipeline Events can escape before later actions cancel their Pod.
- **Delete a BindRequest after later eviction:** Binder may already have bound the Pod; deleting the request cannot undo Events or other changes.
- **Reorder actions or ban pipelined victims:** may avoid the reported problem but does not allow general cancellation between actions.
- **Run all end-of-session API calls one at a time:** simplifies ordering but gives up parallel API writes.
