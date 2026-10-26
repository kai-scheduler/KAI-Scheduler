// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/structured"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/ptr"
)

// DeviceHolder is a claim allocated a device in the session: a claim of the cache, or an
// in-flight allocation signaled with SignalClaimPendingAllocation.
type DeviceHolder struct {
	Claim    *resourceapi.ResourceClaim
	InFlight bool
}

// holderKey identifies a cache claim by namespace/name, and an in-flight allocation by claim UID.
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
// drops the device although the other claim still holds it.
type SessionClaimTracker struct {
	ksf.ResourceClaimTracker

	mutex    sync.RWMutex
	inFlight map[types.UID]*resourceapi.ResourceClaim
	// holders is built on first use, so sessions that never ask pay nothing.
	holders      map[structured.DeviceID]map[holderKey]*resourceapi.ResourceClaim
	claimDevices map[holderKey][]structured.DeviceID
}

type sessionDRAManager struct {
	ksf.SharedDRAManager
	claims *SessionClaimTracker
}

func (m *sessionDRAManager) ResourceClaims() ksf.ResourceClaimTracker {
	return m.claims
}

func newSessionDRAManager(manager ksf.SharedDRAManager) ksf.SharedDRAManager {
	return &sessionDRAManager{
		SharedDRAManager: manager,
		claims: &SessionClaimTracker{
			ResourceClaimTracker: manager.ResourceClaims(),
			inFlight:             map[types.UID]*resourceapi.ResourceClaim{},
		},
	}
}

// DeviceHolders returns the claims allocated to the device.
func (t *SessionClaimTracker) DeviceHolders(device structured.DeviceID) []DeviceHolder {
	t.ensureIndex()
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	holders := make([]DeviceHolder, 0, len(t.holders[device]))
	for key, claim := range t.holders[device] {
		holders = append(holders, DeviceHolder{Claim: claim, InFlight: key.inFlight})
	}
	return holders
}

func (t *SessionClaimTracker) AssumeClaimAfterAPICall(claim *resourceapi.ResourceClaim) error {
	if err := t.ResourceClaimTracker.AssumeClaimAfterAPICall(claim); err != nil {
		return err
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.indexClaim(cacheClaimKey(claim), claim)
	return nil
}

func (t *SessionClaimTracker) AssumedClaimRestore(namespace, claimName string) {
	t.ResourceClaimTracker.AssumedClaimRestore(namespace, claimName)
	claim, err := t.Get(namespace, claimName)
	if err != nil {
		return
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.indexClaim(cacheClaimKey(claim), claim)
}

func (t *SessionClaimTracker) SignalClaimPendingAllocation(
	claimUID types.UID, allocatedClaim *resourceapi.ResourceClaim,
) error {
	if err := t.ResourceClaimTracker.SignalClaimPendingAllocation(claimUID, allocatedClaim); err != nil {
		return err
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.inFlight[claimUID] = allocatedClaim
	t.indexClaim(inFlightKey(claimUID), allocatedClaim)
	return nil
}

func (t *SessionClaimTracker) RemoveClaimPendingAllocation(claimUID types.UID) bool {
	deleted := t.ResourceClaimTracker.RemoveClaimPendingAllocation(claimUID)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	delete(t.inFlight, claimUID)
	t.indexClaim(inFlightKey(claimUID), nil)
	return deleted
}

func (t *SessionClaimTracker) ensureIndex() {
	t.mutex.RLock()
	built := t.holders != nil
	t.mutex.RUnlock()
	if built {
		return
	}
	claims, err := t.List()
	if err != nil {
		return
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.holders != nil {
		return
	}
	t.holders = map[structured.DeviceID]map[holderKey]*resourceapi.ResourceClaim{}
	t.claimDevices = map[holderKey][]structured.DeviceID{}
	for _, claim := range claims {
		t.indexClaim(cacheClaimKey(claim), claim)
	}
	for uid, claim := range t.inFlight {
		t.indexClaim(inFlightKey(uid), claim)
	}
}

// indexClaim replaces the devices indexed for the key with those of the claim; nil unindexes it.
func (t *SessionClaimTracker) indexClaim(key holderKey, claim *resourceapi.ResourceClaim) {
	if t.holders == nil {
		return
	}
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
