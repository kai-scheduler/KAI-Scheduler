// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// Only a simulation may undo an eviction of a task found on the node; a real allocation that waits
// for terminating pods pipelines it.
func TestAllocateTaskToNodeUndoesEvictionOnlyInSimulation(t *testing.T) {
	for _, tt := range []struct {
		name           string
		isSimulation   bool
		expectedStatus pod_status.PodStatus
	}{
		{name: "simulation", isSimulation: true, expectedStatus: pod_status.Running},
		{name: "real allocation waiting for terminating pods", isSimulation: false, expectedStatus: pod_status.Pipelined},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vectorMap := resource_info.NewResourceVectorMap()
			jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps([]*jobs_fake.TestJobBasic{{
				Name:                "job0",
				RequiredGPUsPerTask: 1,
				QueueName:           "queue0",
				Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0"}},
			}}, vectorMap)
			nodesInfoMap := nodes_fake.BuildNodesInfoMap(
				map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 1}}, tasksToNodeMap, nil, vectorMap)
			ssn := &framework.Session{ClusterInfo: &api.ClusterInfo{PodGroupInfos: jobsInfoMap, Nodes: nodesInfoMap}}
			stmt := ssn.Statement()
			task := jobsInfoMap["job0"].GetAllPodsMap()["job0-0"]
			require.NoError(t, stmt.Evict(task, "evicted", eviction_info.EvictionMetadata{}))

			require.True(t, allocateTaskToNode(ssn, stmt, task, nodesInfoMap["node0"], tt.isSimulation, true))
			assert.Equal(t, tt.expectedStatus, task.Status)
		})
	}
}
