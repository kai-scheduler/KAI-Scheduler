<!--
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# Vectorizing Resource Representation

## Overview

This document describes the design for converting KAI Scheduler's resource representation from discrete struct-based types to vector-based representations. The vectorization enables efficient bulk operations on resource data, facilitating faster scenario feasibility checks in the scheduler's allocation and reclaim logic.

The goal is to improve scheduler performance at scale (2000+ nodes) by accelerating the scenario filtering phase of the reclaim algorithm through vectorized resource comparisons and multi-resource bin-packing heuristics.

## Motivation

Current scheduler performance degrades significantly with cluster scale:

- **Scale test performance**: Full scheduling cycles take 3-4 minutes for 1000 nodes, 20+ minutes for 1000+ nodes for some test cases (observed in the [scale test cluster](https://github.com/kai-scheduler/KAI-scheduler/tree/main/test/e2e/scale))
- **Bottleneck**: Node ordering functions dominate during allocation simulations (filtering scenarios)
- **Root cause**: Resource comparisons iterate over individual nodes and resources in sequence; no bulk operations

With topology-aware scheduling, time-aware scheduling, and large reclaim scenarios becoming more common, the scheduler will face increasingly complex allocation decisions. Vectorizing resources allows:

1. **Vectorized comparisons**: Compare resources for multiple nodes concurrently (the vector-per-node layout enables goroutine-based parallelism; future refactors can adopt a column-major layout for SIMD iterations)
2. **Efficient bin-packing**: Use normalized resource metrics (sum or DRF) for node sorting heuristics
3. **Scenario filtering acceleration**: Pre-compute vector representations to enable quick feasibility checks

## Goals / Non-Goals

### Goals

- Design a vector representation for resources that maintains semantic equivalence with current Resource and ResourceRequirements types
- Enable efficient resource comparisons and arithmetic operations (add, subtract, less-than-or-equal)
- Support multi-resource scenarios (CPU, memory, GPUs, custom resources, MIG profiles)
- Provide clear migration path from struct-based to vector-based representations
- Maintain backward compatibility during transition

### Non-Goals

- Redesign the scenario filtering algorithm itself (only optimize existing heuristics)
- Change the dominant-resource-fairness (DRF) algorithm for fairness calculations
- Implement concurrent/parallel scenario filtering (prerequisite for future work)

## Current Implementation

### Resource Type

The current resource representation uses a struct with discrete fields:

```go
// pkg/scheduler/api/resource_info/resource_info.go
type Resource struct {
    BaseResource
    gpus float64
}

// pkg/scheduler/api/resource_info/base_resources.go
type BaseResource struct {
    milliCpu        float64
    memory          float64
    scalarResources map[v1.ResourceName]int64
}
```

**Key operations**:
- `Resource.LessEqual(other *Resource) bool` - Check if resource requirements can fit
- `Resource.Add(other *Resource)` - Aggregate resources
- `Resource.Sub(other *Resource)` - Remove allocated resources
- `Resource.Get(resourceName) float64` - Retrieve specific resource value

### ResourceRequirements Type

Pod and job resource requirements use a similar structure:

```go
// pkg/scheduler/api/resource_info/resource_requirment.go
type ResourceRequirements struct {
    BaseResource
    GpuResourceRequirement
}

// GpuResourceRequirement supports both whole and fractional GPU allocation
type GpuResourceRequirement struct {
    count   float64 // Number of whole GPUs
    portion float64 // Fractional GPU portion
}
```

**Limitations**:
1. **Struct overhead**: Each resource allocation carries full struct overhead
2. **Sequential comparisons**: LessEqual iterates field-by-field
3. **Map overhead**: scalarResources map has lookup/iteration overhead
4. **No bulk operations**: Cannot compare multiple resources in a vectorized manner

### Current Usage Locations

The `Resource` and `ResourceRequirements` structs are used throughout the scheduler codebase. This section documents all locations that will need to be migrated to vector-based representations.

#### Core API Struct Fields

| File | Field | Type |
|------|-------|------|
| `api/node_info/node_info.go` | `Releasing`, `Idle`, `Used`, `Allocatable` | `*resource_info.Resource` |
| `api/podgroup_info/job_info.go` | `Allocated` | `*resource_info.Resource` |
| `api/podgroup_info/job_info.go` | `tasksToAllocateInitResource` | `*resource_info.Resource` |
| `api/pod_info/pod_info.go` | `ResReq`, `AcceptedResource` | `*resource_info.ResourceRequirements` |
| `plugins/topology/topology_structs.go` | `IdleOrReleasingResources` | `*resource_info.Resource` |
| `plugins/proportion/reclaimable/reclaimer_info.go` | `RequiredResources` | `*resource_info.Resource` |
| `k8s_internal/predicates/maxNodeResources.go` | `maxResources` | `*resource_info.Resource` |

#### NodeInfo Resource Field Accessors

Methods and code paths that access NodeInfo's `Idle`, `Used`, `Releasing`, `Allocatable` fields:

| File | Location | Usage |
|------|----------|-------|
| `framework/statement.go` | lines 252, 309 | Logging node resource state |
| `api/node_info/node_info.go` | `NonAllocatedResources()` | Returns `*resource_info.Resource` |
| `api/node_info/node_info.go` | `isTaskAllocatableOnNonAllocatedResources()` | Resource comparison |
| `api/node_info/node_info.go` | `FittingError()` | Error message generation |
| `api/node_info/gpu_sharing_node_info.go` | `getAcceptedTaskResourceWithoutSharedGPU()` | GPU sharing calculations |
| `cache/cluster_info/cluster_info_test.go` | Test assertions | Verifying `node.Idle`, `node.Used` |

#### PodGroupInfo Resource Field Accessors

| File | Location | Usage |
|------|----------|-------|
| `api/podgroup_info/job_info.go` | `GetTasksActiveAllocatedReqResource()` | Returns `*resource_info.Resource` |
| `api/podgroup_info/allocation_info.go` | `GetAllocatedResource()` | Returns `*resource_info.Resource` |

#### Plugins Using `*resource_info.Resource`

| Plugin | File | Functions/Usage |
|--------|------|-----------------|
| **proportion** | `proportion.go` | `totalVictimsResources map`, `getVictimResources()`, `getResources()` |
| **proportion/reclaimable** | `reclaimable.go` | `reclaimeeResourcesByQueue`, `reclaimedResources`, `getInvolvedResourcesNames()` |
| **proportion/reclaimable/strategies** | `strategies.go` | `reclaimerResources` parameter, `reclaimerWillGoOverQuota()` |
| **proportion/utils** | `utils.go` | `QuantifyResource(resource *resource_info.Resource)` |
| **topology** | `job_filtering.go` | `getTasksAllocationMetadata()`, `calcSubTreeFreeResources()`, `sortTree()`, `getJobRatioToFreeResources()` |

#### Error Handling and Display

These locations use Resource structs to generate human-readable error messages:

| File | Function | Usage |
|------|----------|-------|
| `api/common_info/pod_errors.go` | `NewInsufficientNodeResourcesError()` | `usedResource, capacityResource *resource_info.Resource` |
| `api/common_info/job_errors.go` | `NewInsufficientClusterResourcesError()` | `resourceRequested, availableResource *resource_info.Resource` |

#### Test Utilities

| File | Functions |
|------|-----------|
| `test_utils/resources_fake/resources.go` | `BuildResource()` returns `*resource_info.Resource` |
| `test_utils/jobs_fake/jobs.go` | `BuildJobInfo()`, `generateTasks()`, `CalcJobAndPodResources()` |
| `api/common_info/test_utils.go` | `BuildResource()`, `BuildResourceWithGpu()` |
| `framework/statement_test_utils.go` | Test helper structs and functions |

#### Test Files with Resource Assertions

The following test files contain assertions or test data using `*resource_info.Resource`:

- `framework/statement_test.go` - Statement execution tests
- `api/node_info/node_info_test.go` - NodeInfo unit tests
- `cache/cluster_info/cluster_info_test.go` - Cluster snapshot tests
- `plugins/proportion/reclaimable/reclaimable_test.go` - Reclaimable plugin tests
- `plugins/proportion/reclaimable/strategies/strategies_test.go` - Strategy tests
- `plugins/topology/node_scoring_test.go` - Topology scoring tests
- `api/common_info/pod_errors_test.go` - Pod error message tests
- `api/common_info/job_errors_test.go` - Job error message tests

## Vector Representation Design

### Core Types

```go
// pkg/scheduler/api/resource_info/resource_vector.go

// ResourceVector represents a single entity's resources as a fixed-length array.
// All vectors use the same index mapping defined by ResourceVectorMap.
type ResourceVector []float64

// ResourceVectorMap maintains the mapping from indices to resource names.
// This is created once during cluster info snapshot and shared across all nodes and pods.
// Resource names are normalized (e.g., "nvidia.com/gpu" → "gpu").
type ResourceVectorMap struct {
    resourceNames []string
    namesToIndex  map[string]int
}

// NewResourceVectorMap creates a new ResourceVectorMap initialized with core resources
// (CPU, Memory, GPU, Pods) to ensure consistent ordering.
func NewResourceVectorMap() *ResourceVectorMap

// NewResourceVector creates a zero-filled vector of the correct length for the given map.
// All vectors should be created through this factory to guarantee length consistency.
func NewResourceVector(indexMap *ResourceVectorMap) ResourceVector
```

### Resource Vector Mapping Example

For a cluster with resources: CPU, Memory, GPUs, EFA, Custom resources:

```text
resourceVectorMap:
  Index 0: v1.ResourceCPU       → milliCPU value
  Index 1: v1.ResourceMemory    → memory bytes
  Index 2: nvidia.com/gpu       → GPU count
  Index 3: example.com/efa      → EFA count
  Index 4: custom-resource      → custom value

Example Vector:
  Node capacity:  [64000, 256e9, 8, 4, 100]   (64 cores, 256GB memory, 8 GPUs, 4 EFA, 100 custom)
  Pod request:    [1000, 4e9, 0.5, 0, 0]     (1 core, 4GB memory, 0.5 GPU, 0 EFA, 0 custom)
```

### Vector Operations

All operations are methods on `ResourceVector`. When vectors have mismatched lengths, operations
handle this gracefully: `Add`/`Sub` extend the shorter vector, and `LessEqual` treats missing
indices as zero. `Sub` can produce negative values (used to track over-subscription).

```go
// LessEqual checks if all resources in v fit within other.
// Mismatched lengths are handled: extra elements in v must be <= 0,
// extra elements in other must be >= 0.
func (v ResourceVector) LessEqual(other ResourceVector) bool

// Add aggregates resource allocations. Extends v if other is longer.
func (v *ResourceVector) Add(other ResourceVector)

// Sub removes allocated resources. Extends v if other is longer.
// Results can be negative (indicating over-subscription).
func (v *ResourceVector) Sub(other ResourceVector)

// SetMax sets each element of v to the maximum of v[i] and other[i].
func (v ResourceVector) SetMax(other ResourceVector)

// Clone returns a deep copy of the vector.
func (v ResourceVector) Clone() ResourceVector

// Get returns the value at the given index, or 0 if index is out of bounds.
func (v ResourceVector) Get(index int) float64

// Set sets the value at the given index (no-op if out of bounds).
func (v ResourceVector) Set(index int, value float64)

// IsZero returns true if all elements are zero.
func (v ResourceVector) IsZero() bool
```

Normalization metrics for sorting (used in scenario filtering):

```go
// Normalized sum: sum(resource[i] / totalCapacity[i])
func NormalizedSum(vec, totalCapacity ResourceVector) float64 {
    var sum float64
    for i := range vec {
        if totalCapacity[i] > 0 {
            sum += vec[i] / totalCapacity[i]
        }
    }
    return sum
}

// Dominant resource (max ratio): max(resource[i] / totalCapacity[i])
func DominantResource(vec, totalCapacity ResourceVector) float64 {
    var maxRatio float64
    for i := range vec {
        if totalCapacity[i] > 0 {
            ratio := vec[i] / totalCapacity[i]
            if ratio > maxRatio {
                maxRatio = ratio
            }
        }
    }
    return maxRatio
}
```

### Conversion Functions

Conversion between existing `Resource`/`ResourceRequirements` structs and vectors:

```go
// ToVector converts a Resource struct to a ResourceVector using the given map.
func (r *Resource) ToVector(indexMap *ResourceVectorMap) ResourceVector

// FromVector populates a Resource struct from a ResourceVector.
func (r *Resource) FromVector(vec ResourceVector, indexMap *ResourceVectorMap)

// ToVector converts ResourceRequirements to a ResourceVector.
func (r *ResourceRequirements) ToVector(indexMap *ResourceVectorMap) ResourceVector

// FromVector populates ResourceRequirements from a ResourceVector.
func (r *ResourceRequirements) FromVector(vec ResourceVector, indexMap *ResourceVectorMap)

// NewResourceVectorFromResourceList creates a vector from a Kubernetes ResourceList.
func NewResourceVectorFromResourceList(resourceList v1.ResourceList, indexMap *ResourceVectorMap) ResourceVector
```

## Migration Plan

### Phase 1: Type Introduction
- Introduce `ResourceVector`, `ResourceVectorMap` types
- Create conversion functions: `Resource.ToVector()`, `Resource.FromVector()`, `NewResourceVectorFromResourceList()`
- Add vector operations as methods: `Add`, `Sub`, `LessEqual`, `Clone`, `SetMax`, `IsZero`
- Create `NewResourceVector` factory to guarantee correct vector length
- Create unit tests for vector operations

### Phase 2: Vector Map Generation
- Extend `ClusterInfoSnapshot` to build `ResourceVectorMap` from cluster state using `BuildResourceVectorMap`
- Pass shared `ResourceVectorMap` to all nodes and pods during session initialization
- Document vector map lifecycle and cache strategy

### Phase 3: Pod & Node Info Vectorization
- Vectorize pod and node resource representations in `PodInfo`, `NodeInfo`
- Use current Resource structs behind the scenes

### Phase 4: Resource Structs Deprecation and Removal
- Deprecate older Resource structs
- Remove all uses of Resource structs and implement vector resources instead

### Phase 5: Validation & Optimization
- Comprehensive performance testing at scale (100-2000 nodes)
- Final optimization passes
- Document performance improvements and trade-offs

## Baseline Performance

This section establishes baseline metrics for the current struct-based implementation. These metrics will be compared against the vector-based implementation (Phase 5) to quantify performance improvements.

### Test Environment

- **System**: Intel Core Ultra 7 165H
- **CPU Governor**: performance
- **Go Version**: Latest stable
- **Benchmark Parameters**: `-benchmem -count=10` (10 samples per benchmark)
  - Action benchmarks: `-benchtime=10x` (10 iterations per sample, sufficient for ms-level operations)
  - API micro-benchmarks (PodInfo.Clone, IsTaskAllocatable): default auto-calibrated `b.N` (millions of iterations for stable ns-level timing)

### Benchmark Methodology

Ten benchmark runs were executed (`-count=10`). Results below report the mean across runs.

### Baseline Results Summary

Benchmarking focus areas:
1. **AllocateAction**: Core allocation logic across small (10 nodes), medium (50 nodes), and large (100 nodes) clusters
2. **ReclaimAction**: Reclaim decision-making
3. **PreemptAction**: Preemption scenario validation
4. **ConsolidationAction**: Workload consolidation logic
5. **API Operations**: Direct internal API types operations (PodInfo.Clone() , NodeInfo.IsTaskAllocatable)

### Key Performance Metrics (Average of 10 runs)

| Benchmark | Configuration | Time (ns/op) | Memory (B/op) | Allocations |
|-----------|---------------|-------------|--------------|------------|
| AllocateAction | Small Cluster (10 nodes) | 106.6M | 2.33Mi | 36.9k |
| AllocateAction | Medium Cluster (50 nodes) | 127.1M | 12.53Mi | 327.8k |
| AllocateAction | Large Cluster (100 nodes) | 183.3M | 43.20Mi | 1.401M |
| ReclaimAction | Small Cluster (10 nodes) | 102.7M | 971.5Ki | 8.8k |
| ReclaimAction | Medium Cluster (50 nodes) | 105.0M | 3.15Mi | 28.1k |
| ReclaimLargeJobs | 10 nodes | 104.4M | 1.86Mi | 19.9k |
| ReclaimLargeJobs | 50 nodes | 130.2M | 17.41Mi | 229.8k |
| ReclaimLargeJobs | 100 nodes | 241.2M | 59.23Mi | 856.0k |
| ReclaimLargeJobs | 200 nodes | 816.0M | 234.13Mi | 3.620M |
| ReclaimLargeJobs | 500 nodes | 8.97s | 1.70Gi | 29.050M |
| PreemptAction | Small Cluster (10 nodes) | 104.7M | 1.07Mi | 11.5k |
| PreemptAction | Medium Cluster (50 nodes) | 110.5M | 4.25Mi | 39.9k |
| ConsolidationAction | Small Cluster (10 nodes) | 111.4M | 5.83Mi | 74.6k |
| ConsolidationAction | Medium Cluster (50 nodes) | 187.5M | 48.24Mi | 691.9k |
| PodInfo.Clone | Minimal | 506ns | 976B | 12 |
| PodInfo.Clone | With GPU | 511ns | 976B | 12 |
| PodInfo.Clone | With Multiple GPUs | 617ns | 1184B | 13 |
| IsTaskAllocatable | best-effort-cpu-only | 55ns | 0B | 0 |
| IsTaskAllocatable | regular-gpu | 105ns | 48B | 3 |
| IsTaskAllocatable | fractional-gpu | 107ns | 48B | 3 |
| IsTaskAllocatable | mig-1g-10gb | 201ns | 0B | 0 |
| IsTaskAllocatable | gpu-memory-request | 106ns | 48B | 3 |
| IsTaskAllocatable | custom-resources-1-present | 117ns | 0B | 0 |
| IsTaskAllocatable | custom-resources-2-present | 133ns | 0B | 0 |
| IsTaskAllocatable | custom-resources-5-present | 174ns | 0B | 0 |
| IsTaskAllocatable | custom-resources-10-present | 253ns | 0B | 0 |
| IsTaskAllocatable | custom-resources-1-with-1-missing | 123ns | 48B | 3 |
| IsTaskAllocatable | custom-resources-2-with-1-missing | 132ns | 48B | 3 |
| IsTaskAllocatable | custom-resources-5-with-1-missing | 153ns | 48B | 3 |
| IsTaskAllocatable | custom-resources-10-with-1-missing | 196ns | 48B | 3 |

Notes:
- BenchmarkReclaimLargeJobs_1000Node did not complete within a 40m timeout and is omitted.
- Action benchmarks use `-benchtime=10x` (10 iterations); API micro-benchmarks use default auto-calibration (millions of iterations) for stable timing.


## After Optimization (filled in Phase 5)

### Test Environment

Same hardware and configuration as baseline (Intel Core Ultra 7 165H, performance governor).

This refresh was run on commit `91be47d8` (`refactor(scheduler): remove Resource fields from PodInfo and PodGroupInfo (#1238)`), which is the merged `origin/main` commit for the vectorizing-resources refactor effort. `main` had already moved on by the time these measurements were rerun, so the results below are anchored to that refactor endpoint rather than current `main`.

- **Action benchmarks**: `make benchmark BENCH_OUTPUT=benchmark-91be47d8.txt`
  - Expands to `go test -bench=. -benchmem -count=6 -run=^$ ./pkg/scheduler/actions/...`
- **API micro-benchmarks**:
  - `go test -bench='BenchmarkPodInfoClone' -benchmem -count=6 -run='^$' ./pkg/scheduler/api/pod_info`
  - `go test -bench='BenchmarkIsTaskAllocatable' -benchmem -count=6 -run='^$' ./pkg/scheduler/api/node_info`

### Methodology Note

The original baseline action numbers in this document were collected with `-benchtime=10x`. This refresh uses the repository's current benchmark targets with Go's default auto-calibrated `b.N`. The post-refactor numbers below are current measurements for the merged refactor commit, but percentage deltas against the original baseline should be treated as directional rather than perfectly apples-to-apples.

### API-level Benchmarks: IsTaskAllocatable

`NodeInfo.IsTaskAllocatable()` remains one of the clearest wins from the vectorized model, especially as the number of tracked resources grows.

| Benchmark | Baseline (ns/op) | After (ns/op) | Speedup | Allocs Before → After |
|-----------|----------------:|-------------:|--------:|----------------------:|
| best-effort-cpu-only | 55 | 49.7 | **1.1x** | 0 → 0 |
| regular-gpu | 105 | 82.5 | **1.3x** | 3 (48B) → 3 (48B) |
| fractional-gpu | 107 | 75.4 | **1.4x** | 3 (48B) → 3 (48B) |
| mig-1g-10gb | 201 | 72.8 | **2.8x** | 0 → 0 |
| gpu-memory-request | 106 | 75.5 | **1.4x** | 3 (48B) → 3 (48B) |
| custom-resources-1-present | 117 | 43.5 | **2.7x** | 0 → 0 |
| custom-resources-2-present | 133 | 46.9 | **2.8x** | 0 → 0 |
| custom-resources-5-present | 174 | 46.5 | **3.7x** | 0 → 0 |
| custom-resources-10-present | 253 | 51.6 | **4.9x** | 0 → 0 |
| custom-resources-1-with-1-missing | 123 | 44.7 | **2.7x** | 3 (48B) → **0 (0B)** |
| custom-resources-2-with-1-missing | 132 | 41.4 | **3.2x** | 3 (48B) → **0 (0B)** |
| custom-resources-5-with-1-missing | 153 | 45.1 | **3.4x** | 3 (48B) → **0 (0B)** |
| custom-resources-10-with-1-missing | 196 | 52.6 | **3.7x** | 3 (48B) → **0 (0B)** |

Key observation: custom-resource checks now stay in a narrow 41-52ns band even as the number of resources grows, and missing-resource cases no longer allocate.

### API-level Benchmarks: PodInfo.Clone

| Benchmark | Baseline (ns/op) | After (ns/op) | Delta | Memory | Allocs |
|-----------|----------------:|-------------:|------:|-------:|-------:|
| Minimal | 506 | 571.7 | +13.0% | 976B → 784B | 12 → 10 |
| With GPU | 511 | 388.1 | -24.1% | 976B → 784B | 12 → 10 |
| With Multiple GPUs | 617 | 393.6 | -36.2% | 1184B → 784B | 13 → 10 |

Clone results are mixed in this rerun: the GPU-bearing cases improved materially, while the minimal case regressed slightly in raw time but still reduced memory and allocation count.

### Action-level Benchmarks

At the full action level, the improvements are modest because session construction and broader scheduling setup still dominate total runtime.

| Benchmark | Baseline (ns/op) | After (ns/op) | Delta | After Memory | After Allocs |
|-----------|----------------:|-------------:|------:|-------------:|-------------:|
| AllocateAction Small | 106.6M | 108.8M | +2.1% | 2.24Mi | 34.9k |
| AllocateAction Medium | 127.1M | 130.2M | +2.5% | 12.03Mi | 312.2k |
| AllocateAction Large | 183.3M | 189.1M | +3.1% | 41.43Mi | 1.337M |
| ReclaimAction Small | 102.7M | 103.1M | +0.4% | 886.7Ki | 8.0k |
| ReclaimAction Medium | 105.0M | 106.1M | +1.0% | 2.89Mi | 25.3k |
| PreemptAction Small | 104.7M | 103.9M | -0.8% | 1.02Mi | 10.9k |
| PreemptAction Medium | 110.5M | 112.1M | +1.4% | 4.11Mi | 37.8k |
| ConsolidationAction Small | 111.4M | 115.2M | +3.4% | 5.44Mi | 69.5k |
| ConsolidationAction Medium | 187.5M | 186.7M | -0.4% | 46.40Mi | 667.1k |

### Additional Action Benchmarks Added During the Refactor

These were not part of the original baseline table, so they are listed as post-refactor reference points only.

| Benchmark | After (ns/op) | After Memory | After Allocs |
|-----------|--------------:|-------------:|-------------:|
| FullSchedulingCycle Small | 105.9M | 1.39Mi | 20.5k |
| FullSchedulingCycle Medium | 119.9M | 6.85Mi | 167.5k |
| FullSchedulingCycle Large | 148.6M | 22.72Mi | 696.8k |
| ManyQueues Medium | 135.0M | 16.30Mi | 350.0k |
| GangScheduling Medium | 143.4M | 17.13Mi | 571.3k |

### Reclaim Scaling (Primary Bottleneck)

The large-scale reclaim path was the main motivation for the vectorization work. This rerun confirms the critical outcome: the 1000-node reclaim benchmark now completes reliably instead of timing out.

| Nodes | Baseline (ns/op) | After (ns/op) | Delta | After Memory | After Allocs |
|------:|----------------:|-------------:|------:|-------------:|-------------:|
| 10 | 104.4M | 105.2M | +0.8% | 1.71Mi | 18.1k |
| 50 | 130.2M | 130.7M | +0.4% | 15.42Mi | 207.2k |
| 100 | 241.2M | 213.4M | -11.5% | 51.95Mi | 779.6k |
| 200 | 816.0M | 728.0M | -10.8% | 205.16Mi | 3.350M |
| 500 | 8.97s | 9.17s | +2.2% | 1.49Gi | 27.654M |
| 1000 | **>40min (timeout)** | **76.30s** | **completes** | 7.87Gi | 157.955M |

### Takeaways

- The most important functional result held: `BenchmarkReclaimLargeJobs_1000Node` now completes in about 76 seconds on average instead of timing out after 40 minutes.
- `IsTaskAllocatable()` still shows the strongest CPU-path improvements, particularly for custom-resource-heavy cases, with 2.7x-4.9x speedups and zero allocations in missing-resource scenarios.
- End-to-end action benchmarks mostly stayed within low single-digit movement, which suggests the vectorized resource model removes a real hot-path cost without dramatically changing total scheduler action time on its own.

## Future Work: Complete Resource Struct Removal

### Task Description

After the core NodeInfo, PodInfo, and PodGroupInfo migrations are complete, additional work is needed to remove `resource_info.Resource` usage from plugins and utilities. This requires a separate design effort due to the breadth of changes and potential API implications.

### Scope

The following areas require further planning:

1. **Proportion Plugin Suite**
   - `plugins/proportion/proportion.go` - Victim resource tracking
   - `plugins/proportion/reclaimable/` - Reclaimer/reclaimee resource calculations
   - `plugins/proportion/utils/utils.go` - `QuantifyResource()` function

2. **Topology Plugin**
   - `plugins/topology/job_filtering.go` - Tree sorting and job ratio calculations
   - `plugins/topology/topology_structs.go` - `DomainInfo.IdleOrReleasingResources`

3. **Error Message Generation**
   - `api/common_info/pod_errors.go`, `job_errors.go`

4. **Predicates**
   - `k8s_internal/predicates/maxNodeResources.go`
