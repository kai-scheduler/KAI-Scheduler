// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// numaEvaluator decides whether a set of requests can be NUMA-aligned on a node and returns the
// expected per-zone placement. Each request is one alignment unit — the whole pod under pod scope,
// one container under container scope.
type numaEvaluator interface {
	evaluate(topo *node_info.NumaTopology, ignoreList sets.Set[v1.ResourceName], requests []v1.ResourceList) (placement pod_info.NUMAPlacement, admit bool)
}

func evaluatorFor(policy node_info.TopologyManagerPolicy) numaEvaluator {
	switch policy {
	case node_info.TopologyPolicySingleNUMANode:
		return singleNUMAEvaluator{}
	case node_info.TopologyPolicyRestricted:
		return restrictedEvaluator{}
	default:
		return nil
	}
}

// singleNUMAEvaluator requires each request to fit entirely within one NUMA zone (the lowest
// that fits). Requests may land on different zones (container scope), but none may span zones.
type singleNUMAEvaluator struct{}

func (singleNUMAEvaluator) evaluate(topo *node_info.NumaTopology, ignoreList sets.Set[v1.ResourceName], requests []v1.ResourceList) (pod_info.NUMAPlacement, bool) {
	available := cloneAvailable(topo.Zones)
	allocation := map[int]v1.ResourceList{}

	for _, request := range requests {
		req := extractNumaRequest(request, topo.Resources, ignoreList)
		idx, ok := lowestZoneFitting(available, req)
		if !ok {
			return nil, false
		}
		subtract(available[idx], req)
		addAllocation(allocation, idx, req)
	}
	return placementFromAllocation(allocation), true
}

// restrictedEvaluator reproduces the kubelet's hint merge: a request is admitted iff there is a
// single minimal-width NUMA mask that is a preferred (minimal-width) satisfying hint for every
// resource it requests. Equivalently, all per-resource minimal widths must agree and a mask of
// that width must satisfy every resource at once. single-numa-node is the |mask|==1 case.
//
// Preferred width is computed from Allocatable (matching the kubelet device manager's
// m.allDevices pass), while feasibility is checked against Available (matching the kubelet's
// available-device pass). When some devices are already allocated the two can disagree: a
// single-zone placement may be preferred by capacity but infeasible by availability, making the
// only feasible mask (multi-zone) non-preferred → restricted rejects, matching kubelet behavior.
type restrictedEvaluator struct{}

func (restrictedEvaluator) evaluate(topo *node_info.NumaTopology, ignoreList sets.Set[v1.ResourceName], requests []v1.ResourceList) (pod_info.NUMAPlacement, bool) {
	available := cloneAvailable(topo.Zones)
	allocatable := cloneAllocatable(topo.Zones)
	allocation := map[int]v1.ResourceList{}

	for _, request := range requests {
		req := extractNumaRequest(request, topo.Resources, ignoreList)
		mask, ok := preferredCommonMask(available, allocatable, req)
		if !ok {
			return nil, false
		}
		for idx, amt := range splitAcrossMask(available, mask, req) {
			subtract(available[idx], amt)
			addAllocation(allocation, idx, amt)
		}
	}
	return placementFromAllocation(allocation), true
}

// placementFromAllocation converts the evaluator's zone-index→amounts accumulation into a
// pod_info.NUMAPlacement, ordered by zone index for a deterministic placement (so the eviction
// dedup's comparison is stable). Index-keyed: the internal scheduler representation. Translation
// to the durable zone id happens only at the persistence boundary (BindRequest / annotation).
func placementFromAllocation(allocation map[int]v1.ResourceList) pod_info.NUMAPlacement {
	indices := make([]int, 0, len(allocation))
	for idx := range allocation {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	placement := make(pod_info.NUMAPlacement, 0, len(indices))
	for _, idx := range indices {
		placement = append(placement, pod_info.ZonePlacement{
			ZoneIndex: idx,
			Amount:    allocation[idx],
		})
	}
	return placement
}

// preferredCommonMask finds the lowest minimal-width NUMA mask that satisfies every requested
// resource, or reports false. It rejects when per-resource minimal widths disagree.
//
// allocatable is used for min-width (preferred) computation; scratch (available) is used for
// feasibility. This matches the kubelet device manager, which uses m.allDevices for
// minAffinitySize and the available set for the per-mask device count check.
func preferredCommonMask(available, allocatable []v1.ResourceList, req v1.ResourceList) ([]int, bool) {
	width := -1
	for r, qty := range req {
		w, ok := minWidthForResource(allocatable, r, qty)
		if !ok {
			return nil, false
		}
		if width == -1 {
			width = w
		} else if w != width {
			return nil, false
		}
	}
	if width <= 0 {
		return []int{}, true
	}
	return lowestSatisfyingMask(available, req, width)
}

// minWidthForResource is the fewest zones whose largest Available values sum to at least qty,
// i.e. the resource's preferred (minimal) NUMA-node count. Reports false when even all zones
// together cannot satisfy qty.
func minWidthForResource(scratch []v1.ResourceList, r v1.ResourceName, qty resource.Quantity) (int, bool) {
	vals := make([]resource.Quantity, len(scratch))
	total := resource.Quantity{}
	for i := range scratch {
		v := amountOf(scratch[i], r)
		vals[i] = v
		total.Add(v)
	}
	if total.Cmp(qty) < 0 {
		return 0, false
	}

	sort.Slice(vals, func(i, j int) bool { return vals[i].Cmp(vals[j]) > 0 })
	acc := resource.Quantity{}
	for k := range vals {
		acc.Add(vals[k])
		if acc.Cmp(qty) >= 0 {
			return k + 1, true
		}
	}
	return 0, false
}

// lowestSatisfyingMask returns the lexicographically-lowest width-sized zone mask whose summed
// Available satisfies every requested resource.
func lowestSatisfyingMask(available []v1.ResourceList, req v1.ResourceList, width int) ([]int, bool) {
	var found []int
	combinations(len(available), width, func(mask []int) bool {
		if maskSatisfies(available, req, mask) {
			found = append([]int(nil), mask...)
			return false
		}
		return true
	})
	return found, found != nil
}

func maskSatisfies(available []v1.ResourceList, req v1.ResourceList, mask []int) bool {
	for r, qty := range req {
		sum := resource.Quantity{}
		for _, i := range mask {
			sum.Add(amountOf(available[i], r))
		}
		if sum.Cmp(qty) < 0 {
			return false
		}
	}
	return true
}

// splitAcrossMask distributes each resource greedily across the mask's zones (lowest first),
// producing the per-zone amounts to allocate. The kubelet does not fix the per-zone split at
// admission, so any split drawing each resource entirely from the mask is acceptable; this is
// internal accounting only.
func splitAcrossMask(scratch []v1.ResourceList, mask []int, req v1.ResourceList) map[int]v1.ResourceList {
	split := map[int]v1.ResourceList{}
	for r, qty := range req {
		remaining := qty.DeepCopy()
		for _, i := range mask {
			if remaining.Sign() <= 0 {
				break
			}
			take := amountOf(scratch[i], r)
			if take.Cmp(remaining) > 0 {
				take = remaining.DeepCopy()
			}
			if take.Sign() <= 0 {
				continue
			}
			if split[i] == nil {
				split[i] = v1.ResourceList{}
			}
			cur := amountOf(split[i], r)
			cur.Add(take)
			split[i][r] = cur
			remaining.Sub(take)
		}
	}
	return split
}

// combinations yields every size-k subset of [0,n) as ascending index slices, in
// lexicographic order, until yield returns false.
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

// extractNumaRequest keeps only the resources that constrain zone selection: those reported per-zone
// (aware) and not ignored, dropping zero-quantity entries. ignoreList is applied here rather
// than at ingestion because it is plugin configuration, unknown to the topology builder.
func extractNumaRequest(request v1.ResourceList, aware, ignoreList sets.Set[v1.ResourceName]) v1.ResourceList {
	out := v1.ResourceList{}
	for r, qty := range request {
		if qty.Sign() == 0 || !aware.Has(r) || ignoreList.Has(r) {
			continue
		}
		out[r] = qty.DeepCopy()
	}
	return out
}

func cloneAvailable(zones []*node_info.NumaZone) []v1.ResourceList {
	available := make([]v1.ResourceList, len(zones))
	for i, zone := range zones {
		amounts := make(v1.ResourceList, len(zone.Available))
		for r, qty := range zone.Available {
			amounts[r] = qty.DeepCopy()
		}
		available[i] = amounts
	}
	return available
}

func cloneAllocatable(zones []*node_info.NumaZone) []v1.ResourceList {
	allocatable := make([]v1.ResourceList, len(zones))
	for i, zone := range zones {
		amounts := make(v1.ResourceList, len(zone.Allocatable))
		for r, qty := range zone.Allocatable {
			amounts[r] = qty.DeepCopy()
		}
		allocatable[i] = amounts
	}
	return allocatable
}

func lowestZoneFitting(scratch []v1.ResourceList, req v1.ResourceList) (int, bool) {
	for i := range scratch {
		fits := true
		for r, qty := range req {
			if avail := amountOf(scratch[i], r); avail.Cmp(qty) < 0 {
				fits = false
				break
			}
		}
		if fits {
			return i, true
		}
	}
	return 0, false
}

func subtract(amounts, delta v1.ResourceList) {
	for r, qty := range delta {
		v := amountOf(amounts, r)
		v.Sub(qty)
		amounts[r] = v
	}
}

func add(amounts, delta v1.ResourceList) {
	for r, qty := range delta {
		v := amountOf(amounts, r)
		v.Add(qty)
		amounts[r] = v
	}
}

func addAllocation(allocation map[int]v1.ResourceList, idx int, amt v1.ResourceList) {
	cur := allocation[idx]
	if cur == nil {
		cur = v1.ResourceList{}
		allocation[idx] = cur
	}
	for r, qty := range amt {
		v := amountOf(cur, r)
		v.Add(qty)
		cur[r] = v
	}
}

func amountOf(amounts v1.ResourceList, r v1.ResourceName) resource.Quantity {
	if qty, ok := amounts[r]; ok {
		return qty.DeepCopy()
	}
	return resource.Quantity{}
}
