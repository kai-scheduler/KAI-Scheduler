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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func BenchmarkPodInfoClone_Minimal(b *testing.B) {
	benchmarkPodInfoClone(b, createMinimalPodInfo())
}

func BenchmarkPodInfoClone_WithGPU(b *testing.B) {
	benchmarkPodInfoClone(b, createPodInfoWithGPU())
}

func BenchmarkPodInfoClone_WithMultipleGPUs(b *testing.B) {
	benchmarkPodInfoClone(b, createPodInfoWithMultipleGPUs())
}

func benchmarkPodInfoClone(b *testing.B, podInfo *PodInfo) {
	for b.Loop() {
		_ = podInfo.Clone()
	}
}

// createMinimalPodInfo creates a PodInfo with minimal fields
func createMinimalPodInfo() *PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("pod-minimal-1"),
			Name:      "minimal-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: common_info.BuildResourceList("1000m", "1G"),
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}

	podInfo := NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
	return podInfo
}

// createPodInfoWithGPU creates a PodInfo with GPU resources
func createPodInfoWithGPU() *PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("pod-gpu-1"),
			Name:      "gpu-pod",
			Namespace: "default",
			Annotations: map[string]string{
				common_info.GPUFraction: "0.5",
			},
			Labels: map[string]string{
				GPUGroup: "group-1",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: common_info.BuildResourceListWithGPU("4000m", "8G", "1"),
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}

	podInfo := NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
	podInfo.ResourceRequestType = RequestTypeGpuMemory
	return podInfo
}

// createPodInfoWithMultipleGPUs creates a PodInfo with multiple GPU groups
func createPodInfoWithMultipleGPUs() *PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("pod-multigpu-1"),
			Name:      "multigpu-pod",
			Namespace: "default",
			Annotations: map[string]string{
				constants.GpuFractionsNumDevices: "3",
				common_info.GPUFraction:          "0.5",
			},
			Labels: map[string]string{
				constants.MultiGpuGroupLabelPrefix + "gpu-group-0": "group-0",
				constants.MultiGpuGroupLabelPrefix + "gpu-group-1": "group-1",
				constants.MultiGpuGroupLabelPrefix + "gpu-group-2": "group-2",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: common_info.BuildResourceList("16000m", "32G"),
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}

	podInfo := NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
	podInfo.AcceptedGpuRequirement = *podInfo.GpuRequirement.Clone()
	podInfo.AcceptedResourceVector = podInfo.ResReqVector.Clone()
	return podInfo
}
