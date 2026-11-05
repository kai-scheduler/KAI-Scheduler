# Cross-Dimensional Resource Balance Scoring

**Author:** Abhishek Srivastava (@0x-auth)  
**Date:** 2026-04-03 (updated 2026-10-23)  
**Status:** Proposal  
**Issue:** [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)

## Summary

Add a `balanced` strategy to the existing `nodeplacement` plugin, next to `binpack` and `spread`. It reduces cross-dimensional fragmentation by combining post-placement balance (population standard deviation across dimensions) with cosine alignment between the pod's request vector and the node's free capacity vector. The alignment term is applied to every node and steers pods toward nodes where their resource shape fills the gap.

The balance term uses the same population standard deviation as Kubernetes `balancedResourceScore` (`(1 - stddev)` scaled into the score). Upstream `BalancedAllocation` then blends that with the score from before placement (`balancedResourceScorer`). This proposal uses only the after-placement term. KAI does not run `BalancedAllocation`, `MostAllocated`, or `LeastAllocated`.

The first version scores CPU and memory only. Weights below are candidates from an older formula and are not settled.

## Motivation

### The Problem

`nodeplacement` has two strategies, `binpack` and `spread` (`pkg/scheduler/constants/placementStrategy.go`). Each one scores a single resource.

`OnSessionOpen` installs one `NodeOrderFn` per job type (`pkg/scheduler/plugins/nodeplacement/nodeplacement.go`). A task that is not CPU-only (`!IsCPUOnlyRequest`) uses the GPU function. That includes whole-GPU, GPU-memory, MIG, and DRA GPU requests. The GPU function scores `nvidia.com/gpu` only. A CPU-only task uses the CPU function, which scores CPU only. Memory never enters either score, and GPU-memory percent never enters either score.

- **binpack** scores the free amount of that one resource, `NonAllocatedResource` (idle plus releasing). Across fitting nodes that have some of the resource, the score is `scores.MaxHighDensity` (9) times a min/max normalization that prefers less free resource (`pack.go`). It is not mean utilization. A node with none of the resource scores 0. When every fitting node has the same free amount, every node scores 9.
- **spread** scores `nonAllocated / resourceCount` for that same resource (`spread.go`). For CPU, `resourceCount` is allocatable CPU. For GPU, it is `GetNumberOfGPUsInNode()` (the `nvidia.com/gpu.count` annotation, or allocatable GPU count when the annotation is missing). A larger free fraction ranks higher. Two CPU-only nodes with the same capacity and free CPU fractions 0.5 and 0.1 score 0.5 and 0.1. They are not tied, for any memory split.

Shape across dimensions is invisible to both. A CPU-only pod can fill CPU and leave memory unused, and the score will not say so.

Kubernetes `BalancedAllocation` asks how even a node is across resources. This proposal is an opt-in KAI score that asks that, and also whether the pod's request points the same way as the node's free capacity.

### Real-World Impact

Mixed inference workloads are often described as lopsided across GPU memory, GPU compute, CPU, and RAM:

| Workload Type | GPU-Mem | GPU-Compute | CPU | RAM | Stranded Resource |
|---|---|---|---|---|---|
| LLaMA 70B serving | ~95% | ~20% | ~30% | ~60% | GPU-Compute |
| Batch small-model inference | ~30% | ~85% | ~70% | ~20% | GPU-Memory, RAM |
| CPU preprocessing pipeline | ~0% | ~0% | ~90% | ~25% | RAM, all GPU |

KAI cannot score that GPU-compute versus GPU-memory split today. Those are not separate request fields, and GPU tasks are ordered by GPU count only. The first version is the CPU and memory case: pods requesting 2 CPU + 4Gi placed on nodes with 32 CPU + 128Gi free leave memory stranded in the same way. That case needs no new resource fields.

[#1311](https://github.com/kai-scheduler/KAI-Scheduler/issues/1311) is related but different. It is about defragmenting a cluster after small jobs end and leave whole-GPU holes. This proposal works at scheduling time on cross-dimensional shape. The two are complementary; this proposal does not solve #1311.

## Proposal

### Scope

The first version scores **CPU and memory** only, and only when that dimension's node capacity is greater than zero. Capacity 0 is skipped, so the fraction does not divide by zero. A CPU-only node has GPU allocatable 0. Some nodes have no memory.

The resource vector already has GPU count at `GPUIndex` (`pkg/scheduler/api/resource_info/resource_vector.go`, stored as `gpu` and looked up for `nvidia.com/gpu`) and a pods entry at `PodsIndex`. v1 does not pass either into this formula. GPU memory is a `GpuResourceRequirement`, not a vector component, and those pods still take the GPU score path. Scoring `gpu`, `gpu-compute`, and `gpu-memory` as separate balance dimensions is premature until users can request compute and memory separately (@davidLif in [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)).

`balanced` replaces the `NodeOrderFn` of the job type it is set on. The other job type is unchanged.

- `cpu: balanced` runs only when `IsCPUOnlyRequest` is true. A GPU, GPU-memory, MIG, or DRA GPU task never consults the CPU strategy, so this setting does not change GPU placement. Those tasks stay ordered by free GPU count (`binpack` or `spread`). The same split applies anywhere the session scores a node, including preempt, reclaim, and consolidation simulations.
- `gpu: balanced` would replace that free-GPU-count score with a CPU/memory score and would stop ordering GPU tasks by GPU count. That is not a v1 setting. `gpu` stays `binpack` or `spread` until a later phase adds GPU count as an active dimension.

Further dimensions can be added later by passing more positive-capacity entries. The formula does not change.

### Score Function (candidate weights, not settled)

For each candidate node, compute five components. The coefficients are the retired search in the [appendix](#appendix-retired-variance-times-4-simulation-rerun-required). They are not chosen values.

```
score = 0.6 × balance_score
      + 0.2 × alignment_score
      + 0.1 × headroom_score
      − pressure_penalty
      − strand_penalty
```

`nodeplacement` returns this score divided by 100 and clamped to `[0, 1]`, the same range `spread` returns. Penalties can drive the raw score negative; the clamp keeps the returned value at 0 or above.

#### Component 1: Post-Placement Balance Score (candidate weight 0.6)

```
non_allocated[i] = idle[i] + releasing[i]     # NodeInfo.NonAllocatedResource
after_frac[i]    = (capacity[i] - non_allocated[i] + pod.req[i]) / capacity[i]
mean             = average(after_frac)
stddev           = sqrt(average((after_frac[i] - mean)²))    # population stddev
balance_score    = (1.0 - stddev) × 100
```

Active dimensions only (`capacity > 0`). Free capacity here is idle plus releasing, the same quantity binpack and spread already treat as free, so a releasing pod counts as free.

This is the after-placement population standard deviation, scaled like Kubernetes `balancedResourceScore`. It asks how evenly utilized the node will be across the active dimensions after this pod. A node at 60% CPU / 60% RAM after placement scores higher than one at 90% CPU / 30% RAM. Upstream also folds in the before-placement score; this component does not.

An earlier draft used `variance × 4`. That scaling is retired. At the candidate weight 0.6 this term would dominate the sum. That weight is not settled.

#### Component 2: Cosine Alignment Score (candidate weight 0.2)

```
node_free[i]  = non_allocated[i] / capacity[i]   # idle + releasing, active dimensions
pod_frac[i]   = pod.req[i] / capacity[i]

alignment       = cosine_similarity(node_free, pod_frac)
alignment_score = alignment × 100
```

This asks whether the pod's resource shape points the same way as the node's free capacity. A CPU-heavy pod (large cpu request, small memory request) scores higher on a node with a lot of free CPU and little free memory.

This is the term Kubernetes `BalancedAllocation` does not have. It is applied to every node, not only on near-ties. In the retired simulation, applying alignment only when balance scores were within ε lost the stranding reduction on every ε that was tried ([details](https://github.com/koordinator-sh/koordinator/pull/2839)). That result is not a measurement of this formula.

#### Component 3: Headroom Score (candidate weight 0.1)

```
headroom_score = mean(node_free) × 100
```

A small bonus for nodes with more total free capacity on the active dimensions. The intent is to avoid putting every pod on one node while others sit idle. The 0.1 coefficient is not settled.

#### Component 4: Pressure Penalty

```
for each active dimension i:
    used_after = (capacity[i] - non_allocated[i] + pod.req[i]) / capacity[i]
    if used_after > 0.92:  penalty += (used_after - 0.92) × 500
    elif used_after > 0.85: penalty += (used_after - 0.85) × 50
```

This subtracts from the score. It is not a filter and not a hard gate. A node above 92% on one dimension can still rank first when every other node scores lower.

Comparisons are strict. Exactly 0.85 and exactly 0.92 add nothing from this penalty. A request equal to free is scored like any other request; predicates decide whether the pod fits. Fractions are not clamped at 1 (upstream `balancedResourceScore` does clamp). The 0.85 and 0.92 levels are candidates from the retired search.

#### Component 5: Strand Penalty

```
for each pair of active dimensions (i, j):
    if after_frac[i] > 0.80 and after_frac[j] < 0.20:
        penalty += 15
```

If placing this pod would leave one active dimension above 80% and another below 20%, the penalty discourages that placement. Exactly 0.80 and exactly 0.20 add nothing. The thresholds are candidates from the retired search.

### Why These Weights

The numbers 0.6 / 0.2 / 0.1 were **not** measured for this formula. A 120-point search produced them for `variance × 4` over 6 simulated dimensions, including GPU sub-resources this design does not score. They have to be searched again with the standard-deviation base on CPU and memory before implementation.

Until that rerun, do not treat those weights, the 0.92 cliff, or the 0.80/0.20 strand pair as chosen. The appendix keeps the old tables so the search is not lost. There is no production data.

### Worked Numerical Example

**Setup (CPU, memory).** Both nodes have the same capacity, so one request-fraction vector is valid for both. If the capacities differed, the fractions would differ by node. Free fractions are non-allocated (idle plus releasing) over capacity.

- Node A free fractions: `[CPU: 0.45, RAM: 0.60]`
- Node B free fractions: `[CPU: 0.80, RAM: 0.25]`
- Pod request fractions: `[CPU: 0.20, RAM: 0.05]` (CPU-heavy pod)
- Both nodes have the same mean free fraction (0.525), so headroom does not decide.

`after_frac[i] = (1 − free[i]) + req[i]`

**Node A:**
- After placement: `[0.75, 0.45]` → stddev = 0.15 → balance_score = 85.0
- Alignment: cos([0.20, 0.05], [0.45, 0.60]) = 0.776 → alignment_score = 77.6
- Headroom: 52.5
- No pressure or strand penalties
- **Score: 0.6×85.0 + 0.2×77.6 + 0.1×52.5 = 71.8** (candidate weights)

**Node B:**
- After placement: `[0.40, 0.80]` → stddev = 0.20 → balance_score = 80.0
- Alignment: cos([0.20, 0.05], [0.80, 0.25]) = 0.998 → alignment_score = 99.8
- Headroom: 52.5
- No pressure or strand penalties
- **Score: 0.6×80.0 + 0.2×99.8 + 0.1×52.5 = 73.2** (candidate weights)

**Result:** With these unsettled weights, Node B scores higher (73.2 > 71.8). The balance term favors Node A. Alignment favors Node B because the CPU-heavy pod matches Node B's free CPU. Node A's free memory stays available for a memory-heavy pod.

**What binpack and spread do on this pod** (CPU-only, so CPU only; memory is ignored):

- **spread** ranks Node B higher (free CPU fraction 0.80 versus 0.45).
- **binpack** ranks Node A higher (less free CPU). The numeric score also depends on the min and max free CPU across the whole fitting set, so it is not a per-node utilization.
- They do not tie on mean utilization.

A Kubernetes after-placement standard deviation, on its own, would rank Node A higher (0.15 versus 0.20).

### Integration Point

This is a new strategy inside the existing `nodeplacement` plugin, not a separate plugin.

Today each job type (`gpu`, `cpu`) is `binpack` or `spread`, and each strategy is an `api.NodeOrderFn` (`pack.go`, `spread.go`). Anything other than `spread` selects binpack. The v1 change:

- `constants/placementStrategy.go`: add `BalancedStrategy = "balanced"`.
- `nodeplacement/balanced.go`: `nodeResourceBalanced() api.NodeOrderFn`, reading CPU and memory from `AllocatableVector` and `NonAllocatedResource` (idle plus releasing) and the task request. Skip a dimension whose capacity is 0. Return a score in `[0, 1]` (the formula above divided by 100, clamped).
- `nodeplacement.go`: in `OnSessionOpen`, select `nodeResourceBalanced` only when the **cpu** argument is `balanced`. The GPU argument stays `binpack` or `spread`, so GPU tasks keep the free-GPU-count function. The CPU pre-order function for `balanced` is `noopPreOrderFn`; the balance score does not need a per-session min/max. The GPU pre-order function stays whatever binpack or spread already uses.
- `balanced_test.go`: table tests for the component scores, the worked example above, capacity 0, and an infeasible node. Existing `nodepack_test.go` and `nodespread_test.go` stay unchanged.

Scheduler plugin arguments for v1:

```yaml
plugins:
  - name: nodeplacement
    arguments:
      cpu: balanced
      gpu: binpack
```

The user-facing field is `SchedulingShard.spec.placementStrategy`, not this map (`docs/operator/scheduling-shards.md`). The CRD text says `binpack` / `spread` for `gpu` and `cpu`. The string is not an enum.

`SchedulingShardSpec.SetDefaultsWhereNeeded` lives in `github.com/kai-scheduler/api` (this repo depends on `v0.2.0`). It copies both strings into the `nodeplacement` arguments. Device plugins follow the GPU string only: `gpupack` and `gpusharingorder` are enabled when GPU equals `binpack`; any other GPU string enables `gpuspread` and disables those two. Consolidation is disabled only when GPU or CPU equals `spread`.

For `placementStrategy.cpu: balanced`, the api change should:

- copy `cpu: balanced` into `nodeplacement`
- leave consolidation enabled (`balanced` is not `spread`; today's `!= spread` check already does this)
- leave `gpupack`, `gpuspread`, and `gpusharingorder` on whatever `placementStrategy.gpu` selects

`placementStrategy.gpu: balanced` is not a v1 value. If it were copied through today, the api would treat it as not-`binpack` (device spread on, `gpupack` and `gpusharingorder` off) and not-`spread` (consolidation stays on when CPU is not `spread`), while `nodeplacement` would still bin-pack GPU tasks because only `spread` switches the GPU function. The api change should not accept that combination. Accepting `balanced` on the CPU field, and keeping the consolidation and device-plugin behavior above, starts in `kai-scheduler/api`.

The function is stateless, allocates nothing per call, and adds no CRDs or background goroutines. A larger overhaul of node scoring has been mentioned by maintainers; the function is independent of the plugin framework and can move into that design when it exists.

### Interaction with DRF

Dominant Resource Fairness in KAI is the `proportion` plugin. `OnSessionOpen` registers a queue-order function (`AddQueueOrderFn` in `proportion.go`) and does not register a job-order or node-order function. The queue comparison receives the candidate job, and one of its steps is dominant resource share (`queue_order.go`). Other plugins order jobs (`priority`, `elastic`).

`balanced` only orders nodes for a task that has already been chosen. It does not pick the queue or the job.

Node choice still changes how fragmented the cluster becomes, which changes what fits later and which jobs need reclaim or preemption. That effect has not been measured. It needs a simulation with multiple queues and reclaim enabled before this is implemented.

## Benchmark Results

The weights and the results tables are from a retired formula. They need a rerun on the standard-deviation, CPU and memory formula before implementation. There is no production data. The scripts are not in this repository. See the [appendix](#appendix-retired-variance-times-4-simulation-rerun-required).

## Alternatives Considered

### Pure Cosine Alignment (V1)

Using only cosine similarity with entropy reduction. In the retired simulation it over-steered and lost to a variance-style score on all 5 scenarios. That comparison has to be rerun with this formula.

### Alignment + Complement Penalties (V2)

Adding explicit penalties for mismatched dimensions. Better pairwise decisions in that same retired simulation, and it still lost the aggregate comparison because complement penalties rejected viable placements. Rerun before relying on that.

### Configurable Weights

Replace fixed 0.6/0.2/0.1 with user-configurable parameters. Deferred. The retired search's top combinations sat near those values; whether that is still true for standard deviation on CPU and memory is part of the rerun.

### Separate Plugin

The first draft proposed a standalone scoring plugin. Folding it into `nodeplacement` as another strategy reuses the existing per-job-type configuration and keeps one place for placement policy.

### Gated Alignment (true tiebreaker)

Apply alignment only when balance scores are within ε. In the retired simulation every ε tried (0.5 to 10) lost the stranding reduction. A per-node `NodeOrderFn` also cannot express a cross-node comparison without two-pass selection.

### Integration with Topology-Aware Scheduling

The balance score can be applied inside a topology domain rather than across the whole cluster. Future work. It does not change the core function.

## Implementation Plan

1. **Before implementation:** rerun the benchmark with the standard-deviation base on CPU and memory; simulate proportion queues with reclaim enabled. Do not ship the candidate weights ahead of that rerun.
2. **Phase 1:** `balanced` for CPU-only tasks in `nodeplacement`, opt-in through `placementStrategy.cpu` (and the plugin argument it copies). GPU tasks stay on `binpack` or `spread`. The shard defaulting change starts in `kai-scheduler/api`.
3. **Phase 2:** GPU count as an active dimension, which is when `gpu: balanced` can replace the free-GPU-count score without dropping it. GPU memory and GPU compute stay out until users can request them separately.
4. **Phase 3:** Optional configurable weights if requested.

## Appendix: retired variance times 4 simulation (rerun required)

The numbers in this appendix were produced with `variance × 4` over 6 simulated dimensions (CPU, RAM, GPU-compute, GPU-memory, IOPS, network). They are not results for the standard-deviation CPU and memory formula above, and they are not a reason to keep 0.6/0.2/0.1. Rerun before implementation. All of them come from an external simulation (`benchmark_simulation.py`) and a 3-node minikube (`benchmark_full.py`). There is no production data.

The source called its candidate "Lambda-G V3". That name is not a KAI strategy. The dollar column is that simulation's cost model, not a KAI metric.

### Methodology

Simulation benchmark scheduling N pods onto M heterogeneous nodes across 6 resource dimensions (CPU, RAM, GPU-Compute, GPU-Memory, IOPS, Network). Each scenario uses a fixed random seed (42) for reproducibility. Node types include CPU-optimized (16 CPU, 32GB RAM, no GPU), RAM-optimized (8 CPU, 128GB RAM, no GPU), GPU-inference (8 CPU, 32GB RAM, 50 GPU-Compute, 80GB VRAM), and GPU-training (32 CPU, 128GB RAM, 100 GPU-Compute, 80GB VRAM). Pod types include LLM serving (VRAM-heavy), batch inference (GPU-compute-heavy), training (everything-heavy), CPU preprocessing, API services, and ETL pipelines.

**Balance Score** = 0.7 × (100 − avg_imbalance × 400) + 0.3 × schedule_rate × 100, where avg_imbalance is the mean per-node cross-dimensional variance. Higher is better.

### Strategies Compared

| Strategy | Description |
|---|---|
| **LeastAllocated** | K8s default. Score = mean(free%). Prefer emptiest nodes. |
| **MostAllocated** | Bin-packing. Score = mean(used%). Prefer fullest nodes. |
| **BalancedAllocation** | K8s built-in. Minimize post-placement variance across dimensions. |
| **DominantResource** | Score by dominant (most-consumed) resource dimension per node. |
| **Lambda-G V3** | External run of the retired formula. 0.6×variance + 0.2×alignment + 0.1×headroom − penalties. |

These rows are the simulation's labels. They are not the KAI `binpack` and `spread` functions described above.

### Results — Balance Score (higher = better)

| Scenario | LeastAlloc | MostAlloc | BalancedAlloc | DominantRes | Lambda-G V3 |
|---|---|---|---|---|---|
| Mixed GPU — AI Workload (30n×120p) | 72.0 | 70.0 | 78.7 | 79.4 | **81.8** |
| GPU Cluster — Inference Heavy (20n×80p) | 70.5 | 70.0 | 81.3 | 76.6 | **81.9** |
| GPU Cluster — Training Heavy (20n×60p) | 75.3 | 70.0 | 79.4 | 78.9 | **82.8** |
| CPU + Few GPUs — CPU Workload (25n×100p) | 65.1 | 70.0 | 72.3 | 68.8 | **74.7** |
| Scale Test (60n×300p) | 65.3 | 70.0 | 74.1 | 73.8 | **76.7** |

### Results — Stranded Nodes (lower = better)

| Scenario | LeastAlloc | MostAlloc | BalancedAlloc | DominantRes | Lambda-G V3 |
|---|---|---|---|---|---|
| Mixed GPU — AI Workload | 9 | 0 | 6 | 4 | **3** |
| GPU Cluster — Inference Heavy | 17 | 0 | 6 | 11 | **5** |
| GPU Cluster — Training Heavy | 14 | 0 | 6 | 7 | **6** |
| CPU + Few GPUs — CPU Workload | 16 | 0 | 8 | 17 | **7** |
| Scale Test | 36 | 0 | 18 | 20 | **13** |

### Aggregate Summary

| Metric | LeastAlloc | MostAlloc | BalancedAlloc | DominantRes | Lambda-G V3 |
|---|---|---|---|---|---|
| **Scenarios Won** | 0 | 0 | 0 | 0 | **5** |
| **Total Stranded Nodes** | 92 | 0 | 44 | 59 | **34** |
| **Total Monthly Waste** | $205,249 | $0 | $144,588 | $116,304 | **$67,975** |

Note: MostAllocated shows 0 stranded and $0 waste because it packs so aggressively that most pods cannot schedule at all (660 pending out of 660 total). Its zeros are an artifact of having no placed pods to strand, not evidence of good scheduling. These claims, including "wins all 5 scenarios", belong to the retired formula only.

### Key Observations

1. **Retired formula vs BalancedAllocation** (closest comparator in that run): the retired formula won all 5 scenarios on its own metric. 23% fewer stranded nodes (34 vs 44) and 53% less of that model's wasted capacity ($68k vs $145k). Rerun before carrying either percentage forward.

2. **Retired formula vs DominantResource**: that run's dominant-resource score looked at one dimension. The retired formula used every simulated dimension. This is not a measurement of KAI's `proportion` plugin.

3. **Scaling behavior**: in that run the gap held at 60 nodes (76.7 vs 74.1). Rerun before treating that as scale behavior of this formula.

### Scoring Latency

For N=6 dimensions the external run reported sub-microsecond per node and zero heap allocations. Re-measure for the two-dimension function before citing it.

Benchmark source and reproduction: [github.com/0x-auth/lambda-g-auditor](https://github.com/0x-auth/lambda-g-auditor)

## References

- Issue: [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)
- Cluster defragmentation: [#1311](https://github.com/kai-scheduler/KAI-Scheduler/issues/1311)
- Koordinator proposal: [koordinator-sh/koordinator#2839](https://github.com/koordinator-sh/koordinator/pull/2839)
- Auditor + benchmark (retired formula): [github.com/0x-auth/lambda-g-auditor](https://github.com/0x-auth/lambda-g-auditor)
- External scheduler prototype: [github.com/0x-auth/lambda-g-scheduler](https://github.com/0x-auth/lambda-g-scheduler)
- Docker image: [bitsabhi/lambda-g-controller](https://hub.docker.com/r/bitsabhi/lambda-g-controller)
