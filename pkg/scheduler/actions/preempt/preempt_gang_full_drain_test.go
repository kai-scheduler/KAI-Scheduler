// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"fmt"
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestPreemptGangFullDrainMultiVictimPerNode reproduces the "binary-search
// local minimum" failure mode discussed on issue #1591.
//
// Setup (sized to match the production gang from #1591):
//   - 11 nodes, each with 4 GPUs.
//   - 44 running preemptible pods (PriorityTrain), 1 GPU each, 4 per node.
//     Every node is fully occupied (no idle GPU anywhere).
//   - 1 gang job with 11 pending pods, 4 GPUs per pod (a full node each).
//
// Cluster capacity if every victim is evicted = 44 GPUs.
// Gang demand                                  = 44 GPUs (11 pods * 4 GPUs).
//
// To fit any single gang pod, all 4 victims on its target node must be
// evicted. To fit the whole gang, all 44 victims must be evicted across
// 11 different nodes.
//
// What the current JobSolver does:
//  1. searchMaxSolvableK probes k = 1, 2, 4, 8 (capped at n=11).
//  2. Each probe's scenario builder adds *one* victim job at a time and the
//     by_pod_solver only evicts victims from the latest victim's node per
//     scenario. So a probe at k=K only converges if the queue order happens
//     to cluster victims by node and the cumulative recorded set grows to
//     include K fully-drained nodes.
//  3. searchMaxSolvableK finds some maxSolvableK < n. The state captured at
//     that point has fewer drained nodes than n.
//  4. The final all-or-nothing probe at k=n must drain the remaining nodes
//     within a *single* successful scenario — but no single scenario can
//     drain more than one new node beyond the recorded set, so the probe
//     can never succeed for the full gang.
//
// Expected (gang-aware solver): all 11 gang pods Pipelined; all 44 victims
// Releasing.
// Observed (current main): gang stays Pending; preempt fails with "Didn't
// find a preemption strategy". This is identical to the production failure
// captured in the snapshot logs for issue #1591.
func TestPreemptGangFullDrainMultiVictimPerNode(t *testing.T) {
	const (
		nodeCount   = 11 // matches the gang size in production issue #1591
		gpusPerNode = 4
		gangPodGPUs = 4 // each gang pod needs a full node
		queueName   = "queue0"
	)

	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	nodes := map[string]nodes_fake.TestNodeBasic{}
	for i := 0; i < nodeCount; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{GPUs: gpusPerNode}
	}

	// 4 victims per node, one task each (so each victim is 1 GPU).
	jobs := []*jobs_fake.TestJobBasic{}
	for i := 0; i < nodeCount; i++ {
		for j := 0; j < gpusPerNode; j++ {
			jobs = append(jobs, &jobs_fake.TestJobBasic{
				Name:                fmt.Sprintf("victim-n%d-g%d", i, j),
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           queueName,
				Tasks: []*tasks_fake.TestTaskBasic{
					{NodeName: fmt.Sprintf("node%d", i), State: pod_status.Running},
				},
			})
		}
	}

	// Gang of N pods, each requiring a full node (gpusPerNode GPUs).
	gangTasks := []*tasks_fake.TestTaskBasic{}
	for i := 0; i < nodeCount; i++ {
		gangTasks = append(gangTasks, &tasks_fake.TestTaskBasic{State: pod_status.Pending})
	}
	jobs = append(jobs, &jobs_fake.TestJobBasic{
		Name:                "gang-preemptor",
		RequiredGPUsPerTask: gangPodGPUs,
		Priority:            constants.PriorityBuildNumber,
		QueueName:           queueName,
		Tasks:               gangTasks,
	})

	expected := map[string]test_utils.TestExpectedResultBasic{
		"gang-preemptor": {
			GPUsRequired:         float64(nodeCount * gangPodGPUs), // 16
			Status:               pod_status.Pipelined,
			DontValidateGPUGroup: true,
		},
	}
	for i := 0; i < nodeCount; i++ {
		for j := 0; j < gpusPerNode; j++ {
			expected[fmt.Sprintf("victim-n%d-g%d", i, j)] = test_utils.TestExpectedResultBasic{
				GPUsRequired: 1,
				Status:       pod_status.Releasing,
			}
		}
	}

	test := test_utils.TestTopologyBasic{
		Name:  "issue-1591 reproducer: 11-pod gang of 4-GPU pods, fully-packed 11-node × 4-GPU cluster, 1-GPU victims",
		Nodes: nodes,
		Queues: []test_utils.TestQueueBasic{
			{
				Name:               queueName,
				DeservedGPUs:       float64(nodeCount * gpusPerNode), // 16
				GPUOverQuotaWeight: 1,
				MaxAllowedGPUs:     -1,
			},
		},
		Jobs:               jobs,
		JobExpectedResults: expected,
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions:  nodeCount * gpusPerNode, // 16
				NumberOfPipelineActions: nodeCount,               // 4
			},
		},
	}

	ssn := test_utils.BuildSession(test, controller)
	preemptAction := preempt.New()
	preemptAction.Execute(ssn)

	test_utils.MatchExpectedAndRealTasks(t, 0, test, ssn)
}
