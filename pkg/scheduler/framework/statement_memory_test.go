// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestStatementMemoryOnlyMoveAndDiscard(t *testing.T) {
	for _, zones := range [][]pod_info.ZonePlacement{
		nil,
		{{ZoneIndex: 0, Amount: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
	} {
		t.Run("zones-"+string(rune('0'+len(zones))), func(t *testing.T) {
			testStatementMemoryMove(t, zones)
		})
	}
}

func testStatementMemoryMove(t *testing.T, zones []pod_info.ZonePlacement) {
	t.Helper()
	vectorMap := resource_info.NewResourceVectorMap()
	jobs, tasks, _ := jobs_fake.BuildJobsAndTasksMaps([]*jobs_fake.TestJobBasic{{Name: "owner", QueueName: "q", Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node"}}}}, vectorMap)
	nodes := nodes_fake.BuildNodesInfoMap(map[string]nodes_fake.TestNodeBasic{"node": {CPUMemory: 1000}}, tasks, nil, vectorMap)
	node := nodes["node"]
	var task *pod_info.PodInfo
	for _, candidate := range jobs["owner"].GetAllPodsMap() {
		task = candidate
	}
	require.NotNil(t, task)
	amount := v1.ResourceList{v1.ResourceMemory: resource.MustParse("10Gi")}
	original := pod_info.NUMAPlacement{Zones: zones, MemoryGroups: []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: amount}}}
	task.NUMAPlacement = original.Clone()
	node.PodInfos[pod_info.PodKey(task.Pod)].NUMAPlacement = original.Clone()
	node.NumaTopology = nodes_fake.NewNumaTopologyWithMap(node_info.TopologyPolicyNone, node_info.TopologyScopeContainer, vectorMap,
		node_info.NumaZoneSpec{ID: "node-0", Allocatable: v1.ResourceList{v1.ResourceMemory: resource.MustParse("100Gi")}},
		node_info.NumaZoneSpec{ID: "node-1", Allocatable: v1.ResourceList{v1.ResourceMemory: resource.MustParse("100Gi")}})
	state := &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown, Groups: map[pod_info.NUMAMask]*node_info.MemoryGroup{}}
	node.NumaTopology.MemoryGroups = state
	owner := types.UID(task.UID)
	require.NoError(t, state.ValidateOwner(node.NumaTopology, owner, original.MemoryGroups))
	state.ApplyOwner(owner, original.MemoryGroups)
	session := &Session{ClusterInfo: &api.ClusterInfo{Nodes: nodes, PodGroupInfos: jobs}}
	session.AddEventHandler(&EventHandler{
		AllocateFunc: func(event *Event) {
			state.ApplyOwner(owner, event.Task.NUMAPlacement.MemoryGroups)
		},
		DeallocateFunc: func(event *Event) { state.RemoveOwner(owner) },
	})
	statement := session.Statement()
	require.NoError(t, statement.Evict(task, "move", eviction_info.EvictionMetadata{}))
	task.NUMAPlacement = original.Clone()
	task.NUMAPlacement.MemoryGroups[0].Mask = 2
	require.NoError(t, statement.Pipeline(task, "node", false))
	require.Contains(t, state.Groups, pod_info.NUMAMask(2))
	require.NotContains(t, state.Groups, pod_info.NUMAMask(1))
	require.Equal(t, pod_status.Pipelined, task.Status)
	statement.Discard()
	require.True(t, task.NUMAPlacement.Equal(original))
	require.Equal(t, pod_status.Running, task.Status)
	require.Contains(t, state.Groups, pod_info.NUMAMask(1))
	require.NotContains(t, state.Groups, pod_info.NUMAMask(2))
}
