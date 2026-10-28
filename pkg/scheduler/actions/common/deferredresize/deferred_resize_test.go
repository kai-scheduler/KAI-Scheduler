// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deferredresize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/deferredresize"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func deferredTask(nodeName string) *tasks_fake.TestTaskBasic {
	return &tasks_fake.TestTaskBasic{
		State:        pod_status.Running,
		NodeName:     nodeName,
		ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: 1, Deferred: true},
	}
}

func victimJob(name, nodeName string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name: name, QueueName: "queue0", Priority: constants.PriorityTrainNumber, RequiredCPUsPerTask: 2,
		Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: nodeName}},
	}
}

// Each node holds a 3-CPU resizing pod (1 enacted) and a 2-CPU victim, so both nodes are over-committed.
func buildSession(t *testing.T, resizer *jobs_fake.TestJobBasic, enabled bool) *framework.Session {
	test_utils.InitTestingInfrastructure()
	resizer.Name, resizer.QueueName, resizer.RequiredCPUsPerTask = "resizer", "queue0", 3
	resizer.Priority = constants.PriorityInteractivePreemptibleNumber
	return test_utils.BuildSession(test_utils.TestTopologyBasic{
		Jobs: []*jobs_fake.TestJobBasic{resizer, victimJob("victim0", "node0"), victimJob("victim1", "node1")},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {CPUMillis: 4},
			"node1": {CPUMillis: 4},
		},
		Queues:                   []test_utils.TestQueueBasic{{Name: "queue0"}},
		Mocks:                    &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 1}},
		DeferredResizePreemption: enabled,
	}, gomock.NewController(t))
}

type solveCall struct {
	job  *podgroup_info.PodGroupInfo
	node string
}

func recordingSolve(calls *[]solveCall) deferredresize.Solve {
	return func(job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) (*framework.Statement, *solvers.SearchResult) {
		*calls = append(*calls, solveCall{job, node.Name})
		return nil, nil
	}
}

func neverStop(*solvers.SearchResult) bool { return false }

func TestNonPendingJobFilter(t *testing.T) {
	ssn := buildSession(t, &jobs_fake.TestJobBasic{Tasks: []*tasks_fake.TestTaskBasic{deferredTask("node0")}}, true)
	filter := deferredresize.NonPendingJobFilter(ssn)
	require.NotNil(t, filter)
	assert.True(t, filter(ssn.ClusterInfo.PodGroupInfos["resizer"]))
	assert.False(t, filter(ssn.ClusterInfo.PodGroupInfos["victim0"]))

	ssn.OverrideDeferredResizePreemption(false)
	assert.Nil(t, deferredresize.NonPendingJobFilter(ssn))
}

func TestHasActiveAllocatedTaskOnNode(t *testing.T) {
	ssn := buildSession(t, &jobs_fake.TestJobBasic{Tasks: []*tasks_fake.TestTaskBasic{deferredTask("node0")}}, true)
	victim := ssn.ClusterInfo.PodGroupInfos["victim0"]
	assert.True(t, deferredresize.HasActiveAllocatedTaskOnNode(victim, "node0"))
	assert.False(t, deferredresize.HasActiveAllocatedTaskOnNode(victim, "node1"))
}

func TestAttemptForJob_SolvesEachDeferredTaskWithAStandIn(t *testing.T) {
	ssn := buildSession(t, &jobs_fake.TestJobBasic{
		RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
		Tasks: []*tasks_fake.TestTaskBasic{
			deferredTask("node1"), deferredTask("node0"),
			{State: pod_status.Running, NodeName: "node0"},
			{State: pod_status.Pending},
		},
	}, true)
	job := ssn.ClusterInfo.PodGroupInfos["resizer"]

	var calls []solveCall
	require.False(t, deferredresize.AttemptForJob(ssn, job, framework.Preempt, recordingSolve(&calls), neverStop))

	require.Len(t, calls, 2)
	assert.Equal(t, "node1", calls[0].node, "tasks are tried in task order")
	assert.Equal(t, "node0", calls[1].node)
	for i, call := range calls {
		resizing := taskByName(job, []string{"resizer-0", "resizer-1"}[i])
		require.Len(t, call.job.GetAllPodsMap(), 3, "pending pods are left out")
		standIn := call.job.GetAllPodsMap()[resizing.UID]
		assert.True(t, standIn.IsResizeStandIn)
		assert.Equal(t, pod_status.Pending, standIn.Status)
		assert.Equal(t, resizing.NodeName, standIn.NodeName)
		assert.True(t, standIn.ResReqVector.IsZero(), "the node already charges the target size")
		assert.Equal(t, pod_status.Running, resizing.Status)
		assert.False(t, resizing.IsResizeStandIn)
	}
}

func TestAttemptForJob_Skips(t *testing.T) {
	tests := []struct {
		name    string
		resizer *jobs_fake.TestJobBasic
		enabled bool
		mutate  func(ssn *framework.Session)
	}{
		{
			name:    "feature disabled",
			resizer: &jobs_fake.TestJobBasic{Tasks: []*tasks_fake.TestTaskBasic{deferredTask("node0")}},
		},
		{
			name: "gang not secured",
			resizer: &jobs_fake.TestJobBasic{
				RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
				Tasks:           []*tasks_fake.TestTaskBasic{deferredTask("node0"), {State: pod_status.Pending}},
			},
			enabled: true,
		},
		{
			name:    "victim already terminating",
			resizer: &jobs_fake.TestJobBasic{Tasks: []*tasks_fake.TestTaskBasic{deferredTask("node0")}},
			enabled: true,
			mutate: func(ssn *framework.Session) {
				stmt := ssn.Statement()
				require.NoError(t, stmt.Evict(taskByName(ssn.ClusterInfo.PodGroupInfos["victim0"], "victim0-0"), "",
					eviction_info.EvictionMetadata{}))
				require.NoError(t, stmt.Commit())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := buildSession(t, tt.resizer, tt.enabled)
			if tt.mutate != nil {
				tt.mutate(ssn)
			}
			var calls []solveCall
			job := ssn.ClusterInfo.PodGroupInfos["resizer"]
			require.False(t, deferredresize.AttemptForJob(ssn, job, framework.Preempt, recordingSolve(&calls), neverStop))
			assert.Empty(t, calls)
		})
	}
}

func TestAttemptForJob_CommitsTheVictimsOnly(t *testing.T) {
	ssn := buildSession(t, &jobs_fake.TestJobBasic{Tasks: []*tasks_fake.TestTaskBasic{deferredTask("node0")}}, true)
	victim := taskByName(ssn.ClusterInfo.PodGroupInfos["victim0"], "victim0-0")
	solve := func(*podgroup_info.PodGroupInfo, *node_info.NodeInfo) (*framework.Statement, *solvers.SearchResult) {
		stmt := ssn.Statement()
		require.NoError(t, stmt.Evict(victim, "", eviction_info.EvictionMetadata{}))
		return stmt, nil
	}

	job := ssn.ClusterInfo.PodGroupInfos["resizer"]
	require.False(t, deferredresize.AttemptForJob(ssn, job, framework.Preempt, solve, neverStop))

	assert.Equal(t, pod_status.Releasing, victim.Status)
	resizing := taskByName(job, "resizer-0")
	assert.Equal(t, pod_status.Running, resizing.Status)
	assert.True(t, ssn.ClusterInfo.Nodes["node0"].IsInPlaceResizeAllocatable(resizing))
}

func TestAttemptForJob_StopsWhenTheSearchMustStop(t *testing.T) {
	ssn := buildSession(t, &jobs_fake.TestJobBasic{
		Tasks: []*tasks_fake.TestTaskBasic{deferredTask("node0"), deferredTask("node1")},
	}, true)
	var calls []solveCall
	stop := func(*solvers.SearchResult) bool { return true }
	job := ssn.ClusterInfo.PodGroupInfos["resizer"]
	require.True(t, deferredresize.AttemptForJob(ssn, job, framework.Preempt, recordingSolve(&calls), stop))
	assert.Len(t, calls, 1)
}

func taskByName(job *podgroup_info.PodGroupInfo, name string) *pod_info.PodInfo {
	for _, task := range job.GetAllPodsMap() {
		if task.Name == name {
			return task
		}
	}
	return nil
}
