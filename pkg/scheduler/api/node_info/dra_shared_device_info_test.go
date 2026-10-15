// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const gpuDeviceClass = "gpu.nvidia.com"

// sharedGPUClaim builds a ResourceClaim that requests one GPU device and is
// allocated to the given physical device. The same claim object is referenced
// by every pod that shares the device (status.reservedFor with multiple
// entries), matching a DRA time-slicing / MPS setup.
func sharedGPUClaim(name, namespace, driver, pool, device string) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{
					{
						Name: "gpu",
						Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: gpuDeviceClass,
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           1,
						},
					},
				},
			},
		},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{
					Results: []resourceapi.DeviceRequestAllocationResult{
						{Request: "gpu", Driver: driver, Pool: pool, Device: device},
					},
				},
			},
		},
	}
}

// draConsumerPod builds a running pod that consumes the given claim by name.
func draConsumerPod(name, namespace, nodeName, claimName string) *v1.Pod {
	pod := common_info.BuildPod(namespace, name, nodeName, v1.PodRunning,
		common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{},
		map[string]string{}, map[string]string{
			pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
			commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
		})
	pod.Spec.ResourceClaims = []v1.PodResourceClaim{
		{Name: claimName, ResourceClaimName: ptr.To(claimName)},
	}
	return pod
}

// newGPUNodeInfo builds a NodeInfo for a node with the given whole-GPU count,
// wired with a mock pod-affinity that expects addPods AddPod and rmPods
// RemovePod calls, and a vectorMap that knows the DRA GPU device class.
func newGPUNodeInfo(t *testing.T, name, gpuCount string, addPods, rmPods int) (*NodeInfo, *resource_info.ResourceVectorMap) {
	node := common_info.BuildNode(name, common_info.BuildResourceListWithGPU("8000m", "16G", gpuCount))

	ctrl := gomock.NewController(t)
	affinity := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
	affinity.EXPECT().AddPod(gomock.Any()).Times(addPods)
	affinity.EXPECT().RemovePod(gomock.Any()).Times(rmPods)

	vectorMap := resource_info.NewResourceVectorMap()
	for resourceName := range node.Status.Allocatable {
		vectorMap.AddResource(resourceName)
	}
	// DRA GPU counts are tracked under the device-class resource name.
	vectorMap.AddResource(v1.ResourceName(gpuDeviceClass))

	return NewNodeInfo(node, affinity, vectorMap), vectorMap
}

// TestAddTask_SharedDRAClaimCountedOnce is the regression test for the
// shared-ResourceClaim double count. Two pods share one physical GPU through a
// single claim (reservedFor has both). Naive per-task accounting adds the GPU
// once per pod, driving the node's used GPU count to 2 on a 1-GPU node and
// IdleVector negative, which makes the node unschedulable for every task. The
// fix must keep the used count at exactly 1.
//
// This exercises the real accounting path (AddTask -> addTaskResources ->
// UsedVector), not the dedup helper in isolation, so it fails if the dedup is
// not wired into AddTask.
func TestAddTask_SharedDRAClaimCountedOnce(t *testing.T) {
	ni, vectorMap := newGPUNodeInfo(t, "atlas", "1", 2, 0)

	claim := sharedGPUClaim("voice-atlas-shared", "voice-pipeline", gpuDeviceClass, "atlas", "gpu-0")
	pod1 := draConsumerPod("voice-tts", "voice-pipeline", "atlas", "voice-atlas-shared")
	pod2 := draConsumerPod("voice-worker", "voice-pipeline", "atlas", "voice-atlas-shared")

	task1 := pod_info.NewTaskInfo(pod1, vectorMap, pod_info.TaskInfoOptions{
		DraPodClaims: []*resourceapi.ResourceClaim{claim},
	})
	task2 := pod_info.NewTaskInfo(pod2, vectorMap, pod_info.TaskInfoOptions{
		DraPodClaims: []*resourceapi.ResourceClaim{claim},
	})

	assert.NoError(t, ni.AddTask(task1))
	assert.Equal(t, 1.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"first consumer of the shared device must be counted")

	assert.NoError(t, ni.AddTask(task2))
	assert.Equal(t, 1.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"second consumer of the same shared device must not be double-counted")

	idleGPUs, _ := ni.GetSumOfIdleGPUs()
	assert.Equal(t, 0.0, idleGPUs, "idle GPUs must be 0, never negative, on a fully-shared 1-GPU node")
}

// TestAddRemoveTask_SharedDRAClaimSymmetry verifies the used count follows the
// number of distinct physical devices as consumers are added and removed: it
// stays at 1 while any consumer of the shared device remains, and returns to 0
// only when the last one leaves.
func TestAddRemoveTask_SharedDRAClaimSymmetry(t *testing.T) {
	ni, vectorMap := newGPUNodeInfo(t, "atlas", "1", 2, 2)

	claim := sharedGPUClaim("voice-atlas-shared", "voice-pipeline", gpuDeviceClass, "atlas", "gpu-0")
	pod1 := draConsumerPod("voice-tts", "voice-pipeline", "atlas", "voice-atlas-shared")
	pod2 := draConsumerPod("voice-worker", "voice-pipeline", "atlas", "voice-atlas-shared")
	task1 := pod_info.NewTaskInfo(pod1, vectorMap, pod_info.TaskInfoOptions{DraPodClaims: []*resourceapi.ResourceClaim{claim}})
	task2 := pod_info.NewTaskInfo(pod2, vectorMap, pod_info.TaskInfoOptions{DraPodClaims: []*resourceapi.ResourceClaim{claim}})

	assert.NoError(t, ni.AddTask(task1))
	assert.NoError(t, ni.AddTask(task2))
	assert.Equal(t, 1.0, ni.UsedVector.Get(resource_info.GPUIndex))

	assert.NoError(t, ni.RemoveTask(task2))
	assert.Equal(t, 1.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"removing one of two consumers must keep the shared device counted")

	assert.NoError(t, ni.RemoveTask(task1))
	assert.Equal(t, 0.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"removing the last consumer must release the shared device")
}

// draReservationPod builds a resource-reservation pod that references the given
// claim. addTaskResources zeroes such a pod's GPU index, so it must not affect
// the shared-device GPU accounting at all.
func draReservationPod(name, namespace, nodeName, claimName string) *v1.Pod {
	pod := common_info.BuildPod(namespace, name, nodeName, v1.PodRunning,
		common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{},
		map[string]string{
			commonconstants.AppLabelName: "kai-resource-reservation",
		}, map[string]string{
			pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
			commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
		})
	pod.Spec.ResourceClaims = []v1.PodResourceClaim{
		{Name: claimName, ResourceClaimName: ptr.To(claimName)},
	}
	return pod
}

// TestAddTask_DistinctDRADevicesEachCounted guards against over-dedup: two pods
// on two different physical GPUs (own claim each) must both be counted.
func TestAddTask_DistinctDRADevicesEachCounted(t *testing.T) {
	ni, vectorMap := newGPUNodeInfo(t, "nyx", "2", 2, 0)

	claimA := sharedGPUClaim("coder-claim", "vllm", gpuDeviceClass, "nyx", "gpu-4")
	claimB := sharedGPUClaim("gemma-claim", "vllm", gpuDeviceClass, "nyx", "gpu-6")
	podA := draConsumerPod("coder", "vllm", "nyx", "coder-claim")
	podB := draConsumerPod("gemma", "vllm", "nyx", "gemma-claim")
	taskA := pod_info.NewTaskInfo(podA, vectorMap, pod_info.TaskInfoOptions{DraPodClaims: []*resourceapi.ResourceClaim{claimA}})
	taskB := pod_info.NewTaskInfo(podB, vectorMap, pod_info.TaskInfoOptions{DraPodClaims: []*resourceapi.ResourceClaim{claimB}})

	assert.NoError(t, ni.AddTask(taskA))
	assert.NoError(t, ni.AddTask(taskB))
	assert.Equal(t, 2.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"two distinct physical devices must both be counted")
}

// TestAddRemoveTask_ReservationPodDoesNotCorruptSharedDRAAccounting guards the
// dedup against resource-reservation tasks. addTaskResources zeroes a
// reservation pod's GPU index, so it contributes 0 GPUs; if the dedup still
// tracked that pod's shared device it would subtract a device from a 0 vector
// (driving UsedVector negative and corrupting node capacity) and inflate the
// reference count, masking the real consumers. A reservation pod referencing
// the same shared claim as a real consumer must leave the GPU used count at 1
// on add and on remove, in any order.
func TestAddRemoveTask_ReservationPodDoesNotCorruptSharedDRAAccounting(t *testing.T) {
	ni, vectorMap := newGPUNodeInfo(t, "atlas", "1", 2, 2)

	claim := sharedGPUClaim("voice-atlas-shared", "voice-pipeline", gpuDeviceClass, "atlas", "gpu-0")
	consumer := draConsumerPod("voice-tts", "voice-pipeline", "atlas", "voice-atlas-shared")
	reservation := draReservationPod("kai-resource-reservation-abc", "voice-pipeline", "atlas", "voice-atlas-shared")
	consumerTask := pod_info.NewTaskInfo(consumer, vectorMap, pod_info.TaskInfoOptions{DraPodClaims: []*resourceapi.ResourceClaim{claim}})
	reservationTask := pod_info.NewTaskInfo(reservation, vectorMap, pod_info.TaskInfoOptions{DraPodClaims: []*resourceapi.ResourceClaim{claim}})

	assert.NoError(t, ni.AddTask(consumerTask))
	assert.Equal(t, 1.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"real consumer of the shared device must be counted once")

	assert.NoError(t, ni.AddTask(reservationTask))
	assert.Equal(t, 1.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"reservation pod contributes no GPU and must not change the used count")

	idleGPUs, _ := ni.GetSumOfIdleGPUs()
	assert.Equal(t, 0.0, idleGPUs, "idle GPUs must stay 0, never negative")

	assert.NoError(t, ni.RemoveTask(reservationTask))
	assert.Equal(t, 1.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"removing the reservation pod must leave the real consumer's device counted")

	assert.NoError(t, ni.RemoveTask(consumerTask))
	assert.Equal(t, 0.0, ni.UsedVector.Get(resource_info.GPUIndex),
		"removing the last real consumer must release the shared device")
}
