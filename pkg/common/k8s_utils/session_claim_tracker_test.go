// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"
)

func TestSessionClaimTrackerDeviceHolders(t *testing.T) {
	device := structured.MakeDeviceID("driver", "node0", "0")
	allocation := func(adminAccess bool) *resourceapi.AllocationResult {
		return &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{
				Results: []resourceapi.DeviceRequestAllocationResult{
					{Driver: "driver", Pool: "node0", Device: "0", AdminAccess: ptr.To(adminAccess)},
				},
			},
		}
	}
	claim := func(name string, allocation *resourceapi.AllocationResult) *resourceapi.ResourceClaim {
		return &resourceapi.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name), ResourceVersion: "1"},
			Status:     resourceapi.ResourceClaimStatus{Allocation: allocation},
		}
	}
	holderNames := func(tracker *SessionClaimTracker) []string {
		var names []string
		for _, holder := range tracker.DeviceHolders(device) {
			names = append(names, holder.Claim.Name)
		}
		return names
	}

	releasing := claim("releasing", allocation(false))
	pending := claim("pending", nil)
	admin := claim("admin", allocation(true))
	manager := NewSessionDRAManager([]*resourceapi.ResourceClaim{releasing, pending, admin}, nil,
		informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0))
	tracker, ok := manager.ResourceClaims().(*SessionClaimTracker)
	require.True(t, ok)

	assert.ElementsMatch(t, []string{"releasing"}, holderNames(tracker), "admin access does not hold a device")

	pipelined := pending.DeepCopy()
	pipelined.Status.Allocation = allocation(false)
	require.NoError(t, tracker.AssumeClaimAfterAPICall(pipelined))
	assert.ElementsMatch(t, []string{"releasing", "pending"}, holderNames(tracker))

	// Un-assuming one claim keeps the other holder, unlike the upstream allocated set.
	tracker.AssumedClaimRestore("test", "pending")
	assert.ElementsMatch(t, []string{"releasing"}, holderNames(tracker))

	inFlight := claim("in-flight", allocation(false))
	require.NoError(t, tracker.SignalClaimPendingAllocation(inFlight.UID, inFlight))
	holders := tracker.DeviceHolders(device)
	require.Len(t, holders, 2)
	for _, holder := range holders {
		assert.Equal(t, holder.Claim.Name == "in-flight", holder.InFlight)
	}

	tracker.RemoveClaimPendingAllocation(inFlight.UID)
	assert.ElementsMatch(t, []string{"releasing"}, holderNames(tracker))
}
