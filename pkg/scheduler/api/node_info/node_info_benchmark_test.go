// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info_test

import (
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
)

const migEnabledLabelKey = "node-role.kubernetes.io/mig-enabled"

func init() {
	test_utils.InitTestingInfrastructure()
}

func BenchmarkIsTaskAllocatable(b *testing.B) {
	scenarios := []struct {
		name      string
		setupNode func(*gomock.Controller) *node_info.NodeInfo
		setupTask func() *pod_info.PodInfo
	}{
		{
			name:      "best-effort-cpu-only",
			setupNode: createNodeBestEffort,
			setupTask: createTaskBestEffort,
		},
		{
			name:      "regular-gpu",
			setupNode: createNodeWithGPU,
			setupTask: createTaskWithGPU,
		},
		{
			name:      "fractional-gpu",
			setupNode: createNodeWithGPU,
			setupTask: createTaskFractionalGPU,
		},
		{
			name:      "mig-1g-10gb",
			setupNode: createNodeWithMIG,
			setupTask: createTaskMIG,
		},
		{
			name:      "gpu-memory-request",
			setupNode: createNodeWithGPUMemory,
			setupTask: createTaskGPUMemory,
		},
		{
			name:      "custom-resources-1-present",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 1) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(1) },
		},
		{
			name:      "custom-resources-2-present",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 2) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(2) },
		},
		{
			name:      "custom-resources-5-present",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 5) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(5) },
		},
		{
			name:      "custom-resources-10-present",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 10) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(10) },
		},
		{
			name:      "custom-resources-1-with-1-missing",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 0) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(1) },
		},
		{
			name:      "custom-resources-2-with-1-missing",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 1) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(2) },
		},
		{
			name:      "custom-resources-5-with-1-missing",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 4) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(5) },
		},
		{
			name:      "custom-resources-10-with-1-missing",
			setupNode: func(ctrl *gomock.Controller) *node_info.NodeInfo { return createNodeWithCustomResources(ctrl, 9) },
			setupTask: func() *pod_info.PodInfo { return createTaskWithCustomResources(10) },
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()
			ctrl := gomock.NewController(b)
			defer ctrl.Finish()

			node := scenario.setupNode(ctrl)
			task := scenario.setupTask()

			for b.Loop() {
				node.IsTaskAllocatable(task)
			}
		})
	}
}

func createNodeBestEffort(ctrl *gomock.Controller) *node_info.NodeInfo {
	k8sNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-best-effort"},
		Status: v1.NodeStatus{
			Capacity:    common_info.BuildResourceList("8", "16G"),
			Allocatable: common_info.BuildResourceList("8", "16G"),
		},
	}
	return buildNodeInfoFromK8sNode(k8sNode, ctrl)
}

func createNodeWithGPU(ctrl *gomock.Controller) *node_info.NodeInfo {
	k8sNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-gpu",
			Labels: map[string]string{
				node_info.GpuCountLabel: "8",
			},
		},
		Status: v1.NodeStatus{
			Capacity:    common_info.BuildResourceListWithGPU("8", "16G", "8"),
			Allocatable: common_info.BuildResourceListWithGPU("8", "16G", "8"),
		},
	}
	return buildNodeInfoFromK8sNode(k8sNode, ctrl)
}

func createNodeWithMIG(ctrl *gomock.Controller) *node_info.NodeInfo {
	k8sNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-mig",
			Labels: map[string]string{
				migEnabledLabelKey: "true",
			},
		},
		Status: v1.NodeStatus{
			Capacity:    common_info.BuildResourceListWithMig("8", "16G", "nvidia.com/mig-1g.10gb"),
			Allocatable: common_info.BuildResourceListWithMig("8", "16G", "nvidia.com/mig-1g.10gb"),
		},
	}
	return buildNodeInfoFromK8sNode(k8sNode, ctrl)
}

func createNodeWithGPUMemory(ctrl *gomock.Controller) *node_info.NodeInfo {
	k8sNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-gpu-memory",
			Labels: map[string]string{
				node_info.GpuCountLabel:  "2",
				node_info.GpuMemoryLabel: "40000",
			},
		},
		Status: v1.NodeStatus{
			Capacity:    common_info.BuildResourceListWithGPU("8", "16G", "2"),
			Allocatable: common_info.BuildResourceListWithGPU("8", "16G", "2"),
		},
	}
	return buildNodeInfoFromK8sNode(k8sNode, ctrl)
}

func createNodeWithCustomResources(ctrl *gomock.Controller, numResources int) *node_info.NodeInfo {
	resourceList := common_info.BuildResourceList("8", "16G")
	customResourceNames := getCustomResourceNames(numResources)

	for _, resName := range customResourceNames {
		resourceList[v1.ResourceName(resName)] = resource.MustParse("100")
	}

	k8sNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-custom"},
		Status: v1.NodeStatus{
			Capacity:    resourceList,
			Allocatable: resourceList,
		},
	}
	return buildNodeInfoFromK8sNode(k8sNode, ctrl)
}

func createTaskBestEffort() *pod_info.PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("task-best-effort"),
			Name:      "best-effort-task",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{},
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	return pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
}

func createTaskWithGPU() *pod_info.PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("task-gpu"),
			Name:      "gpu-task",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: common_info.BuildResourceListWithGPU("1000m", "4G", "1"),
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	return pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
}

func createTaskFractionalGPU() *pod_info.PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("task-frac-gpu"),
			Name:      "frac-gpu-task",
			Namespace: "default",
			Annotations: map[string]string{
				common_info.GPUFraction: "0.25",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: common_info.BuildResourceList("1000m", "4G"),
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	return pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
}

func createTaskMIG() *pod_info.PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("task-mig"),
			Name:      "mig-task",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: common_info.BuildResourceListWithMig("1000m", "4G", "nvidia.com/mig-1g.10gb"),
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	return pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
}

func createTaskGPUMemory() *pod_info.PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("task-gpu-mem"),
			Name:      "gpu-memory-task",
			Namespace: "default",
			Annotations: map[string]string{
				pod_info.GpuMemoryAnnotationName: "40000",
				common_info.GPUFraction:          "0.5",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: common_info.BuildResourceList("1000m", "4G"),
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	return pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
}

func createTaskWithCustomResources(numResources int) *pod_info.PodInfo {
	resourceList := common_info.BuildResourceList("1000m", "4G")
	customResourceNames := getCustomResourceNames(numResources)

	for _, resName := range customResourceNames {
		resourceList[v1.ResourceName(resName)] = resource.MustParse("1")
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("task-custom"),
			Name:      "custom-task",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: resourceList,
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	return pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
}

func getCustomResourceNames(count int) []string {
	names := []string{
		"example.com/accelerator",
		"example.com/fpga",
		"example.com/network-card",
		"example.com/crypto-engine",
		"example.com/ai-chip",
		"example.com/codec",
		"example.com/storage-nvme",
		"example.com/infiniband",
		"example.com/dpu",
		"example.com/tpu",
	}
	if count > len(names) {
		count = len(names)
	}
	return names[:count]
}

func buildNodeInfoFromK8sNode(k8sNode *v1.Node, ctrl *gomock.Controller) *node_info.NodeInfo {
	mockPodAffinity := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
	mockPodAffinity.EXPECT().AddPod(gomock.Any()).Times(0)
	mockPodAffinity.EXPECT().RemovePod(gomock.Any()).Times(0)
	vectorMap := resource_info.NewResourceVectorMap()
	for resourceName := range k8sNode.Status.Allocatable {
		vectorMap.AddResource(resourceName)
	}
	return node_info.NewNodeInfo(k8sNode, mockPodAffinity, vectorMap)
}
