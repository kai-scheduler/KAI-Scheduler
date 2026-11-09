// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/ptr"
)

type holderKey struct {
	id       string
	inFlight bool
}

func cacheClaimKey(claim *resourceapi.ResourceClaim) holderKey {
	return holderKey{id: claim.Namespace + "/" + claim.Name}
}

func inFlightKey(claimUID types.UID) holderKey {
	return holderKey{id: string(claimUID), inFlight: true}
}

// SessionClaimTracker indexes, per dedicated device, the claims allocated to it. The upstream
// allocated set is not reference counted: un-assuming one of two claims allocated to a device
// drops the device although the other claim still holds it. It is used by a single session, so
// it needs no locking.
type SessionClaimTracker struct {
	ksf.ResourceClaimTracker

	holders      map[structured.DeviceID]map[holderKey]*resourceapi.ResourceClaim
	claimDevices map[holderKey][]structured.DeviceID
	// shared are the devices that had more than one holder: the only ones the upstream set can drop.
	shared sets.Set[structured.DeviceID]
}

type sessionDRAManager struct {
	ksf.SharedDRAManager
	claims *SessionClaimTracker
}

func (m *sessionDRAManager) ResourceClaims() ksf.ResourceClaimTracker {
	return m.claims
}

func newSessionDRAManager(manager ksf.SharedDRAManager) ksf.SharedDRAManager {
	tracker := &SessionClaimTracker{
		ResourceClaimTracker: manager.ResourceClaims(),
		holders:              map[structured.DeviceID]map[holderKey]*resourceapi.ResourceClaim{},
		claimDevices:         map[holderKey][]structured.DeviceID{},
		shared:               sets.New[structured.DeviceID](),
	}
	if claims, err := tracker.List(); err == nil {
		for _, claim := range claims {
			tracker.indexClaim(cacheClaimKey(claim), claim)
		}
	}
	return &sessionDRAManager{SharedDRAManager: manager, claims: tracker}
}

func (t *SessionClaimTracker) GatherAllocatedState() (*structured.AllocatedState, error) {
	state, err := t.ResourceClaimTracker.GatherAllocatedState()
	if err != nil {
		return nil, err
	}
	t.insertHeldSharedDevices(state.AllocatedDevices)
	return state, nil
}

func (t *SessionClaimTracker) ListAllAllocatedDevices() (sets.Set[structured.DeviceID], error) {
	devices, err := t.ResourceClaimTracker.ListAllAllocatedDevices()
	if err != nil {
		return nil, err
	}
	t.insertHeldSharedDevices(devices)
	return devices, nil
}

func (t *SessionClaimTracker) insertHeldSharedDevices(devices sets.Set[structured.DeviceID]) {
	for device := range t.shared {
		if len(t.holders[device]) > 0 {
			devices.Insert(device)
		}
	}
}

func (t *SessionClaimTracker) AssumeClaimAfterAPICall(claim *resourceapi.ResourceClaim) error {
	if err := t.ResourceClaimTracker.AssumeClaimAfterAPICall(claim); err != nil {
		return err
	}
	t.indexClaim(cacheClaimKey(claim), claim)
	return nil
}

func (t *SessionClaimTracker) AssumedClaimRestore(namespace, claimName string) {
	t.ResourceClaimTracker.AssumedClaimRestore(namespace, claimName)
	claim, err := t.Get(namespace, claimName)
	if err != nil {
		return
	}
	t.indexClaim(cacheClaimKey(claim), claim)
}

func (t *SessionClaimTracker) SignalClaimPendingAllocation(
	claimUID types.UID, allocatedClaim *resourceapi.ResourceClaim,
) error {
	if err := t.ResourceClaimTracker.SignalClaimPendingAllocation(claimUID, allocatedClaim); err != nil {
		return err
	}
	t.indexClaim(inFlightKey(claimUID), allocatedClaim)
	return nil
}

func (t *SessionClaimTracker) RemoveClaimPendingAllocation(claimUID types.UID) bool {
	deleted := t.ResourceClaimTracker.RemoveClaimPendingAllocation(claimUID)
	t.indexClaim(inFlightKey(claimUID), nil)
	return deleted
}

func (t *SessionClaimTracker) indexClaim(key holderKey, claim *resourceapi.ResourceClaim) {
	for _, device := range t.claimDevices[key] {
		delete(t.holders[device], key)
		if len(t.holders[device]) == 0 {
			delete(t.holders, device)
		}
	}
	delete(t.claimDevices, key)

	devices := DedicatedDevices(claim)
	if len(devices) == 0 {
		return
	}
	t.claimDevices[key] = devices
	for _, device := range devices {
		if t.holders[device] == nil {
			t.holders[device] = map[holderKey]*resourceapi.ResourceClaim{}
		}
		t.holders[device][key] = claim
		if len(t.holders[device]) > 1 {
			t.shared.Insert(device)
		}
	}
}

// DedicatedDevices returns the devices the claim's allocation takes exclusively. Admin access
// does not take a device, and consumable capacity shares are tracked by the allocator separately.
func DedicatedDevices(claim *resourceapi.ResourceClaim) []structured.DeviceID {
	if claim == nil || claim.Status.Allocation == nil {
		return nil
	}
	var devices []structured.DeviceID
	for _, result := range claim.Status.Allocation.Devices.Results {
		if ptr.Deref(result.AdminAccess, false) || result.ShareID != nil {
			continue
		}
		devices = append(devices, structured.MakeDeviceID(result.Driver, result.Pool, result.Device))
	}
	return devices
}
