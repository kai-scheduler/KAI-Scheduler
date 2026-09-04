// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodes_fake

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
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
	NumaTopology    *node_info.NumaTopology
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
			podsIdx := resource_info.PodsIndex
			nodeInfo.AllocatableVector.Set(podsIdx, float64(*nodeMetadata.MaxTaskNum))
			usedPods := nodeInfo.UsedVector.Get(podsIdx)
			availablePods := float64(*nodeMetadata.MaxTaskNum) - usedPods
			if availablePods < 0 {
				availablePods = 0
			}
			nodeInfo.IdleVector.Set(podsIdx, availablePods)
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
	nodeInfo.NumaTopology = nodeMetadata.NumaTopology

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

// NewNumaZone builds a NUMA zone spec whose Allocatable equals its Available (no in-flight usage).
// Amounts are resource-name to quantity-string, e.g. {"cpu": "4", "memory": "16Gi"}.
func NewNumaZone(id string, available map[v1.ResourceName]string) node_info.NumaZoneSpec {
	return NewNumaZoneWithAllocatable(id, available, available)
}

// NewNumaZoneWithAllocatable builds a NUMA zone spec with distinct static capacity (allocatable)
// and current headroom (available) — used to model zones already partly consumed by running pods.
func NewNumaZoneWithAllocatable(id string, allocatable, available map[v1.ResourceName]string) node_info.NumaZoneSpec {
	return node_info.NumaZoneSpec{
		ID:          id,
		Allocatable: parseQuantities(allocatable),
		Available:   parseQuantities(available),
	}
}

// NewNumaTopology builds a NumaTopology from zone specs against a fresh resource map seeded with the
// zones' resources, mirroring production where the cluster map is populated from node resources
// before topologies are built. Goes through the real NRT path (BuildNumaTopology).
func NewNumaTopology(
	policy node_info.TopologyManagerPolicy, scope node_info.TopologyManagerScope, zones ...node_info.NumaZoneSpec,
) *node_info.NumaTopology {
	vectorMap := resource_info.NewResourceVectorMap()
	for _, zone := range zones {
		vectorMap.AddResourceList(zone.Allocatable)
		vectorMap.AddResourceList(zone.Available)
	}
	return NewNumaTopologyWithMap(policy, scope, vectorMap, zones...)
}

// NewNumaTopologyWithMap builds a NumaTopology against a caller-provided resource map, for tests
// that index tasks and zones through one shared map. Zone-reported resources absent from the map
// are ignored, matching production BuildNumaTopology.
func NewNumaTopologyWithMap(
	policy node_info.TopologyManagerPolicy, scope node_info.TopologyManagerScope,
	vectorMap *resource_info.ResourceVectorMap, zones ...node_info.NumaZoneSpec,
) *node_info.NumaTopology {
	return node_info.BuildNumaTopology(numaTopologyNRT(policy, scope, zones), vectorMap)
}

// numaTopologyNRT renders zone specs as the NodeResourceTopology object an exporter would publish,
// so tests exercise the same parsing path as production. Policy/scope map to the canonical kubelet
// attribute strings; zone type "Node" is the only level BuildNumaTopology models.
func numaTopologyNRT(
	policy node_info.TopologyManagerPolicy, scope node_info.TopologyManagerScope, zones []node_info.NumaZoneSpec,
) *nrtv1alpha2.NodeResourceTopology {
	nrtZones := make(nrtv1alpha2.ZoneList, len(zones))
	for i, zone := range zones {
		names := sets.New[v1.ResourceName]()
		for name := range zone.Allocatable {
			names.Insert(name)
		}
		for name := range zone.Available {
			names.Insert(name)
		}
		resources := make(nrtv1alpha2.ResourceInfoList, 0, len(names))
		for name := range names {
			resources = append(resources, nrtv1alpha2.ResourceInfo{
				Name:        string(name),
				Allocatable: zone.Allocatable[name],
				Available:   zone.Available[name],
			})
		}
		nrtZones[i] = nrtv1alpha2.Zone{Name: zone.ID, Type: "Node", Resources: resources}
	}
	return &nrtv1alpha2.NodeResourceTopology{
		Attributes: nrtv1alpha2.AttributeList{
			{Name: "topologyManagerPolicy", Value: numaPolicyAttr(policy)},
			{Name: "topologyManagerScope", Value: numaScopeAttr(scope)},
		},
		Zones: nrtZones,
	}
}

func numaPolicyAttr(policy node_info.TopologyManagerPolicy) string {
	switch policy {
	case node_info.TopologyPolicySingleNUMANode:
		return "single-numa-node"
	case node_info.TopologyPolicyRestricted:
		return "restricted"
	case node_info.TopologyPolicyBestEffort:
		return "best-effort"
	default:
		return "none"
	}
}

func numaScopeAttr(scope node_info.TopologyManagerScope) string {
	if scope == node_info.TopologyScopePod {
		return "pod"
	}
	return "container"
}

func parseQuantities(amounts map[v1.ResourceName]string) v1.ResourceList {
	out := make(v1.ResourceList, len(amounts))
	for name, qty := range amounts {
		out[name] = resource.MustParse(qty)
	}
	return out
}

// NumaObservedPlacementAnnotation builds the kai.scheduler/numa-placement-observed pod annotation
// (zone id to per-resource amount) the binder would have persisted for a running pod. The numa
// plugin reconstructs the pod's NUMAPlacement from it at session open. Merge it via TestTaskBasic.Annotations.
func NumaObservedPlacementAnnotation(zonePlacements map[string]map[v1.ResourceName]string) map[string]string {
	record := make([]schedulingv1alpha2.NUMAZonePlacement, 0, len(zonePlacements))
	for zoneID, amounts := range zonePlacements {
		record = append(record, schedulingv1alpha2.NUMAZonePlacement{Zone: zoneID, Amount: parseQuantities(amounts)})
	}
	data, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return map[string]string{commonconstants.NumaPlacementObserved: string(data)}
}
