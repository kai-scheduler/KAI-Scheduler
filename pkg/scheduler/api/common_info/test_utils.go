/*
Copyright 2018 The Kubernetes Authors.

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

package common_info

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

const FakePogGroupId = "12345678"
const GPUFraction = "gpu-fraction"

func BuildNode(name string, alloc v1.ResourceList) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Status: v1.NodeStatus{
			Capacity:    alloc,
			Allocatable: alloc,
		},
	}
}

func BuildPod(namespace, name, nodeName string, phase v1.PodPhase, req v1.ResourceList,
	owner []metav1.OwnerReference, labels map[string]string, annotations map[string]string,
) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v/%v", namespace, name)),
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: owner,
			Labels:          labels,
			Annotations:     annotations,
		},
		Status: v1.PodStatus{
			Phase: phase,
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: req,
					},
				},
			},
		},
	}
	if gpuRequest, found := pod.Spec.Containers[0].Resources.Requests[resource_info.GPUResourceName]; found {
		if (gpuRequest.MilliValue() / 1000) < 1 {
			delete(pod.Spec.Containers[0].Resources.Requests, resource_info.GPUResourceName)
			if pod.Annotations == nil {
				pod.Annotations = map[string]string{}
			}
			pod.Annotations[GPUFraction] = gpuRequest.AsDec().String()
		}
	}

	return pod
}

func BuildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
		v1.ResourcePods:   resource.MustParse("110"),
	}
}

func BuildResourceListWithGPU(cpu string, memory string, gpu string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:                resource.MustParse(cpu),
		v1.ResourceMemory:             resource.MustParse(memory),
		resource_info.GPUResourceName: resource.MustParse(gpu),
	}
}

func BuildResourceListWithGPUAndPods(cpu string, memory string, gpu string, pods string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:                resource.MustParse(cpu),
		v1.ResourceMemory:             resource.MustParse(memory),
		resource_info.GPUResourceName: resource.MustParse(gpu),
		v1.ResourcePods:               resource.MustParse(pods),
	}
}

func BuildResourceListWithMig(cpu string, memory string, migProfiles ...string) v1.ResourceList {
	resources := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
		v1.ResourcePods:   resource.MustParse("110"),
	}

	for _, profile := range migProfiles {
		if _, found := resources[v1.ResourceName(profile)]; !found {
			resources[v1.ResourceName(profile)] = resource.MustParse("0")
		}

		quant := resources[v1.ResourceName(profile)]
		quant.Add(resource.MustParse("1"))
		resources[v1.ResourceName(profile)] = quant
	}

	return resources
}

func BuildResource(cpu string, memory string) *resource_info.Resource {
	return resource_info.ResourceFromResourceList(v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	})
}

func BuildResourceWithPods(cpu string, memory string, pods string) *resource_info.Resource {
	return resource_info.ResourceFromResourceList(v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
		v1.ResourcePods:   resource.MustParse(pods),
	})
}

func BuildResourceWithGpu(cpu, memory, gpu, pods string) *resource_info.Resource {
	return resource_info.ResourceFromResourceList(v1.ResourceList{
		v1.ResourceCPU:                resource.MustParse(cpu),
		v1.ResourceMemory:             resource.MustParse(memory),
		resource_info.GPUResourceName: resource.MustParse(gpu),
		v1.ResourcePods:               resource.MustParse(pods),
	})
}

func BuildResourceWithGpuNoPods(cpu, memory, gpu string) *resource_info.Resource {
	return resource_info.ResourceFromResourceList(v1.ResourceList{
		v1.ResourceCPU:                resource.MustParse(cpu),
		v1.ResourceMemory:             resource.MustParse(memory),
		resource_info.GPUResourceName: resource.MustParse(gpu),
	})
}

func BuildResourceRequirements(cpu string, memory string) *resource_info.ResourceRequirements {
	return resource_info.RequirementsFromResourceList(v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	})
}

func BuildOwnerReference(owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

func BuildStorageCapacity(name, namespace, storageClass string, capacity, maxVolumeSize int64, nodeTopology *metav1.LabelSelector) *v12.CSIStorageCapacity {
	return &v12.CSIStorageCapacity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(fmt.Sprintf("%v-%v", namespace, name)),
		},
		NodeTopology:      nodeTopology,
		StorageClassName:  storageClass,
		Capacity:          resource.NewQuantity(capacity, resource.BinarySI),
		MaximumVolumeSize: resource.NewQuantity(maxVolumeSize, resource.BinarySI),
	}
}
