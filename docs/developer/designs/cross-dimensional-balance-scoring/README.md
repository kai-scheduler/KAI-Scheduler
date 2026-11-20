# Cross-Dimensional Resource Balance Scoring

**Author:** Abhishek Srivastava (@0x-auth)  
**Date:** 2026-04-03 (updated 2026-10-23)  
**Status:** Proposal  
**Issue:** [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)

## Summary

Add a `balanced` strategy to the existing `nodeplacement` plugin, next to `binpack` and `spread`. It reduces cross-dimensional fragmentation by combining post-placement balance (standard deviation across dimensions, as in Kubernetes `BalancedAllocation`) with cosine alignment between the pod's request vector and the node's free capacity vector. The alignment term is applied to every node and steers pods toward nodes where their resource shape fills the gap.

The first version scores CPU and Memory only.

## Motivation

### The Problem

KAI-Scheduler currently supports two node-ordering strategies:

- **Bin-packing (MostAllocated):** Prefers nodes with the highest overall utilization. A node at 95% GPU-Memory / 15% CPU looks "mostly full" — bin-packing will avoid it, stranding CPU capacity.
- **Spread (LeastAllocated):** Prefers nodes with the most free resources. The same node looks "partially used" — spread will send more pods to it, but those pods may need GPU-Memory that isn't there.

Neither strategy considers the *shape* of resource usage across dimensions. Both treat a node at 50% CPU / 50% RAM identically to one at 90% CPU / 10% RAM. The second node has stranded RAM — you're paying for memory nobody can use.

The existing **BalancedAllocation** strategy (minimize per-node variance between dimensions) is better, but it only asks "will this node be more even after placement?" It does not ask "does this pod's resource shape *match* what this node needs?" It ignores the shape of the pod's request. This proposal adds a directional alignment term so the pod goes to the node where it fills the gap.

### Real-World Impact

In GPU clusters running mixed inference workloads, cross-dimensional fragmentation is common:

| Workload Type | GPU-Mem | GPU-Compute | CPU | RAM | Stranded Resource |
|---|---|---|---|---|---|
| LLaMA 70B serving | ~95% | ~20% | ~30% | ~60% | GPU-Compute |
| Batch small-model inference | ~30% | ~85% | ~70% | ~20% | GPU-Memory, RAM |
| CPU preprocessing pipeline | ~0% | ~0% | ~90% | ~25% | RAM, all GPU |

A CPU and Memory example without GPUs: pods requesting 2 CPU + 4Gi placed on nodes with 32 CPU + 128Gi free leave memory stranded in the same way. This case needs no new resource fields and is the scope of the first version.

[#1311](https://github.com/kai-scheduler/KAI-Scheduler/issues/1311) is related but different. It is about defragmenting a cluster after small jobs end and leave whole-GPU holes. This proposal works at scheduling time on cross-dimensional shape. The two are complementary; this proposal does not solve #1311.

## Proposal

### Scope

The first version scores **CPU and Memory** only. GPU sub-resources (`gpu`, `gpu-compute`, `gpu-memory`) are dependent on each other and users cannot request compute and memory separately today, so scoring across them is premature (raised by @davidLif in [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)).

The function itself is not limited to two dimensions. It operates on whatever resource vector is passed in, so further dimensions can be added once the resource model supports them without changing the formula.

### Score Function (V3 — Hybrid Variance-Alignment)

For each candidate node, compute five components:

```
score = 0.6 × variance_score
      + 0.2 × alignment_score
      + 0.1 × headroom_score
      − pressure_penalty
      − strand_penalty
```

#### Component 1: Post-Placement Balance Score (weight: 0.6)

```
after_frac[i] = (node.used[i] + pod.req[i]) / node.capacity[i]   for each active dimension
mean          = average(after_frac)
stddev        = sqrt(average((after_frac[i] - mean)² for each i))
balance_score = (1.0 - stddev) × 100
```

This matches Kubernetes [BalancedAllocation](https://github.com/kubernetes/kubernetes/blob/master/pkg/scheduler/framework/plugins/noderesources/balanced_allocation.go), which uses standard deviation. An earlier draft used `variance × 4`, an empirical scaling; standard deviation is consistent with upstream and easier to reason about. It asks: "how evenly utilized will this node be across all dimensions after placing this pod?" A node at 60% CPU / 60% RAM after placement scores higher than one at 90% CPU / 30% RAM.

This component does the majority of the work (60% weight) because balance is the single strongest predictor of cluster-wide resource balance.

#### Component 2: Cosine Alignment Score (weight: 0.2)

```
node_free[i]  = free_capacity[i] / total_capacity[i]   for each active dimension
pod_frac[i]   = pod.req[i] / node.capacity[i]          for each active dimension

alignment       = cosine_similarity(node_free, pod_frac)
alignment_score = alignment × 100
```

This asks: "does the pod's resource shape point in the same direction as the node's available capacity?" A CPU-heavy pod (large cpu_req, small ram_req) gets a high alignment score on a node with lots of free CPU and little free RAM. The pod "fills in" exactly what the node has to offer.

This is the term that distinguishes this strategy from BalancedAllocation. It is applied to every node, not only on near-ties. A gated tiebreaker (apply alignment only when balance scores are within ε) was benchmarked and lost every configuration: stranding returned to the BalancedAllocation level ([details](https://github.com/koordinator-sh/koordinator/pull/2839)).

#### Component 3: Headroom Score (weight: 0.1)

```
headroom_score = mean(node_free_frac) × 100
```

A small bonus for nodes with more total free capacity. Prevents over-concentrating load on a single node while others sit idle.

#### Component 4: Pressure Penalty (hard gate)

```
for each active dimension i:
    used_after = (node.used[i] + pod.req[i]) / node.capacity[i]
    if used_after > 0.92:  penalty += (used_after - 0.92) × 500   # hard cliff
    elif used_after > 0.85: penalty += (used_after - 0.85) × 50   # gentle slope
```

This creates a two-stage back-pressure. Above 85% on any single dimension, the score starts declining. Above 92%, it drops sharply. This prevents the scheduler from packing one dimension to exhaustion while others have room.

#### Component 5: Strand Penalty

```
for each pair of active dimensions (i, j):
    if after_frac[i] > 0.80 and after_frac[j] < 0.20:
        penalty += 15
```

If placing this pod would cause one dimension to exceed 80% while another stays below 20%, that's a stranding risk. The penalty discourages placements that create lopsided utilization.

### Why These Weights

The weights (0.6 / 0.2 / 0.1) were determined by grid search over 120 weight combinations across 5 heterogeneous cluster scenarios. Key findings:

- Variance dominance (0.6) is essential — without it, cosine alignment over-steers and creates new imbalances.
- Alignment at 0.2 is enough to change placement shape without overwhelming balance.
- Headroom above 0.1 causes under-packing. Below 0.05 has no measurable effect.
- The pressure cliff at 0.92 and strand threshold at 0.80/0.20 were determined empirically; they are the values where the composite score (balance − stranded × 0.5 − waste/10000) is maximized.

The weights were searched with the earlier `variance × 4` base over 6 dimensions. They will be re-searched with the standard-deviation base on CPU and Memory before implementation (see [Benchmark Results](#benchmark-results)).

### Worked Numerical Example

**Setup (CPU, Memory):**
- Node A free fractions: `[CPU: 0.45, RAM: 0.60]`
- Node B free fractions: `[CPU: 0.80, RAM: 0.25]`
- Pod request fractions: `[CPU: 0.20, RAM: 0.05]` (CPU-heavy pod)
- Both nodes have the same mean free capacity (0.525), so headroom does not decide.

`after_frac[i] = (1 − free[i]) + req[i]`

**Node A:**
- After placement: `[0.75, 0.45]` → stddev = 0.15 → balance_score = 85.0
- Alignment: cos([0.20, 0.05], [0.45, 0.60]) = 0.776 → alignment_score = 77.6
- Headroom: 52.5
- No pressure or strand penalties
- **Score: 0.6×85.0 + 0.2×77.6 + 0.1×52.5 = 71.8**

**Node B:**
- After placement: `[0.40, 0.80]` → stddev = 0.20 → balance_score = 80.0
- Alignment: cos([0.20, 0.05], [0.80, 0.25]) = 0.998 → alignment_score = 99.8
- Headroom: 52.5
- No pressure or strand penalties
- **Score: 0.6×80.0 + 0.2×99.8 + 0.1×52.5 = 73.2**

**Result:** Node B wins (73.2 > 71.8). Balance favors Node A; alignment favors Node B because the CPU-heavy pod matches Node B's free CPU. Node A's free memory stays available for a memory-heavy pod.

**What other strategies would do:**
- **binpack / spread:** Tie on mean utilization. Today they also score a single resource per job type (GPU count for GPU jobs, CPU for CPU-only jobs), so they do not see the memory dimension at all.
- **BalancedAllocation:** Node A (lower post-placement stddev).

### Integration Point

Following review, this is a new strategy inside the existing `nodeplacement` plugin rather than a separate plugin.

Today `nodeplacement` takes one strategy per job type (`gpu`, `cpu`), each `binpack` or `spread`, and each strategy is an `api.NodeOrderFn` in its own file (`pack.go`, `spread.go`). The change:

- `constants/placementStrategy.go`: add `BalancedStrategy = "balanced"`.
- `nodeplacement/balanced.go`: `nodeResourceBalanced() api.NodeOrderFn`, reading CPU and Memory from `node.AllocatableVector` / `NonAllocatedResource` and the task's request. Returns a score in `[0, 1]` (the formula above divided by 100, clamped), the same range `spread` returns.
- `nodeplacement.go`: in `OnSessionOpen`, select `nodeResourceBalanced` when the argument for a job type is `balanced`. The pre-order function is `noopPreOrderFn`; no per-session min/max is needed.
- `balanced_test.go`: table tests for the component scores, the worked example above, and an infeasible node. Existing `nodepack_test.go` and `nodespread_test.go` stay unchanged.

Configuration:

```yaml
plugins:
  - name: nodeplacement
    arguments:
      cpu: balanced
      gpu: binpack
```

The function is stateless, allocates nothing per call, and adds no CRDs or background goroutines. A larger overhaul of node scoring has been mentioned by maintainers; the function is independent of the plugin framework and can move into that design when it exists.

### Interaction with DRF

In KAI, Dominant Resource Fairness is used in the `proportion` plugin (`queue_order.go`, `resource_share`) to order queues and jobs by dominant resource share. It does not score nodes. The `balanced` strategy only orders nodes for a task that has already been chosen.

So the two act at different stages and do not pull the same decision in opposite directions: DRF decides which job goes next, and `balanced` decides which node that job's task lands on.

The interaction that remains is indirect: node choice changes how fragmented the cluster becomes, which changes what fits later and therefore which jobs need reclaim or preemption. That effect has not been measured. It needs a simulation with multiple queues and reclaim enabled before this is implemented.

## Benchmark Results

> **Status:** The numbers below were produced with the earlier formula (`variance × 4`) over 6 simulated dimensions including GPU sub-resources. They are kept for reference and will be rerun with the standard-deviation base on CPU and Memory only. All results are from simulation (`benchmark_simulation.py`) and a 3-node minikube (`benchmark_full.py`); there is no production data.

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
| **Lambda-G V3** | This proposal. 0.6×variance + 0.2×alignment + 0.1×headroom − penalties. |

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

Note: MostAllocated shows 0 stranded and $0 waste because it packs so aggressively that most pods cannot schedule at all (660 pending out of 660 total). Its zeros are an artifact of having no placed pods to strand, not evidence of good scheduling.

### Key Observations

1. **Lambda-G V3 vs BalancedAllocation** (closest real competitor): Lambda-G wins all 5 scenarios. 23% fewer stranded nodes (34 vs 44) and 53% less wasted capacity ($68k vs $145k). The variance component provides the same foundation as BalancedAllocation, while cosine alignment breaks ties by steering pods to nodes where their resource shape fits the gap.

2. **Lambda-G V3 vs DominantResource**: DRF looks at only the single most-consumed dimension. It misses cross-dimensional interactions — a node can have a moderate dominant resource but severe imbalance between other dimensions. Lambda-G considers all dimensions simultaneously through both variance and alignment.

3. **Scaling behavior**: The gap between Lambda-G and BalancedAllocation holds at 60-node scale (76.7 vs 74.1), confirming the approach doesn't degrade with cluster size.

### Scoring Latency

For N=6 dimensions: sub-microsecond per node, zero heap allocations. Negligible compared to API server round-trip time.

Benchmark source and reproduction: [github.com/0x-auth/lambda-g-auditor](https://github.com/0x-auth/lambda-g-auditor)

## Alternatives Considered

### Pure Cosine Alignment (V1)

Using only cosine similarity with entropy reduction. Over-steers in aggregate benchmarks — too aggressive about directional matching, creates new imbalances. Lost to BalancedAllocation on all 5 scenarios.

### Alignment + Complement Penalties (V2)

Adding explicit penalties for mismatched dimensions. Better pairwise decisions but still lost the aggregate benchmark because complement penalties rejected viable placements.

### Configurable Weights

Replace fixed 0.6/0.2/0.1 with user-configurable parameters. Deferred — grid search shows the top 10 weight combinations all cluster near 0.6/0.2/0.1, suggesting the optimum is broad and stable.

### Separate Plugin

The first draft proposed a standalone scoring plugin. Folding it into `nodeplacement` as a third strategy reuses the existing per-job-type configuration and keeps one place for placement policy.

### Gated Alignment (true tiebreaker)

Apply alignment only when balance scores are within ε. Every ε tested (0.5 to 10) lost the stranding reduction. A per-node `NodeOrderFn` also cannot express a cross-node comparison without two-pass selection.

### Integration with Topology-Aware Scheduling

Lambda-G scoring can compose with TAS — score within a topology domain rather than globally. Future work; doesn't affect the core scoring function.

## Implementation Plan

1. **Before implementation:** rerun the benchmark with the standard-deviation base on CPU and Memory; simulate DRF queues with reclaim enabled.
2. **Phase 1:** `balanced` strategy in `nodeplacement` for CPU and Memory, opt-in through plugin arguments.
3. **Phase 2:** Further dimensions (GPU memory, GPU compute) once users can request them separately.
4. **Phase 3:** Optional configurable weights if requested.

## References

- Issue: [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)
- Cluster defragmentation: [#1311](https://github.com/kai-scheduler/KAI-Scheduler/issues/1311)
- Koordinator proposal: [koordinator-sh/koordinator#2839](https://github.com/koordinator-sh/koordinator/pull/2839)
- Auditor + benchmark: [github.com/0x-auth/lambda-g-auditor](https://github.com/0x-auth/lambda-g-auditor)
- Scheduler implementation: [github.com/0x-auth/lambda-g-scheduler](https://github.com/0x-auth/lambda-g-scheduler)
- Docker image: [bitsabhi/lambda-g-controller](https://hub.docker.com/r/bitsabhi/lambda-g-controller)
