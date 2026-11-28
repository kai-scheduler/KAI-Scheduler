// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/structured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestAssumePendingExtendedClaim(t *testing.T) {
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "test", UID: "pod-a-uid"}}
	allocation := &resourceapi.AllocationResult{
		Devices: resourceapi.DeviceAllocationResult{
			Results: []resourceapi.DeviceRequestAllocationResult{
				{Request: "nic", Driver: "nic.example.com", Pool: "node", Device: "nic-0"},
			},
		},
	}
	claim := buildSpecialClaim(pod)
	claim.Name = "pod-a-extended-resources-abc"
	persistedClaim := claim.DeepCopy()
	persistedClaim.Status.Allocation = allocation

	for _, tc := range []struct {
		name    string
		claim   *resourceapi.ResourceClaim
		phase   v1.PodPhase
		pending bool
	}{
		{name: "claim not created", pending: true},
		{name: "claim created without allocation", claim: claim, pending: true},
		{name: "allocation already in snapshot", claim: persistedClaim},
		{name: "pod terminated", phase: v1.PodSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var claims []*resourceapi.ResourceClaim
			if tc.claim != nil {
				claims = append(claims, tc.claim)
			}
			testPod := pod.DeepCopy()
			testPod.Status.Phase = tc.phase
			bindRequest := &schedulingv1alpha2.BindRequest{
				Spec: schedulingv1alpha2.BindRequestSpec{
					ExtendedResourceClaimAllocation: &schedulingv1alpha2.ExtendedResourceClaimAllocation{
						Allocation: allocation,
					},
				},
			}
			task := pod_info.NewTaskInfo(testPod, resource_info.NewResourceVectorMap(), pod_info.TaskInfoOptions{
				BindRequest: bindrequest_info.NewBindRequestInfo(bindRequest),
			})
			groupID := common_info.NewPodGroupID(pod.Namespace, "group")
			ssn := &framework.Session{ClusterInfo: &api.ClusterInfo{
				ResourceClaims: claims,
				PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
					groupID: podgroup_info.NewPodGroupInfo(groupID, task),
				},
			}}
			factory := informers.NewSharedInformerFactory(fake.NewClientset(), 0)
			plugin := &draPlugin{manager: k8s_utils.NewSessionDRAManager(claims, nil, factory)}
			plugin.assumePendingClaims(ssn)

			tracker := plugin.manager.ResourceClaims()
			state, err := tracker.GatherAllocatedState()
			require.NoError(t, err)
			device := structured.MakeDeviceID("nic.example.com", "node", "nic-0")
			require.Equal(t, tc.phase != v1.PodSucceeded, state.AllocatedDevices.Has(device))
			if tc.claim != nil {
				require.Equal(t, tc.pending, tracker.ClaimHasPendingAllocation(tc.claim.UID))
			}
		})
	}
}
