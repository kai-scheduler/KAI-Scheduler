// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_affinity"
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

	err := ni.NodeInfo.RemovePod(logger, pod)
	if err == nil {
		ni.clusterPodAffinityInfo.UpdateNodeAffinity(ni)
	}
	return err
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
