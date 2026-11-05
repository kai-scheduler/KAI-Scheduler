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

func TestSessionClaimTrackerKeepsSharedDevices(t *testing.T) {
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
	isAllocated := func(tracker *SessionClaimTracker) bool {
		state, err := tracker.GatherAllocatedState()
		require.NoError(t, err)
		return state.AllocatedDevices.Has(device)
	}

	releasing := claim("releasing", allocation(false))
	pending := claim("pending", nil)
	admin := claim("admin", allocation(true))
	manager := NewSessionDRAManager([]*resourceapi.ResourceClaim{releasing, pending, admin}, nil,
		informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0))
	tracker, ok := manager.ResourceClaims().(*SessionClaimTracker)
	require.True(t, ok)

	// A pipelined claim takes the device of a claim that is deallocated and then restored, as a
	// terminating pod's claim is around a check without the terminating pods.
	deallocated := releasing.DeepCopy()
	deallocated.Status.Allocation = nil
	require.NoError(t, tracker.AssumeClaimAfterAPICall(deallocated))
	assert.False(t, isAllocated(tracker))
	pipelined := pending.DeepCopy()
	pipelined.Status.Allocation = allocation(false)
	require.NoError(t, tracker.AssumeClaimAfterAPICall(pipelined))
	require.NoError(t, tracker.AssumeClaimAfterAPICall(releasing.DeepCopy()))

	// Un-assuming one holder keeps the device allocated, unlike the upstream allocated set.
	tracker.AssumedClaimRestore("test", "pending")
	assert.True(t, isAllocated(tracker), "the remaining holder keeps the device allocated")

	require.NoError(t, tracker.AssumeClaimAfterAPICall(deallocated))
	assert.False(t, isAllocated(tracker), "no claim holds the device")
}
