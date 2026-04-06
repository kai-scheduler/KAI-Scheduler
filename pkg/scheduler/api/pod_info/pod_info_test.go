/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"reflect"
	"testing"

	"gotest.tools/assert"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
)

func TestGetPodResourceRequest(t *testing.T) {
	tests := []struct {
		name             string
		pod              *v1.Pod
		expectedResource *resource_info.ResourceRequirements
	}{
		{
			name: "get resource for pod without init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: resource_info.RequirementsFromResourceList(common_info.BuildResourceList("3000m", "2G")),
		},
		{
			name: "get resource for pod with init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "5G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: resource_info.RequirementsFromResourceList(common_info.BuildResourceList("3000m", "5G")),
		},
		{
			name: "get resource for pod with init containers and gpus",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "5G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: resource_info.NewResourceRequirements(1, 3000, 5000000000),
		},
		{
			name: "pod with overhead resources",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
					Overhead: common_info.BuildResourceList("1000m", "1G"),
				},
			},
			expectedResource: resource_info.NewResourceRequirements(1, 4000, 3000000000),
		},
	}
	for i, test := range tests {
		if _, exists := test.expectedResource.ScalarResources()[resource_info.PodsResourceName]; !exists {
			test.expectedResource.ScalarResources()[resource_info.PodsResourceName] = 1
		}

		req := getPodResourceRequest(test.pod)
		if !reflect.DeepEqual(req, test.expectedResource) {
			t.Errorf("case %d(%s) failed: \n expected %v, \n got: %v \n",
				i, test.name, test.expectedResource, req)
		}
	}
}

func TestGetPodResourceWithoutInitContainers(t *testing.T) {
	tests := []struct {
		name             string
		pod              *v1.Pod
		expectedResource *resource_info.ResourceRequirements
	}{
		{
			name: "get resource for pod without init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: resource_info.RequirementsFromResourceList(common_info.BuildResourceList("3000m", "2G")),
		},
		{
			name: "get resource for pod with init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "5G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: common_info.BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: resource_info.RequirementsFromResourceList(common_info.BuildResourceList("3000m", "2G")),
		},
	}

	for i, test := range tests {
		req := getPodResourceWithoutInitContainers(test.pod)
		if !reflect.DeepEqual(req, test.expectedResource) {
			t.Errorf("case %d(%s) failed: \n expected %v, \n got: %v \n",
				i, test.name, test.expectedResource, req)
		}
	}
}

func TestPodInfo_updatePodAdditionalFields(t *testing.T) {
	type podFields struct {
		Job            common_info.PodGroupID
		Name           string
		Namespace      string
		Status         pod_status.PodStatus
		Pod            *v1.Pod
		bindingRequest *bindrequest_info.BindRequestInfo
	}
	type expected struct {
		// Resreq are the resources that used when pod is running. (main containers resources, no init containers)
		Resreq *resource_info.ResourceRequirements
		// ResReq are the minimal resources that needed to launch a pod. (includes init containers resources)
		InitResreq           *resource_info.ResourceRequirements
		AcceptedResource     *resource_info.ResourceRequirements
		ResourceRequestType  string
		ResourceReceivedType string
		GPUGroups            []string
		SelectedMigProfile   string
		IsBound              bool
		IsChiefPod           bool
	}
	tests := []struct {
		name     string
		fields   podFields
		expected expected
	}{
		{
			"No gpu",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceList("2000m", "2G"),
					nil,
					map[string]string{},
					map[string]string{}),
			},
			expected{
				Resreq:              resource_info.EmptyResourceRequirements(),
				InitResreq:          resource_info.EmptyResourceRequirements(),
				AcceptedResource:    nil,
				ResourceRequestType: "",
				GPUGroups:           nil,
				SelectedMigProfile:  "",
				IsBound:             false,
				IsChiefPod:          true,
			},
		},
		{
			"One whole gpu",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceListWithGPU("2000m", "2G", "1"),
					nil,
					map[string]string{},
					map[string]string{}),
			},
			expected{
				Resreq:              resource_info.EmptyResourceRequirements(),
				InitResreq:          resource_info.EmptyResourceRequirements(),
				AcceptedResource:    nil,
				ResourceRequestType: "",
				GPUGroups:           nil,
				SelectedMigProfile:  "",
				IsBound:             false,
				IsChiefPod:          true,
			},
		},
		{
			"Gpu memory request",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceList("2000m", "2G"),
					nil,
					map[string]string{},
					map[string]string{
						GpuMemoryAnnotationName: "1024",
					}),
			},
			expected{
				Resreq: &resource_info.ResourceRequirements{
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(
						0, 1024),
					BaseResource: *resource_info.EmptyBaseResource(),
				},
				InitResreq: &resource_info.ResourceRequirements{
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(
						0, 1024),
					BaseResource: *resource_info.EmptyBaseResource(),
				},
				AcceptedResource:    nil,
				ResourceRequestType: "GpuMemory",
				GPUGroups:           nil,
				SelectedMigProfile:  "",
				IsBound:             false,
				IsChiefPod:          true,
			},
		},
		{
			"Gpu memory multi request",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceList("2000m", "2G"),
					nil,
					map[string]string{},
					map[string]string{
						GpuMemoryAnnotationName:                "1024",
						commonconstants.GpuFractionsNumDevices: "2",
					}),
			},
			expected{
				Resreq: &resource_info.ResourceRequirements{
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithMultiFraction(
						2, 0, 1024),
					BaseResource: *resource_info.EmptyBaseResource(),
				},
				InitResreq: &resource_info.ResourceRequirements{
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithMultiFraction(
						2, 0, 1024),
					BaseResource: *resource_info.EmptyBaseResource(),
				},
				AcceptedResource:    nil,
				ResourceRequestType: "GpuMemory",
				GPUGroups:           nil,
				SelectedMigProfile:  "",
				IsBound:             false,
				IsChiefPod:          true,
			},
		},
		{
			"Regular fraction gpu",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceList("2000m", "2G"),
					nil,
					map[string]string{},
					map[string]string{
						common_info.GPUFraction: "0.5",
					}),
			},
			expected{
				Resreq:              resource_info.NewResourceRequirementsWithGpus(0.5),
				InitResreq:          resource_info.NewResourceRequirementsWithGpus(0.5),
				AcceptedResource:    nil,
				ResourceRequestType: "Fraction",
				GPUGroups:           nil,
				SelectedMigProfile:  "",
				IsBound:             false,
				IsChiefPod:          true,
			},
		},
		{
			"Regular fraction gpu - has binding request",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceList("2000m", "2G"),
					nil,
					map[string]string{},
					map[string]string{
						common_info.GPUFraction: "0.5",
					}),
				bindingRequest: &bindrequest_info.BindRequestInfo{
					BindRequest: &schedulingv1alpha2.BindRequest{
						Spec: schedulingv1alpha2.BindRequestSpec{
							SelectedGPUGroups:    []string{"1"},
							ReceivedResourceType: string(RequestTypeFraction),
						},
					},
				},
			},
			expected{
				Resreq:              resource_info.NewResourceRequirementsWithGpus(0.5),
				InitResreq:          resource_info.NewResourceRequirementsWithGpus(0.5),
				AcceptedResource:    nil,
				ResourceRequestType: "Fraction",
				GPUGroups:           []string{"1"},
				SelectedMigProfile:  "",
				IsBound:             false,
				IsChiefPod:          true,
			},
		},
		{
			"Multi fraction gpu",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceList("2000m", "2G"),
					nil,
					map[string]string{},
					map[string]string{
						common_info.GPUFraction:                "0.5",
						commonconstants.GpuFractionsNumDevices: "3",
					}),
			},
			expected{
				Resreq: &resource_info.ResourceRequirements{
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithMultiFraction(
						3, 0.5, 0),
					BaseResource: *resource_info.EmptyBaseResource(),
				},
				InitResreq: &resource_info.ResourceRequirements{
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithMultiFraction(
						3, 0.5, 0),
					BaseResource: *resource_info.EmptyBaseResource(),
				},
				AcceptedResource:    nil,
				ResourceRequestType: "Fraction",
				GPUGroups:           nil,
				SelectedMigProfile:  "",
				IsBound:             false,
				IsChiefPod:          true,
			},
		},
		{
			"Get ReceivedResourceType from annotation",
			podFields{
				Job:       common_info.FakePogGroupId,
				Name:      "p1",
				Namespace: "ns1",
				Status:    pod_status.Pending,
				Pod: common_info.BuildPod("ns1", "p1", "node1", v1.PodPending,
					common_info.BuildResourceList("2000m", "2G"),
					nil,
					map[string]string{},
					map[string]string{
						ReceivedResourceTypeAnnotationName: "Regular",
					}),
			},
			expected{
				Resreq:               resource_info.EmptyResourceRequirements(),
				InitResreq:           resource_info.EmptyResourceRequirements(),
				AcceptedResource:     nil,
				ResourceReceivedType: "Regular",
				SelectedMigProfile:   "",
				IsBound:              false,
				IsChiefPod:           true,
				GPUGroups:            nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vectorMap := resource_info.NewResourceVectorMap()
			for _, container := range append(tt.fields.Pod.Spec.InitContainers, tt.fields.Pod.Spec.Containers...) {
				vectorMap.AddResourceList(container.Resources.Requests)
			}

			pi := &PodInfo{
				Job:       tt.fields.Job,
				Name:      tt.fields.Name,
				Namespace: tt.fields.Namespace,
				ResReq:    resource_info.EmptyResourceRequirements(),
				Status:    tt.fields.Status,
				Pod:       tt.fields.Pod,
				GPUGroups: make([]string, 0),
				VectorMap: vectorMap,
			}
			pi.updatePodAdditionalFields(tt.fields.bindingRequest)

			if !reflect.DeepEqual(pi.ResReq, tt.expected.InitResreq) {
				t.Errorf("case (%s) failed: ResReq \n expected %v, \n got: %v \n",
					tt.name, tt.expected.InitResreq, pi.ResReq)
			}
			if !reflect.DeepEqual(pi.AcceptedResource, tt.expected.AcceptedResource) {
				t.Errorf("case (%s) failed: AcceptedResource \n expected %v, \n got: %v \n",
					tt.name, tt.expected.AcceptedResource, pi.AcceptedResource)
			}
			assert.Equal(t, string(pi.ResourceRequestType), tt.expected.ResourceRequestType)
			if !reflect.DeepEqual(pi.GPUGroups, tt.expected.GPUGroups) {
				t.Errorf("case (%s) failed: GPUGroups \n expected %v, \n got: %v \n",
					tt.name, tt.expected.GPUGroups, pi.GPUGroups)
			}
		})
	}
}

func TestGetPodStorageClaims(t *testing.T) {
	pod := &PodInfo{
		UID:                "pod-uid",
		Name:               "pod-name",
		Namespace:          "test-namespace",
		ownedStorageClaims: map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
		storageClaims:      map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
	}

	assert.Equal(t, 0, len(pod.GetAllStorageClaims()))
	assert.Equal(t, 0, len(pod.GetOwnedStorageClaims()))

	nonOwnedClaimKey := storageclaim_info.NewKey("test-namespace", "non-owned-pvc-name")
	claim := &storageclaim_info.StorageClaimInfo{
		Key:               nonOwnedClaimKey,
		Name:              "non-owned-pvc-name",
		Namespace:         "test-namespace",
		StorageClass:      "standard",
		Phase:             v1.ClaimPending,
		PodOwnerReference: nil,
	}
	pod.UpsertStorageClaim(claim)

	assert.Equal(t, 1, len(pod.GetAllStorageClaims()))
	assert.Equal(t, 0, len(pod.GetOwnedStorageClaims()))
	assert.Equal(t, v1.ClaimPending, pod.GetAllStorageClaims()[nonOwnedClaimKey].Phase)

	// verify that we override the existing storageclaim
	claimDup := &storageclaim_info.StorageClaimInfo{
		Key:               nonOwnedClaimKey,
		Name:              "non-owned-pvc-name",
		Namespace:         "test-namespace",
		StorageClass:      "standard",
		Phase:             v1.ClaimBound,
		PodOwnerReference: nil,
	}
	pod.UpsertStorageClaim(claimDup)

	assert.Equal(t, 1, len(pod.GetAllStorageClaims()))
	assert.Equal(t, 0, len(pod.GetOwnedStorageClaims()))
	assert.Equal(t, v1.ClaimBound, pod.GetAllStorageClaims()[nonOwnedClaimKey].Phase)

	// check owned storage claim
	ownedClaimKey := storageclaim_info.NewKey("test-namespace", "owned-pvc-name")
	claimOwned := &storageclaim_info.StorageClaimInfo{
		Key:          ownedClaimKey,
		Name:         "owned-pvc-name",
		Namespace:    "test-namespace",
		StorageClass: "standard",
		Phase:        v1.ClaimBound,
		PodOwnerReference: &storageclaim_info.PodOwnerReference{
			PodID:        "pod-uid",
			PodName:      "pod-name",
			PodNamespace: "test-namespace",
		},
	}
	pod.UpsertStorageClaim(claimOwned)

	assert.Equal(t, 2, len(pod.GetAllStorageClaims()))
	assert.Equal(t, 1, len(pod.GetOwnedStorageClaims()))
	assert.Equal(t, "owned-pvc-name", pod.GetOwnedStorageClaims()[ownedClaimKey].Name)
}

func TestIsRequireAnyKindOfGPU_DRA(t *testing.T) {
	req := resource_info.EmptyResourceRequirements()
	req.GpuResourceRequirement.SetDraGpus(map[string]int64{"nvidia.com/gpu": 2})
	pi := &PodInfo{
		Name:   "dra-pod",
		ResReq: req,
		Pod:    &v1.Pod{},
	}
	assert.Assert(t, pi.IsRequireAnyKindOfGPU(), "pod with only DRA GPU requests should require GPU")
	assert.Assert(t, !pi.IsCPUOnlyRequest(), "pod with only DRA GPU requests should not be CPU-only")
}

func TestNewTaskInfoWithBindRequest_ResourceClaimInfo(t *testing.T) {
	alloc := &resourceapi.AllocationResult{
		Devices: resourceapi.DeviceAllocationResult{
			Results: []resourceapi.DeviceRequestAllocationResult{
				{Request: "gpu-claim", Driver: "nvidia.com/gpu", Pool: "node0", Device: "0"},
			},
		},
	}
	draClaim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-claim",
			Namespace: "ns1",
		},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: alloc,
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("pod-uid"),
			Name:      "p1",
			Namespace: "ns1",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Resources: v1.ResourceRequirements{Requests: common_info.BuildResourceList("1000m", "1G")}},
			},
			ResourceClaims: []v1.PodResourceClaim{
				{Name: "gpu-claim", ResourceClaimName: ptr.To("gpu-claim")},
			},
		},
		Status: v1.PodStatus{Phase: v1.PodPending},
	}
	pi := NewTaskInfoWithBindRequest(pod, nil, []*resourceapi.ResourceClaim{draClaim}, resource_info.NewResourceVectorMap())
	assert.Assert(t, pi != nil)
	assert.Equal(t, 1, len(pi.ResourceClaimInfo))
	allocation, ok := pi.ResourceClaimInfo["gpu-claim"]
	assert.Assert(t, ok)
	assert.Equal(t, "gpu-claim", allocation.Name)
	assert.DeepEqual(t, alloc, allocation.Allocation)
}

func TestNewTaskInfoWithBindRequest_ResourceClaimInfo_BindRequestAllocationOverridesClaim(t *testing.T) {
	claimAlloc := &resourceapi.AllocationResult{}
	bindRequestAlloc := &resourceapi.AllocationResult{
		Devices: resourceapi.DeviceAllocationResult{
			Results: []resourceapi.DeviceRequestAllocationResult{
				{Request: "gpu-claim", Driver: "nvidia.com/gpu", Pool: "node1", Device: "1"},
			},
		},
	}
	draClaim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-claim",
			Namespace: "ns1",
		},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: claimAlloc,
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("pod-uid"),
			Name:      "p1",
			Namespace: "ns1",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Resources: v1.ResourceRequirements{Requests: common_info.BuildResourceList("1000m", "1G")}},
			},
			ResourceClaims: []v1.PodResourceClaim{
				{Name: "gpu-claim", ResourceClaimName: ptr.To("gpu-claim")},
			},
		},
		Status: v1.PodStatus{Phase: v1.PodPending},
	}
	bindRequest := &schedulingv1alpha2.BindRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "br1", Namespace: "ns1"},
		Spec: schedulingv1alpha2.BindRequestSpec{
			PodName:      "p1",
			SelectedNode: "node1",
			ResourceClaimAllocations: []schedulingv1alpha2.ResourceClaimAllocation{
				{Name: "gpu-claim", Allocation: bindRequestAlloc},
			},
		},
	}
	bindRequestInfo := bindrequest_info.NewBindRequestInfo(bindRequest)

	pi := NewTaskInfoWithBindRequest(pod, bindRequestInfo, []*resourceapi.ResourceClaim{draClaim}, resource_info.NewResourceVectorMap())
	assert.Assert(t, pi != nil)
	assert.Equal(t, 1, len(pi.ResourceClaimInfo))
	assert.Equal(t, "node1", pi.NodeName, "NodeName should come from BindRequest SelectedNode")

	allocation, ok := pi.ResourceClaimInfo["gpu-claim"]
	assert.Assert(t, ok)
	assert.Equal(t, "gpu-claim", allocation.Name)
	assert.DeepEqual(t, bindRequestAlloc, allocation.Allocation)
	assert.Assert(t, allocation.Allocation != claimAlloc,
		"Allocation should be from BindRequest (node1/device 1), not claim status (empty)")
}

func TestNewTaskInfoWithBindRequest_ResourceClaimInfo_TemplateClaimSkippedWhenNotCreated(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("pod-uid"), Name: "p1", Namespace: "ns1"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Resources: v1.ResourceRequirements{Requests: common_info.BuildResourceList("1000m", "1G")}},
			},
			ResourceClaims: []v1.PodResourceClaim{
				{Name: "gpu-claim", ResourceClaimTemplateName: ptr.To("gpu-template")},
			},
		},
		Status: v1.PodStatus{Phase: v1.PodPending},
	}
	pi := NewTaskInfoWithBindRequest(pod, nil, nil, resource_info.NewResourceVectorMap())
	assert.Assert(t, pi != nil)
	assert.Equal(t, 0, len(pi.ResourceClaimInfo))
}

func TestPodInfo_Clone_ResourceClaimInfo(t *testing.T) {
	alloc := &resourceapi.AllocationResult{
		Devices: resourceapi.DeviceAllocationResult{
			Results: []resourceapi.DeviceRequestAllocationResult{
				{Request: "gpu-claim", Driver: "nvidia.com/gpu", Pool: "node0", Device: "0"},
			},
		},
	}
	pi := &PodInfo{
		UID:              "uid",
		Name:             "p1",
		Namespace:        "ns1",
		ResReq:           resource_info.EmptyResourceRequirements(),
		AcceptedResource: resource_info.EmptyResourceRequirements(),
		ResourceClaimInfo: bindrequest_info.ResourceClaimInfo{
			"gpu-claim": {Name: "gpu-claim", Allocation: alloc},
		},
	}
	cloned := pi.Clone()
	assert.Assert(t, cloned != nil)
	assert.Equal(t, pi.UID, cloned.UID)
	assert.Equal(t, len(pi.ResourceClaimInfo), len(cloned.ResourceClaimInfo))
	clonedAlloc, ok := cloned.ResourceClaimInfo["gpu-claim"]
	assert.Assert(t, ok)
	assert.Equal(t, "gpu-claim", clonedAlloc.Name)
	assert.DeepEqual(t, alloc, clonedAlloc.Allocation)
	assert.Assert(t, cloned.ResourceClaimInfo["gpu-claim"].Allocation != pi.ResourceClaimInfo["gpu-claim"].Allocation,
		"Clone should copy Allocation, not share pointer")
}

func TestPodInfo_ShouldAllocate(t *testing.T) {
	tests := []struct {
		name             string
		status           pod_status.PodStatus
		isVirtualStatus  bool
		isRealAllocation bool
		expected         bool
	}{
		{name: "Pending with real allocation", status: pod_status.Pending, isVirtualStatus: false, isRealAllocation: true,
			expected: true},
		{name: "Pending with virtual allocation", status: pod_status.Pending, isVirtualStatus: false, isRealAllocation: false,
			expected: true},
		{name: "Releasing not virtual with real allocation", status: pod_status.Releasing, isVirtualStatus: false, isRealAllocation: true,
			expected: false},
		{name: "Releasing virtual with non-real allocation", status: pod_status.Releasing, isVirtualStatus: true, isRealAllocation: false,
			expected: true},
		{name: "Releasing virtual with real allocation", status: pod_status.Releasing, isVirtualStatus: true, isRealAllocation: true,
			expected: false},
		{name: "Running", status: pod_status.Running, isVirtualStatus: false, isRealAllocation: true,
			expected: false},
		{name: "Bound", status: pod_status.Bound, isVirtualStatus: false, isRealAllocation: true,
			expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := &PodInfo{Status: tt.status, IsVirtualStatus: tt.isVirtualStatus}
			got := pi.ShouldAllocate(tt.isRealAllocation)
			assert.Equal(t, tt.expected, got)
		})
	}
}
