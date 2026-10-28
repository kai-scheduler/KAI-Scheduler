// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
)

// Holds on an already-allocated claim's releasing devices from pods with a BindRequest.
func TestAssumePendingClaimHoldsReleasingDevicesOfAllocatedClaim(t *testing.T) {
	for _, test := range []struct {
		name        string
		podUID      string
		wantHolders int
	}{
		{
			// Not in ReservedFor yet, so the claim looks held only by the releasing pod.
			name:        "in-flight consumer holds the device",
			podUID:      "consumer",
			wantHolders: 1,
		},
		{
			// Successful BindRequests stay attached to bound pods, including terminating ones.
			name:        "releasing pod already in ReservedFor does not hold its own device",
			podUID:      "holder",
			wantHolders: 0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			device := structured.MakeDeviceID("driver", "node0", "0")
			allocation := &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{
					Results: []resourceapi.DeviceRequestAllocationResult{{Driver: "driver", Pool: "node0", Device: "0"}},
				},
			}
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "shared-claim", Namespace: "test", UID: "shared-claim-uid"},
				Status: resourceapi.ResourceClaimStatus{
					Allocation: allocation,
					ReservedFor: []resourceapi.ResourceClaimConsumerReference{
						{Resource: "pods", Name: "holder", UID: "holder"},
					},
				},
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: test.podUID, Namespace: "test", UID: types.UID(test.podUID)},
				Spec: v1.PodSpec{ResourceClaims: []v1.PodResourceClaim{
					{Name: "shared-claim", ResourceClaimName: ptr.To("shared-claim")},
				}},
			}

			drap := &draPlugin{
				manager: k8s_utils.NewSessionDRAManager([]*resourceapi.ResourceClaim{claim}, nil,
					informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)),
				releasingDevices: &releasingDevices{
					devices: sets.New(device),
					holders: map[structured.DeviceID]int{},
				},
			}

			err := drap.assumePendingClaim(&schedulingv1alpha2.ResourceClaimAllocation{
				Name:       "shared-claim",
				Allocation: allocation,
			}, pod)
			assert.NoError(t, err)
			assert.Equal(t, test.wantHolders, drap.releasingDevices.holders[device])
		})
	}
}
