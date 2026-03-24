// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"golang.org/x/exp/maps"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache/cluster_info"
)

type K8sClusterPodAffinityInfo struct {
	k8sNodes                 map[string]ksf.NodeInfo
	k8sNodesArr              []ksf.NodeInfo
	nodesWithPodAffinity     map[string]ksf.NodeInfo
	nodesWithPodAntiAffinity map[string]ksf.NodeInfo
}

func NewK8sClusterPodAffinityInfo() *K8sClusterPodAffinityInfo {
	return &K8sClusterPodAffinityInfo{
		k8sNodes:                 make(map[string]ksf.NodeInfo),
		nodesWithPodAffinity:     make(map[string]ksf.NodeInfo),
		nodesWithPodAntiAffinity: make(map[string]ksf.NodeInfo),
	}
}

func (ci *K8sClusterPodAffinityInfo) List() ([]ksf.NodeInfo, error) {
	return ci.k8sNodesArr, nil
}

func (ci *K8sClusterPodAffinityInfo) Get(nodeName string) (ksf.NodeInfo, error) {
	return ci.k8sNodes[nodeName], nil
}

func (ci *K8sClusterPodAffinityInfo) HavePodsWithAffinityList() ([]ksf.NodeInfo, error) {
	return maps.Values(ci.nodesWithPodAffinity), nil
}

func (ci *K8sClusterPodAffinityInfo) HavePodsWithRequiredAntiAffinityList() ([]ksf.NodeInfo, error) {
	return maps.Values(ci.nodesWithPodAntiAffinity), nil
}

func (ci *K8sClusterPodAffinityInfo) AddNode(name string, nodeInfo *k8sframework.NodeInfo) {
	ci.k8sNodes[name] = nodeInfo
	ci.k8sNodesArr = append(ci.k8sNodesArr, nodeInfo)
}

func (ci *K8sClusterPodAffinityInfo) UpdateNodeAffinity(podAffinityInfo pod_affinity.NodePodAffinityInfo) {
	if podAffinityInfo.HasPodsWithPodAffinity() {
		ci.nodesWithPodAffinity[podAffinityInfo.Name()] = podAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo
	} else {
		if _, found := ci.nodesWithPodAffinity[podAffinityInfo.Name()]; found {
			delete(ci.nodesWithPodAffinity, podAffinityInfo.Name())
		}
	}

	if podAffinityInfo.HasPodsWithPodAntiAffinity() {
		ci.nodesWithPodAntiAffinity[podAffinityInfo.Name()] = podAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo
	} else {
		if _, found := ci.nodesWithPodAntiAffinity[podAffinityInfo.Name()]; found {
			delete(ci.nodesWithPodAntiAffinity, podAffinityInfo.Name())
		}
	}
}
