<!--
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# DRA-Backed Extended Resources

*Status: Draft*

Related: [KEP-5004](https://kep.k8s.io/5004) (alpha v1.34, beta-on-by-default v1.36, GA v1.37)

## Motivation

Dynamic Resource Allocation (DRA) is the upstream path for managing GPU and other accelerator devices going forward. Cluster administrators who migrate device management to a DRA driver today break every existing workload that uses the classic extended-resource syntax (`nvidia.com/gpu: 2`), because DRA-only nodes carry no `nvidia.com/gpu` entry in `node.Status.Allocatable`.

KEP-5004 solves this at the Kubernetes level: a DeviceClass can declare an `ExtendedResourceName`, and the scheduler synthesizes a special ResourceClaim for pods that use that name, routing allocation through the DRA machinery while the pod spec stays unchanged.

KAI needs to support this flow so that workloads using extended-resource syntax can be scheduled onto DRA-managed nodes without modification, and so quota, fairshare, and preemption accounting remain correct throughout.

## Goals / Non-Goals

**Goals**

- Accept `nvidia.com/gpu: N` on pods targeting DRA-only nodes, with correct quota, fairshare, fit, and preemption accounting.
- Accept other DRA-backed extended resources on pods targeting DRA-only nodes (allocation via the DRA allocator; fit delegated to the allocator rather than KAI's vector check).
- Work for any extended resource name without requiring changes to workload APIs or PodGroup schemas.

**Non-Goals**

- Quota and fairshare accounting for non-GPU DRA-backed extended resources (capacity injection from ResourceSlices deferred to a follow-up; GPU accounting is in scope because KAI's existing GPU vector is used cluster-wide for fairshare and preemption).
- Fractional / GPU-sharing requests on DRA-backed extended resources (phase 2; the existing DRA consumable-capacity path handles that separately).
- MIG resources via DRA extended-resource bridge (can be added when a DeviceClass → MIG-profile mapping is defined upstream).

## Background: What the Codebase Already Has

Most of the infrastructure is already in place:

| Layer | Existing mechanism |
|---|---|
| Node capacity (GPU) | `AddDRAGPUs` injects DRA GPU count into `AllocatableVector` / `IdleVector` at `GPUIndex`, counted from ResourceSlices in `cluster_info.go` |
| Task request (GPU) | `ExtractDRAGPUResourcesFromClaims` + `GpuRequirement.SetDraGpus` unifies DRA claim GPUs into `ResReqVector[GPUIndex]` |
| Task request (all other scalars) | Extended-resource container requests flow through `ResourceFromResourceList` → `scalarResources` map → `ResReqVector` via `ToVector` |
| Fit check (scalars) | `lessEqualVectorsExcludingGPU` does element-wise vector comparison for all dimensions including scalar extended resources |
| Quota / fairshare | `AcceptedResourceVector` / `ResReqVector` are consumed by proportion and capacity plugins — no resource-specific logic |
| DRA allocator | `pkg/scheduler/plugins/dynamicresources` already calls `structured.NewAllocator` and handles claim allocation / deallocation |
| API types | `DeviceClass.Spec.ExtendedResourceName`, `Pod.Status.ExtendedResourceClaimStatus`, and the `resource.kubernetes.io/extended-resource-claim` annotation are all present in `k8s.io/api@v0.35.4` |
| Helper library | `k8s.io/dynamic-resource-allocation/deviceclass/extendedresourcecache` (already a dependency) maintains the `extendedResourceName → DeviceClass` reverse index |

There is also an explicit temporary guard ([`node_info.go:316`](../../pkg/scheduler/api/node_info/node_info.go)) that rejects extended-resource GPU requests on DRA-only nodes, with a comment marking it for removal once this feature exists.

## Design

### 1. Fit Check: Skip DRA-Backed Extended Resources

Upstream (`noderesources/fit.go:shouldDelegateResourceToDRA`) skips the vector fit check for any extended resource that is not in `node.Status.Allocatable` and has a DeviceClass mapping — the DRA allocator is the sole source of truth for those resources. Non-GPU DRA ResourceClaim pods already behave this way (they carry no resource quantity in container requests).

KAI adopts the same approach. In `lessEqualVectorsExcludingGPU` (or just before it), for each scalar resource dimension in the request vector: if the resource is not in the node's `Allocatable` and `ExtendedResourceCache.GetDeviceClass(resourceName) != nil`, skip that dimension and let the DRA allocator decide.

This means no capacity injection is needed for non-GPU extended resources — the fit check delegates them to the DRA allocator, exactly as today's ResourceClaim pods are handled.

### 2. Node Capacity Injection (GPU only)

GPU extended resources are a special case: KAI's own GPU accounting (`GPUIndex` in the resource vector) is used for quota, fairshare, preemption, and GPU-sharing decisions across the entire scheduler. This accounting must remain correct when GPUs are managed by DRA.

The existing `populateDRAGPUs` loop already injects DRA GPU capacity from ResourceSlices into `AllocatableVector`/`IdleVector`. It is generalized to also handle `ExtendedResourceName`-mapped DeviceClasses: for a DeviceClass whose `ExtendedResourceName` is `nvidia.com/gpu` (or any GPU resource name), count devices from node-local ResourceSlices and add them at `GPUIndex`.

The existing loop already counts all devices from node-local ResourceSlices with no selector evaluation — matching the behaviour of today's `populateDRAGPUs`. The same approach is kept here: all devices in matching slices are counted regardless of `Spec.Selectors`.

```
for each DeviceClass with ExtendedResourceName:
  if extendedResourceName already in node.Status.Allocatable: skip
  if extendedResourceName is a GPU resource:
    for each ResourceSlice on this node:
      gpuCount += len(slice.Spec.Devices)
  nodeInfo.AddDRAGPUs(gpuCount)
```

`HasDRAGPUs` is kept to drive the GPU-sharing guard until GPU sharing via DRA is implemented.

### 3. Task Request

For GPU extended resources: the request already lands in `ResReqVector[GPUIndex]` via the `scalarResources` path. No change needed.

For non-GPU extended resources: the request lands in the scalar resource vector dimension. The fit-check skip in section 1 ensures it is not compared against (absent) node capacity. Quota and fairshare accounting still see the resource quantity in `ResReqVector` — no special-casing needed there.

### 3a. IdleVector: Exclude DRA-Delegated Dimensions

When a pod with a non-GPU DRA-backed extended resource is assigned to a node, `addTaskResources` subtracts its request from `IdleVector`. Because `AllocatableVector` has 0 for resources not in `node.Status.Allocatable`, this drives `IdleVector` negative for those dimensions.

Negative `IdleVector` values are not merely cosmetic: `calcSubTreeFreeResources` and `calcNodeAccommodation` in the topology plugin sum `IdleVector` across nodes and compare it against job requests using the plain `ResourceVector.LessEqual` (no DRA-aware skip). Negative values would cause topology pre-filtering to falsely reject topology domains that the DRA allocator would have accepted.

Fix: in `addTaskResources` and `removeTaskResources`, before applying the vector to `UsedVector` / `IdleVector` / `ReleasingVector`, zero out any scalar dimension `i > PodsIndex` where `ni.AllocatableVector.Get(i) == 0`. This condition reliably identifies resources for which KAI delegates capacity decisions to the DRA allocator — no DeviceClass cache lookup is needed at this layer. CPU, memory, GPU, and pods always have non-zero allocatable on real nodes, so they are unaffected.

### 4. DRA Allocator: Special-Claim Synthesis

When the dynamicresources plugin sees a pod with no `pod.Spec.ResourceClaims` but whose container requests include an extended resource name backed by a DeviceClass, it synthesizes an in-memory "special claim" and runs it through `structured.NewAllocator`:

```
preFilter / allocateHandlerFn:
  for each container resource request:
    if extendedResourceCache.GetDeviceClass(resourceName) != nil:
      build in-memory ResourceClaim targeting that DeviceClass
      request count = resource request quantity
  if special claim built:
    run through existing structured.Allocator path
    store allocation result in session state (keyed by pod UID)
```

The upstream reference implementation is `k8s.io/kubernetes@v1.35.4/pkg/scheduler/framework/plugins/dynamicresources/extendeddynamicresources.go`. The claim is named `<extended-resources>` in memory and gets a temporary UID; it is never written to the API server during scheduling.

At bind time (via BindRequest), the binder creates the real ResourceClaim in the API server (annotated `resource.kubernetes.io/extended-resource-claim`), finalizes the allocation, reserves it for the pod, and patches `pod.Status.extendedResourceClaimStatus`. This follows the same sequence as `createExtendedResourceClaimInAPI` upstream.

### 5. Double-Count Guard

Pods bound via the extended-resource bridge will have both a container extended-resource request and a generated ResourceClaim. The existing `ExtractDRAGPUResourcesFromClaims` (and any future equivalent) must skip claims carrying the `resource.kubernetes.io/extended-resource-claim` annotation, so the resource is counted only once — from the container request, not the claim.

### 6. Fit Guard Removal

Once the above is in place, the temporary guard in `node_info.go:316` that rejects extended-resource GPU requests on DRA-only nodes can be removed. Its comment already marks this intent.

## Component Summary

| Component | Change |
|---|---|
| `node_info.go` — `lessEqualVectorsExcludingGPU` | Skip scalar dimensions backed by a DRA DeviceClass and absent from `node.Status.Allocatable` |
| `node_info.go` — `addTaskResources` / `removeTaskResources` | Zero out scalar dimensions where `AllocatableVector[i] == 0` before applying to `UsedVector` / `IdleVector` / `ReleasingVector` |
| `cluster_info.go` — `populateDRAGPUs` | Generalize to handle GPU DeviceClasses with `ExtendedResourceName`; wire `ExtendedResourceCache` |
| `cluster_info.go` — session init | Build `ExtendedResourceCache` from DeviceClass informer |
| `dynamicresources` scheduler plugin | Detect extended-resource requests backed by DRA; synthesize and allocate in-memory special claim |
| `node_info.go` | Remove temporary guard at line 316 |
| `resource_info` / `dra_resource_utils.go` | Skip `extended-resource-claim`-annotated claims in claim extraction |
| binder `dynamicresources` plugin | Create real ResourceClaim in API, patch pod status at bind time |

## Open Questions

- **Multi-resource extended resources**: a pod requesting multiple DRA-backed extended resource types produces a single special claim whose `Spec.Devices.Requests` contains one entry per (container, resource type) combination, named `container-{i}-request-{j}`. The allocator handles all resource types in a single `Allocate` call.
- **Binder idempotency**: if the ResourceClaim is created but the pod status patch fails, the binder must not create a second claim. Use deterministic naming (e.g., `<pod-name>-<resource-name>`) matching upstream, so a second attempt finds the existing claim.
- **Quota for generated claims**: generated claims should not count toward queue claim quotas (if any such limit is added). Filter by annotation.
