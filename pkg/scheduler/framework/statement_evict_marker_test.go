// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func buildEvictMarkerSession() (*Statement, *pod_info.PodInfo, *node_info.NodeInfo) {
	jobs := []*jobs_fake.TestJobBasic{{
		Name:                "running_job0",
		RequiredGPUsPerTask: 1,
		QueueName:           "queue0",
		Priority:            constants.PriorityTrainNumber,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0"}},
	}}
	nodes := map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 2}}
	vectorMap := resource_info.NewResourceVectorMap()
	jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
	nodesInfoMap := nodes_fake.BuildNodesInfoMap(nodes, tasksToNodeMap, nil, vectorMap)
	ssn := &Session{ClusterInfo: &api.ClusterInfo{PodGroupInfos: jobsInfoMap, Nodes: nodesInfoMap}}
	stmt := &Statement{operations: []Operation{}, ssn: ssn, sessionID: "1234"}
	task := jobsInfoMap["running_job0"].GetAllPodsMap()["running_job0-0"]
	return stmt, task, nodesInfoMap["node0"]
}

func podInAffinityIndex(node *node_info.NodeInfo, task *pod_info.PodInfo) bool {
	for _, p := range node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo.Pods {
		if p.GetPod().UID == task.Pod.UID {
			return true
		}
	}
	return false
}

// Evict marks the victim as this session's before the node re-indexes it, so the victim
// leaves the inter-pod affinity index; Discard reverses both.
func TestStatementEvictMarksVictimBeforeReindex(t *testing.T) {
	stmt, task, node := buildEvictMarkerSession()
	require.True(t, podInAffinityIndex(node, task))

	require.NoError(t, stmt.Evict(task, "evict", eviction_info.EvictionMetadata{Action: "reclaim", EvictionGangSize: 1}))
	assert.True(t, task.IsVirtualStatus)
	assert.Equal(t, pod_status.Releasing, task.Status)
	assert.False(t, podInAffinityIndex(node, task), "session victim must leave the affinity index")

	stmt.Discard()
	assert.False(t, task.IsVirtualStatus)
	assert.Equal(t, pod_status.Running, task.Status)
	assert.True(t, podInAffinityIndex(node, task), "unevict must re-index the pod")
}

// If node.UpdateTask fails after the marker was set, Evict must restore the marker to its
// previous value and record no operation.
func TestStatementEvictRestoresMarkerWhenNodeUpdateFails(t *testing.T) {
	for _, previous := range []bool{false, true} {
		stmt, task, node := buildEvictMarkerSession()
		// Detach the task from the node: UpdateTask -> RemoveTask then fails with "not found".
		require.NoError(t, node.RemoveTask(task))
		task.IsVirtualStatus = previous

		err := stmt.Evict(task, "evict", eviction_info.EvictionMetadata{Action: "reclaim", EvictionGangSize: 1})
		require.Error(t, err)
		assert.Equal(t, previous, task.IsVirtualStatus, "marker must be restored to its previous value")
		assert.Empty(t, stmt.operations, "a failed eviction must not be recorded")
	}
}
