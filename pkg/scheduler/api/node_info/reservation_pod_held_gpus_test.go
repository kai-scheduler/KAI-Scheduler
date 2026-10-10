// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const heldGpusTestGroup = "0a8231f4-e9fc-434b-bf77-a1aebd41b442"

type heldGpusFixture struct {
	ni        *NodeInfo
	vectorMap *resource_info.ResourceVectorMap
}

func newHeldGpusFixture(t *testing.T, gpus string) *heldGpusFixture {
	node := common_info.BuildNode("n1", common_info.BuildResourceListWithGPUAndPods("8000m", "10G", gpus, "110"))
	node.Labels[GpuMemoryLabel] = "81920"
	controller := NewController(t)
	affinity := pod_affinity.NewMockNodePodAffinityInfo(controller)
	affinity.EXPECT().AddPod(Any()).AnyTimes()
	affinity.EXPECT().RemovePod(Any()).AnyTimes()
	vectorMap := resource_info.NewResourceVectorMap()
	vectorMap.AddResourceList(node.Status.Allocatable)
	return &heldGpusFixture{ni: NewNodeInfo(node, affinity, vectorMap), vectorMap: vectorMap}
}

func (f *heldGpusFixture) reservationPod(name, gpuGroup string) *pod_info.PodInfo {
	pod := common_info.BuildPod("kai-resource-reservation", name, "n1", v1.PodRunning,
		common_info.BuildResourceListWithGPU("100m", "100M", "1"),
		[]metav1.OwnerReference{}, map[string]string{
			commonconstants.AppLabelName: "kai-resource-reservation",
			commonconstants.GPUGroup:     gpuGroup,
		}, map[string]string{
			commonconstants.PodGroupAnnotationForPod: common_info.FakePogGroupId,
		})
	return pod_info.NewTaskInfo(pod, f.vectorMap)
}

func (f *heldGpusFixture) fractionPod(name, gpuGroup string) *pod_info.PodInfo {
	pod := common_info.BuildPod("team-a", name, "n1", v1.PodRunning,
		common_info.BuildResourceList("100m", "100M"),
		[]metav1.OwnerReference{}, map[string]string{}, map[string]string{
			commonconstants.PodGroupAnnotationForPod: common_info.FakePogGroupId,
			commonconstants.GpuFraction:              "0.5",
		})
	task := pod_info.NewTaskInfo(pod, f.vectorMap)
	if gpuGroup != "" {
		task.SetGPUGroupIDs([]string{gpuGroup})
	}
	return task
}

func (f *heldGpusFixture) wholeGpuPod(name string) *pod_info.PodInfo {
	pod := common_info.BuildPod("team-a", name, "n1", v1.PodRunning,
		common_info.BuildResourceListWithGPU("100m", "100M", "1"),
		[]metav1.OwnerReference{}, map[string]string{}, map[string]string{
			commonconstants.PodGroupAnnotationForPod: common_info.FakePogGroupId,
		})
	return pod_info.NewTaskInfo(pod, f.vectorMap)
}

func TestShouldHoldOneGpuWhenReservationPodHasNoFractionPods(t *testing.T) {
	// Precondition.
	f := newHeldGpusFixture(t, "2")

	// Under test.
	assert.NoError(t, f.ni.AddTask(f.reservationPod("gpu-reservation-1", heldGpusTestGroup)))

	// Postcondition.
	assert.Equal(t, int64(1), f.ni.GpusHeldByIdleReservationPods())
	idleGPUs, _ := f.ni.GetSumOfIdleGPUs()
	assert.Equal(t, float64(1), idleGPUs)
	assert.Equal(t, float64(2), f.ni.IdleVector.Get(resource_info.GPUIndex), "IdleVector itself is unchanged")
}

func TestShouldNotHoldGpuWhenFractionPodUsesTheGroup(t *testing.T) {
	// Precondition.
	f := newHeldGpusFixture(t, "2")
	assert.NoError(t, f.ni.AddTask(f.reservationPod("gpu-reservation-1", heldGpusTestGroup)))

	// Under test.
	assert.NoError(t, f.ni.AddTask(f.fractionPod("sim-1", heldGpusTestGroup)))

	// Postcondition.
	assert.Equal(t, int64(0), f.ni.GpusHeldByIdleReservationPods())
	idleGPUs, _ := f.ni.GetSumOfIdleGPUs()
	assert.Equal(t, 1.5, idleGPUs)
}

func TestShouldHoldGpuAgainWhenLastFractionPodLeavesTheGroup(t *testing.T) {
	// Precondition.
	f := newHeldGpusFixture(t, "2")
	assert.NoError(t, f.ni.AddTask(f.reservationPod("gpu-reservation-1", heldGpusTestGroup)))
	sim := f.fractionPod("sim-1", heldGpusTestGroup)
	assert.NoError(t, f.ni.AddTask(sim))

	// Under test.
	assert.NoError(t, f.ni.RemoveTask(sim))

	// Postcondition.
	assert.Equal(t, int64(1), f.ni.GpusHeldByIdleReservationPods())
}

func TestShouldStopHoldingGpuWhenReservationPodIsRemoved(t *testing.T) {
	// Precondition.
	f := newHeldGpusFixture(t, "2")
	reservation := f.reservationPod("gpu-reservation-1", heldGpusTestGroup)
	assert.NoError(t, f.ni.AddTask(reservation))

	// Under test.
	assert.NoError(t, f.ni.RemoveTask(reservation))

	// Postcondition.
	assert.Equal(t, int64(0), f.ni.GpusHeldByIdleReservationPods())
	assert.Empty(t, f.ni.ReservationPodsPerGpuGroup)
}

func TestShouldRejectNewGpuGroupWhenEveryDeviceIsHeld(t *testing.T) {
	// Precondition.
	f := newHeldGpusFixture(t, "1")
	assert.NoError(t, f.ni.AddTask(f.reservationPod("gpu-reservation-1", heldGpusTestGroup)))

	// Under test.
	fractionAllocatable := f.ni.IsTaskAllocatable(f.fractionPod("sim-new", ""))
	wholeAllocatable := f.ni.IsTaskAllocatable(f.wholeGpuPod("train-1"))

	// Postcondition.
	assert.False(t, fractionAllocatable, "a new gpu group would need a device the kubelet has already handed out")
	assert.False(t, wholeAllocatable, "a whole-GPU pod would be rejected by the kubelet")
}

func TestShouldAllowNewGpuGroupWhenADeviceIsStillFree(t *testing.T) {
	// Precondition.
	f := newHeldGpusFixture(t, "2")
	assert.NoError(t, f.ni.AddTask(f.reservationPod("gpu-reservation-1", heldGpusTestGroup)))

	// Under test.
	fractionAllocatable := f.ni.IsTaskAllocatable(f.fractionPod("sim-new", ""))
	wholeAllocatable := f.ni.IsTaskAllocatable(f.wholeGpuPod("train-1"))

	// Postcondition.
	assert.True(t, fractionAllocatable)
	assert.True(t, wholeAllocatable)
}

func TestShouldCloneHeldReservationPodGroups(t *testing.T) {
	// Precondition.
	f := newHeldGpusFixture(t, "2")
	assert.NoError(t, f.ni.AddTask(f.reservationPod("gpu-reservation-1", heldGpusTestGroup)))

	// Under test.
	clone := f.ni.GpuSharingNodeInfo.Clone()

	// Postcondition.
	assert.Equal(t, 1, clone.ReservationPodsPerGpuGroup[heldGpusTestGroup])
}
