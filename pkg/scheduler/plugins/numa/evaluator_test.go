// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

const gpu = "nvidia.com/gpu"

func applyTestMemoryOwner(state *node_info.MemoryGroupState, topology *node_info.NumaTopology, owner types.UID, placements []pod_info.MemoryGroupPlacement) error {
	if err := state.ValidateOwner(topology, owner, placements); err != nil {
		return err
	}
	state.ApplyOwner(owner, placements)
	return nil
}

// noIgnoreList is the empty ignoreList passed in tests that do not exercise it.
var noIgnoreList = sets.New[v1.ResourceName]()

// evalPlacement runs the solver on requests (as the concurrent set) against topo and returns the
// resulting placement and admit decision — the form the worked-example assertions read.
func evalPlacement(topo *node_info.NumaTopology, ignore sets.Set[v1.ResourceName], requests []v1.ResourceList) (pod_info.NUMAPlacement, bool) {
	plugin, _, node := wiredPlugin(topo)
	plugin.ignoreList = ignore
	placement, err := plugin.evaluate(restrictedPod("placement", requests), node)
	return placement, err == nil
}

// amountAt returns the amounts placed on a given zone index, or nil if the zone is not in the placement.
func amountAt(p pod_info.NUMAPlacement, zoneIndex int) v1.ResourceList {
	for _, zp := range p.Zones {
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

func knownMemoryNode(policy node_info.TopologyManagerPolicy, scope node_info.TopologyManagerScope) (*numaPlugin, *node_info.NodeInfo) {
	topology := twoZoneNode(policy, req("memory", "100Gi"))
	topology.Scope = scope
	topology.MemoryGroups = &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown, Groups: map[pod_info.NUMAMask]*node_info.MemoryGroup{}}
	plugin, _, node := wiredPlugin(topology)
	return plugin, node
}

func TestMemorySolverDelayedBinding(t *testing.T) {
	plugin, node := knownMemoryNode(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer)
	first := makeGuaranteedTask("first", map[string]string{"memory": "120Gi"})
	placement, err := plugin.evaluate(first, node)
	require.NoError(t, err)
	require.Empty(t, placement.Zones)
	require.Len(t, placement.MemoryGroups, 1)
	require.Equal(t, []int{0, 1}, placement.MemoryGroups[0].Mask.Indices())
	require.NoError(t, applyTestMemoryOwner(node.NumaTopology.MemoryGroups, node.NumaTopology, "first", placement.MemoryGroups))
	_, err = plugin.evaluate(makeGuaranteedTask("second", map[string]string{"memory": "40Gi"}), node)
	require.Error(t, err)
	node.NumaTopology.MemoryGroups.RemoveOwner("first")
	_, err = plugin.evaluate(makeGuaranteedTask("second", map[string]string{"memory": "40Gi"}), node)
	require.NoError(t, err)
}

func TestMemorySolverGroupCapacityAndPolicy(t *testing.T) {
	for _, policy := range []node_info.TopologyManagerPolicy{node_info.TopologyPolicyBestEffort, node_info.TopologyPolicyRestricted, node_info.TopologyPolicySingleNUMANode} {
		policyNames := []string{"none", "best-effort", "restricted", "single-numa-node"}
		t.Run(policyNames[policy], func(t *testing.T) {
			plugin, node := knownMemoryNode(policy, node_info.TopologyScopeContainer)
			mask, _ := pod_info.NewNUMAMask([]int{0, 1})
			require.NoError(t, applyTestMemoryOwner(node.NumaTopology.MemoryGroups, node.NumaTopology, "existing", []pod_info.MemoryGroupPlacement{{Mask: mask, Amount: req("memory", "120Gi")}}))
			placement, err := plugin.evaluate(makeGuaranteedTask("small", map[string]string{"memory": "40Gi"}), node)
			if policy == node_info.TopologyPolicyRestricted || policy == node_info.TopologyPolicySingleNUMANode {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, mask, placement.MemoryGroups[0].Mask)
			_, err = plugin.evaluate(makeGuaranteedTask("large", map[string]string{"memory": "90Gi"}), node)
			require.Error(t, err)
		})
	}
}

func TestMemorySolverFitReasons(t *testing.T) {
	plugin, node := knownMemoryNode(node_info.TopologyPolicyBestEffort, node_info.TopologyScopeContainer)
	singleton, _ := pod_info.NewNUMAMask([]int{0})
	require.NoError(t, applyTestMemoryOwner(node.NumaTopology.MemoryGroups, node.NumaTopology, "pinned", []pod_info.MemoryGroupPlacement{{Mask: singleton, Amount: req("memory", "1Gi")}}))
	_, err := plugin.evaluate(makeGuaranteedTask("wide", map[string]string{"memory": "120Gi"}), node)
	require.ErrorIs(t, err, errMemoryGroupConflict)
	node.NumaTopology.MemoryGroups.RemoveOwner("pinned")
	mask, _ := pod_info.NewNUMAMask([]int{0, 1})
	require.NoError(t, applyTestMemoryOwner(node.NumaTopology.MemoryGroups, node.NumaTopology, "wide", []pod_info.MemoryGroupPlacement{{Mask: mask, Amount: req("memory", "120Gi")}}))
	_, err = plugin.evaluate(makeGuaranteedTask("full", map[string]string{"memory": "90Gi"}), node)
	require.ErrorIs(t, err, errMemoryGroupCapacity)
}

func TestMemorySolverMergedAndFinalMasksDiffer(t *testing.T) {
	topology := numaTopology(node_info.TopologyPolicyBestEffort, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{"memory": "100Gi", gpu: "1"}),
		numaZone("node-1", map[string]string{"memory": "100Gi", gpu: "1"}))
	topology.MemoryGroups = &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown}
	mask, _ := pod_info.NewNUMAMask([]int{0, 1})
	require.NoError(t, applyTestMemoryOwner(topology.MemoryGroups, topology, "pinned", []pod_info.MemoryGroupPlacement{{Mask: mask, Amount: req("memory", "120Gi")}}))
	plugin, _, node := wiredPlugin(topology)
	placement, err := plugin.evaluate(makeGuaranteedTask("mixed", map[string]string{"memory": "40Gi", gpu: "1"}), node)
	require.NoError(t, err)
	require.Len(t, placement.Zones, 1)
	require.Equal(t, 0, placement.Zones[0].ZoneIndex)
	require.Equal(t, mask, placement.MemoryGroups[0].Mask)
}

func TestMemorySolverScopes(t *testing.T) {
	for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
		plugin, node := knownMemoryNode(node_info.TopologyPolicyRestricted, scope)
		task := makeGuaranteedTask("mixed", map[string]string{"memory": "40Gi"})
		task.Pod.Spec.Containers = append(task.Pod.Spec.Containers, v1.Container{Resources: v1.ResourceRequirements{Requests: req("memory", "120Gi")}})
		placement, err := plugin.evaluate(task, node)
		if scope == node_info.TopologyScopeContainer {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Len(t, placement.MemoryGroups, 1)
		require.Equal(t, []int{0, 1}, placement.MemoryGroups[0].Mask.Indices())
		amount := placement.MemoryGroups[0].Amount[v1.ResourceMemory]
		require.Equal(t, req("memory", "160Gi")[v1.ResourceMemory], amount)
	}
}

func TestMemorySolverIgnoresOrdinaryInitMemory(t *testing.T) {
	for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			plugin, node := knownMemoryNode(node_info.TopologyPolicyRestricted, scope)
			task := restrictedPod("init-and-sidecar", []v1.ResourceList{req("memory", "40Gi")},
				initReq{req: req("memory", "250Gi")},
				initReq{req: req("memory", "20Gi"), restartable: true})
			placement, err := plugin.evaluate(task, node)
			require.NoError(t, err)
			require.Empty(t, placement.Zones)
			require.Len(t, placement.MemoryGroups, 1)
			require.Equal(t, pod_info.NUMAMask(1), placement.MemoryGroups[0].Mask)
			amount := placement.MemoryGroups[0].Amount[v1.ResourceMemory]
			expected := resource.MustParse("60Gi")
			require.Zero(t, expected.Cmp(amount))
			require.Empty(t, node.NumaTopology.MemoryGroups.Groups)
			task.NodeName = node.Name
			task.NUMAPlacement = placement
			plugin.allocate(&framework.Event{Task: task})
			second, err := plugin.evaluate(makeGuaranteedTask("second", map[string]string{"memory": "40Gi"}), node)
			require.NoError(t, err)
			require.Equal(t, pod_info.NUMAMask(1), second.MemoryGroups[0].Mask)
		})
	}
}

func TestOrdinaryInitCPUAndDevicesStillConstrainAdmission(t *testing.T) {
	for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
		for _, name := range []v1.ResourceName{v1.ResourceCPU, gpu} {
			t.Run(fmt.Sprintf("%v/%s", scope, name), func(t *testing.T) {
				topology := numaTopology(node_info.TopologyPolicySingleNUMANode, scope,
					numaZone("node-0", map[string]string{"memory": "100Gi", "cpu": "4", gpu: "4"}),
					numaZone("node-1", map[string]string{"memory": "100Gi", "cpu": "4", gpu: "4"}))
				topology.MemoryGroups = &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown}
				plugin, _, node := wiredPlugin(topology)
				task := restrictedPod("init-resource", []v1.ResourceList{req("memory", "40Gi")},
					initReq{req: req(string(name), "5", "memory", "250Gi")})
				_, err := plugin.evaluate(task, node)
				require.ErrorIs(t, err, errNotNumaAligned)
			})
		}
	}
}

func TestMemorySolverMemoryHintsJointResources(t *testing.T) {
	topology := numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{"memory": "100Gi", "hugepages-2Mi": "2Gi"}),
		numaZone("node-1", map[string]string{"memory": "100Gi", "hugepages-2Mi": "2Gi"}))
	topology.MemoryGroups = &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown}
	plugin, _, node := wiredPlugin(topology)
	placement, err := plugin.evaluate(makeGuaranteedTask("joint", map[string]string{"memory": "40Gi", "hugepages-2Mi": "3Gi"}), node)
	require.NoError(t, err)
	require.Equal(t, []int{0, 1}, placement.MemoryGroups[0].Mask.Indices())
}

func TestUnifiedSolverUnknownMemoryState(t *testing.T) {
	policies := []node_info.TopologyManagerPolicy{node_info.TopologyPolicyBestEffort, node_info.TopologyPolicyRestricted, node_info.TopologyPolicySingleNUMANode}
	for _, policy := range policies {
		for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
			for _, missing := range []bool{false, true} {
				t.Run(fmt.Sprintf("policy-%d/scope-%d/missing-%t", policy, scope, missing), func(t *testing.T) {
					plugin, node := knownMemoryNode(policy, scope)
					if missing {
						node.NumaTopology.MemoryGroups = nil
					} else {
						node.NumaTopology.MemoryGroups.Status = node_info.MemoryGroupsUnknown
					}
					task := makeGuaranteedTask("unknown", map[string]string{"memory": "40Gi"})
					_, err := plugin.evaluate(task, node)
					require.ErrorIs(t, err, errMemoryStateUnknown)
					require.ErrorIs(t, plugin.predicate(task, nil, node), errMemoryStateUnknown)
					_, err = plugin.placement(task, node)
					require.ErrorIs(t, err, errMemoryStateUnknown)
				})
			}
		}
	}
}

func TestUnifiedSolverUnknownMemoryDoesNotBlockUnmanagedRequests(t *testing.T) {
	for _, policy := range []node_info.TopologyManagerPolicy{node_info.TopologyPolicyNone, node_info.TopologyPolicyBestEffort, node_info.TopologyPolicyRestricted, node_info.TopologyPolicySingleNUMANode} {
		for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
			t.Run(fmt.Sprintf("policy-%d/scope-%d", policy, scope), func(t *testing.T) {
				topology := numaTopology(policy, scope,
					numaZone("node-0", map[string]string{"cpu": "4", "memory": "100Gi", gpu: "1"}),
					numaZone("node-1", map[string]string{"cpu": "4", "memory": "100Gi", gpu: "1"}))
				plugin, _, node := wiredPlugin(topology)
				for _, task := range []*pod_info.PodInfo{
					makeGuaranteedTask("cpu", map[string]string{"cpu": "2"}),
					makeGuaranteedTask("device", map[string]string{gpu: "1"}),
					makeBurstableTask(req(gpu, "1", "cpu", "8", "memory", "200Gi")),
				} {
					placement, err := plugin.evaluate(task, node)
					require.NoError(t, err)
					require.Empty(t, placement.MemoryGroups)
					require.NoError(t, plugin.predicate(task, nil, node))
				}
			})
		}
	}
}

func TestUnifiedSolverPodOverheadDoesNotRequireMemoryGroups(t *testing.T) {
	for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
		for _, policy := range []node_info.TopologyManagerPolicy{node_info.TopologyPolicyNone, node_info.TopologyPolicyBestEffort, node_info.TopologyPolicyRestricted, node_info.TopologyPolicySingleNUMANode} {
			t.Run(fmt.Sprintf("policy-%d/scope-%d", policy, scope), func(t *testing.T) {
				topology := numaTopology(policy, scope,
					numaZone("node-0", map[string]string{"cpu": "4", "memory": "100Gi"}),
					numaZone("node-1", map[string]string{"cpu": "4", "memory": "100Gi"}))
				plugin, _, node := wiredPlugin(topology)
				task := makeGuaranteedTask("overhead", map[string]string{"cpu": "2"})
				task.Pod.Spec.Overhead = req("memory", "10Gi")
				placement, err := plugin.evaluate(task, node)
				require.NoError(t, err)
				require.Empty(t, placement.MemoryGroups)
			})
		}
	}
}

func BenchmarkUnifiedSolverCPUDevices(b *testing.B) {
	for _, policy := range []node_info.TopologyManagerPolicy{node_info.TopologyPolicyRestricted, node_info.TopologyPolicyBestEffort} {
		for _, zoneCount := range []int{2, 4, 8} {
			b.Run(fmt.Sprintf("policy-%d/zones-%d", policy, zoneCount), func(b *testing.B) {
				zones := make([]node_info.NumaZoneSpec, zoneCount)
				for index := range zones {
					zones[index] = numaZone(fmt.Sprintf("node-%d", index), map[string]string{"cpu": "8", gpu: "4"})
				}
				plugin, _, node := wiredPlugin(numaTopology(policy, node_info.TopologyScopeContainer, zones...))
				task := makeGuaranteedTask("bench", map[string]string{"cpu": "4", gpu: "2"})
				b.ReportAllocs()
				b.ResetTimer()
				for iteration := 0; iteration < b.N; iteration++ {
					if _, err := plugin.evaluate(task, node); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestEvaluatePreservesInputState(t *testing.T) {
	for _, failing := range []bool{false, true} {
		name := "success"
		if failing {
			name = "failure after tentative allocation"
		}
		t.Run(name, func(t *testing.T) {
			topology := numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer,
				numaZone("node-0", map[string]string{"memory": "100Gi", gpu: "1"}),
				numaZone("node-1", map[string]string{"memory": "100Gi", gpu: "1"}))
			plugin, _, node := wiredPlugin(topology)
			topology.MemoryGroups = &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown}
			require.NoError(t, applyTestMemoryOwner(topology.MemoryGroups, topology, "existing", []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: req("memory", "10Gi")}}))
			task := makeGuaranteedTask("candidate", map[string]string{"memory": "20Gi", gpu: "1"})
			if failing {
				task.Pod.Spec.Containers = append(task.Pod.Spec.Containers, v1.Container{Resources: v1.ResourceRequirements{Requests: req("memory", "120Gi")}})
			}
			task.NUMAPlacement = pod_info.NUMAPlacement{
				Zones:        []pod_info.ZonePlacement{{ZoneIndex: 1, Amount: req(gpu, "1")}},
				MemoryGroups: []pod_info.MemoryGroupPlacement{{Mask: 2, Amount: req("memory", "5Gi")}},
			}
			beforeTopology := topology.Clone()
			beforePlacement := task.NUMAPlacement.Clone()
			beforePod := task.Pod.DeepCopy()
			placement, err := plugin.evaluate(task, node)
			if failing {
				require.Error(t, err)
				require.True(t, placement.IsEmpty())
			} else {
				require.NoError(t, err)
				require.False(t, placement.IsEmpty())
			}
			require.Equal(t, beforeTopology, topology)
			require.Equal(t, beforePlacement, task.NUMAPlacement)
			require.Equal(t, beforePod, task.Pod)
		})
	}
}

func TestEvaluateValidationPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		status       node_info.MemoryGroupStatus
		expected     error
		errorMessage string
	}{
		{name: "unknown before zone limit", expected: errMemoryStateUnknown},
		{name: "known enforces zone limit", status: node_info.MemoryGroupsKnown, errorMessage: "NUMA placement supports at most 8 NUMA zones"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plugin, node := knownMemoryNode(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer)
			topology := node.NumaTopology
			for len(topology.Zones) <= pod_info.MaxNUMAZones {
				topology.Zones = append(topology.Zones, topology.Zones[0])
			}
			topology.MemoryGroups.Status = test.status
			_, err := plugin.evaluate(makeGuaranteedTask("candidate", map[string]string{"memory": "1Gi"}), node)
			if test.expected != nil {
				require.ErrorIs(t, err, test.expected)
				return
			}
			require.EqualError(t, err, test.errorMessage)
		})
	}
}

func TestEvaluateCanonicalLongRunningMemoryGroups(t *testing.T) {
	plugin, node := knownMemoryNode(node_info.TopologyPolicyBestEffort, node_info.TopologyScopeContainer)
	task := restrictedPod("init-and-sidecar", []v1.ResourceList{req("memory", "60Gi"), req("memory", "60Gi")},
		initReq{req: req("memory", "20Gi")},
		initReq{req: req("memory", "10Gi"), restartable: true})
	placement, err := plugin.evaluate(task, node)
	require.NoError(t, err)
	require.Empty(t, placement.Zones)
	require.Len(t, placement.MemoryGroups, 2)
	require.Equal(t, pod_info.NUMAMask(1), placement.MemoryGroups[0].Mask)
	require.Equal(t, pod_info.NUMAMask(2), placement.MemoryGroups[1].Mask)
	for index, expected := range []string{"70Gi", "60Gi"} {
		amount := placement.MemoryGroups[index].Amount[v1.ResourceMemory]
		quantity := req("memory", expected)[v1.ResourceMemory]
		require.Zero(t, quantity.Cmp(amount))
	}
}

func TestIgnoredMemoryRequiresObservedManagerEvidence(t *testing.T) {
	for _, observed := range []bool{false, true} {
		plugin, session, node := memoryTestPlugin()
		plugin.ignoreList = sets.New(v1.ResourceMemory)
		owner := memoryTestTask("external-owner", "120Gi")
		annotation := commonconstants.NumaMemoryGroupsPredicted
		if observed {
			annotation = commonconstants.NumaMemoryGroupsObserved
		}
		owner.Pod.Annotations = map[string]string{annotation: memoryAnnotation(t, memoryRecord("120Gi", "node-0", "node-1"))}
		node.PodInfos = pod_info.PodsMap{owner.UID: owner}
		plugin.seedMemoryGroups(session)
		incoming := memoryTestTask("incoming", "40Gi")
		if observed {
			require.Error(t, plugin.predicate(incoming, nil, node))
			continue
		}
		require.NoError(t, plugin.predicate(incoming, nil, node))
	}
}

func TestMemoryLedgerIndependentOfZoneReconstruction(t *testing.T) {
	plugin, session, node := memoryTestPlugin()
	plugin.reconstructAvailable = false
	owner := memoryTestTask("external-owner", "120Gi")
	owner.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsObserved: memoryAnnotation(t, memoryRecord("120Gi", "node-0", "node-1"))}
	node.PodInfos = pod_info.PodsMap{owner.UID: owner}
	plugin.seedMemoryGroups(session)
	require.Equal(t, int64(80<<30), node.NumaTopology.MemoryGroups.Available(node.NumaTopology, 3, v1.ResourceMemory))
	require.Error(t, plugin.predicate(memoryTestTask("small", "40Gi"), nil, node))
	node.NumaTopology.Policy = node_info.TopologyPolicyBestEffort
	require.NoError(t, plugin.predicate(memoryTestTask("small", "40Gi"), nil, node))
	require.Error(t, plugin.predicate(memoryTestTask("large", "90Gi"), nil, node))
}

func TestExternalPodMemoryConflictsMakeNodeUnknown(t *testing.T) {
	for _, record := range []string{
		memoryAnnotation(t, memoryRecord("20Gi", "node-0")),
		memoryAnnotation(t, memoryRecord("90Gi", "node-0", "node-1")),
	} {
		plugin, session, node := memoryTestPlugin()
		first := memoryTestTask("external-first", "120Gi")
		first.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsObserved: memoryAnnotation(t, memoryRecord("120Gi", "node-0", "node-1"))}
		second := memoryTestTask("external-second", "20Gi")
		second.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsObserved: record}
		node.PodInfos = pod_info.PodsMap{first.UID: first, second.UID: second}
		plugin.seedMemoryGroups(session)
		require.Equal(t, node_info.MemoryGroupsUnknown, node.NumaTopology.MemoryGroups.Status)
		require.Empty(t, node.NumaTopology.MemoryGroups.Groups)
	}
}

func TestNonePolicySkipsEvaluation(t *testing.T) {
	for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
		for _, condition := range []string{"known", "unknown", "missing", "conflict", "exhausted", "too-many-zones"} {
			t.Run(fmt.Sprintf("scope-%d/%s", scope, condition), func(t *testing.T) {
				plugin, node := knownMemoryNode(node_info.TopologyPolicyNone, scope)
				topology := node.NumaTopology
				switch condition {
				case "unknown":
					topology.MemoryGroups.Status = node_info.MemoryGroupsUnknown
				case "missing":
					topology.MemoryGroups = nil
				case "conflict":
					require.NoError(t, applyTestMemoryOwner(topology.MemoryGroups, topology, "existing", []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: req("memory", "1Gi")}}))
				case "exhausted":
					require.NoError(t, applyTestMemoryOwner(topology.MemoryGroups, topology, "existing", []pod_info.MemoryGroupPlacement{{Mask: 3, Amount: req("memory", "200Gi")}}))
				case "too-many-zones":
					for len(topology.Zones) < 65 {
						topology.Zones = append(topology.Zones, topology.Zones[0])
					}
				}
				task := makeGuaranteedTask("candidate", map[string]string{"memory": "120Gi", "cpu": "100", gpu: "100"})
				task.NUMAPlacement = pod_info.NUMAPlacement{MemoryGroups: []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: req("memory", "1Gi")}}}
				beforeTopology := topology.Clone()
				beforePlacement := task.NUMAPlacement.Clone()
				beforePod := task.Pod.DeepCopy()
				placement, err := plugin.evaluate(task, node)
				require.NoError(t, err)
				require.True(t, placement.IsEmpty())
				require.NoError(t, plugin.predicate(task, nil, node))
				placement, err = plugin.placement(task, node)
				require.NoError(t, err)
				require.True(t, placement.IsEmpty())
				require.Equal(t, beforeTopology, topology)
				require.Equal(t, beforePlacement, task.NUMAPlacement)
				require.Equal(t, beforePod, task.Pod)
			})
		}
	}
}

func TestNonePolicySkipsAccounting(t *testing.T) {
	for _, scope := range []node_info.TopologyManagerScope{node_info.TopologyScopeContainer, node_info.TopologyScopePod} {
		t.Run(fmt.Sprintf("scope-%d", scope), func(t *testing.T) {
			plugin, node := knownMemoryNode(node_info.TopologyPolicyNone, scope)
			task := memoryTestTask("owner", "40Gi")
			task.NodeName = node.Name
			task.NUMAPlacement = pod_info.NUMAPlacement{
				Zones:        []pod_info.ZonePlacement{{ZoneIndex: 0, Amount: req("memory", "40Gi")}},
				MemoryGroups: []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: req("memory", "40Gi")}},
			}
			before := node.NumaTopology.Clone()
			plugin.allocate(&framework.Event{Task: task})
			require.Equal(t, before, node.NumaTopology)
			require.NoError(t, applyTestMemoryOwner(node.NumaTopology.MemoryGroups, node.NumaTopology, memoryOwner(task), task.NUMAPlacement.MemoryGroups))
			before = node.NumaTopology.Clone()
			plugin.deallocate(&framework.Event{Task: task})
			require.Equal(t, before, node.NumaTopology)
		})
	}
}

func TestNonePolicySkipsMemorySeedingAndCaches(t *testing.T) {
	topology := twoZoneNode(node_info.TopologyPolicyNone, req("memory", "100Gi", gpu, "1"))
	topology.MemoryGroups = &node_info.MemoryGroupState{Status: node_info.MemoryGroupsUnknown}
	plugin, session, node := wiredPlugin(topology)
	session.ClusterInfo.ResourceVectorMap = topology.VectorMap
	owner := memoryTestTask("owner", "120Gi")
	owner.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsObserved: memoryAnnotation(t, memoryRecord("120Gi", "node-0", "node-1"))}
	node.PodInfos = pod_info.PodsMap{owner.UID: owner}
	before := node.NumaTopology.Clone()
	plugin.seedMemoryGroups(session)
	require.Equal(t, before, node.NumaTopology)
	require.Empty(t, plugin.observedMemoryByNode)
	plugin.initCaches(session)
	require.False(t, plugin.hasScoredNodes)
	require.Empty(t, plugin.awareDeviceIndices)
	plugin.ignoreList.Insert(v1.ResourceMemory)
	plugin.initCaches(session)
	require.Empty(t, plugin.effectiveAwareByNode)
	task := makeGuaranteedTask("candidate", map[string]string{"memory": "120Gi"})
	require.NoError(t, plugin.prePredicate(task, nil))
	require.Empty(t, plugin.numaRequestCache)
	require.NoError(t, plugin.nodePreOrder(task, []*node_info.NodeInfo{node}))
	require.Empty(t, plugin.numaRequestCache)
	span, ok := plugin.assumedSpan(task, node)
	require.True(t, ok)
	require.Equal(t, 2, span)
	score, err := plugin.nodeScore(task, node)
	require.NoError(t, err)
	require.Equal(t, float64(scores.Numa)/2, score)
}
