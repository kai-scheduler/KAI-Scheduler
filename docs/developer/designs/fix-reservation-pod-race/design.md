# Fix: Reservation Pod Premature Deletion Race Condition

## Problem Statement

The binder's resource reservation sync logic (`syncForPods`) prematurely deletes
GPU reservation pods when concurrent bind operations race with informer cache
propagation. This manifests as a flaky E2E test failure where reservation pods
disappear during the binding of fractional GPU pods to the same node.

## Root Cause

### The Deletion Logic

`syncForPods` in `pkg/binder/binding/resourcereservation/resource_reservation.go`
decides to delete a reservation pod when it finds no "fraction pods" (user
workload pods in Running/Pending phase) for that GPU group:

```go
for gpuGroup, reservationPod := range reservationPods {
    if _, found := fractionPods[gpuGroup]; !found {
        // DELETE reservation pod
    }
}
```

The pod list comes from `syncForGpuGroupWithLock`, which queries the informer
cache using label selectors (`runai-gpu-group=<group>`).

### The Race

There are 3 independent triggers that call `SyncForGpuGroup`:

| Trigger | When | Location |
|---|---|---|
| `Binder.Bind()` → `SyncForNode()` | Start of pod binding | `binder.go:44` |
| `BindRequestReconciler.deleteHandler()` | BindRequest deleted | `bindrequest_controller.go:217` |
| `PodReconciler.syncReservationIfNeeded()` | Pod deleted/completed | `pod_controller.go:122` |

With `MaxConcurrentReconciles=10`, multiple BindRequests are reconciled in
parallel. The race occurs as follows:

1. **Thread A** (binding Pod 1 to Node A, GPU group X):
   - `ReserveGpuDevice()` creates reservation pod for group X
   - `updatePodGPUGroup()` patches Pod 1 with label `runai-gpu-group=X`
   - The label patch is sent to the API server → API server persists it →
     Watch event is generated → informer cache receives the event

2. **Thread B** (binding Pod 2 to Node A, same GPU group X):
   - `Bind()` calls `SyncForNode("NodeA")`
   - `SyncForNode` lists pods on the node, finds the reservation pod (which
     has `runai-gpu-group=X`), extracts group X
   - Calls `SyncForGpuGroup("X")` → `syncForGpuGroupWithLock`
   - Lists pods with label `runai-gpu-group=X` from **informer cache**
   - **Cache lag**: Pod 1's label patch has not propagated to the cache yet
   - Finds: reservation pod (in binder namespace) → `reservationPods["X"]`
   - Does NOT find Pod 1 (label not in cache yet) → `fractionPods["X"]` is empty
   - **Deletes the reservation pod** ← BUG

The `gpuGroupMutex` provides per-group serialization but does NOT prevent this
race because Thread B's `SyncForNode` observes the reservation pod (just
created, already in cache) but not Pod 1's updated labels (patch not yet in cache).

### Why the Cache Shows the Reservation Pod but Not the Label

- The reservation pod is a **new object** (CREATE event) — informer receives
  it quickly
- Pod 1's label is an **update to an existing object** (UPDATE/PATCH event) —
  may be in a different event batch or processed after the CREATE
- The informer processes events sequentially per type, but CREATE and UPDATE
  events for different objects can have different propagation times

## Fix: Check Active BindRequests Before Deleting Reservation Pods

### Approach

Before deleting a reservation pod due to missing fraction pods, check if there
are any **active (non-succeeded, non-failed) BindRequests** that reference this
GPU group. If any exist, skip the deletion — a binding operation is in progress
and the fraction pod label hasn't propagated yet.

### Why This Works

The BindRequest lifecycle provides a durable intent signal:

1. A BindRequest is created by the scheduler **before** the binder starts
   labeling pods
2. A BindRequest is NOT deleted until **after** binding succeeds (and the
   scheduler cleans it up) or permanently fails
3. BindRequests contain `SelectedGPUGroups` which identifies which GPU groups
   are in-flight

So during the cache lag window (reservation pod visible, pod label not visible),
the BindRequest is guaranteed to still exist. Checking for it prevents the
false-negative deletion.

### Logic Change

```go
// BEFORE (unsafe):
for gpuGroup, reservationPod := range reservationPods {
    if _, found := fractionPods[gpuGroup]; !found {
        deleteReservationPod(reservationPod)
    }
}

// AFTER (safe):
for gpuGroup, reservationPod := range reservationPods {
    if _, found := fractionPods[gpuGroup]; !found {
        if hasActiveBindRequestsForGpuGroup(ctx, gpuGroup) {
            logger.Info("Skipping reservation pod deletion, active BindRequests exist",
                "gpuGroup", gpuGroup)
            continue
        }
        deleteReservationPod(reservationPod)
    }
}
```

### Implementation Details

1. **Add BindRequest listing capability to the resource reservation service**:
   The `service` struct needs access to list BindRequests. Since it already has
   `kubeClient client.WithWatch`, and the scheme includes `schedulingv1alpha2`,
   we can list BindRequests directly using the same cached client.

2. **Filter logic**: List all BindRequests, check if any have:
   - `Status.Phase` is NOT `Succeeded` and NOT `Failed` (with exhausted retries)
   - `Spec.SelectedGPUGroups` contains the GPU group in question
   - `Spec.ReceivedResourceType` is `Fraction` (only fractional allocations use
     GPU groups)

3. **Pass the function/checker to `syncForPods`**: Either modify `syncForPods`
   to accept a checker function, or have `syncForGpuGroupWithLock` perform the
   check before calling `syncForPods`, or integrate it directly into
   `syncForPods`.

### Downsides / Considerations

- **Slight cleanup delay**: If a BindRequest exists but binding has failed and
  the BindRequest hasn't been cleaned up yet, the reservation pod lingers until
  the next sync after cleanup. This is safe — just delayed cleanup.
- **Additional List call**: One cached List of BindRequests per sync. Since this
  goes through the informer cache, it's cheap (no API server load).
- **BindRequest scheme registration**: The `kubeClient` used by the resource
  reservation service must have the `schedulingv1alpha2` scheme registered.
  This is already the case in production (see `cmd/binder/app/app.go`), but
  needs verification in tests.

## Files Modified

- `pkg/binder/binding/resourcereservation/resource_reservation.go`: Added
  `hasActiveBindRequestsForGpuGroup` check in `syncForPods`
- `pkg/binder/binding/resourcereservation/resource_reservation_test.go`: Registered
  `schedulingv1alpha2` scheme so the fake client can list BindRequests
- `pkg/binder/controllers/integration_tests/reservation_race_test.go`: Integration
  test reproducing the race with the full binder controller
- `pkg/env-tests/reservation_race_scale_test.go`: Scale envtest (4 nodes × 8 GPUs)
  with mock device plugin goroutine
- `test/e2e/suites/allocate/resources/reservation_pod_race_specs.go`: E2E stress
  test binding 32 fractional GPU pods concurrently

## Test Plan

### Integration Test (reservation_race_test.go)

Full binder controller integration test in
`pkg/binder/controllers/integration_tests/reservation_race_test.go`:

- Starts the real binder controller with `MaxConcurrentReconciles=10`
- Creates a node with 8 GPUs, a queue, and 32 fraction pods with BindRequests
- Lets the controller bind all pods concurrently, triggering the race window
- Verifies all 32 reservation pods survive (none prematurely deleted)

### Scale EnvTest (reservation_race_scale_test.go)

Scale reproduction test in `pkg/env-tests/reservation_race_scale_test.go`:

- Runs the full binder controller autonomously (4 nodes × 8 GPUs = 32 groups)
- Includes a goroutine that simulates the GPU device plugin by patching
  reservation pods with GPU index annotations and heartbeat timestamps
- Pre-creates shared-GPU ConfigMaps referenced by fraction pod annotations
- Verifies all 32 reservation pods survive concurrent binding

### E2E Stress Test (reservation_pod_race_specs.go)

Dedicated stress test in
`test/e2e/suites/allocate/resources/reservation_pod_race_specs.go`:

- Submits 32 fractional GPU pods to a single node in a real cluster
- Waits for all pods to reach Running state
- Verifies all reservation pods remain present after binding completes
