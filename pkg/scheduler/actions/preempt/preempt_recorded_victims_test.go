// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"fmt"
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// A higher-priority gang of whole-node pods preempts an elastic job of its queue with two pods on
// every node. The probes of the search record the elastic pods of the smaller probes, and the
// committed solution evicts each pod once.
func TestPreemptEvictsEachVictimOnce(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	for _, c := range []struct{ nodes, gangSize int }{{4, 2}, {6, 3}, {8, 4}, {8, 8}} {
		t.Run(fmt.Sprintf("%d nodes, gang of %d", c.nodes, c.gangSize), func(t *testing.T) {
			controller := NewController(t)
			defer controller.Finish()
			ssn := test_utils.BuildSession(elasticVictimsTopology(c.nodes, c.gangSize), controller)

			preempt.New().Execute(ssn)

			expectGangOnVictimNodes(t, ssn, c.gangSize)
		})
	}
}

// elasticVictimsTopology fills nodes of 8 GPUs with an elastic job of two 4-GPU pods per node
// and adds a pending gang of gangSize 8-GPU pods of higher priority in the same queue. The cache mock
// fails the test if more evictions than the 2*gangSize needed are committed.
func elasticVictimsTopology(nodes, gangSize int) test_utils.TestTopologyBasic {
	nodesMap := map[string]nodes_fake.TestNodeBasic{}
	var elasticTasks []*tasks_fake.TestTaskBasic
	for i := range nodes {
		nodeName := fmt.Sprintf("node%d", i)
		nodesMap[nodeName] = nodes_fake.TestNodeBasic{GPUs: 8}
		elasticTasks = append(elasticTasks,
			&tasks_fake.TestTaskBasic{NodeName: nodeName, State: pod_status.Running},
			&tasks_fake.TestTaskBasic{NodeName: nodeName, State: pod_status.Running})
	}
	var gangTasks []*tasks_fake.TestTaskBasic
	for range gangSize {
		gangTasks = append(gangTasks, &tasks_fake.TestTaskBasic{State: pod_status.Pending})
	}
	return test_utils.TestTopologyBasic{
		Name: "elastic victims",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "elastic",
				RequiredGPUsPerTask: 4,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
				Tasks:               elasticTasks,
			},
			{
				Name:                "gang",
				RequiredGPUsPerTask: 8,
				Priority:            constants.PriorityBuildNumber,
				QueueName:           "queue0",
				RootSubGroupSet:     jobs_fake.DefaultSubGroup(int32(gangSize)),
				Tasks:               gangTasks,
			},
		},
		Nodes:  nodesMap,
		Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: float64(8 * gangSize)}},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions:  2 * gangSize,
				NumberOfPipelineActions: gangSize,
			},
		},
	}
}

// expectGangOnVictimNodes checks that every gang pod is pipelined, onto exactly the nodes whose two
// elastic pods are evicted, and that no other elastic pod is.
func expectGangOnVictimNodes(t *testing.T, ssn *framework.Session, gangSize int) {
	t.Helper()
	gangNodes, victimNodes := map[string]int{}, map[string]int{}
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		for _, task := range job.GetAllPodsMap() {
			switch {
			case job.Name == "gang" && task.Status == pod_status.Pipelined:
				gangNodes[task.NodeName]++
			case job.Name == "gang":
				t.Errorf("gang pod %s is %s, want %s", task.Name, task.Status, pod_status.Pipelined)
			case task.Status == pod_status.Releasing:
				victimNodes[task.NodeName]++
			case task.Status != pod_status.Running:
				t.Errorf("elastic pod %s is %s, want %s or %s", task.Name, task.Status,
					pod_status.Running, pod_status.Releasing)
			}
		}
	}
	if len(gangNodes) != gangSize {
		t.Errorf("gang pods pipelined on %d nodes, want %d: %v", len(gangNodes), gangSize, gangNodes)
	}
	for nodeName := range gangNodes {
		if gangNodes[nodeName] != 1 || victimNodes[nodeName] != 2 {
			t.Errorf("node %s holds %d gang pods and %d evicted elastic pods, want 1 and 2",
				nodeName, gangNodes[nodeName], victimNodes[nodeName])
		}
	}
	for nodeName := range victimNodes {
		if _, found := gangNodes[nodeName]; !found {
			t.Errorf("elastic pods evicted on node %s, which the gang does not use", nodeName)
		}
	}
}
