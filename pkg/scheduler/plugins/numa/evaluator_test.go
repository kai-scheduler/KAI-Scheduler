// Copyright 2025 NVIDIA CORPORATION
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
)

const gpu = "nvidia.com/gpu"

// noIgnoreList is the empty ignoreList passed to evaluate in tests that do not exercise it.
var noIgnoreList = sets.New[v1.ResourceName]()

// amountAt returns the amounts placed on a given zone index, or nil if the zone is not in the placement.
func amountAt(p pod_info.NUMAPlacement, zoneIndex int) v1.ResourceList {
	for _, zp := range p {
		if zp.ZoneIndex == zoneIndex {
			return zp.Amount
		}
	}
	return nil
}

// req builds a request unit from resource-name/quantity-string pairs.
func req(pairs ...string) resourceAmounts {
	out := resourceAmounts{}
	for i := 0; i < len(pairs); i += 2 {
		out[v1.ResourceName(pairs[i])] = resource.MustParse(pairs[i+1])
	}
	return out
}

// twoZoneNode builds a restricted/single-numa node with two identical NUMA zones.
func twoZoneNode(policy node_info.TopologyManagerPolicy, perZone resourceAmounts) *node_info.NumaTopology {
	toStrings := map[string]string{}
	for r, q := range perZone {
		toStrings[string(r)] = q.String()
	}
	return numaTopology(policy, node_info.TopologyScopeContainer,
		numaZone("node-0", toStrings),
		numaZone("node-1", toStrings),
	)
}

func TestSingleNUMAEvaluator(t *testing.T) {
	node := twoZoneNode(node_info.TopologyPolicySingleNUMANode, req(gpu, "4", "cpu", "16"))
	eval := singleNUMAEvaluator{}

	t.Run("fits the lowest zone", func(t *testing.T) {
		allocation, admit := eval.evaluate(node, noIgnoreList, []resourceAmounts{req(gpu, "2", "cpu", "4")})
		assert.True(t, admit)
		assert.Equal(t, []int{0}, allocation.ZoneIndices(), "prefers the lowest zone")
		gpuAllocated := amountAt(allocation, 0)[gpu]
		assert.Equal(t, int64(2), gpuAllocated.Value())
	})

	t.Run("rejects a unit larger than any single zone", func(t *testing.T) {
		_, admit := eval.evaluate(node, noIgnoreList, []resourceAmounts{req(gpu, "6")})
		assert.False(t, admit, "6 GPUs cannot fit one 4-GPU zone")
	})

	t.Run("rejects when resources cannot co-locate on one zone", func(t *testing.T) {
		// gpu only on node-0, cpu only on node-1.
		split := numaTopology(node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{gpu: "4"}),
			numaZone("node-1", map[string]string{"cpu": "16"}),
		)
		_, admit := singleNUMAEvaluator{}.evaluate(split, noIgnoreList, []resourceAmounts{req(gpu, "1", "cpu", "1")})
		assert.False(t, admit)
	})

	t.Run("ignored resource is not aligned", func(t *testing.T) {
		// memory only on node-1; with memory ignored the cpu-only request fits node-0.
		split := numaTopology(node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{"cpu": "4"}),
			numaZone("node-1", map[string]string{"cpu": "4", "memory": "16Gi"}),
		)
		ignoreList := sets.New[v1.ResourceName]("memory")
		_, admit := singleNUMAEvaluator{}.evaluate(split, ignoreList, []resourceAmounts{req("cpu", "2", "memory", "8Gi")})
		assert.True(t, admit, "ignored memory drops out, cpu fits a single zone")
	})
}

func TestSingleNUMAContainerScopeSharesHeadroom(t *testing.T) {
	// Two 4-core zones; three containers requesting 3, 3, 2 cores. Two 3-core containers each
	// take a zone (leaving 1 core each), so the 2-core container cannot be aligned.
	node := twoZoneNode(node_info.TopologyPolicySingleNUMANode, req("cpu", "4"))
	requests := []resourceAmounts{req("cpu", "3"), req("cpu", "3"), req("cpu", "2")}

	_, admit := singleNUMAEvaluator{}.evaluate(node, noIgnoreList, requests)
	assert.False(t, admit)

	// The first two fit (one per zone).
	_, admit = singleNUMAEvaluator{}.evaluate(node, noIgnoreList, requests[:2])
	assert.True(t, admit)
}

func TestRestrictedEvaluatorWorkedExamples(t *testing.T) {
	t.Run("reject: per-resource minimal widths disagree (6 GPU + 10 CPU)", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "4", "cpu", "16"))
		_, admit := restrictedEvaluator{}.evaluate(node, noIgnoreList, []resourceAmounts{req(gpu, "6", "cpu", "10")})
		assert.False(t, admit, "GPU needs 2 nodes, CPU needs 1 — no common preferred mask")
	})

	t.Run("admit on the common width-2 mask (6 GPU + 24 CPU)", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "4", "cpu", "16"))
		allocation, admit := restrictedEvaluator{}.evaluate(node, noIgnoreList, []resourceAmounts{req(gpu, "6", "cpu", "24")})
		assert.True(t, admit)
		assert.Equal(t, []int{0, 1}, allocation.ZoneIndices(), "spans both NUMA zones")

		gpu0, gpu1 := amountAt(allocation, 0)[gpu], amountAt(allocation, 1)[gpu]
		totalGPU := gpu0.Value() + gpu1.Value()
		assert.Equal(t, int64(6), totalGPU, "the full GPU request is allocated across the mask")
	})

	t.Run("reject: 4-GPU + 1-CPU footgun", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "2", "cpu", "100"))
		_, admit := restrictedEvaluator{}.evaluate(node, noIgnoreList, []resourceAmounts{req(gpu, "4", "cpu", "1")})
		assert.False(t, admit, "GPU needs 2 nodes, CPU needs 1")
	})

	t.Run("admit on a single zone when width is 1", func(t *testing.T) {
		node := twoZoneNode(node_info.TopologyPolicyRestricted, req(gpu, "4", "cpu", "16"))
		allocation, admit := restrictedEvaluator{}.evaluate(node, noIgnoreList, []resourceAmounts{req(gpu, "2", "cpu", "8")})
		assert.True(t, admit)
		assert.Equal(t, []int{0}, allocation.ZoneIndices(), "width 1 stays on one zone")
	})
}

func TestRestrictedSelectsLowestMask(t *testing.T) {
	// Three zones; a width-2 request should select {0,1}, the lowest satisfying mask.
	node := numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{gpu: "2"}),
		numaZone("node-1", map[string]string{gpu: "2"}),
		numaZone("node-2", map[string]string{gpu: "2"}),
	)
	allocation, admit := restrictedEvaluator{}.evaluate(node, noIgnoreList, []resourceAmounts{req(gpu, "4")})
	assert.True(t, admit)
	assert.Equal(t, []int{0, 1}, allocation.ZoneIndices(), "selects the lowest satisfying mask, not node-2")
}

func TestEvaluatorFor(t *testing.T) {
	assert.IsType(t, singleNUMAEvaluator{}, evaluatorFor(node_info.TopologyPolicySingleNUMANode))
	assert.IsType(t, restrictedEvaluator{}, evaluatorFor(node_info.TopologyPolicyRestricted))
	assert.Nil(t, evaluatorFor(node_info.TopologyPolicyBestEffort))
	assert.Nil(t, evaluatorFor(node_info.TopologyPolicyNone))
}
