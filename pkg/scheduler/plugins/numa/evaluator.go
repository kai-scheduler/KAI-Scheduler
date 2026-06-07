// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

// resourceAmounts is a set of resource quantities — a request, a zone's allocatable, or a
// per-zone allocation.
type resourceAmounts = map[v1.ResourceName]resource.Quantity

// numaEvaluator decides whether a set of requests can be NUMA-aligned on a node and returns the expected allocation.
// Each request is one alignment unit — the whole pod under pod scope, one container under container scope.
type numaEvaluator interface {
	evaluate(nt *nodeTopology, requests []resourceAmounts) (allocation map[int]resourceAmounts, admit bool)
}

func evaluatorFor(policy tmPolicy) numaEvaluator {
	switch policy {
	case policySingleNUMANode:
		return singleNUMAEvaluator{}
	case policyRestricted:
		return restrictedEvaluator{}
	default:
		return nil
	}
}

// singleNUMAEvaluator requires each request to fit entirely within one NUMA zone (the lowest
// that fits). Requests may land on different zones (container scope), but none may span zones.
type singleNUMAEvaluator struct{}

func (singleNUMAEvaluator) evaluate(nt *nodeTopology, requests []resourceAmounts) (map[int]resourceAmounts, bool) {
	scratch := cloneScratch(nt.zones)
	allocation := map[int]resourceAmounts{}

	for _, request := range requests {
		req := project(request, nt.topologyAware)
		idx, ok := lowestZoneFitting(scratch, req)
		if !ok {
			return nil, false
		}
		subtract(scratch[idx], req)
		addAllocation(allocation, idx, req)
	}
	return allocation, true
}

// restrictedEvaluator reproduces the kubelet's hint merge: a request is admitted iff there is a
// single minimal-width NUMA mask that is a preferred (minimal-width) satisfying hint for every
// resource it requests. Equivalently, all per-resource minimal widths must agree and a mask of
// that width must satisfy every resource at once. single-numa-node is the |mask|==1 case.
type restrictedEvaluator struct{}

func (restrictedEvaluator) evaluate(nt *nodeTopology, requests []resourceAmounts) (map[int]resourceAmounts, bool) {
	scratch := cloneScratch(nt.zones)
	allocation := map[int]resourceAmounts{}

	for _, request := range requests {
		req := project(request, nt.topologyAware)
		mask, ok := preferredCommonMask(scratch, req)
		if !ok {
			return nil, false
		}
		for idx, amt := range splitAcrossMask(scratch, mask, req) {
			subtract(scratch[idx], amt)
			addAllocation(allocation, idx, amt)
		}
	}
	return allocation, true
}

// preferredCommonMask finds the lowest minimal-width NUMA mask that satisfies every requested
// resource, or reports false. It rejects when per-resource minimal widths disagree.
func preferredCommonMask(scratch []resourceAmounts, req resourceAmounts) ([]int, bool) {
	width := -1
	for r, qty := range req {
		w, ok := minWidthForResource(scratch, r, qty)
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
	return lowestSatisfyingMask(scratch, req, width)
}

// minWidthForResource is the fewest zones whose largest Available values sum to at least qty,
// i.e. the resource's preferred (minimal) NUMA-node count. Reports false when even all zones
// together cannot satisfy qty.
func minWidthForResource(scratch []resourceAmounts, r v1.ResourceName, qty resource.Quantity) (int, bool) {
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
func lowestSatisfyingMask(scratch []resourceAmounts, req resourceAmounts, width int) ([]int, bool) {
	var found []int
	combinations(len(scratch), width, func(mask []int) bool {
		if maskSatisfies(scratch, req, mask) {
			found = append([]int(nil), mask...)
			return false
		}
		return true
	})
	return found, found != nil
}

func maskSatisfies(scratch []resourceAmounts, req resourceAmounts, mask []int) bool {
	for r, qty := range req {
		sum := resource.Quantity{}
		for _, i := range mask {
			sum.Add(amountOf(scratch[i], r))
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
func splitAcrossMask(scratch []resourceAmounts, mask []int, req resourceAmounts) map[int]resourceAmounts {
	split := map[int]resourceAmounts{}
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
				split[i] = resourceAmounts{}
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

func project(request resourceAmounts, aware sets.Set[v1.ResourceName]) resourceAmounts {
	out := resourceAmounts{}
	for r, qty := range request {
		if qty.Sign() == 0 || !aware.Has(r) {
			continue
		}
		out[r] = qty.DeepCopy()
	}
	return out
}

func cloneScratch(zones []*numaZone) []resourceAmounts {
	scratch := make([]resourceAmounts, len(zones))
	for i, zone := range zones {
		amounts := make(resourceAmounts, len(zone.available))
		for r, qty := range zone.available {
			amounts[r] = qty.DeepCopy()
		}
		scratch[i] = amounts
	}
	return scratch
}

func lowestZoneFitting(scratch []resourceAmounts, req resourceAmounts) (int, bool) {
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

func subtract(amounts, delta resourceAmounts) {
	for r, qty := range delta {
		v := amountOf(amounts, r)
		v.Sub(qty)
		amounts[r] = v
	}
}

func add(amounts, delta resourceAmounts) {
	for r, qty := range delta {
		v := amountOf(amounts, r)
		v.Add(qty)
		amounts[r] = v
	}
}

func addAllocation(allocation map[int]resourceAmounts, idx int, amt resourceAmounts) {
	cur := allocation[idx]
	if cur == nil {
		cur = resourceAmounts{}
		allocation[idx] = cur
	}
	for r, qty := range amt {
		v := amountOf(cur, r)
		v.Add(qty)
		cur[r] = v
	}
}

func amountOf(amounts resourceAmounts, r v1.ResourceName) resource.Quantity {
	if qty, ok := amounts[r]; ok {
		return qty.DeepCopy()
	}
	return resource.Quantity{}
}
