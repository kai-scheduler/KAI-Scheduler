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
			pi := &PodInfo{
				Job:       tt.fields.Job,
				Name:      tt.fields.Name,
				Namespace: tt.fields.Namespace,
				ResReq:    resource_info.EmptyResourceRequirements(),
				Status:    tt.fields.Status,
				Pod:       tt.fields.Pod,
				GPUGroups: make([]string, 0),
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
