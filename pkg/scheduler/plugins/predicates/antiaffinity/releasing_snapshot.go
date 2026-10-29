// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package antiaffinity

import (
	"fmt"

	k8sframework "k8s.io/kube-scheduler/framework"
)

type releasingHandle struct {
	k8sframework.Handle
	snapshot *releasingSnapshot
}

func (h releasingHandle) SnapshotSharedLister() k8sframework.SharedLister { return h.snapshot }

// releasingSnapshot exists only while preparing upstream topology counts.
type releasingSnapshot struct {
	nodes                []k8sframework.NodeInfo
	requiredAntiAffinity []k8sframework.NodeInfo
}

func (s *releasingSnapshot) NodeInfos() k8sframework.NodeInfoLister       { return s }
func (s *releasingSnapshot) StorageInfos() k8sframework.StorageInfoLister { return nil }
func (s *releasingSnapshot) List() ([]k8sframework.NodeInfo, error)       { return s.nodes, nil }
func (s *releasingSnapshot) HavePodsWithRequiredAntiAffinityList() ([]k8sframework.NodeInfo, error) {
	return s.requiredAntiAffinity, nil
}
func (s *releasingSnapshot) HavePodsWithAffinityList() ([]k8sframework.NodeInfo, error) {
	var nodes []k8sframework.NodeInfo
	for _, node := range s.nodes {
		if len(node.GetPodsWithAffinity()) > 0 {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}
func (s *releasingSnapshot) Get(name string) (k8sframework.NodeInfo, error) {
	for _, node := range s.nodes {
		if node.Node().Name == name {
			return node, nil
		}
	}
	return nil, fmt.Errorf("node %q not in releasing snapshot", name)
}
