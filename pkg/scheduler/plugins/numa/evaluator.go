// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	commonpod "github.com/kai-scheduler/KAI-scheduler/pkg/common/pod"
	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

type evaluationState struct {
	topology        *node_info.NumaTopology
	zoneResources   []int
	consumed        []float64
	zoneAllocations zoneAllocation
	memory          *memorySolver
}

func (pp *numaPlugin) evaluate(task *pod_info.PodInfo, node *node_info.NodeInfo) (pod_info.NUMAPlacement, error) {
	state, err := pp.prepareEvaluation(task, node)
	if err != nil || state == nil {
		return pod_info.NUMAPlacement{}, err
	}
	requests := pp.numaRequestsFor(task, state.topology.VectorMap)
	podHint, err := state.sharedPodHint(requests.podScope)
	if err != nil {
		return pod_info.NUMAPlacement{}, err
	}
	for sequence, unit := range requests.units {
		if err := state.allocateUnit(sequence, unit, podHint); err != nil {
			return pod_info.NUMAPlacement{}, err
		}
	}
	return state.persistentPlacement(), nil
}

func (pp *numaPlugin) prepareEvaluation(task *pod_info.PodInfo, node *node_info.NodeInfo) (*evaluationState, error) {
	if node == nil || node.NumaTopology == nil || len(node.NumaTopology.Zones) == 0 || task.Pod == nil {
		return nil, nil
	}
	topo := node.NumaTopology
	if topo.Policy == node_info.TopologyPolicyNone {
		return nil, nil
	}
	managed := pp.evaluationMemoryResources(task, node)
	hasMemory := len(managed) > 0
	if !hasMemory && !pp.shouldScore(task, topo) {
		return nil, nil
	}
	if err := validateEvaluationTopology(topo, hasMemory); err != nil {
		return nil, err
	}
	state := &evaluationState{
		topology:        topo,
		zoneResources:   make([]int, 0, len(topo.AwareIndices)),
		consumed:        make([]float64, len(topo.Zones)*topo.VectorMap.Len()),
		zoneAllocations: zoneAllocation{},
	}
	if hasMemory {
		state.memory = newMemorySolver(task, topo, managed)
	}
	for _, index := range pp.alignedAware(task, node) {
		if !commonresources.IsMemoryResource(topo.AwareNames[index]) {
			state.zoneResources = append(state.zoneResources, index)
		}
	}
	return state, nil
}

func (pp *numaPlugin) evaluationMemoryResources(task *pod_info.PodInfo, node *node_info.NodeInfo) sets.Set[v1.ResourceName] {
	if !commonpod.IsGuaranteed(task.Pod) {
		return nil
	}
	managed := pp.managedMemoryResources(node)
	if !requestsMemory(task.Pod, managed) {
		return nil
	}
	return managed
}

func validateEvaluationTopology(topo *node_info.NumaTopology, hasMemory bool) error {
	if hasMemory {
		state := topo.MemoryGroups
		if state == nil || state.Status != node_info.MemoryGroupsKnown {
			return errMemoryStateUnknown
		}
	}
	if len(topo.Zones) > pod_info.MaxNUMAZones {
		return fmt.Errorf("NUMA placement supports at most %d NUMA zones", pod_info.MaxNUMAZones)
	}
	return nil
}

func (state *evaluationState) sharedPodHint(request resource_info.ResourceVector) (*topologymanager.TopologyHint, error) {
	if state.topology.Scope != node_info.TopologyScopePod {
		return nil, nil
	}
	hint, err := state.mergedHint(request)
	if err != nil {
		return nil, err
	}
	return &hint, nil
}

func (state *evaluationState) allocateUnit(sequence int, unit admissionUnit, podHint *topologymanager.TopologyHint) error {
	hint, err := state.unitHint(unit.request, podHint)
	if err != nil {
		return err
	}
	if state.memory != nil {
		if err := state.memory.allocateUnit(sequence, unit, hint); err != nil {
			return err
		}
	}
	return state.allocateUnitZones(unit, hint.NUMANodeAffinity)
}

func (state *evaluationState) unitHint(request resource_info.ResourceVector, podHint *topologymanager.TopologyHint) (topologymanager.TopologyHint, error) {
	if podHint != nil {
		return *podHint, nil
	}
	return state.mergedHint(request)
}

func (state *evaluationState) allocateUnitZones(unit admissionUnit, affinity bitmask.BitMask) error {
	topo := state.topology
	if affinity == nil {
		affinity, _ = bitmask.NewBitMask(allZoneIndices(topo)...)
	}
	if !maskSatisfiesReq(topo, state.zoneResources, unit.request, state.consumed, topo.VectorMap.Len(), affinity.GetBits()) {
		if topo.Policy != node_info.TopologyPolicyBestEffort {
			return fmt.Errorf("aligned resources do not fit")
		}
		affinity, _ = bitmask.NewBitMask(allZoneIndices(topo)...)
		if !maskSatisfiesReq(topo, state.zoneResources, unit.request, state.consumed, topo.VectorMap.Len(), affinity.GetBits()) {
			return fmt.Errorf("resources do not fit")
		}
	}
	if !unit.ordinaryInit {
		drawAcrossMask(topo, state.zoneResources, affinity.GetBits(), unit.request, state.consumed, state.zoneAllocations, topo.VectorMap.Len())
	}
	return nil
}

func (state *evaluationState) persistentPlacement() pod_info.NUMAPlacement {
	result := placementFromAllocation(state.zoneAllocations, state.topology)
	if state.memory != nil {
		result.MemoryGroups = state.memory.persistentGroups()
	}
	return result
}

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
	if commonpod.IsGuaranteed(task.Pod) {
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

	placement := pod_info.NUMAPlacement{Zones: make([]pod_info.ZonePlacement, 0, len(indices))}
	for _, idx := range indices {
		placement.Zones = append(placement.Zones, pod_info.ZonePlacement{
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

func allZoneIndices(topo *node_info.NumaTopology) []int {
	indices := make([]int, len(topo.Zones))
	for index := range indices {
		indices[index] = index
	}
	return indices
}

func enumerateNUMAMasks(topo *node_info.NumaTopology, yield func(pod_info.NUMAMask)) {
	for width := 1; width <= len(topo.Zones); width++ {
		combinations(len(topo.Zones), width, func(indices []int) bool { mask, _ := pod_info.NewNUMAMask(indices); yield(mask); return true })
	}
}

func (state *evaluationState) mergedHint(request resource_info.ResourceVector) (topologymanager.TopologyHint, error) {
	providers, err := state.hintProviders(request, true)
	if err != nil {
		return topologymanager.TopologyHint{}, err
	}
	hint, err := mergeTopologyHints(state.topology, providers)
	if err != nil || hint.Preferred || state.topology.Policy != node_info.TopologyPolicyBestEffort {
		return hint, err
	}
	providers, err = state.hintProviders(request, false)
	if err != nil {
		return topologymanager.TopologyHint{}, err
	}
	return mergeTopologyHints(state.topology, providers)
}

func (state *evaluationState) hintProviders(request resource_info.ResourceVector, preferredOnly bool) ([]map[string][]topologymanager.TopologyHint, error) {
	topo := state.topology
	var providers []map[string][]topologymanager.TopologyHint
	for _, index := range state.zoneResources {
		if request.Get(index) <= 0 {
			continue
		}
		hints := state.zoneResourceHints(request, index, preferredOnly)
		providers = append(providers, map[string][]topologymanager.TopologyHint{string(topo.AwareNames[index]): hints})
	}
	if state.memory == nil {
		return providers, nil
	}
	hints, err := state.memory.providerHints(request, preferredOnly)
	if err != nil {
		return nil, err
	}
	if hints != nil {
		providers = append(providers, map[string][]topologymanager.TopologyHint{string(v1.ResourceMemory): hints})
	}
	return providers, nil
}

func (state *evaluationState) zoneResourceHints(request resource_info.ResourceVector, index int, preferredOnly bool) []topologymanager.TopologyHint {
	topo := state.topology
	preferredWidth, _ := minWidthFromPrefix(topo.AllocatablePrefix[index], request.Get(index))
	var hints []topologymanager.TopologyHint
	appendHint := func(indices []int) bool {
		if !maskSatisfiesReq(topo, []int{index}, request, state.consumed, topo.VectorMap.Len(), indices) {
			return true
		}
		affinity, _ := bitmask.NewBitMask(indices...)
		hints = append(hints, topologymanager.TopologyHint{NUMANodeAffinity: affinity, Preferred: len(indices) == preferredWidth})
		return true
	}
	if preferredOnly {
		if topo.Policy != node_info.TopologyPolicySingleNUMANode || preferredWidth == 1 {
			combinations(len(topo.Zones), preferredWidth, appendHint)
		}
	} else {
		enumerateNUMAMasks(topo, func(mask pod_info.NUMAMask) {
			appendHint(mask.Indices())
		})
	}
	if len(hints) == 0 {
		return []topologymanager.TopologyHint{{Preferred: false}}
	}
	return hints
}
