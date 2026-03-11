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

package api

import (
	"fmt"

	v1 "k8s.io/api/core/v1"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/configmap_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/csidriver_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storagecapacity_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclass_info"
	resourceapi "k8s.io/api/resource/v1"
)

// ClusterInfo is a snapshot of cluster by cache.
type ClusterInfo struct {
	Pods                        []*v1.Pod
	PodGroupInfos               map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
	Nodes                       map[string]*node_info.NodeInfo
	ResourceClaims              []*resourceapi.ResourceClaim
	BindRequests                bindrequest_info.BindRequestMap
	BindRequestsForDeletedNodes []*bindrequest_info.BindRequestInfo
	Queues                      map[common_info.QueueID]*queue_info.QueueInfo
	QueueResourceUsage          queue_info.ClusterUsage
	Departments                 map[common_info.QueueID]*queue_info.QueueInfo
	StorageClaims               map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo
	StorageCapacities           map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo
	CSIDrivers                  map[common_info.CSIDriverID]*csidriver_info.CSIDriverInfo
	StorageClasses              map[common_info.StorageClassID]*storageclass_info.StorageClassInfo
	ConfigMaps                  map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo
	Topologies                  []*kaiv1alpha1.Topology

	MinNodeGPUMemory int64

	// Shared resource vector index map for this scheduling cycle
	ResourceVectorMap *resource_info.ResourceVectorMap
}

func NewClusterInfo() *ClusterInfo {
	return &ClusterInfo{
		Pods:               []*v1.Pod{},
		Nodes:              make(map[string]*node_info.NodeInfo),
		BindRequests:       make(bindrequest_info.BindRequestMap),
		PodGroupInfos:      make(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo),
		Queues:             make(map[common_info.QueueID]*queue_info.QueueInfo),
		QueueResourceUsage: *queue_info.NewClusterUsage(),
		Departments:        make(map[common_info.QueueID]*queue_info.QueueInfo),
		StorageClaims:      make(map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo),
		StorageCapacities:  make(map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo),
		ConfigMaps:         make(map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo),
		Topologies:         []*kaiv1alpha1.Topology{},
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
			str = str + fmt.Sprintf("\t Job(%s) name(%s)\n",
				job.UID, job.Name)

			for _, subGroup := range job.GetSubGroups() {
				str = str + fmt.Sprintf("\t\t subGroup(%s), minAvailable(%v)\n",
					subGroup.GetName(), subGroup.GetMinAvailable())
			}

			i := 0
			for _, task := range job.GetAllPodsMap() {
				str = str + fmt.Sprintf("\t\t task %d: %v\n", i, task)
				i++
			}
		}
	}

	return str
}
