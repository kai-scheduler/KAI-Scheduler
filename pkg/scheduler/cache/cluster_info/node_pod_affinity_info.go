// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	schedutil "k8s.io/kubernetes/pkg/scheduler/util"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
)

var logger = klog.NewKlogr()

type K8sNodePodAffinityInfo struct {
	NodeInfo               *k8sframework.NodeInfo
	clusterPodAffinityInfo pod_affinity.ClusterPodAffinityInfo
	name                   string
}

func NewK8sNodePodAffinityInfo(
	node *v1.Node, clusterPodAffinityInfo pod_affinity.ClusterPodAffinityInfo,
) pod_affinity.NodePodAffinityInfo {
	nodeInfo := k8sframework.NewNodeInfo()
	nodeInfo.SetNode(node)

	if node != nil {
		clusterPodAffinityInfo.AddNode(node.Name, nodeInfo)
	}

	return &K8sNodePodAffinityInfo{
		NodeInfo:               nodeInfo,
		clusterPodAffinityInfo: clusterPodAffinityInfo,
		name:                   node.Name,
	}
}

func (ni *K8sNodePodAffinityInfo) AddPod(pod *v1.Pod) {
	ni.NodeInfo.AddPod(pod)
	ni.clusterPodAffinityInfo.UpdateNodeAffinity(ni)
}

func (ni *K8sNodePodAffinityInfo) RemovePod(pod *v1.Pod) error {

	err := RemovePodKeepingSharedPorts(ni.NodeInfo, pod)
	if err == nil {
		ni.clusterPodAffinityInfo.UpdateNodeAffinity(ni)
	}
	return err
}

// RemovePodKeepingSharedPorts removes the pod from the node. k8s tracks used host ports as a set,
// so removing one of two pods holding a port frees it for both; the ports of the pods left on the
// node are added back. Two pods hold one port when one is pipelined onto a terminating pod's port.
func RemovePodKeepingSharedPorts(nodeInfo *k8sframework.NodeInfo, pod *v1.Pod) error {
	if err := nodeInfo.RemovePod(logger, pod); err != nil {
		return err
	}
	if len(schedutil.GetHostPorts(pod)) == 0 {
		return nil
	}
	for _, podInfo := range nodeInfo.Pods {
		for _, port := range schedutil.GetHostPorts(podInfo.GetPod()) {
			nodeInfo.UsedPorts.Add(port.HostIP, string(port.Protocol), port.HostPort)
		}
	}
	return nil
}

func (ni *K8sNodePodAffinityInfo) HasPodsWithPodAffinity() bool {
	return len(ni.NodeInfo.PodsWithAffinity) > 0
}

func (ni *K8sNodePodAffinityInfo) HasPodsWithPodAntiAffinity() bool {
	return len(ni.NodeInfo.PodsWithAffinity) > 0
}

func (ni *K8sNodePodAffinityInfo) Name() string {
	return ni.name
}
