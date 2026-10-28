// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"slices"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// releasingDevices are the devices allocated only to claims of releasing pods. They are freed once
// those pods exit, so pipeline checks treat them as free unless a claim allocated in this session
// has taken them. Built once per session, so a check costs O(releasing devices), not O(claims).
type releasingDevices struct {
	devices sets.Set[structured.DeviceID]
	holders map[structured.DeviceID]int
	// shared is set when a releasing device is in a pool reachable from more than one node.
	shared bool
}

type devicePool struct {
	driver string
	name   string
}

func newReleasingDevices(
	claims []*resourceapi.ResourceClaim, slices []*resourceapi.ResourceSlice, releasingPods *framework.ReleasingPods,
) *releasingDevices {
	r := &releasingDevices{
		devices: sets.New[structured.DeviceID](),
		holders: map[structured.DeviceID]int{},
	}
	if releasingPods.IsEmpty() {
		return r
	}
	sharedPools := sets.New[devicePool]()
	for _, slice := range slices {
		if ptr.Deref(slice.Spec.NodeName, "") == "" {
			sharedPools.Insert(devicePool{driver: slice.Spec.Driver, name: slice.Spec.Pool.Name})
		}
	}
	for _, claim := range claims {
		if claim.Status.Allocation == nil || !isReservedOnlyFor(claim, releasingPods) ||
			!isFreedWithConsumers(claim, releasingPods) {
			continue
		}
		// Shared (consumable capacity) allocations stay allocated: pipelining onto them is not supported.
		for _, result := range claim.Status.Allocation.Devices.Results {
			if ptr.Deref(result.AdminAccess, false) || result.ShareID != nil {
				continue
			}
			r.devices.Insert(structured.MakeDeviceID(result.Driver, result.Pool, result.Device))
			if sharedPools.Has(devicePool{driver: result.Driver, name: result.Pool}) {
				r.shared = true
			}
		}
	}
	return r
}

// isFreedWithConsumers reports whether the claim's devices are freed once its consumers exit. The
// claim controller deallocates claims carrying its finalizer, and deletes claims generated for a
// pod together with the pod. Other claims stay allocated after their last consumer leaves.
func isFreedWithConsumers(claim *resourceapi.ResourceClaim, releasingPods *framework.ReleasingPods) bool {
	if slices.Contains(claim.Finalizers, resourceapi.Finalizer) {
		return true
	}
	owner := metav1.GetControllerOf(claim)
	return owner != nil && owner.APIVersion == "v1" && owner.Kind == "Pod" && releasingPods.Has(owner.UID)
}

func isReservedOnlyFor(claim *resourceapi.ResourceClaim, releasingPods *framework.ReleasingPods) bool {
	if len(claim.Status.ReservedFor) == 0 {
		return false
	}
	for _, consumer := range claim.Status.ReservedFor {
		if consumer.APIGroup != "" || consumer.Resource != "pods" || !releasingPods.Has(consumer.UID) {
			return false
		}
	}
	return true
}

// hold records that a claim allocated in this session uses the allocation's releasing devices.
func (r *releasingDevices) hold(allocation *resourceapi.AllocationResult) {
	r.forEachReleasingDevice(allocation, func(device structured.DeviceID) {
		r.holders[device]++
	})
}

// release undoes hold, when the pod that took the allocation's releasing devices leaves its claim.
func (r *releasingDevices) release(allocation *resourceapi.AllocationResult) {
	r.forEachReleasingDevice(allocation, func(device structured.DeviceID) {
		if r.holders[device] > 0 {
			r.holders[device]--
		}
	})
}

func (r *releasingDevices) forEachReleasingDevice(
	allocation *resourceapi.AllocationResult, fn func(device structured.DeviceID),
) {
	if r == nil || allocation == nil || r.devices.Len() == 0 {
		return
	}
	for _, result := range allocation.Devices.Results {
		// Admin access does not consume the device.
		if ptr.Deref(result.AdminAccess, false) {
			continue
		}
		device := structured.MakeDeviceID(result.Driver, result.Pool, result.Device)
		if r.devices.Has(device) {
			fn(device)
		}
	}
}

// apply adjusts the claim tracker's allocated devices to the predicate mode.
func (r *releasingDevices) apply(state *structured.AllocatedState, mode api.PredicateMode) {
	if r == nil || r.devices.Len() == 0 {
		return
	}
	// The tracker does not reference-count devices: dropping a pipelined claim that took a releasing
	// device also drops the device, although the releasing pod's claim still holds it.
	for device := range r.devices {
		if mode == api.PipelinePredicate && r.holders[device] == 0 {
			state.AllocatedDevices.Delete(device)
		} else {
			state.AllocatedDevices.Insert(device)
		}
	}
}
