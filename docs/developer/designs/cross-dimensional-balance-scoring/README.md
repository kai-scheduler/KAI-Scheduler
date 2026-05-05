# Cross-Dimensional Resource Balance Scoring

**Author:** Abhishek Srivastava (@0x-auth)  
**Date:** 2026-04-03  
**Status:** Proposal  
**Issue:** [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)

## Summary

Add a scoring plugin that prevents resource fragmentation by combining post-placement variance minimization with cosine alignment between the pod's resource request vector and the node's free capacity vector. The plugin measures how much *more balanced* a node becomes after placing a pod, then uses directional alignment as a tiebreaker to steer pods toward nodes where their resource shape fills the gap.

## Motivation

### The Problem

KAI-Scheduler currently supports two node-ordering strategies:

- **Bin-packing (MostAllocated):** Prefers nodes with the highest overall utilization. A node at 95% GPU-Memory / 15% CPU looks "mostly full" — bin-packing will avoid it, stranding CPU capacity.
- **Spread (LeastAllocated):** Prefers nodes with the most free resources. The same node looks "partially used" — spread will send more pods to it, but those pods may need GPU-Memory that isn't there.

Neither strategy considers the *shape* of resource usage across dimensions. Both treat a node at 50% CPU / 50% RAM identically to one at 90% CPU / 10% RAM. The second node has stranded RAM — you're paying for memory nobody can use.

The existing **BalancedAllocation** strategy (minimize per-node variance between dimensions) is better, but it only asks "will this node be more even after placement?" It does not ask "does this pod's resource shape *match* what this node needs?" When multiple nodes have similar variance, BalancedAllocation picks arbitrarily. Lambda-G uses directional alignment to pick the node where the pod actually fills the gap.

### Real-World Impact

In GPU clusters running mixed inference workloads, cross-dimensional fragmentation is common:

| Workload Type | GPU-Mem | GPU-Compute | CPU | RAM | Stranded Resource |
|---|---|---|---|---|---|
| LLaMA 70B serving | ~95% | ~20% | ~30% | ~60% | GPU-Compute |
| Batch small-model inference | ~30% | ~85% | ~70% | ~20% | GPU-Memory, RAM |
| CPU preprocessing pipeline | ~0% | ~0% | ~90% | ~25% | RAM, all GPU |

This is the "jagged cluster" problem described in [#1311](https://github.com/kai-scheduler/KAI-Scheduler/issues/1311), but caught at scheduling time rather than repaired after the fact.

### Why Now

The vectorized resource representation landing in [#1353](https://github.com/kai-scheduler/KAI-Scheduler/issues/1353) makes a vector-based scoring plugin natural — the infrastructure for multi-dimensional resource vectors is already there.

## Proposal

### Scope

This proposal covers CPU, Memory, and GPU-Memory as scoring dimensions. GPU-Compute requests are not currently supported in KAI-Scheduler (as noted by @enoodle in [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)), but the scoring function generalizes to any number of dimensions without modification.

### Score Function (V3 — Hybrid Variance-Alignment)

For each candidate node, compute five components:

```
score = 0.6 × variance_score
      + 0.2 × alignment_score
      + 0.1 × headroom_score
      − pressure_penalty
      − strand_penalty
```

#### Component 1: Post-Placement Variance Score (weight: 0.6)

```
after_frac[i] = (node.used[i] + pod.req[i]) / node.capacity[i]   for each active dimension
mean          = average(after_frac)
variance      = average((after_frac[i] - mean)² for each i)
variance_score = max(0, (1.0 - variance × 4) × 100)
```

This is the same signal as Kubernetes BalancedAllocation. It asks: "how evenly utilized will this node be across all dimensions after placing this pod?" A node where CPU, RAM, and GPU-Memory are all at 60% scores higher than one at 90% CPU / 30% RAM / 50% GPU-Memory.

This component does the majority of the work (60% weight) because variance reduction is the single strongest predictor of cluster-wide resource balance.

#### Component 2: Cosine Alignment Score (weight: 0.2)

```
node_free[i]  = free_capacity[i] / total_capacity[i]   for each active dimension
pod_frac[i]   = pod.req[i] / node.capacity[i]          for each active dimension

alignment       = cosine_similarity(node_free, pod_frac)
alignment_score = alignment × 100
```

This asks: "does the pod's resource shape point in the same direction as the node's available capacity?" A CPU-heavy pod (large cpu_req, small ram_req) gets a high alignment score on a node with lots of free CPU and little free RAM. The pod "fills in" exactly what the node has to offer.

This is the tiebreaker that distinguishes Lambda-G from BalancedAllocation. When two nodes have similar post-placement variance, alignment steers the pod to the node where it actually matches the gap shape.

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
- Alignment at 0.2 provides enough signal to break ties without overwhelming variance.
- Headroom above 0.1 causes under-packing. Below 0.05 has no measurable effect.
- The pressure cliff at 0.92 and strand threshold at 0.80/0.20 were determined empirically; they are the values where the composite score (balance − stranded × 0.5 − waste/10000) is maximized.

The weights are fixed constants, not per-cluster tuning parameters. They can be made configurable later if the community requests it.

### Worked Numerical Example

**Setup (4D for clarity):**
- Node A free fractions: `[CPU: 0.20, RAM: 0.70, GPU-Mem: 0.50, IOPS: 0.50]`
- Node B free fractions: `[CPU: 0.75, RAM: 0.25, GPU-Mem: 0.50, IOPS: 0.50]`
- Pod request fractions: `[CPU: 0.15, RAM: 0.02, GPU-Mem: 0.05, IOPS: 0.05]` (CPU-heavy pod)

**Node A (CPU tight, RAM plentiful):**
- After placement: `[0.35, 0.32, 0.55, 0.55]` → variance = 0.0096 → variance_score = 96.2
- Alignment: cos([0.15, 0.02, 0.05, 0.05], [0.20, 0.70, 0.50, 0.50]) = 0.52 → alignment_score = 52.0
- Headroom: mean([0.20, 0.70, 0.50, 0.50]) = 0.475 → headroom_score = 47.5
- No pressure or strand penalties
- **Score: 0.6×96.2 + 0.2×52.0 + 0.1×47.5 = 72.9**

**Node B (CPU plentiful, RAM tight):**
- After placement: `[0.40, 0.77, 0.55, 0.55]` → variance = 0.0190 → variance_score = 92.4
- Alignment: cos([0.15, 0.02, 0.05, 0.05], [0.75, 0.25, 0.50, 0.50]) = 0.81 → alignment_score = 81.0
- Headroom: mean([0.75, 0.25, 0.50, 0.50]) = 0.50 → headroom_score = 50.0
- No pressure or strand penalties
- **Score: 0.6×92.4 + 0.2×81.0 + 0.1×50.0 = 76.6**

**Result:** Node B wins (76.6 > 72.9). The variance component slightly favors Node A (96.2 vs 92.4), but alignment strongly favors Node B (81.0 vs 52.0) because the CPU-heavy pod matches Node B's available CPU capacity. Lambda-G correctly sends the CPU-heavy pod to the CPU-rich node.

**What other strategies would do:**
- **LeastAllocated:** Tie (both have mean free ~0.475/0.50). Arbitrary pick.
- **MostAllocated:** Node A (higher utilization). Sends CPU-heavy pod to CPU-constrained node. **Wrong.**
- **BalancedAllocation:** Node A (lower post-placement variance). Misses the directional match. **Suboptimal.**
- **DominantResource:** Node B (lower dominant dimension after placement). Same winner as Lambda-G here, but for a different reason — DRF only looks at the max dimension, not the full shape.

### Integration Point

The scoring function is stateless and fits the existing `NodeOrderFn` plugin pattern:

```go
func (lg *LambdaGPlugin) Score(ctx context.Context, state *framework.CycleState,
    pod *v1.Pod, nodeName string) (int64, *framework.Status) {
    // Read node free capacity from snapshot
    // Compute pod request fractions
    // Return score (0-100 normalized)
}
```

No additional state, no background goroutines, no CRDs.

## Benchmark Results

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

### Integration with Topology-Aware Scheduling

Lambda-G scoring can compose with TAS — score within a topology domain rather than globally. Future work; doesn't affect the core scoring function.

## Implementation Plan

1. **Phase 1:** Scoring plugin for CPU + Memory + GPU-Memory, behind a feature flag
2. **Phase 2:** Expand to additional dimensions as KAI's resource model supports them
3. **Phase 3:** Optional configurable weights if community requests tuning

## References

- Issue: [#1373](https://github.com/kai-scheduler/KAI-Scheduler/issues/1373)
- Cluster defragmentation: [#1311](https://github.com/kai-scheduler/KAI-Scheduler/issues/1311)
- Vectorized resources: [#1353](https://github.com/kai-scheduler/KAI-Scheduler/issues/1353)
- Koordinator proposal: [koordinator-sh/koordinator#2839](https://github.com/koordinator-sh/koordinator/pull/2839)
- Auditor + benchmark: [github.com/0x-auth/lambda-g-auditor](https://github.com/0x-auth/lambda-g-auditor)
- Scheduler implementation: [github.com/0x-auth/lambda-g-scheduler](https://github.com/0x-auth/lambda-g-scheduler)
- Docker image: [bitsabhi/lambda-g-controller](https://hub.docker.com/r/bitsabhi/lambda-g-controller)
