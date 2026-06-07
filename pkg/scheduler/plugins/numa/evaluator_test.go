// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

const gpu = "nvidia.com/gpu"

// req builds a request unit from resource-name/quantity-string pairs.
func req(pairs ...string) resourceAmounts {
	out := resourceAmounts{}
	for i := 0; i < len(pairs); i += 2 {
		out[v1.ResourceName(pairs[i])] = resource.MustParse(pairs[i+1])
	}
	return out
}

// twoZoneNode builds a restricted/single-numa node with two identical NUMA zones.
func twoZoneNode(policy string, perZone resourceAmounts) *nodeTopology {
	toStrings := map[string]string{}
	for r, q := range perZone {
		toStrings[string(r)] = q.String()
	}
	return buildNodeTopology(
		nrtWithAttributes(policy, scopeValueContainer,
			numaNodeZone("node-0", toStrings),
			numaNodeZone("node-1", toStrings),
		),
		sets.New[v1.ResourceName](),
	)
}

func TestSingleNUMAEvaluator(t *testing.T) {
	node := twoZoneNode(policyValueSingleNUMANode, req(gpu, "4", "cpu", "16"))
	eval := singleNUMAEvaluator{}

	t.Run("fits the lowest zone", func(t *testing.T) {
		allocation, admit := eval.evaluate(node, []resourceAmounts{req(gpu, "2", "cpu", "4")})
		assert.True(t, admit)
		assert.Len(t, allocation, 1)
		assert.Contains(t, allocation, 0, "prefers the lowest zone")
		gpuAllocated := allocation[0][gpu]
		assert.Equal(t, int64(2), gpuAllocated.Value())
	})

	t.Run("rejects a unit larger than any single zone", func(t *testing.T) {
		_, admit := eval.evaluate(node, []resourceAmounts{req(gpu, "6")})
		assert.False(t, admit, "6 GPUs cannot fit one 4-GPU zone")
	})

	t.Run("rejects when resources cannot co-locate on one zone", func(t *testing.T) {
		// gpu only on node-0, cpu only on node-1.
		split := buildNodeTopology(
			nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer,
				numaNodeZone("node-0", map[string]string{gpu: "4"}),
				numaNodeZone("node-1", map[string]string{"cpu": "16"}),
			),
			sets.New[v1.ResourceName](),
		)
		_, admit := singleNUMAEvaluator{}.evaluate(split, []resourceAmounts{req(gpu, "1", "cpu", "1")})
		assert.False(t, admit)
	})
}

func TestSingleNUMAContainerScopeSharesHeadroom(t *testing.T) {
	// Two 4-core zones; three containers requesting 3, 3, 2 cores. Two 3-core containers each
	// take a zone (leaving 1 core each), so the 2-core container cannot be aligned.
	node := twoZoneNode(policyValueSingleNUMANode, req("cpu", "4"))
	requests := []resourceAmounts{req("cpu", "3"), req("cpu", "3"), req("cpu", "2")}

	_, admit := singleNUMAEvaluator{}.evaluate(node, requests)
	assert.False(t, admit)

	// The first two fit (one per zone).
	_, admit = singleNUMAEvaluator{}.evaluate(node, requests[:2])
	assert.True(t, admit)
}

func TestRestrictedEvaluatorWorkedExamples(t *testing.T) {
	t.Run("reject: per-resource minimal widths disagree (6 GPU + 10 CPU)", func(t *testing.T) {
		node := twoZoneNode(policyValueRestricted, req(gpu, "4", "cpu", "16"))
		_, admit := restrictedEvaluator{}.evaluate(node, []resourceAmounts{req(gpu, "6", "cpu", "10")})
		assert.False(t, admit, "GPU needs 2 nodes, CPU needs 1 — no common preferred mask")
	})

	t.Run("admit on the common width-2 mask (6 GPU + 24 CPU)", func(t *testing.T) {
		node := twoZoneNode(policyValueRestricted, req(gpu, "4", "cpu", "16"))
		allocation, admit := restrictedEvaluator{}.evaluate(node, []resourceAmounts{req(gpu, "6", "cpu", "24")})
		assert.True(t, admit)
		assert.Len(t, allocation, 2, "spans both NUMA zones")

		gpu0, gpu1 := allocation[0][gpu], allocation[1][gpu]
		totalGPU := gpu0.Value() + gpu1.Value()
		assert.Equal(t, int64(6), totalGPU, "the full GPU request is allocated across the mask")
	})

	t.Run("reject: 4-GPU + 1-CPU footgun", func(t *testing.T) {
		node := twoZoneNode(policyValueRestricted, req(gpu, "2", "cpu", "100"))
		_, admit := restrictedEvaluator{}.evaluate(node, []resourceAmounts{req(gpu, "4", "cpu", "1")})
		assert.False(t, admit, "GPU needs 2 nodes, CPU needs 1")
	})

	t.Run("admit on a single zone when width is 1", func(t *testing.T) {
		node := twoZoneNode(policyValueRestricted, req(gpu, "4", "cpu", "16"))
		allocation, admit := restrictedEvaluator{}.evaluate(node, []resourceAmounts{req(gpu, "2", "cpu", "8")})
		assert.True(t, admit)
		assert.Len(t, allocation, 1, "width 1 stays on one zone")
		assert.Contains(t, allocation, 0)
	})
}

func TestRestrictedSelectsLowestMask(t *testing.T) {
	// Three zones; a width-2 request should select {0,1}, the lowest satisfying mask.
	node := buildNodeTopology(
		nrtWithAttributes(policyValueRestricted, scopeValueContainer,
			numaNodeZone("node-0", map[string]string{gpu: "2"}),
			numaNodeZone("node-1", map[string]string{gpu: "2"}),
			numaNodeZone("node-2", map[string]string{gpu: "2"}),
		),
		sets.New[v1.ResourceName](),
	)
	allocation, admit := restrictedEvaluator{}.evaluate(node, []resourceAmounts{req(gpu, "4")})
	assert.True(t, admit)
	assert.Len(t, allocation, 2)
	assert.Contains(t, allocation, 0)
	assert.Contains(t, allocation, 1)
	assert.NotContains(t, allocation, 2)
}

func TestEvaluatorFor(t *testing.T) {
	assert.IsType(t, singleNUMAEvaluator{}, evaluatorFor(policySingleNUMANode))
	assert.IsType(t, restrictedEvaluator{}, evaluatorFor(policyRestricted))
	assert.Nil(t, evaluatorFor(policyBestEffort))
	assert.Nil(t, evaluatorFor(policyNone))
}
