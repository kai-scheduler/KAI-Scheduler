// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
)

const terminatingPodUID = "terminating"

var releasingDevice = structured.MakeDeviceID("driver", "node0", "0")

func deviceAllocation() *resourceapi.AllocationResult {
	return &resourceapi.AllocationResult{
		Devices: resourceapi.DeviceAllocationResult{
			Results: []resourceapi.DeviceRequestAllocationResult{{Driver: "driver", Pool: "node0", Device: "0"}},
		},
	}
}

func testClaim(name string, allocation *resourceapi.AllocationResult, consumers ...string) *resourceapi.ResourceClaim {
	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "test", UID: types.UID(name), ResourceVersion: "1",
		},
		Status: resourceapi.ResourceClaimStatus{Allocation: allocation},
	}
	for _, consumer := range consumers {
		claim.Status.ReservedFor = append(claim.Status.ReservedFor,
			resourceapi.ResourceClaimConsumerReference{Resource: "pods", Name: consumer, UID: types.UID(consumer)})
	}
	return claim
}

func testPod(name string, claimNames ...string) *v1.Pod {
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name)}}
	for _, claimName := range claimNames {
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims,
			v1.PodResourceClaim{Name: claimName, ResourceClaimName: ptr.To(claimName)})
	}
	return pod
}

func newTestDRAOverlay(t *testing.T, claims ...*resourceapi.ResourceClaim) (*draOverlay, *k8s_utils.SessionClaimTracker) {
	manager := k8s_utils.NewSessionDRAManager(claims, nil,
		informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0))
	tracker := manager.ResourceClaims().(*k8s_utils.SessionClaimTracker)
	// The tracker's allocated set is populated asynchronously.
	require.Eventually(t, func() bool {
		state, err := tracker.GatherAllocatedState()
		return err == nil && state.AllocatedDevices.Has(releasingDevice)
	}, time.Second, time.Millisecond)
	return &draOverlay{
		terminating: &terminatingPods{uids: sets.New[types.UID](terminatingPodUID)},
		claims:      tracker,
		releasing:   sets.New(releasingDevice),
	}, tracker
}

func allocatedDevices(t *testing.T, overlay *draOverlay, pod *v1.Pod, future bool) sets.Set[structured.DeviceID] {
	state, err := overlay.allocatedState(pod, future)
	require.NoError(t, err)
	return state.AllocatedDevices
}

func TestDRAOverlayAllocatedState(t *testing.T) {
	pending := testPod("pending", "pending-claim")

	t.Run("current view keeps a releasing device, future view frees it", func(t *testing.T) {
		overlay, _ := newTestDRAOverlay(t, testClaim("releasing-claim", deviceAllocation(), terminatingPodUID))
		assert.True(t, allocatedDevices(t, overlay, pending, false).Has(releasingDevice))
		assert.False(t, allocatedDevices(t, overlay, pending, true).Has(releasingDevice))
	})

	t.Run("a claim allocated in the session holds the device until rolled back", func(t *testing.T) {
		overlay, tracker := newTestDRAOverlay(t,
			testClaim("releasing-claim", deviceAllocation(), terminatingPodUID), testClaim("pending-claim", nil))
		require.NoError(t, tracker.AssumeClaimAfterAPICall(testClaim("pending-claim", deviceAllocation(), "pending")))
		assert.True(t, allocatedDevices(t, overlay, testPod("other"), true).Has(releasingDevice))

		require.NoError(t, tracker.AssumeClaimAfterAPICall(testClaim("pending-claim", nil)))
		// The tracker dropped the device with the rolled back claim; the releasing claim still holds it.
		assert.True(t, allocatedDevices(t, overlay, pending, false).Has(releasingDevice))
		assert.False(t, allocatedDevices(t, overlay, pending, true).Has(releasingDevice))
	})

	t.Run("an in-flight allocation holds the device", func(t *testing.T) {
		overlay, tracker := newTestDRAOverlay(t, testClaim("releasing-claim", deviceAllocation(), terminatingPodUID))
		extended := testClaim("extended", deviceAllocation(), "pending")
		require.NoError(t, tracker.SignalClaimPendingAllocation(extended.UID, extended))
		assert.True(t, allocatedDevices(t, overlay, pending, true).Has(releasingDevice))
	})

	t.Run("a releasing claim joined by a running consumer holds the device", func(t *testing.T) {
		overlay, tracker := newTestDRAOverlay(t, testClaim("shared-claim", deviceAllocation(), terminatingPodUID))
		require.NoError(t, tracker.AssumeClaimAfterAPICall(
			testClaim("shared-claim", deviceAllocation(), terminatingPodUID, "pending")))
		assert.True(t, allocatedDevices(t, overlay, testPod("other"), true).Has(releasingDevice))
	})

	t.Run("the pod's own allocated claim keeps the device from its other claims", func(t *testing.T) {
		overlay, _ := newTestDRAOverlay(t, testClaim("shared-claim", deviceAllocation(), terminatingPodUID))
		assert.True(t, allocatedDevices(t, overlay, testPod("pending", "shared-claim", "own-claim"), true).Has(releasingDevice))
	})
}

func TestDRAOverlayIsFreedWithTerminatingPods(t *testing.T) {
	overlay := &draOverlay{
		terminating: &terminatingPods{uids: sets.New[types.UID](terminatingPodUID)},
	}
	assert.True(t, overlay.isFreedWithTerminatingPods(testClaim("standalone", deviceAllocation(), terminatingPodUID)))
	assert.False(t, overlay.isFreedWithTerminatingPods(testClaim("unreserved", deviceAllocation())))
	assert.False(t, overlay.isFreedWithTerminatingPods(testClaim("unallocated", nil, terminatingPodUID)))
	assert.False(t, overlay.isFreedWithTerminatingPods(
		testClaim("shared", deviceAllocation(), terminatingPodUID, "running")))
}
