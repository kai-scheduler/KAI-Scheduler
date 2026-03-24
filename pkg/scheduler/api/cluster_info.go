// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"

	v1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/configmap_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/csidriver_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/storagecapacity_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/storageclass_info"
)

// ClusterInfo is a snapshot of cluster by cache.
type ClusterInfo struct {
	Pods                        []*v1.Pod
	PodGroupInfos               map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
	Nodes                       map[string]*node_info.NodeInfo
	BindRequests                bindrequest_info.BindRequestMap
	BindRequestsForDeletedNodes []*bindrequest_info.BindRequestInfo
	Queues                      map[common_info.QueueID]*queue_info.QueueInfo
	Departments                 map[common_info.QueueID]*queue_info.QueueInfo
	StorageClaims               map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo
	StorageCapacities           map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo
	CSIDrivers                  map[common_info.CSIDriverID]*csidriver_info.CSIDriverInfo
	StorageClasses              map[common_info.StorageClassID]*storageclass_info.StorageClassInfo
	ConfigMaps                  map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo

	MinNodeGPUMemory int64
}

func NewClusterInfo() *ClusterInfo {
	return &ClusterInfo{
		Pods:              []*v1.Pod{},
		Nodes:             make(map[string]*node_info.NodeInfo),
		BindRequests:      make(bindrequest_info.BindRequestMap),
		PodGroupInfos:     make(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo),
		Queues:            make(map[common_info.QueueID]*queue_info.QueueInfo),
		Departments:       make(map[common_info.QueueID]*queue_info.QueueInfo),
		StorageClaims:     make(map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo),
		StorageCapacities: make(map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo),
		ConfigMaps:        make(map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo),
	}
}

func (ci ClusterInfo) String() string {

	str := "Cache:\n"

	if len(ci.Nodes) != 0 {
		str = str + "Nodes:\n"
		for _, n := range ci.Nodes {
			str = str + fmt.Sprintf("\t %s: idle(%v) used(%v) allocatable(%v) pods(%d)\n",
				n.Name, n.Idle, n.Used, n.Allocatable, len(n.PodInfos))

			i := 0
			for _, p := range n.PodInfos {
				str = str + fmt.Sprintf("\t\t %d: %v\n", i, p)
				i++
			}
		}
	}

	if len(ci.PodGroupInfos) != 0 {
		str = str + "PodGroupInfos:\n"
		for _, job := range ci.PodGroupInfos {
			str = str + fmt.Sprintf("\t Job(%s) name(%s) minAvailable(%v)\n",
				job.UID, job.Name, job.MinAvailable)

			i := 0
			for _, task := range job.PodInfos {
				str = str + fmt.Sprintf("\t\t %d: %v\n", i, task)
				i++
			}
		}
	}

	return str
}
