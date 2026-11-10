// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	resourceapi "k8s.io/api/resource/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// draDeviceKey uniquely identifies a physical DRA device on the node.
func draDeviceKey(result resourceapi.DeviceRequestAllocationResult) string {
	return result.Driver + "/" + result.Pool + "/" + result.Device
}

// allocatedGPUDeviceKeys returns the keys of all GPU devices allocated to the
// task via DRA ResourceClaims. Non-GPU devices are ignored: they are not part
// of the GPU accounting that this dedup protects.
func (ni *NodeInfo) allocatedGPUDeviceKeys(task *pod_info.PodInfo) []string {
	var keys []string
	for _, claimAllocation := range task.ResourceClaimInfo {
		if claimAllocation == nil || claimAllocation.Allocation == nil {
			continue
		}
		for _, result := range claimAllocation.Allocation.Devices.Results {
			if !resources.IsGPUDeviceClass(result.Driver) {
				continue
			}
			keys = append(keys, draDeviceKey(result))
		}
	}
	return keys
}

// sharedDRAGpuDiscount returns the number of GPU devices the task requests via
// DRA claims that are already counted on this node for other pods. A task that
// shares an allocated device with a running pod does not need additional GPU
// capacity for that device.
func (ni *NodeInfo) sharedDRAGpuDiscount(task *pod_info.PodInfo) float64 {
	discount := 0.0
	for _, key := range ni.allocatedGPUDeviceKeys(task) {
		if ni.DRASharedDeviceRefCount[key] > 0 {
			discount++
		}
	}
	return discount
}

// dedupSharedDRAGpus removes from resourcesToTrack the GPU count that would
// double-count physical DRA devices already referenced by other pods on the
// node. It also updates the node's per-device reference count. It must be
// called once per addTaskResources, before the vector is added to UsedVector.
func (ni *NodeInfo) dedupSharedDRAGpus(task *pod_info.PodInfo, resourcesToTrack resource_info.ResourceVector) {
	current := resourcesToTrack.Get(resource_info.GPUIndex)
	if current <= 0 {
		// The task contributes no GPUs to the used vector (e.g. a resource
		// reservation task whose GPU index was zeroed). Tracking its devices
		// would both risk a negative deduction below and mask the reference
		// count of the real consuming pods, so leave the accounting untouched.
		return
	}

	alreadyCounted := 0.0
	for _, key := range ni.allocatedGPUDeviceKeys(task) {
		if ni.DRASharedDeviceRefCount[key] > 0 {
			// Another pod on this node already contributed this physical
			// device to the used vector: do not count it again.
			alreadyCounted++
		}
		ni.DRASharedDeviceRefCount[key]++
	}

	if alreadyCounted > current {
		// Never deduct more than the task's own GPU contribution.
		alreadyCounted = current
	}
	if alreadyCounted > 0 {
		resourcesToTrack.Set(resource_info.GPUIndex, current-alreadyCounted)
	}
}

// releaseSharedDRAGpus is the inverse of dedupSharedDRAGpus: it decrements the
// per-device reference count and adds back the GPU count for devices that
// remain referenced by other pods (and were therefore never subtracted on this
// task's removal path). It must be called once per removeTaskResources.
func (ni *NodeInfo) releaseSharedDRAGpus(task *pod_info.PodInfo, resourcesToTrack resource_info.ResourceVector) {
	current := resourcesToTrack.Get(resource_info.GPUIndex)
	if current <= 0 {
		// Mirror of dedupSharedDRAGpus: a task that contributed no GPUs never
		// incremented the reference count, so it must not decrement it here.
		return
	}

	stillShared := 0.0
	for _, key := range ni.allocatedGPUDeviceKeys(task) {
		if ni.DRASharedDeviceRefCount[key] > 1 {
			// The device stays referenced by another pod after this removal:
			// it must remain in the used vector, so this task's removal must
			// not subtract it.
			stillShared++
		}
		if ni.DRASharedDeviceRefCount[key] > 0 {
			ni.DRASharedDeviceRefCount[key]--
		}
		if ni.DRASharedDeviceRefCount[key] == 0 {
			delete(ni.DRASharedDeviceRefCount, key)
		}
	}

	if stillShared > current {
		// Never add back more than the task's own GPU contribution.
		stillShared = current
	}
	if stillShared > 0 {
		resourcesToTrack.Set(resource_info.GPUIndex, current-stillShared)
	}
}
