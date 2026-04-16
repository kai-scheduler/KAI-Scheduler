/*
Copyright 2017 The Kubernetes Authors.

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

package cluster_info

import (
	"fmt"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	kubeAiSchedulerinfo "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/informers/externalversions"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	pg "github.com/kai-scheduler/KAI-scheduler/pkg/common/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/configmap_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info/data_lister"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/status_updater"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/utils"

	kueue "sigs.k8s.io/kueue/apis/kueue/v1alpha1"
	kueueinformer "sigs.k8s.io/kueue/client-go/informers/externalversions"
)

type ClusterInfo struct {
	dataLister               data_lister.DataLister
	podGroupSync             status_updater.PodGroupsSync
	nodePoolParams           *conf.SchedulingNodePoolParams
	restrictNodeScheduling   bool
	clusterPodAffinityInfo   pod_affinity.ClusterPodAffinityInfo
	includeCSIStorageObjects bool
	nodePoolSelector         labels.Selector
	fairnessLevelType        FairnessLevelType
	collectUsageData         bool
}

type FairnessLevelType string

const (
	FullFairness         FairnessLevelType = "fullFairness"
	ProjectLevelFairness FairnessLevelType = "projectLevelFairness"
)

func New(
	informerFactory informers.SharedInformerFactory,
	kubeAiSchedulerInformerFactory kubeAiSchedulerinfo.SharedInformerFactory,
	kueueInformerFactory kueueinformer.SharedInformerFactory,
	usageLister *usagedb.UsageLister,
	nodePoolParams *conf.SchedulingNodePoolParams,
	restrictNodeScheduling bool,
	clusterPodAffinityInfo pod_affinity.ClusterPodAffinityInfo,
	includeCSIStorageObjects bool,
	fullHierarchyFairness bool,
	podGroupSync status_updater.PodGroupsSync,
) (*ClusterInfo, error) {
	indexers := cache.Indexers{
		podByPodGroupIndexerName: podByPodGroupIndexer,
	}
	err := informerFactory.Core().V1().Pods().Informer().AddIndexers(indexers)
	if err != nil {
		return nil, err
	}
	nodePoolSelector, err := nodePoolParams.GetLabelSelector()
	if err != nil {
		return nil, fmt.Errorf("error getting nodes selector: %s", err)
	}

	fairnessLevelType := FullFairness
	if !fullHierarchyFairness {
		fairnessLevelType = ProjectLevelFairness
	}

	return &ClusterInfo{
		dataLister:               data_lister.New(informerFactory, kubeAiSchedulerInformerFactory, kueueInformerFactory, usageLister, nodePoolSelector),
		nodePoolParams:           nodePoolParams,
		restrictNodeScheduling:   restrictNodeScheduling,
		clusterPodAffinityInfo:   clusterPodAffinityInfo,
		includeCSIStorageObjects: includeCSIStorageObjects,
		nodePoolSelector:         nodePoolSelector,
		fairnessLevelType:        fairnessLevelType,
		podGroupSync:             podGroupSync,
		collectUsageData:         usageLister != nil,
	}, nil
}

func (c *ClusterInfo) Snapshot() (*api.ClusterInfo, error) {
	snapshot := api.NewClusterInfo()

	// KnownPods is a map of pods in the cluster. Whenever we handle a pod (e.g, when snapshotting nodes/podgroups), we
	// use this map to make sure that they refer to the same objects. See RUN-5317
	existingPods := map[common_info.PodID]*pod_info.PodInfo{}

	var err error
	allPods, err := c.dataLister.ListPods()
	if err != nil {
		return nil, fmt.Errorf("error snapshotting pods: %w", err)
	}

	snapshot.Nodes, err = c.snapshotNodes(c.clusterPodAffinityInfo)
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error snapshotting nodes: %c", err))
		return nil, err
	}

	snapshot.BindRequests, snapshot.BindRequestsForDeletedNodes, err = c.snapshotBindRequests(snapshot.Nodes)
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error snapshotting bind requests: %c", err))
		return nil, err
	}

	snapshot.Pods, err = c.addTasksToNodes(allPods, existingPods, snapshot.Nodes, snapshot.BindRequests)
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error adding tasks to nodes: %c", err))
		return nil, err
	}

	queues, err := c.snapshotQueues()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error snapshotting queues: %c", err))
		return nil, err
	}
	UpdateQueueHierarchy(queues)
	snapshot.Queues = queues

	usage, usageErr := c.snapshotQueueResourceUsage()
	if usageErr != nil {
		log.InfraLogger.V(2).Warnf("error snapshotting queue resource usage: %c", usageErr)
	}
	if usage == nil {
		usage = queue_info.NewClusterUsage()
	}
	snapshot.QueueResourceUsage = *usage

	snapshot.PodGroupInfos, err = c.snapshotPodGroups(snapshot.Queues, existingPods)
	if err != nil {
		return nil, err
	}

	snapshot.ConfigMaps, err = c.snapshotConfigMaps()
	if err != nil {
		return nil, err
	}

	snapshot.Topologies, err = c.snapshotTopologies()
	if err != nil {
		return nil, err
	}

	if c.includeCSIStorageObjects {
		log.InfraLogger.V(7).Infof("Advanced CSI scheduling enabled - snapshotting CSI storage objects")

		snapshot.CSIDrivers, err = c.snapshotCSIStorageDrivers()
		if err != nil {
			return nil, err
		}

		snapshot.StorageClasses, err = c.snapshotStorageClasses()
		if err != nil {
			return nil, err
		}

		snapshot.StorageClasses = filterStorageClasses(snapshot.StorageClasses, snapshot.CSIDrivers)

		snapshot.StorageCapacities, err = c.snapshotStorageCapacities()
		if err != nil {
			return nil, err
		}

		snapshot.StorageClaims, err = c.snapshotStorageClaims()
		if err != nil {
			return nil, err
		}

		snapshot.StorageClaims = filterStorageClaims(snapshot.StorageClaims, snapshot.StorageClasses)

		linkStorageObjects(snapshot.StorageClaims, snapshot.StorageCapacities, existingPods, snapshot.Nodes)
	} else {
		log.InfraLogger.V(7).Infof("Advanced CSI scheduling not enabled - not snapshotting CSI storage objects")
	}

	for _, pg := range snapshot.PodGroupInfos {
		log.InfraLogger.V(6).Infof("Scheduling constraints signature for podgroup %s/%s: %s",
			pg.Namespace, pg.Name, pg.GetSchedulingConstraintsSignature())
	}

	log.InfraLogger.V(4).Infof("Snapshot info - PodGroupInfos: <%d>, BindRequests: <%d>, Queues: <%d>, "+
		"Nodes: <%d> in total for scheduling",
		len(snapshot.PodGroupInfos), len(snapshot.BindRequests), len(snapshot.Queues), len(snapshot.Nodes))
	return snapshot, nil
}

func (c *ClusterInfo) snapshotNodes(
	clusterPodAffinityInfo pod_affinity.ClusterPodAffinityInfo,
) (map[string]*node_info.NodeInfo, error) {
	nodes, err := c.dataLister.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("error listing nodes: %c", err)
	}
	if c.restrictNodeScheduling {
		nodes = filterUnmarkedNodes(nodes)
	}

	resultNodes := map[string]*node_info.NodeInfo{}
	for _, node := range nodes {
		podAffinityInfo := NewK8sNodePodAffinityInfo(node, clusterPodAffinityInfo)
		resultNodes[node.Name] = node_info.NewNodeInfo(node, podAffinityInfo)
	}

	return resultNodes, nil
}

func (c *ClusterInfo) addTasksToNodes(allPods []*v1.Pod, existingPodsMap map[common_info.PodID]*pod_info.PodInfo,
	nodes map[string]*node_info.NodeInfo, bindRequests bindrequest_info.BindRequestMap) (
	[]*v1.Pod, error) {

	nodePodInfosMap, nodeReservationPodInfosMap, err := c.getNodeToPodInfosMap(allPods, bindRequests)
	if err != nil {
		return nil, err
	}

	var resultPods []*v1.Pod
	for _, node := range nodes {
		reservationPodInfos := nodeReservationPodInfosMap[node.Name]
		result := node.AddTasksToNode(reservationPodInfos, existingPodsMap)
		resultPods = append(resultPods, result...)

		podInfos := nodePodInfosMap[node.Name]
		result = node.AddTasksToNode(podInfos, existingPodsMap)
		resultPods = append(resultPods, result...)

		podNames := ""
		for _, pi := range node.PodInfos {
			podNames = fmt.Sprintf("%v, %v", podNames, pi.Name)
		}
		log.InfraLogger.V(6).Infof("Node: %v, indexed %d pods: %v", node.Name, len(node.PodInfos), podNames)
	}
	return resultPods, nil
}

func (c *ClusterInfo) snapshotBindRequests(nodes map[string]*node_info.NodeInfo) (
	bindrequest_info.BindRequestMap, []*bindrequest_info.BindRequestInfo, error) {
	bindRequests, err := c.dataLister.ListBindRequests()
	if err != nil {
		return nil, nil, fmt.Errorf("error listing bind requests: %c", err)
	}

	result := bindrequest_info.BindRequestMap{}
	requestsForDeletedNodes := []*bindrequest_info.BindRequestInfo{}
	for _, bindRequest := range bindRequests {
		if _, found := nodes[bindRequest.Spec.SelectedNode]; !found {
			if c.nodePoolSelector.Matches(labels.Set(bindRequest.Labels)) {
				bri := bindrequest_info.NewBindRequestInfo(bindRequest)
				requestsForDeletedNodes = append(requestsForDeletedNodes, bri)
			}
			continue
		}
		result[bindrequest_info.NewKeyFromRequest(bindRequest)] = bindrequest_info.NewBindRequestInfo(bindRequest)
	}

	return result, requestsForDeletedNodes, nil
}

func (c *ClusterInfo) snapshotPodGroups(
	existingQueues map[common_info.QueueID]*queue_info.QueueInfo,
	existingPods map[common_info.PodID]*pod_info.PodInfo,
) (map[common_info.PodGroupID]*podgroup_info.PodGroupInfo, error) {
	defaultPriority, err := getDefaultPriority(c.dataLister)
	if err != nil {
		log.InfraLogger.Errorf("Error getting default priority: %v", err)
		return nil, err
	}

	podGroups, err := c.dataLister.ListPodGroups()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error listing podgroups: %c", err))
		return nil, err
	}

	for i := range podGroups {
		podGroups[i] = podGroups[i].DeepCopy()
	}

	if c.podGroupSync != nil {
		c.podGroupSync.SyncPodGroupsWithPendingUpdates(podGroups)
	}
	podGroups = c.filterUnassignedPodGroups(podGroups)

	result := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{}
	for _, podGroup := range podGroups {
		podGroupID := common_info.PodGroupID(podGroup.Name)
		podGroupInfo := podgroup_info.NewPodGroupInfo(podGroupID)

		if _, found := existingQueues[common_info.QueueID(podGroup.Spec.Queue)]; !found {
			log.InfraLogger.V(7).Infof("The Queue <%v> of podgroup <%v/%v> does not exist, ignore it.",
				podGroup.Spec.Queue, podGroup.Namespace, podGroup.Name)
			continue
		}

		podGroupInfo.Priority = getPodGroupPriority(podGroup, defaultPriority, c.dataLister)
		log.InfraLogger.V(7).Infof("The priority of job <%s/%s> is <%s/%d>", podGroup.Namespace, podGroup.Name,
			podGroup.Spec.PriorityClassName, podGroupInfo.Priority)

		podGroupInfo.Preemptibility = pg.CalculatePreemptibility(podGroup.Spec.Preemptibility, podGroupInfo.Priority)
		log.InfraLogger.V(7).Infof("The preemptibility of job <%s/%s> is <%s>", podGroup.Namespace, podGroup.Name,
			podGroupInfo.Preemptibility)

		c.setPodGroupWithIndex(podGroup, podGroupInfo)
		rawPods, err := c.dataLister.ListPodByIndex(podByPodGroupIndexerName, podGroup.Name)
		if err != nil {
			log.InfraLogger.Errorf("failed to get indexed pods: %s", err)
			return nil, err
		}
		for _, rawPod := range rawPods {
			pod, ok := rawPod.(*v1.Pod)
			if !ok {
				log.InfraLogger.Errorf("Snapshot podGroups: Error getting pod from rawPod: %c", rawPod)
			}
			podInfo := c.getPodInfo(pod, existingPods)
			podGroupInfo.AddTaskInfo(podInfo)
		}
		result[common_info.PodGroupID(podGroup.Name)] = podGroupInfo
	}

	return result, nil
}

func (c *ClusterInfo) getPodInfo(
	pod *v1.Pod, existingPods map[common_info.PodID]*pod_info.PodInfo,
) *pod_info.PodInfo {
	var podInfo *pod_info.PodInfo
	log.InfraLogger.V(6).Infof("Looking for pod %s/%s/%s in existing pods", pod.Namespace, pod.Name,
		pod.UID)

	podInfo, found := existingPods[common_info.PodID(pod.UID)]
	if !found {
		log.InfraLogger.V(6).Infof("Pod %s/%s/%s not found in existing pods, adding", pod.Namespace,
			pod.Name, pod.UID)
		podInfo = pod_info.NewTaskInfo(pod)
		existingPods[common_info.PodID(pod.UID)] = podInfo
	}
	return podInfo
}

func (c *ClusterInfo) setPodGroupWithIndex(podGroup *enginev2alpha2.PodGroup, podGroupInfo *podgroup_info.PodGroupInfo) {
	podGroupInfo.SetPodGroup(podGroup)
}

func (c *ClusterInfo) getNodeToPodInfosMap(allPods []*v1.Pod, bindRequests bindrequest_info.BindRequestMap) (
	map[string][]*pod_info.PodInfo, map[string][]*pod_info.PodInfo, error) {
	nodePodInfosMap := map[string][]*pod_info.PodInfo{}
	nodeReservationPodInfosMap := map[string][]*pod_info.PodInfo{}
	for _, pod := range allPods {
		podBindRequest := bindRequests.GetBindRequestForPod(pod)
		podInfo := pod_info.NewTaskInfoWithBindRequest(pod, podBindRequest)

		if pod_info.IsResourceReservationTask(podInfo.Pod) {
			podInfos := nodeReservationPodInfosMap[podInfo.NodeName]
			podInfos = append(podInfos, podInfo)
			nodeReservationPodInfosMap[podInfo.NodeName] = podInfos
		} else {
			podInfos := nodePodInfosMap[podInfo.NodeName]
			podInfos = append(podInfos, podInfo)
			nodePodInfosMap[podInfo.NodeName] = podInfos
		}
	}
	return nodePodInfosMap, nodeReservationPodInfosMap, nil
}

func (c *ClusterInfo) snapshotConfigMaps() (map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo, error) {
	configMaps, err := c.dataLister.ListConfigMaps()
	if err != nil {
		return nil, fmt.Errorf("error listing configmaps: %w", err)
	}

	result := map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{}
	for _, configMap := range configMaps {
		configMapInfo := configmap_info.NewConfigMapInfo(configMap)
		result[configMapInfo.UID] = configMapInfo
	}

	return result, nil
}

func (c *ClusterInfo) snapshotTopologies() ([]*kueue.Topology, error) {
	topologies, err := c.dataLister.ListTopologies()
	if err != nil {
		return nil, fmt.Errorf("error listing topologies: %w", err)
	}
	return topologies, nil
}

func getDefaultPriority(dataLister data_lister.DataLister) (int32, error) {
	defaultPriority, found := int32(constants.DefaultPodGroupPriority), false
	priorityClasses, err := dataLister.ListPriorityClasses()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error listing priorityclasses: %c", err))
		return 0, err
	}

	for _, pc := range priorityClasses {
		if pc.GlobalDefault {
			log.InfraLogger.V(7).Infof("Found default priority class %s with value %d", pc.Name, pc.Value)
			defaultPriority = pc.Value
			found = true
			break
		}
	}
	if !found {
		log.InfraLogger.V(7).Infof("Failed to find a default priorityclass, using %d as default priority", defaultPriority)
	}

	return defaultPriority, nil
}

func getPodGroupPriority(
	podGroup *enginev2alpha2.PodGroup, defaultPriority int32, dataLister data_lister.DataLister,
) int32 {
	chosenPriorityClass, err := dataLister.GetPriorityClassByName(podGroup.Spec.PriorityClassName)
	if err != nil {
		log.InfraLogger.V(6).Infof(
			"Couldn't find priorityClass %s for podGroup %s/%s, error: %v. Using default priority %d",
			podGroup.Spec.PriorityClassName, podGroup.Namespace, podGroup.Name, err, defaultPriority)
		return defaultPriority
	}
	return chosenPriorityClass.Value
}

func filterUnmarkedNodes(nodes []*v1.Node) []*v1.Node {
	cpuWorkerLabelKey := conf.GetConfig().CPUWorkerNodeLabelKey
	gpuWorkerLabelKey := conf.GetConfig().GPUWorkerNodeLabelKey
	markedNodes := []*v1.Node{}
	for _, node := range nodes {
		_, foundGpuNode := node.Labels[gpuWorkerLabelKey]
		_, foundCpuNode := node.Labels[cpuWorkerLabelKey]
		if foundGpuNode || foundCpuNode {
			markedNodes = append(markedNodes, node)
			log.InfraLogger.V(6).Infof("Node: <%v> is considered by cpu or gpu label", node.Name)
		} else {
			log.InfraLogger.V(6).Infof("Node: <%v> is filtered out by CPU or GPU Label", node.Name)
		}
	}
	return markedNodes
}

func (c *ClusterInfo) filterUnassignedPodGroups(podGroups []*enginev2alpha2.PodGroup) []*enginev2alpha2.PodGroup {
	assignedPodGroups := make([]*enginev2alpha2.PodGroup, 0)
	for _, podGroup := range podGroups {
		result := c.isPodGroupUpForScheduler(podGroup)
		if result {
			assignedPodGroups = append(assignedPodGroups, podGroup)
		} else {
			log.InfraLogger.V(2).Warnf("Skipping pod group <%s/%s> - not assigned to current scheduler",
				podGroup.Namespace, podGroup.Name)
		}
	}
	return assignedPodGroups
}

func (c *ClusterInfo) isPodGroupUpForScheduler(podGroup *enginev2alpha2.PodGroup) bool {
	if utils.GetSchedulingBackoffValue(podGroup.Spec.SchedulingBackoff) == utils.NoSchedulingBackoff {
		return true
	}

	lastSchedulingCondition := utils.GetLastSchedulingCondition(podGroup)
	if lastSchedulingCondition == nil {
		return true
	}

	currentNodePoolName := utils.GetNodePoolNameFromLabels(podGroup.Labels, c.nodePoolParams.NodePoolLabelKey)
	if lastSchedulingCondition.NodePool != currentNodePoolName {
		return true
	}

	return false
}
