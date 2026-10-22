// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"fmt"
	"maps"
	"slices"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
)

const (
	podAPIGroup = ""
	podResource = "pods"
)

// gatherAllocatedState returns the devices a new allocation must avoid. A pipeline allocation also
// gets the devices held only by terminating pods on the node, since they are freed once those pods exit.
func (drap *draPlugin) gatherAllocatedState(nodeInfo *node_info.NodeInfo, isPipeline bool) (*structured.AllocatedState, error) {
	allocatedState, err := drap.manager.ResourceClaims().GatherAllocatedState()
	if err != nil {
		return nil, fmt.Errorf("failed to gather allocated device state: %v", err)
	}
	// The tracker's device set is not reference counted: unpipelining a claim that reused a releasing
	// device drops the device from the tracker, although the terminating pod still holds it until it exits.
	allocatedState.AllocatedDevices.Insert(drap.releasingDevices.UnsortedList()...)
	if !isPipeline {
		return allocatedState, nil
	}

	// If the pod allocation is a pipeline, we need to check for terminating pods and claims that are reserved for them.
	terminatingPods := terminatingPodUIDs(nodeInfo)
	if len(terminatingPods) == 0 {
		return allocatedState, nil
	}

	allocationClaims, err := drap.claimsIncludingInFlightAllocations()
	if err != nil {
		return nil, err
	}

	// A device can still be held by a live claim, e.g. one pipelined earlier onto a releasing device.
	devicesInUse := sets.New[structured.DeviceID]()
	var releasingClaims []*resourceapi.ResourceClaim
	for _, claim := range allocationClaims {
		if claim.Status.Allocation == nil {
			continue
		}
		if isReservedOnlyFor(claim, terminatingPods) {
			releasingClaims = append(releasingClaims, claim)
			continue
		}
		for _, result := range claim.Status.Allocation.Devices.Results {
			if ptr.Deref(result.AdminAccess, false) {
				continue
			}
			devicesInUse.Insert(structured.MakeDeviceID(result.Driver, result.Pool, result.Device))
		}
	}

	for _, claim := range releasingClaims {
		drap.releaseClaimDevices(allocatedState, claim, devicesInUse)
	}
	return allocatedState, nil
}

// releaseClaimDevices mirrors how the upstream claim tracker accounts a claim's devices, while taking into account pipeline logic.
func (drap *draPlugin) releaseClaimDevices(
	allocatedState *structured.AllocatedState, claim *resourceapi.ResourceClaim, devicesInUse sets.Set[structured.DeviceID],
) {
	for _, result := range claim.Status.Allocation.Devices.Results {
		if ptr.Deref(result.AdminAccess, false) {
			continue
		}
		deviceID := structured.MakeDeviceID(result.Driver, result.Pool, result.Device)
		if drap.allocatorFeatures.ConsumableCapacity && result.ShareID != nil {
			allocatedState.AllocatedSharedDeviceIDs.Delete(structured.MakeSharedDeviceID(deviceID, result.ShareID))
			if result.ConsumedCapacity != nil {
				allocatedState.AggregatedCapacity.Remove(structured.NewDeviceConsumedCapacity(deviceID, result.ConsumedCapacity))
			}
			continue
		}
		drap.releasingDevices.Insert(deviceID) // add the device to the list of devices related only to releasing pods
		if !devicesInUse.Has(deviceID) {
			allocatedState.AllocatedDevices.Delete(deviceID)
		}
	}
}

// claimsIncludingInFlightAllocations lists the session claims, overridden by the in-flight allocations
// signaled by this plugin, which the claim tracker does not expose.
func (drap *draPlugin) claimsIncludingInFlightAllocations() ([]*resourceapi.ResourceClaim, error) {
	listed, err := drap.manager.ResourceClaims().List()
	if err != nil {
		return nil, fmt.Errorf("failed to list resource claims: %v", err)
	}
	claims := slices.Collect(maps.Values(drap.inFlightClaims))
	for _, claim := range listed {
		if _, inFlight := drap.inFlightClaims[claim.UID]; !inFlight {
			claims = append(claims, claim)
		}
	}
	return claims, nil
}

func (drap *draPlugin) signalClaimPendingAllocation(claim *resourceapi.ResourceClaim) error {
	if err := drap.manager.ResourceClaims().SignalClaimPendingAllocation(claim.UID, claim); err != nil {
		return err
	}
	drap.inFlightClaims[claim.UID] = claim
	return nil
}

func (drap *draPlugin) removeClaimPendingAllocation(claimUID types.UID) {
	drap.manager.ResourceClaims().RemoveClaimPendingAllocation(claimUID)
	delete(drap.inFlightClaims, claimUID)
}

func terminatingPodUIDs(nodeInfo *node_info.NodeInfo) sets.Set[types.UID] {
	terminating := sets.New[types.UID]()
	for _, podInfo := range nodeInfo.PodInfos {
		if podInfo.Pod != nil && podInfo.Pod.DeletionTimestamp != nil {
			terminating.Insert(podInfo.Pod.UID)
		}
	}
	return terminating
}

// isReservedOnlyFor checks if the claim is reserved only for the given pods.
func isReservedOnlyFor(claim *resourceapi.ResourceClaim, pods sets.Set[types.UID]) bool {
	if len(claim.Status.ReservedFor) == 0 {
		return false
	}
	for _, consumer := range claim.Status.ReservedFor {
		if consumer.APIGroup != podAPIGroup || consumer.Resource != podResource || !pods.Has(consumer.UID) {
			return false
		}
	}
	return true
}
