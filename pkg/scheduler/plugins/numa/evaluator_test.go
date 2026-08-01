// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const gpu = "nvidia.com/gpu"

// noIgnoreList is the empty ignoreList passed in tests that do not exercise it.
var noIgnoreList = sets.New[v1.ResourceName]()

// evalPlacement runs the solver on requests (as the concurrent set) against topo and returns the
// resulting placement and admit decision — the form the worked-example assertions read.
func evalPlacement(topo *node_info.NumaTopology, ignore sets.Set[v1.ResourceName], requests []v1.ResourceList) (pod_info.NUMAPlacement, bool) {
	aware := topo.AwareIndices
	if ignore.Len() > 0 {
		ig := sets.New[int]()
		for name := range ignore {
			if idx := topo.VectorMap.GetIndex(name); idx >= 0 {
				ig.Insert(idx)
			}
		}
		aware = filterAware(topo.AwareIndices, ig)
	}
	reqs := make([]resource_info.ResourceVector, len(requests))
	for i, r := range requests {
		reqs[i] = resource_info.NewResourceVectorFromResourceList(r, topo.VectorMap)
	}
	alloc := zoneAllocation{}
	ok := solve(topo, aware, reqs, nil, alloc)
	return placementFromAllocation(alloc, topo), ok
}

// amountAt returns the amounts placed on a given zone index, or nil if the zone is not in the placement.
func amountAt(p pod_info.NUMAPlacement, zoneIndex int) v1.ResourceList {
	for _, zp := range p {
		if zp.ZoneIndex == zoneIndex {
			return zp.Amount
		}
	}
	return nil
}

// req builds a request from resource-name/quantity-string pairs.
func req(pairs ...string) v1.ResourceList {
	out := v1.ResourceList{}
	for i := 0; i < len(pairs); i += 2 {
		out[v1.ResourceName(pairs[i])] = resource.MustParse(pairs[i+1])
	}
	return out
}

// partialZone builds a NumaZone whose Allocatable differs from Available, modelling a zone
// that has pre-existing allocations. allocatable is the static per-zone capacity; available is
// what remains after current pod allocations are subtracted.
func partialZone(id string, allocatable, available map[string]string) node_info.NumaZoneSpec {
	alloc := v1.ResourceList{}
	for name, qty := range allocatable {
		alloc[v1.ResourceName(name)] = resource.MustParse(qty)
	}
	avail := v1.ResourceList{}
	for name, qty := range available {
		avail[v1.ResourceName(name)] = resource.MustParse(qty)
	}
	return node_info.NumaZoneSpec{ID: id, Allocatable: alloc, Available: avail}
}

// twoZoneNode builds a restricted/single-numa node with two identical NUMA zones.
func twoZoneNode(policy node_info.TopologyManagerPolicy, perZone v1.ResourceList) *node_info.NumaTopology {
	toStrings := map[string]string{}
	for r, q := range perZone {
		toStrings[string(r)] = q.String()
	}
	return numaTopology(policy, node_info.TopologyScopeContainer,
		numaZone("node-0", toStrings),
		numaZone("node-1", toStrings),
	)
}

func TestSingleNUMASolve(t *testing.T) {
	node := twoZoneNode(node_info.TopologyPolicySingleNUMANode, req(gpu, "4", "cpu", "16"))

	t.Run("fits the lowest zone", func(t *testing.T) {
		allocation, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "2", "cpu", "4")})
		assert.True(t, admit)
		assert.Equal(t, []int{0}, allocation.ZoneIndices(), "prefers the lowest zone")
		gpuAllocated := amountAt(allocation, 0)[gpu]
		assert.Equal(t, int64(2), gpuAllocated.Value())
	})

	t.Run("rejects a request larger than any single zone", func(t *testing.T) {
		_, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "6")})
		assert.False(t, admit, "6 GPUs cannot fit one 4-GPU zone")
	})

	t.Run("rejects when resources cannot co-locate on one zone", func(t *testing.T) {
		// gpu only on node-0, cpu only on node-1.
		split := numaTopology(node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{gpu: "4"}),
			numaZone("node-1", map[string]string{"cpu": "16"}),
		)
		_, admit := evalPlacement(split, noIgnoreList, []v1.ResourceList{req(gpu, "1", "cpu", "1")})
		assert.False(t, admit)
	})

	t.Run("rejects when total fits across zones but no single zone fits", func(t *testing.T) {
		// Both zones carry both resources. The request total (3 GPU, 6 CPU) is within the node's
		// combined capacity (4 GPU, 8 CPU), but exceeds what any single zone holds (2 GPU, 4 CPU).
		// single-numa requires one zone to satisfy everything, so it rejects despite the aggregate fit.
		node := twoZoneNode(node_info.TopologyPolicySingleNUMANode, req(gpu, "2", "cpu", "4"))
		_, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "3", "cpu", "6")})
		assert.False(t, admit, "fits across both zones combined, but neither zone alone fits")
	})

	t.Run("ignored resource is not aligned", func(t *testing.T) {
		// memory only on node-1; with memory ignored the cpu-only request fits node-0.
		split := numaTopology(node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{"cpu": "4"}),
			numaZone("node-1", map[string]string{"cpu": "4", "memory": "16Gi"}),
		)
		ignoreList := sets.New[v1.ResourceName]("memory")
		_, admit := evalPlacement(split, ignoreList, []v1.ResourceList{req("cpu", "2", "memory", "8Gi")})
		assert.True(t, admit, "ignored memory drops out, cpu fits a single zone")
	})
}

func TestSingleNUMAContainerScopeSharesHeadroom(t *testing.T) {
	// Two 4-core zones; three containers requesting 3, 3, 2 cores. Two 3-core containers each
	// take a zone (leaving 1 core each), so the 2-core container cannot be aligned.
	node := twoZoneNode(node_info.TopologyPolicySingleNUMANode, req("cpu", "4"))
	requests := []v1.ResourceList{req("cpu", "3"), req("cpu", "3"), req("cpu", "2")}

	_, admit := evalPlacement(node, noIgnoreList, requests)
	assert.False(t, admit)

	// The first two fit (one per zone).
	_, admit = evalPlacement(node, noIgnoreList, requests[:2])
	assert.True(t, admit)
}

func TestRestrictedAllocatableVsAvailable(t *testing.T) {
	t.Run("reject: per-resource minimal widths disagree (6 GPU + 10 CPU)", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "4", "cpu", "16"))
		_, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "6", "cpu", "10")})
		assert.False(t, admit, "GPU needs 2 nodes, CPU needs 1 — no common preferred mask")
	})

	t.Run("admit on the common width-2 mask (6 GPU + 24 CPU)", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "4", "cpu", "16"))
		allocation, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "6", "cpu", "24")})
		assert.True(t, admit)
		assert.Equal(t, []int{0, 1}, allocation.ZoneIndices(), "spans both NUMA zones")

		gpu0, gpu1 := amountAt(allocation, 0)[gpu], amountAt(allocation, 1)[gpu]
		totalGPU := gpu0.Value() + gpu1.Value()
		assert.Equal(t, int64(6), totalGPU, "the full GPU request is allocated across the mask")
	})

	t.Run("reject: 4-GPU + 1-CPU footgun", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "2", "cpu", "100"))
		_, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "4", "cpu", "1")})
		assert.False(t, admit, "GPU needs 2 nodes, CPU needs 1")
	})

	t.Run("admit on a single zone when width is 1", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "4", "cpu", "16"))
		allocation, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "2", "cpu", "8")})
		assert.True(t, admit)
		assert.Equal(t, []int{0}, allocation.ZoneIndices(), "width 1 stays on one zone")
	})

	// Allocatable-vs-available regression tests: the preferred (minAffinitySize) width must be
	// computed from Allocatable, matching the kubelet device manager's m.allDevices pass. When
	// current availability drops below per-zone allocatable capacity, the single-zone preferred
	// hint may be infeasible, making the only feasible mask non-preferred → restricted rejects.

	t.Run("reject: 4 GPU requested, allocatable=4/zone but only 3 available/zone", func(t *testing.T) {
		// Allocatable: 4 GPU per zone → minAffinitySize=1 (single zone preferred by capacity).
		// Available:   3 GPU per zone → no single-zone mask is feasible.
		// Only feasible mask {z0,z1} has width 2 ≠ minAffinitySize 1 → preferred=false → reject.
		node := numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer,
			partialZone("node-0",
				map[string]string{gpu: "4", "cpu": "95"},
				map[string]string{gpu: "3", "cpu": "45"},
			),
			partialZone("node-1",
				map[string]string{gpu: "4", "cpu": "96"},
				map[string]string{gpu: "3", "cpu": "46"},
			),
		)
		_, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "4", "cpu", "50")})
		assert.False(t, admit, "single-zone preferred by allocatable but infeasible by available → non-preferred width-2 hint → reject")
	})

	t.Run("reject: 1 GPU + 50 CPU, CPU fits by allocatable but fragmented in available", func(t *testing.T) {
		// After two pods (1 GPU + 50 CPU each) land — one per zone — neither zone has 50 CPU
		// available, though both have 50+ by allocatable. GPU still fits single-zone by both.
		// CPU: allocatable minWidth=1, but no single zone is feasible by available → preferred=false → reject.
		node := numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer,
			partialZone("node-0",
				map[string]string{gpu: "4", "cpu": "95"},
				map[string]string{gpu: "3", "cpu": "45"},
			),
			partialZone("node-1",
				map[string]string{gpu: "4", "cpu": "96"},
				map[string]string{gpu: "3", "cpu": "46"},
			),
		)
		_, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "1", "cpu", "50")})
		assert.False(t, admit, "CPU fits by allocatable (minWidth=1) but no zone has 50 CPU available → non-preferred → reject")
	})
}

func TestRestrictedSelectsLowestMask(t *testing.T) {
	// Three zones; a width-2 request should select {0,1}, the lowest satisfying mask.
	node := numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{gpu: "2"}),
		numaZone("node-1", map[string]string{gpu: "2"}),
		numaZone("node-2", map[string]string{gpu: "2"}),
	)
	allocation, admit := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, "4")})
	assert.True(t, admit)
	assert.Equal(t, []int{0, 1}, allocation.ZoneIndices(), "selects the lowest satisfying mask, not node-2")
}
