// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodes_fake

import (
	"fmt"
	"sort"
	"strconv"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/resources_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const (
	cpuMilliOverall     = "30000"
	memoryOverall       = "30G"
	cpuMilliAllocatable = "20000"
	memoryAllocatable   = "20G"
	migEnabledLabelKey  = "node-role.kubernetes.io/mig-enabled"
)

type TestClusterTopology struct {
	Name  string
	Jobs  []*jobs_fake.TestJobBasic
	Nodes map[string]TestNodeBasic
}

type TestNodeBasic struct {
	GPUs            int
	GPUName         string
	MigStrategy     node_info.MigStrategy
	MigInstances    map[v1.ResourceName]int
	CPUMemory       float64
	GPUMemory       int
	CPUMillis       float64
	GpuMemorySynced *bool
	MaxTaskNum      *int
	Labels          map[string]string
}

func BuildNodesInfoMap(
	Nodes map[string]TestNodeBasic, tasksToNodeMap map[string]pod_info.PodsMap,
	clusterPodAffinityInfo *cache.K8sClusterPodAffinityInfo, vectorMap *resource_info.ResourceVectorMap,
	draClusterObjects ...runtime.Object,
) map[string]*node_info.NodeInfo {
	if clusterPodAffinityInfo == nil {
		clusterPodAffinityInfo = cache.NewK8sClusterPodAffinityInfo()
	}
	slicesByNode := calcResourceSlicesMap(draClusterObjects)

	for _, nodeMetadata := range Nodes {
		nodeGpuCount := strconv.Itoa(nodeMetadata.GPUs)
		nodeAllocatableGPUs := nodeGpuCount
		if nodeMetadata.MigStrategy == node_info.MigStrategyMixed {
			nodeAllocatableGPUs = "0"
		}

		cpuMilliAllocatableVal := cpuMilliAllocatable
		memoryAllocatableVal := memoryAllocatable

		if nodeMetadata.CPUMillis > 0 {
			cpuMilliAllocatableVal = strconv.FormatFloat(nodeMetadata.CPUMillis, 'f', -1, 64)
		}

		if nodeMetadata.CPUMemory > 0 {
			memoryAllocatableVal = strconv.FormatFloat(nodeMetadata.CPUMemory, 'f', -1, 64)
		}

		nodeResourceAllocatable := resources_fake.BuildResourceList(&cpuMilliAllocatableVal, &memoryAllocatableVal,
			&nodeAllocatableGPUs, nodeMetadata.MigInstances)
		vectorMap.AddResourceList(*nodeResourceAllocatable)
	}

	nodesInfoMap := map[string]*node_info.NodeInfo{}

	for nodeName, nodeMetadata := range Nodes {
		tasksOfNode := pod_info.PodsMap{}
		if _, found := tasksToNodeMap[nodeName]; found {
			tasksOfNode = tasksToNodeMap[nodeName]
		}

		nodeInfo := buildNodeInfo(nodeName, &nodeMetadata, tasksOfNode, clusterPodAffinityInfo, slicesByNode, vectorMap)
		if nodeMetadata.GpuMemorySynced != nil {
			nodeInfo.GpuMemorySynced = *nodeMetadata.GpuMemorySynced
		}
		if nodeMetadata.MaxTaskNum != nil {
			nodeInfo.MaxTaskNum = *nodeMetadata.MaxTaskNum
			nodeInfo.Allocatable.ScalarResources()[v1.ResourcePods] = int64(*nodeMetadata.MaxTaskNum)
			usedPods := int64(nodeInfo.Used.Get(v1.ResourcePods))
			availablePods := int64(*nodeMetadata.MaxTaskNum) - usedPods
			if availablePods < 0 {
				availablePods = 0
			}
			nodeInfo.Idle.ScalarResources()[v1.ResourcePods] = availablePods
		}
		nodesInfoMap[nodeName] = nodeInfo
	}

	return nodesInfoMap
}

func calcResourceSlicesMap(draClusterObjects []runtime.Object) map[string][]*resourceapi.ResourceSlice {
	slicesByNode := map[string][]*resourceapi.ResourceSlice{}
	for _, draObject := range draClusterObjects {
		if resourceSlice, ok := draObject.(*resourceapi.ResourceSlice); ok {
			if resourceSlice.Spec.NodeName == nil {
				continue
			}
			slicesByNode[*resourceSlice.Spec.NodeName] = append(slicesByNode[*resourceSlice.Spec.NodeName], resourceSlice)
		}
	}
	return slicesByNode
}

func BuildNode(node string, capacity *v1.ResourceList, allocatable *v1.ResourceList) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: node},
		Status: v1.NodeStatus{
			Capacity:    *capacity,
			Allocatable: *allocatable,
		},
	}
}

func buildNodeInfo(
	nodeName string, nodeMetadata *TestNodeBasic, tasksOfNode pod_info.PodsMap,
	clusterPodAffinityInfo *cache.K8sClusterPodAffinityInfo, slicesByNode map[string][]*resourceapi.ResourceSlice,
	vectorMap *resource_info.ResourceVectorMap,
) *node_info.NodeInfo {
	nodeGpuCount := strconv.Itoa(nodeMetadata.GPUs)
	nodeAllocatableGPUs := nodeGpuCount
	if nodeMetadata.MigStrategy == node_info.MigStrategyMixed {
		nodeAllocatableGPUs = "0"
	}

	cpuMilliOverallVal := cpuMilliOverall
	memoryOverallVal := memoryOverall
	cpuMilliAllocatableVal := cpuMilliAllocatable
	memoryAllocatableVal := memoryAllocatable

	if nodeMetadata.CPUMillis > 0 {
		cpuMilliAllocatableVal = strconv.FormatFloat(nodeMetadata.CPUMillis, 'f', -1, 64)
	}

	if nodeMetadata.CPUMemory > 0 {
		memoryAllocatableVal = strconv.FormatFloat(nodeMetadata.CPUMemory, 'f', -1, 64)
	}

	migEnabledLabel := "false"
	if nodeMetadata.MigStrategy != "" {
		migEnabledLabel = "true"
	}

	nodeResource := *resources_fake.BuildResourceList(&cpuMilliOverallVal, &memoryOverallVal, &nodeGpuCount,
		nodeMetadata.MigInstances)
	if _, found := nodeResource[v1.ResourcePods]; !found {
		nodeResource[v1.ResourcePods] = resource.MustParse("110")
	}
	nodeResourceAllocatable := *resources_fake.BuildResourceList(&cpuMilliAllocatableVal, &memoryAllocatableVal,
		&nodeAllocatableGPUs, nodeMetadata.MigInstances)
	if _, found := nodeResourceAllocatable[v1.ResourcePods]; !found {
		nodeResourceAllocatable[v1.ResourcePods] = resource.MustParse("110")
	}
	node := BuildNode(nodeName, &nodeResource, &nodeResourceAllocatable)
	node.Labels = map[string]string{
		commonconstants.GpuCountLabel:    nodeGpuCount,
		node_info.GpuMemoryLabel:         strconv.Itoa(node_info.DefaultGpuMemory),
		migEnabledLabelKey:               migEnabledLabel,
		commonconstants.MigStrategyLabel: string(nodeMetadata.MigStrategy),
		tasks_fake.NodeAffinityKey:       nodeName,
	}
	for labelKey, labelValue := range nodeMetadata.Labels {
		node.Labels[labelKey] = labelValue
	}
	if nodeMetadata.GPUMemory > 0 {
		node.Labels[node_info.GpuMemoryLabel] = strconv.Itoa(nodeMetadata.GPUMemory)
	}
	podAffinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, clusterPodAffinityInfo)
	nodeInfo := node_info.NewNodeInfo(node, podAffinityInfo, vectorMap)

	// Count GPUs from node-specific slices
	var draGPUCount int64
	for _, slice := range slicesByNode[nodeName] {
		if !resources.IsGPUDeviceClass(slice.Spec.Driver) {
			continue
		}
		draGPUCount += int64(len(slice.Spec.Devices))
	}

	if draGPUCount > 0 {
		log.InfraLogger.V(6).Infof("Node %s has %d DRA GPUs from ResourceSlices", nodeName, draGPUCount)
		nodeInfo.AddDRAGPUs(float64(draGPUCount))
	}

	// Order of node task addition matters
	sortedTasks := toSorted(tasksOfNode)

	for _, task := range sortedTasks {
		err := nodeInfo.AddTask(task)
		if err != nil {
			log.InfraLogger.Errorf("Received an error during add task")
		}
		if task.IsLegacyMIGtask {
			nodeInfo.LegacyMIGTasks[task.UID] = fmt.Sprintf("%v/%v", task.Namespace, task.Name)
		}
	}
	nodeInfo.MaxTaskNum = 500

	return nodeInfo
}

func toSorted(tasks pod_info.PodsMap) []*pod_info.PodInfo {
	sortedTasks := []*pod_info.PodInfo{}

	keys := []string{}
	for key := range tasks {
		keys = append(keys, string(key))
	}

	sort.Strings(keys)

	for _, key := range keys {
		sortedTasks = append(sortedTasks, tasks[common_info.PodID(key)])
	}

	return sortedTasks
}
