// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storagecapacity_info"
)

// sharedComputeNode is sharedGpuComputeTestNode with room for more than a
// handful of pods, so a device can be filled share by share.
func sharedComputeNode(t *testing.T, gpuMemoryMiB int64, maxPods int64) (
	*NodeInfo, *resource_info.ResourceVectorMap, *pod_affinity.MockNodePodAffinityInfo,
) {
	t.Helper()

	vectorMap := resource_info.NewResourceVectorMap()
	nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(NewController(t))
	nodeResources := resource_info.NewResource(0, 0, 1)
	nodeResources.ScalarResources()[resource_info.PodsResourceName] = maxPods

	return &NodeInfo{
		Name:                        "node1",
		Node:                        common_info.BuildNode("node1", common_info.BuildResourceListWithGPU("8000m", "10G", "1")),
		VectorMap:                   vectorMap,
		PodInfos:                    map[common_info.PodID]*pod_info.PodInfo{},
		LegacyMIGTasks:              map[common_info.PodID]string{},
		MemoryOfEveryGpuOnNode:      gpuMemoryMiB,
		ComputeOfEveryGpuOnNode:     WholeGpuCompute,
		GpuMemorySynced:             true,
		GpuSharingNodeInfo:          *newGpuSharingNodeInfo(),
		AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		AllocatableVector:           nodeResources.ToVector(vectorMap),
		IdleVector:                  nodeResources.ToVector(vectorMap),
		UsedVector:                  resource_info.NewResourceVector(vectorMap),
		ReleasingVector:             resource_info.NewResourceVector(vectorMap),
		PodAffinityInfo:             nodePodAffinityInfo,
	}, vectorMap, nodePodAffinityInfo
}

// nEqualSharesFitOnCompute places `shares` pods that each ask for exactly
// 1/shares of the device's memory, and asserts the last one still fits. Anything
// but a floor when deriving compute from memory makes the shares sum past a
// whole GPU and rejects a placement that memory allows - a capacity regression
// against what schedules today.
func nEqualSharesFitOnCompute(t *testing.T, gpuMemoryMiB int64, shares int) {
	t.Helper()

	const gpuGroup = "gpu-group-0"
	shareMiB := gpuMemoryMiB / int64(shares)
	node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, gpuMemoryMiB, int64(shares)+1)

	shareAnnotations := map[string]string{
		resources.CalcGpuFractionAnnotationForContainer("c1"): fmt.Sprintf("%dMi", shareMiB),
	}

	for i := 0; i < shares-1; i++ {
		pod := sharedGpuComputeTestPod(fmt.Sprintf("share-%d", i), gpuGroup, v1.PodRunning, shareAnnotations, vectorMap)
		nodePodAffinityInfo.EXPECT().AddPod(pod.Pod).Times(1)
		assert.NoError(t, node.AddTask(pod))
	}

	last := sharedGpuComputeTestPod("share-last", "", v1.PodPending, shareAnnotations, vectorMap)
	assert.True(t, node.enoughMemoryOnGpu(&last.GpuRequirement, gpuGroup),
		"precondition: the last share must fit the device's memory")
	assert.True(t, node.IsTaskFitOnGpuGroup(&last.GpuRequirement, gpuGroup),
		"1/%d of a GPU must fit on a device already holding %d other %ds "+
			"(compute ledger holds %d of %d)",
		shares, shares-1, shares, node.UsedSharedGPUsCompute[gpuGroup], node.ComputeOfEveryGpuOnNode)
	assert.True(t, node.EnoughIdleResourcesOnGpu(&last.GpuRequirement, gpuGroup))
	assert.Empty(t, node.gpuComputeFitErrorReason(last))
}

// An 80GiB device split into eight 10GiB shares: the shares sum to exactly the
// device, so all eight must fit on compute as well as on memory.
func TestNodeInfo_EqualSharesFitOnCompute(t *testing.T) {
	// Memory sizes that really appear on nvidia.com/gpu.memory, divided into
	// shares that do and do not land on a whole percent of the device.
	for _, tt := range []struct {
		gpuMemoryMiB int64
		shares       int
	}{
		{81920, 8},  // 12.5% each - a half percent, the worst case for rounding
		{81920, 3},  // 33.33% each
		{81920, 6},  // 16.66% each
		{81920, 7},  // 14.28% each
		{23028, 3},  // divides evenly
		{23028, 6},  // 16.66% each
		{23028, 8},  // 12.5% each
		{40960, 8},  // 12.5% each
		{40960, 16}, // 6.25% each
		{16384, 8},  // 12.5% each
		{22731, 7},  // does not divide evenly into the device
	} {
		t.Run(fmt.Sprintf("%dMiB/%d", tt.gpuMemoryMiB, tt.shares), func(t *testing.T) {
			nEqualSharesFitOnCompute(t, tt.gpuMemoryMiB, tt.shares)
		})
	}
}

// The ledger is int64 hundredths while requests are float64 portions. Any
// asymmetry between the add and the remove path leaks compute or drives the
// ledger negative, and the leak only shows up after many placements.
func TestNodeInfo_ComputeLedgerRoundTripsToZero(t *testing.T) {
	const (
		gpuMemoryMiB = int64(81920)
		gpuGroup     = "gpu-group-0"
	)
	node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, gpuMemoryMiB, 1000)

	awkward := []string{"0.005", "0.015", "0.335", "0.999", "1", "0.29", "0.0000000001", "0.125", "0.3333333333333333"}
	random := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		awkward = append(awkward, fmt.Sprintf("%.17g", random.Float64()*0.999+0.0005))
	}

	for i, computeRequest := range awkward {
		name := fmt.Sprintf("roundtrip-%d", i)
		pod := sharedGpuComputeTestPod(name, gpuGroup, v1.PodRunning, map[string]string{
			commonconstants.GpuFraction:                                 "0.1",
			resources.CalcGpuComputeRequestAnnotationForContainer("c1"): computeRequest,
		}, vectorMap)
		nodePodAffinityInfo.EXPECT().AddPod(pod.Pod).Times(1)
		nodePodAffinityInfo.EXPECT().RemovePod(pod.Pod).Times(1)

		assert.NoError(t, node.AddTask(pod))
		assert.NoError(t, node.RemoveTask(pod))

		assert.Equal(t, int64(0), node.UsedSharedGPUsCompute[gpuGroup],
			"compute request %q left the used ledger non-zero after add+remove", computeRequest)
		assert.Equal(t, int64(0), node.AllocatedSharedGPUsCompute[gpuGroup],
			"compute request %q left the allocated ledger non-zero after add+remove", computeRequest)
		assert.Equal(t, int64(0), node.ReleasingSharedGPUsCompute[gpuGroup],
			"compute request %q left the releasing ledger non-zero after add+remove", computeRequest)
		assert.Equal(t, int64(0), node.UsedSharedGPUsMemory[gpuGroup])
	}
}

// A request must never be charged more compute than it asked for: charging up
// turns a set of pods that exactly fills a device into one that overflows it.
func TestNodeInfo_ComputeChargeNeverExceedsRequest(t *testing.T) {
	node, _, _ := sharedComputeNode(t, 81920, 10)

	for _, portion := range []float64{
		0.005, 0.015, 0.125, 0.29, 0.335, 0.999, 1, 1.0 / 3.0, 1.0 / 6.0, 1.0 / 7.0,
	} {
		requirement := resource_info.NewGpuResourceRequirementWithGpus(0.1, 0)
		requirement.SetGpuComputePortion(portion)
		charged := node.GetResourceGpuCompute(requirement)

		// The charge tracks the request downwards, except that anything asking
		// for compute at all costs at least 1 hundredth. That floor is not a
		// rounding artefact: the node agent turns the same portion into a whole
		// MPS active-thread percentage with a minimum of 1, so a sub-1% pod is
		// genuinely handed 1% of the device and must be billed for it.
		if want := portion * float64(node.ComputeOfEveryGpuOnNode); want < 1 {
			assert.Equal(t, int64(1), charged,
				"a sub-1%% request (%v of a device) must be charged the 1 hundredth it is actually given", portion)
			continue
		}
		assert.LessOrEqual(t, float64(charged), portion*float64(node.ComputeOfEveryGpuOnNode)+1e-9,
			"a request for %v of a device's compute was charged %d hundredths", portion, charged)
	}
}

// TestNodeInfo_SubOnePercentSharesCannotOversubscribe pins the reason for that
// minimum: charging 0 for a sub-1% share would let an unbounded number of such
// pods onto one device, each of which the node agent then hands 1% of its SMs.
func TestNodeInfo_SubOnePercentSharesCannotOversubscribe(t *testing.T) {
	node, _, _ := sharedComputeNode(t, 81920, 10)

	requirement := resource_info.NewGpuResourceRequirementWithGpus(0.001, 0)
	requirement.SetGpuComputePortion(0.001)

	charged := node.GetResourceGpuCompute(requirement)
	assert.Equal(t, int64(1), charged, "a 0.1%% compute request must still be charged 1 hundredth")

	// 100 of them exhaust the device rather than fitting without limit.
	assert.Equal(t, int64(node.ComputeOfEveryGpuOnNode), charged*100,
		"100 sub-1%% pods must account for exactly one device's compute")
}

// modeGatingPod builds a fractional pod whose compute sharing mode is set
// verbatim - an empty mode leaves the annotation off entirely.
func modeGatingPod(
	name, gpuGroup, mode, fraction, compute string, vectorMap *resource_info.ResourceVectorMap,
) *pod_info.PodInfo {
	labels := map[string]string{}
	nodeName := ""
	if gpuGroup != "" {
		labels[commonconstants.GPUGroup] = gpuGroup
		nodeName = "node1"
	}
	annotations := map[string]string{
		commonconstants.PodGroupAnnotationForPod:                    "pg-" + name,
		commonconstants.GpuFraction:                                 fraction,
		resources.CalcGpuComputeRequestAnnotationForContainer("c1"): compute,
	}
	if mode != "" {
		annotations[resources.CalcGpuComputeSharingModeAnnotationForContainer("c1")] = mode
	}

	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			UID:         types.UID(name),
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: v1.PodSpec{
			NodeName:   nodeName,
			Containers: []v1.Container{{Name: "c1"}},
		},
		Status: v1.PodStatus{Phase: v1.PodRunning},
	}, vectorMap)
}

// Only sm-sharing partitions SMs between pods. Every other mode - including a
// group with no mode at all and one naming a mode this scheduler does not know -
// has to pack exactly as it did before the compute ledger existed.
func TestNodeInfo_ComputeLedgerOnlyConstrainsSmSharing(t *testing.T) {
	const gpuGroup = "gpu-group-0"

	tests := []struct {
		name            string
		mode            string
		wholeGpuCompute int64
		wantConstrained bool
	}{
		{name: "sm-sharing", mode: "sm-sharing", wholeGpuCompute: WholeGpuCompute, wantConstrained: true},
		{name: "time-slicing", mode: "time-slicing", wholeGpuCompute: WholeGpuCompute},
		{name: "no mode set", mode: "", wholeGpuCompute: WholeGpuCompute},
		// A node with no compute capacity has nothing to enforce against.
		{name: "sm-sharing without compute capacity", mode: "sm-sharing", wholeGpuCompute: 0},
		{name: "sm-sharing with negative compute capacity", mode: "sm-sharing", wholeGpuCompute: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, 81920, 20)
			node.ComputeOfEveryGpuOnNode = tt.wholeGpuCompute

			placed := modeGatingPod("placed", gpuGroup, tt.mode, "0.1", "0.9", vectorMap)
			nodePodAffinityInfo.EXPECT().AddPod(placed.Pod).Times(1)
			assert.NoError(t, node.AddTask(placed))

			pending := modeGatingPod("pending", "", tt.mode, "0.1", "0.9", vectorMap)
			assert.True(t, node.enoughMemoryOnGpu(&pending.GpuRequirement, gpuGroup),
				"precondition: memory must not be what rejects")

			assert.Equal(t, tt.wantConstrained, node.isGpuGroupComputeConstrained(gpuGroup))
			assert.Equal(t, !tt.wantConstrained, node.IsTaskFitOnGpuGroup(&pending.GpuRequirement, gpuGroup))
			assert.Equal(t, !tt.wantConstrained, node.EnoughIdleResourcesOnGpu(&pending.GpuRequirement, gpuGroup))
			assert.Equal(t, tt.wantConstrained, node.gpuComputeFitErrorReason(pending) != "")
		})
	}
}

// A gpuGroup can carry a mode this scheduler does not know - a BindRequest
// written by a newer one, say. An unknown mode must fail open: it is not
// sm-sharing, so the compute ledger must not constrain placement on it.
func TestNodeInfo_UnknownGpuGroupModeDoesNotConstrainCompute(t *testing.T) {
	const gpuGroup = "gpu-group-0"

	for _, mode := range []string{"mps-exclusive", "SM-Sharing", "sm_sharing", " sm-sharing"} {
		t.Run(mode, func(t *testing.T) {
			node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, 81920, 20)

			placed := modeGatingPod("placed", gpuGroup, "sm-sharing", "0.1", "0.9", vectorMap)
			placed.SetFractionalGpuGroups([]schedulingv1alpha2.FractionalGpuGroup{
				{ID: gpuGroup, ComputeSharingMode: schedulingv1alpha2.GPUComputeSharingMode(mode)},
			})
			nodePodAffinityInfo.EXPECT().AddPod(placed.Pod).Times(1)
			assert.NoError(t, node.AddTask(placed))
			assert.Equal(t, int64(90), node.UsedSharedGPUsCompute[gpuGroup],
				"precondition: the placed pod must hold the device's compute")

			pending := modeGatingPod("pending", "", "sm-sharing", "0.1", "0.9", vectorMap)
			assert.False(t, node.isGpuGroupComputeConstrained(gpuGroup))
			assert.True(t, node.IsTaskFitOnGpuGroup(&pending.GpuRequirement, gpuGroup))
			assert.True(t, node.EnoughIdleResourcesOnGpu(&pending.GpuRequirement, gpuGroup))
			assert.Empty(t, node.gpuComputeFitErrorReason(pending))
		})
	}
}

// The reservation pod is what the binder actually set the device up as, so it
// outranks anything a workload pod claims about the group. This is the
// production path: every fractional gpuGroup is held by one.
func TestNodeInfo_ReservationPodDecidesTheGroupMode(t *testing.T) {
	const gpuGroup = "gpu-group-0"

	tests := []struct {
		name            string
		reservationMode string
		workloadMode    string
		wantConstrained bool
	}{
		{name: "reservation sm-sharing", reservationMode: "sm-sharing", workloadMode: "sm-sharing", wantConstrained: true},
		{
			name:            "reservation sm-sharing outranks a time-slicing workload",
			reservationMode: "sm-sharing", workloadMode: "time-slicing", wantConstrained: true,
		},
		{
			name:            "reservation time-slicing outranks an sm-sharing workload",
			reservationMode: "time-slicing", workloadMode: "sm-sharing",
		},
		// A reservation pod with no mode annotation predates sm-sharing: fail open.
		{name: "reservation with no mode", reservationMode: "", workloadMode: "sm-sharing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, 81920, 20)

			placed := modeGatingPod("placed", gpuGroup, tt.workloadMode, "0.1", "0.9", vectorMap)
			nodePodAffinityInfo.EXPECT().AddPod(placed.Pod).Times(1)
			assert.NoError(t, node.AddTask(placed))

			reservationAnnotations := map[string]string{}
			if tt.reservationMode != "" {
				reservationAnnotations[resources.CalcGpuComputeSharingModeAnnotationForContainer("reservation")] =
					tt.reservationMode
			}
			reservation := pod_info.NewTaskInfo(&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        commonconstants.GPUReservationPodPrefix + "-abc123",
					Namespace:   "runai-reservation",
					UID:         types.UID("reservation"),
					Labels:      map[string]string{commonconstants.GPUGroup: gpuGroup},
					Annotations: reservationAnnotations,
				},
				Spec:   v1.PodSpec{NodeName: "node1", Containers: []v1.Container{{Name: "reservation"}}},
				Status: v1.PodStatus{Phase: v1.PodRunning},
			}, vectorMap)
			node.PodInfos[pod_info.PodKey(reservation.Pod)] = reservation

			pending := modeGatingPod("pending", "", tt.workloadMode, "0.1", "0.9", vectorMap)
			assert.Equal(t, tt.wantConstrained, node.isGpuGroupComputeConstrained(gpuGroup))
			assert.Equal(t, !tt.wantConstrained, node.IsTaskFitOnGpuGroup(&pending.GpuRequirement, gpuGroup))
		})
	}
}

// A node with no compute capacity cannot report a portion of it.
func TestNodeInfo_GetUsedGpuComputePortionWithoutCapacity(t *testing.T) {
	node, _, _ := sharedComputeNode(t, 81920, 20)
	node.UsedSharedGPUsCompute["gpu-group-0"] = 50

	portion, err := node.GetUsedGpuComputePortion("gpu-group-0")
	assert.NoError(t, err)
	assert.InDelta(t, 0.5, portion, 1e-9)

	node.ComputeOfEveryGpuOnNode = 0
	_, err = node.GetUsedGpuComputePortion("gpu-group-0")
	assert.Error(t, err)

	node.ComputeOfEveryGpuOnNode = -1
	_, err = node.GetUsedGpuComputePortion("gpu-group-0")
	assert.Error(t, err)
}

type ledgerPodSpec struct {
	name     string
	fraction string
	compute  string
}

type ledgerStep struct {
	pod    string
	status pod_status.PodStatus
	remove bool
}

type sharedLedgerState struct {
	idleGPUs            float64
	releasingGPUs       float64
	usedGPUs            float64
	releasingSharedGPUs map[string]bool
	usedMemory          map[string]int64
	releasingMemory     map[string]int64
	allocatedMemory     map[string]int64
}

const ledgerGpuGroup = "gpu-group-0"

// runSharedLedgerScenario replays a sequence of task transitions on one shared
// GPU and reports the whole-GPU accounting that came out of it. withCompute
// controls only whether the pods carry an explicit compute request: everything
// else, the sm-sharing mode included, is identical.
func runSharedLedgerScenario(
	t *testing.T, specs []ledgerPodSpec, steps []ledgerStep, withCompute bool,
) sharedLedgerState {
	t.Helper()

	node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, 81920, 20)
	nodePodAffinityInfo.EXPECT().AddPod(Any()).AnyTimes()
	nodePodAffinityInfo.EXPECT().RemovePod(Any()).Return(nil).AnyTimes()

	pods := map[string]*pod_info.PodInfo{}
	onNode := map[string]bool{}
	for _, spec := range specs {
		annotations := map[string]string{commonconstants.GpuFraction: spec.fraction}
		if withCompute && spec.compute != "" {
			annotations[resources.CalcGpuComputeRequestAnnotationForContainer("c1")] = spec.compute
		}
		pods[spec.name] = sharedGpuComputeTestPod(spec.name, ledgerGpuGroup, v1.PodRunning, annotations, vectorMap)
	}

	for i, step := range steps {
		pod := pods[step.pod]
		switch {
		case step.remove:
			assert.NoError(t, node.RemoveTask(pod), "step %d: remove %s", i, step.pod)
			onNode[step.pod] = false
		case onNode[step.pod]:
			pod.Status = step.status
			assert.NoError(t, node.UpdateTask(pod), "step %d: update %s", i, step.pod)
		default:
			pod.Status = step.status
			assert.NoError(t, node.AddTask(pod), "step %d: add %s", i, step.pod)
			onNode[step.pod] = true
		}
	}

	state := sharedLedgerState{
		idleGPUs:            node.IdleVector.Get(resource_info.GPUIndex),
		releasingGPUs:       node.ReleasingVector.Get(resource_info.GPUIndex),
		usedGPUs:            node.UsedVector.Get(resource_info.GPUIndex),
		releasingSharedGPUs: map[string]bool{},
		usedMemory:          map[string]int64{},
		releasingMemory:     map[string]int64{},
		allocatedMemory:     map[string]int64{},
	}
	for key, value := range node.ReleasingSharedGPUs {
		state.releasingSharedGPUs[key] = value
	}
	for key, value := range node.UsedSharedGPUsMemory {
		state.usedMemory[key] = value
	}
	for key, value := range node.ReleasingSharedGPUsMemory {
		state.releasingMemory[key] = value
	}
	for key, value := range node.AllocatedSharedGPUsMemory {
		state.allocatedMemory[key] = value
	}
	return state
}

// The compute ledger has to stay passive. If it also drove the "is this the last
// releasing task" branches, the whole-GPU idle and releasing counts would move
// twice per task. Every interleaving below must land on exactly the accounting
// it reaches with no compute request in play - including the cases where the
// compute charges diverge sharply from the memory shares.
func TestNodeInfo_ComputeLedgerDoesNotMoveWholeGpuAccounting(t *testing.T) {
	// A charge of 0.005 of a device rounds to zero hundredths, so this pod is
	// invisible to the compute ledger while still holding memory: a compute-driven
	// "everything here is releasing" test would fire while memory says otherwise.
	invisibleCompute := ledgerPodSpec{name: "invisible-compute", fraction: "0.5", compute: "0.005"}
	heavyCompute := ledgerPodSpec{name: "heavy-compute", fraction: "0.1", compute: "0.9"}
	lightCompute := ledgerPodSpec{name: "light-compute", fraction: "0.4", compute: "0.05"}

	tests := []struct {
		name  string
		specs []ledgerPodSpec
		steps []ledgerStep
	}{
		{
			name:  "allocate, pipeline, release, re-allocate",
			specs: []ledgerPodSpec{heavyCompute},
			steps: []ledgerStep{
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Pipelined},
				{pod: "heavy-compute", status: pod_status.Releasing},
				{pod: "heavy-compute", status: pod_status.Running},
			},
		},
		{
			name:  "two pods on one group, the heavier one releases",
			specs: []ledgerPodSpec{heavyCompute, lightCompute},
			steps: []ledgerStep{
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "light-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Releasing},
			},
		},
		{
			name:  "two pods on one group, both release",
			specs: []ledgerPodSpec{heavyCompute, lightCompute},
			steps: []ledgerStep{
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "light-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Releasing},
				{pod: "light-compute", status: pod_status.Releasing},
			},
		},
		{
			// The releasing pod holds every hundredth the compute ledger can see,
			// while the pod staying put holds most of the memory. A compute-driven
			// "everything here is releasing" test fires; a memory-driven one must not.
			name:  "the releasing pod holds all the compute, another holds the memory",
			specs: []ledgerPodSpec{invisibleCompute, heavyCompute},
			steps: []ledgerStep{
				{pod: "invisible-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Releasing},
			},
		},
		{
			name:  "and then the compute-invisible pod is removed",
			specs: []ledgerPodSpec{invisibleCompute, heavyCompute},
			steps: []ledgerStep{
				{pod: "invisible-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Releasing},
				{pod: "invisible-compute", remove: true},
			},
		},
		{
			name:  "the only pod on the group charges no compute and releases",
			specs: []ledgerPodSpec{invisibleCompute},
			steps: []ledgerStep{
				{pod: "invisible-compute", status: pod_status.Running},
				{pod: "invisible-compute", status: pod_status.Releasing},
			},
		},
		{
			name:  "evicting a task pipelined behind a releasing one",
			specs: []ledgerPodSpec{heavyCompute, lightCompute},
			steps: []ledgerStep{
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Releasing},
				{pod: "light-compute", status: pod_status.Pipelined},
				{pod: "light-compute", remove: true},
			},
		},
		{
			name:  "a pipelined task is allocated onto the freed GPU",
			specs: []ledgerPodSpec{heavyCompute, lightCompute},
			steps: []ledgerStep{
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Releasing},
				{pod: "light-compute", status: pod_status.Pipelined},
				{pod: "heavy-compute", remove: true},
				{pod: "light-compute", status: pod_status.Running},
			},
		},
		{
			name:  "the same task added and removed twice",
			specs: []ledgerPodSpec{heavyCompute},
			steps: []ledgerStep{
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "heavy-compute", remove: true},
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "heavy-compute", remove: true},
			},
		},
		{
			name:  "everything released and the group emptied",
			specs: []ledgerPodSpec{heavyCompute, lightCompute},
			steps: []ledgerStep{
				{pod: "heavy-compute", status: pod_status.Running},
				{pod: "light-compute", status: pod_status.Running},
				{pod: "heavy-compute", status: pod_status.Releasing},
				{pod: "light-compute", status: pod_status.Releasing},
				{pod: "heavy-compute", remove: true},
				{pod: "light-compute", remove: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCompute := runSharedLedgerScenario(t, tt.specs, tt.steps, true)
			withoutCompute := runSharedLedgerScenario(t, tt.specs, tt.steps, false)

			assert.Equal(t, withoutCompute.idleGPUs, withCompute.idleGPUs, "idle whole GPUs")
			assert.Equal(t, withoutCompute.releasingGPUs, withCompute.releasingGPUs, "releasing whole GPUs")
			assert.Equal(t, withoutCompute.usedGPUs, withCompute.usedGPUs, "used whole GPUs")
			assert.Equal(t, withoutCompute.releasingSharedGPUs, withCompute.releasingSharedGPUs,
				"the releasing marker must be driven by memory alone")
			assert.Equal(t, withoutCompute.usedMemory, withCompute.usedMemory)
			assert.Equal(t, withoutCompute.releasingMemory, withCompute.releasingMemory)
			assert.Equal(t, withoutCompute.allocatedMemory, withCompute.allocatedMemory)

			// A single shared GPU on a one-GPU node: the whole-GPU counts can only
			// ever be 0 or 1, never the 2 that a double fire would produce.
			assert.LessOrEqual(t, withCompute.idleGPUs, float64(1))
			assert.GreaterOrEqual(t, withCompute.idleGPUs, float64(0))
			assert.LessOrEqual(t, withCompute.releasingGPUs, float64(1))
			assert.GreaterOrEqual(t, withCompute.releasingGPUs, float64(0))
		})
	}
}

// Only an explicit request may reach the binder. Carrying the memory-derived
// default would override the binder's own fallback and change the MPS cap of
// every fractional workload that never asked for compute.
func TestNodeInfo_OnlyExplicitComputeRequestReachesTheBinder(t *testing.T) {
	const gpuGroup = "gpu-group-0"

	tests := []struct {
		name               string
		annotations        map[string]string
		wantLedgerCompute  int64
		wantCarriedPortion float64
	}{
		{
			name: "explicit compute request",
			annotations: map[string]string{
				commonconstants.GpuFraction:                                 "0.5",
				resources.CalcGpuComputeRequestAnnotationForContainer("c1"): "0.25",
			},
			wantLedgerCompute:  25,
			wantCarriedPortion: 0.25,
		},
		{
			name:               "portion request with no compute request",
			annotations:        map[string]string{commonconstants.GpuFraction: "0.5"},
			wantLedgerCompute:  50,
			wantCarriedPortion: 0,
		},
		{
			name: "memory request with no compute request",
			annotations: map[string]string{
				resources.CalcGpuFractionAnnotationForContainer("c1"): "20480Mi",
			},
			wantLedgerCompute:  25,
			wantCarriedPortion: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, 81920, 20)
			pod := sharedGpuComputeTestPod("pod", gpuGroup, v1.PodRunning, tt.annotations, vectorMap)
			nodePodAffinityInfo.EXPECT().AddPod(pod.Pod).Times(1)
			assert.NoError(t, node.AddTask(pod))

			assert.Equal(t, tt.wantLedgerCompute, node.UsedSharedGPUsCompute[gpuGroup],
				"the ledger always charges the effective share")
			assert.Equal(t, tt.wantCarriedPortion, pod.AcceptedGpuRequirement.GpuComputePortion(),
				"only an explicit request may be carried to the binder")
		})
	}
}

// A multi-device fractional pod takes its compute share on each device it lands
// on, and the share is per device rather than split across them.
func TestNodeInfo_MultiDeviceFractionChargesEveryGpuGroup(t *testing.T) {
	node, vectorMap, nodePodAffinityInfo := sharedComputeNode(t, 81920, 20)
	node.AllocatableVector.Set(resource_info.GPUIndex, 2)
	node.IdleVector.Set(resource_info.GPUIndex, 2)

	pod := sharedGpuComputeTestPod("multi", "gpu-group-0", v1.PodRunning, map[string]string{
		commonconstants.GpuFraction:                                 "0.25",
		commonconstants.GpuFractionsNumDevices:                      "2",
		resources.CalcGpuComputeRequestAnnotationForContainer("c1"): "0.3",
	}, vectorMap)
	groupKey, groupValue := resources.GetMultiFractionGpuGroupLabel("gpu-group-1")
	pod.Pod.Labels[groupKey] = groupValue
	pod = pod_info.NewTaskInfo(pod.Pod, vectorMap)
	pod.Status = pod_status.Running
	nodePodAffinityInfo.EXPECT().AddPod(pod.Pod).Times(1)
	nodePodAffinityInfo.EXPECT().RemovePod(pod.Pod).Return(nil).Times(1)

	assert.Equal(t, int64(2), pod.GpuRequirement.GetNumOfGpuDevices())
	assert.NoError(t, node.AddTask(pod))

	for _, gpuGroup := range []string{"gpu-group-0", "gpu-group-1"} {
		assert.Equal(t, int64(30), node.UsedSharedGPUsCompute[gpuGroup], gpuGroup)
		assert.Equal(t, int64(30), node.AllocatedSharedGPUsCompute[gpuGroup], gpuGroup)
		assert.Equal(t, int64(20480), node.UsedSharedGPUsMemory[gpuGroup], gpuGroup)
	}

	assert.NoError(t, node.RemoveTask(pod))
	for _, gpuGroup := range []string{"gpu-group-0", "gpu-group-1"} {
		assert.Equal(t, int64(0), node.UsedSharedGPUsCompute[gpuGroup], gpuGroup)
		assert.Equal(t, int64(0), node.AllocatedSharedGPUsCompute[gpuGroup], gpuGroup)
	}
}

// Every map on GpuSharingNodeInfo has to be copied by Clone. One left out shares
// state with the original - or drops it - and nothing else would notice.
func TestGpuSharingNodeInfo_CloneIsDeepForEveryMap(t *testing.T) {
	original := newGpuSharingNodeInfo()
	original.ReleasingSharedGPUs["0"] = true
	original.UsedSharedGPUsMemory["0"] = 50
	original.ReleasingSharedGPUsMemory["0"] = 10
	original.AllocatedSharedGPUsMemory["0"] = 40
	original.UsedSharedGPUsCompute["0"] = 25
	original.ReleasingSharedGPUsCompute["0"] = 5
	original.AllocatedSharedGPUsCompute["0"] = 20
	original.DRASharedDeviceRefCount["driver/pool/device"] = 2

	cloned := original.Clone()

	// Overwriting, adding and deleting all have to leave the original alone.
	cloned.ReleasingSharedGPUs["0"] = false
	cloned.ReleasingSharedGPUs["new"] = true
	delete(cloned.UsedSharedGPUsMemory, "0")
	cloned.ReleasingSharedGPUsMemory["0"] = 99
	cloned.AllocatedSharedGPUsMemory["new"] = 99
	delete(cloned.UsedSharedGPUsCompute, "0")
	cloned.ReleasingSharedGPUsCompute["0"] = 99
	cloned.AllocatedSharedGPUsCompute["new"] = 99
	cloned.DRASharedDeviceRefCount["driver/pool/device"] = 99

	assert.Equal(t, map[string]bool{"0": true}, original.ReleasingSharedGPUs)
	assert.Equal(t, map[string]int64{"0": 50}, original.UsedSharedGPUsMemory)
	assert.Equal(t, map[string]int64{"0": 10}, original.ReleasingSharedGPUsMemory)
	assert.Equal(t, map[string]int64{"0": 40}, original.AllocatedSharedGPUsMemory)
	assert.Equal(t, map[string]int64{"0": 25}, original.UsedSharedGPUsCompute)
	assert.Equal(t, map[string]int64{"0": 5}, original.ReleasingSharedGPUsCompute)
	assert.Equal(t, map[string]int64{"0": 20}, original.AllocatedSharedGPUsCompute)
	assert.Equal(t, map[string]int{"driver/pool/device": 2}, original.DRASharedDeviceRefCount)
}
