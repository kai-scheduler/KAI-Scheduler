// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// stackZones bounds the mask/scratch stack buffers; nodes with more NUMA zones fall back to heap.
const stackZones = 16

// zoneAllocation accumulates, per zone index, the amounts to place there (as a ResourceVector delta).
// placementFromAllocation materializes it into a pod_info.NUMAPlacement.
type zoneAllocation = map[int]resource_info.ResourceVector

// effectiveAware returns the node's aware indices minus the ignored ones. When nothing is ignored
// (the default) this is the topology's own AwareIndices with no allocation or lookup.
func (pp *numaPlugin) effectiveAware(node *node_info.NodeInfo) []int {
	if len(pp.ignoreIndices) == 0 {
		return node.NumaTopology.AwareIndices
	}
	return pp.effectiveAwareByNode[node.Name]
}

// alignedAware returns the aware indices the kubelet aligns for this task: effectiveAware for a
// Guaranteed task, and for a non-Guaranteed task the same minus the cpu/memory/hugepages indices
// (those align only for Guaranteed pods; devices align for every QoS class).
func (pp *numaPlugin) alignedAware(task *pod_info.PodInfo, node *node_info.NodeInfo) []int {
	aware := pp.effectiveAware(node)
	if isGuaranteed(task) {
		return aware
	}
	topo := node.NumaTopology
	out := make([]int, 0, len(aware))
	for _, idx := range aware {
		if isQoSGatedResource(topo.AwareNames[idx]) {
			continue
		}
		out = append(out, idx)
	}
	return out
}

// allocatable reports whether the kubelet Topology Manager would align the task on the node. A task
// the plugin does not constrain passes through as true.
func (pp *numaPlugin) allocatable(task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	return pp.solveTask(task, node, nil)
}

// evaluate returns the task's expected per-zone allocation on the node (nil for a task the plugin
// does not constrain). Used by the placement path; the predicate uses allocatable (feasibility only).
func (pp *numaPlugin) evaluate(task *pod_info.PodInfo, node *node_info.NodeInfo) (zoneAllocation, bool) {
	alloc := zoneAllocation{}
	if !pp.solveTask(task, node, alloc) {
		return nil, false
	}
	return alloc, true
}

// solveTask resolves the task's requests and scope for the node and runs solve. A task the plugin
// does not constrain passes through as admitted. When alloc is non-nil, solve records the placement
// into it; when nil, it only decides feasibility (zero-allocation).
func (pp *numaPlugin) solveTask(task *pod_info.PodInfo, node *node_info.NodeInfo, alloc zoneAllocation) bool {
	if node == nil || !pp.shouldHandle(task, node.NumaTopology) {
		return true
	}
	topo := node.NumaTopology
	aware := pp.alignedAware(task, node)
	concurrent, serial := pp.numaRequestsFor(task, topo.VectorMap).forScope(topo.Scope)
	return solve(topo, aware, concurrent, serial, alloc)
}

// solve walks the concurrent and serial NUMA requests and reports whether the node can align them.
// The concurrent requests share the per-zone ledger (native sidecars + app containers coexist), so
// each reduces availability for the next; the serial (ordinary init) requests are each aligned
// against the pristine availability, never accumulated. When alloc is non-nil the concurrent
// requests' per-zone placement is recorded there; otherwise the walk is allocation-free (a `consumed`
// scratch is taken only when a later request needs the reduced view).
func solve(topo *node_info.NumaTopology, aware []int, concurrent, serial []resource_info.ResourceVector, alloc zoneAllocation) bool {
	width := topo.VectorMap.Len()
	var maskArr [stackZones]int
	maskBuf := maskArr[:]
	if len(topo.Zones) > stackZones {
		maskBuf = make([]int, len(topo.Zones))
	}

	var consumed []float64
	for i, req := range concurrent {
		mask, ok := feasibleMask(topo, aware, req, consumed, width, maskBuf)
		if !ok {
			return false
		}
		last := i == len(concurrent)-1
		if alloc == nil && last {
			continue // nothing to record and no successor to reduce for
		}
		if consumed == nil && !last {
			consumed = make([]float64, len(topo.Zones)*width)
		}
		drawAcrossMask(topo, aware, mask, req, consumed, alloc, width)
	}
	for _, req := range serial {
		if _, ok := feasibleMask(topo, aware, req, nil, 0, nil); !ok {
			return false
		}
	}
	return true
}

// feasibleMask picks the mask the policy's evaluator would choose for one request, under the current
// availability view (Available minus consumed).
func feasibleMask(topo *node_info.NumaTopology, aware []int, req resource_info.ResourceVector, consumed []float64, width int, maskBuf []int) ([]int, bool) {
	if topo.Policy == node_info.TopologyPolicySingleNUMANode {
		return singleNUMAEvaluator{}.fit(topo, aware, req, consumed, width, maskBuf)
	}
	return restrictedEvaluator{}.fit(topo, aware, req, consumed, width, maskBuf)
}

// singleNUMAEvaluator (single-numa-node) requires each request to fit entirely within one NUMA zone,
// the lowest that holds it.
type singleNUMAEvaluator struct{}

func (singleNUMAEvaluator) fit(topo *node_info.NumaTopology, aware []int, req resource_info.ResourceVector, consumed []float64, width int, maskBuf []int) ([]int, bool) {
	for z := range topo.Zones {
		if reqFitsZone(topo, aware, req, consumed, width, z) {
			return oneZoneMask(maskBuf, z), true
		}
	}
	return nil, false
}

// restrictedEvaluator reproduces the kubelet hint merge: all per-resource preferred widths (from
// static Allocatable) must agree, and a mask of that width must satisfy every resource against
// Available. single-numa-node is the width==1 case.
type restrictedEvaluator struct{}

func (restrictedEvaluator) fit(topo *node_info.NumaTopology, aware []int, req resource_info.ResourceVector, consumed []float64, width int, maskBuf []int) ([]int, bool) {
	w := -1
	for _, idx := range aware {
		need := req.Get(idx)
		if need <= 0 {
			continue
		}
		k, ok := minWidthFromPrefix(topo.AllocatablePrefix[idx], need)
		if !ok {
			return nil, false
		}
		if w == -1 {
			w = k
		} else if k != w {
			return nil, false
		}
	}
	if w <= 0 {
		return maskBuf[:0], true // no positive aware requests: trivially aligned (nil-safe)
	}
	if w == 1 {
		for z := range topo.Zones {
			if reqFitsZone(topo, aware, req, consumed, width, z) {
				return oneZoneMask(maskBuf, z), true
			}
		}
		return nil, false
	}
	return lowestSatisfyingReqMask(topo, aware, req, consumed, width, w, maskBuf)
}

// lowestSatisfyingReqMask returns the lexicographically-lowest width-w zone mask whose summed
// Available satisfies every requested resource.
func lowestSatisfyingReqMask(topo *node_info.NumaTopology, aware []int, req resource_info.ResourceVector, consumed []float64, width, w int, maskBuf []int) ([]int, bool) {
	var found []int
	combinations(len(topo.Zones), w, func(mask []int) bool {
		if maskSatisfiesReq(topo, aware, req, consumed, width, mask) {
			found = append(maskBuf[:0], mask...)
			return false
		}
		return true
	})
	return found, found != nil
}

// reqFitsZone reports whether zone z alone satisfies every requested resource of the request.
func reqFitsZone(topo *node_info.NumaTopology, aware []int, req resource_info.ResourceVector, consumed []float64, width, z int) bool {
	for _, idx := range aware {
		need := req.Get(idx)
		if need <= 0 {
			continue
		}
		if availableAt(topo, consumed, width, z, idx) < need {
			return false
		}
	}
	return true
}

// maskSatisfiesReq reports whether the summed Available over the mask's zones satisfies every
// requested resource of the request.
func maskSatisfiesReq(topo *node_info.NumaTopology, aware []int, req resource_info.ResourceVector, consumed []float64, width int, mask []int) bool {
	for _, idx := range aware {
		need := req.Get(idx)
		if need <= 0 {
			continue
		}
		sum := 0.0
		for _, z := range mask {
			sum += availableAt(topo, consumed, width, z, idx)
		}
		if sum < need {
			return false
		}
	}
	return true
}

// drawAcrossMask draws req greedily (lowest zone first) across its mask. It reduces `consumed` (when
// non-nil) so the next concurrent request sees the draw, and records the per-zone amounts into
// `alloc` (when non-nil) for the placement path. The kubelet does not fix the per-zone split at
// admission, so any split drawing each resource entirely from the mask is acceptable.
func drawAcrossMask(topo *node_info.NumaTopology, aware []int, mask []int, req resource_info.ResourceVector, consumed []float64, alloc zoneAllocation, width int) {
	for _, idx := range aware {
		remaining := req.Get(idx)
		if remaining <= 0 {
			continue
		}
		for _, z := range mask {
			if remaining <= 0 {
				break
			}
			take := availableAt(topo, consumed, width, z, idx)
			if take > remaining {
				take = remaining
			}
			if take <= 0 {
				continue
			}
			if consumed != nil {
				consumed[z*width+idx] += take
			}
			if alloc != nil {
				recordAlloc(alloc, z, idx, take, topo.VectorMap)
			}
			remaining -= take
		}
	}
}

func recordAlloc(alloc zoneAllocation, z, idx int, amount float64, vectorMap *resource_info.ResourceVectorMap) {
	vec := alloc[z]
	if vec == nil {
		vec = resource_info.NewResourceVector(vectorMap)
		alloc[z] = vec
	}
	vec[idx] += amount
}

// availableAt is zone z's Available for resource idx, minus what prior requests in this evaluation
// already consumed (consumed nil = pristine availability).
func availableAt(topo *node_info.NumaTopology, consumed []float64, width, z, idx int) float64 {
	v := topo.Zones[z].Available.Get(idx)
	if consumed != nil {
		v -= consumed[z*width+idx]
	}
	return v
}

// minWidthFromPrefix returns the fewest zones whose largest Allocatable values sum to at least need
// (the resource's preferred NUMA width), from precomputed descending prefix sums.
func minWidthFromPrefix(prefix []float64, need float64) (int, bool) {
	if len(prefix) == 0 || prefix[len(prefix)-1] < need {
		return 0, false
	}
	for k, sum := range prefix {
		if sum >= need {
			return k + 1, true
		}
	}
	return 0, false
}

// oneZoneMask writes a single-zone mask into buf (reusing its backing array, no allocation) and
// returns it. buf is nil only for serial requests, whose returned mask the caller ignores.
func oneZoneMask(buf []int, z int) []int {
	if buf == nil {
		return nil
	}
	buf[0] = z
	return buf[:1]
}

// combinations yields every size-k subset of [0,n) as ascending index slices, in lexicographic
// order, until yield returns false.
func combinations(n, k int, yield func([]int) bool) {
	if k <= 0 || k > n {
		return
	}
	idx := make([]int, k)
	for i := range idx {
		idx[i] = i
	}
	for {
		if !yield(idx) {
			return
		}
		i := k - 1
		for i >= 0 && idx[i] == n-k+i {
			i--
		}
		if i < 0 {
			return
		}
		idx[i]++
		for j := i + 1; j < k; j++ {
			idx[j] = idx[j-1] + 1
		}
	}
}

// placementFromAllocation converts the zone-index→amounts accumulation into a pod_info.NUMAPlacement,
// ordered by zone index for a deterministic placement (so the eviction dedup's comparison is stable).
// Index-keyed: the internal scheduler representation; translation to the durable zone id happens only
// at the persistence boundary (BindRequest / annotation).
func placementFromAllocation(allocation zoneAllocation, topo *node_info.NumaTopology) pod_info.NUMAPlacement {
	indices := make([]int, 0, len(allocation))
	for idx := range allocation {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	placement := make(pod_info.NUMAPlacement, 0, len(indices))
	for _, idx := range indices {
		placement = append(placement, pod_info.ZonePlacement{
			ZoneIndex: idx,
			Amount:    vectorToResourceList(allocation[idx], topo),
		})
	}
	return placement
}

// vectorToResourceList materializes a zone's allocated amounts into a ResourceList at the placement
// boundary: CPU as a milli quantity, every other aware resource as a plain integer quantity. The
// resource name is the NRT-reported one (AwareNames), not the shared map's normalized name.
func vectorToResourceList(vec resource_info.ResourceVector, topo *node_info.NumaTopology) v1.ResourceList {
	out := v1.ResourceList{}
	for _, idx := range topo.AwareIndices {
		val := vec.Get(idx)
		if val <= 0 {
			continue
		}
		name := topo.AwareNames[idx]
		if idx == resource_info.CPUIndex {
			out[name] = *resource.NewMilliQuantity(int64(val), resource.DecimalSI)
		} else {
			out[name] = *resource.NewQuantity(int64(val), resource.DecimalSI)
		}
	}
	return out
}
