// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"fmt"
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type unschedulableDistributedReclaimBenchmarkParams struct {
	NumNodes              int
	GPUsPerNode           int
	PodsPerDistributedJob int
	RunningJobsPerNode    int
	Queue0DeservedGPUs    int
	Queue1DeservedGPUs    int
}

func BenchmarkReclaimUnschedulableDistributedJob_10Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 10)
}

func BenchmarkReclaimUnschedulableDistributedJob_50Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 50)
}

func BenchmarkReclaimUnschedulableDistributedJob_100Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 100)
}

func BenchmarkReclaimUnschedulableDistributedJob_200Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 200)
}

func BenchmarkReclaimUnschedulableDistributedJob_500Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 500)
}

func BenchmarkReclaimUnschedulableDistributedJob_1000Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 1000)
}

func benchmarkReclaimUnschedulableDistributedJob(b *testing.B, numNodes int) {
	defer gock.Off()

	test_utils.InitTestingInfrastructure()
	topology := buildUnschedulableDistributedReclaimBenchmarkTopology(
		defaultUnschedulableDistributedReclaimBenchmarkParams(numNodes),
	)
	action := reclaim.New()

	for b.Loop() {
		controller := NewController(b)
		ssn := test_utils.BuildSession(topology, controller)
		action.Execute(ssn)
		controller.Finish()
	}
}

func defaultUnschedulableDistributedReclaimBenchmarkParams(numNodes int) unschedulableDistributedReclaimBenchmarkParams {
	return unschedulableDistributedReclaimBenchmarkParams{
		NumNodes:              numNodes,
		GPUsPerNode:           8,
		PodsPerDistributedJob: 10,
		RunningJobsPerNode:    8,
		Queue0DeservedGPUs:    (numNodes * 8) - (10 * 8) + 1,
		Queue1DeservedGPUs:    10 * 8,
	}
}

func buildUnschedulableDistributedReclaimBenchmarkTopology(
	params unschedulableDistributedReclaimBenchmarkParams,
) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic, params.NumNodes)
	for i := 0; i < params.NumNodes; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: params.GPUsPerNode,
		}
	}

	totalRunningJobs := params.NumNodes * params.RunningJobsPerNode
	jobs := make([]*jobs_fake.TestJobBasic, 0, totalRunningJobs+1)
	for i := 0; i < totalRunningJobs; i++ {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("running-job-%d", i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue-0",
			Tasks: []*tasks_fake.TestTaskBasic{
				{
					NodeName: fmt.Sprintf("node%d", i%params.NumNodes),
					State:    pod_status.Running,
				},
			},
		})
	}

	distributedTasks := make([]*tasks_fake.TestTaskBasic, params.PodsPerDistributedJob)
	for i := 0; i < params.PodsPerDistributedJob; i++ {
		distributedTasks[i] = &tasks_fake.TestTaskBasic{State: pod_status.Pending}
	}

	jobs = append(jobs, &jobs_fake.TestJobBasic{
		Name:                "unschedulable-distributed-job",
		RequiredGPUsPerTask: 8,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue-1",
		Tasks:               distributedTasks,
	})

	return test_utils.TestTopologyBasic{
		Name:  "unschedulable distributed reclaim benchmark",
		Jobs:  jobs,
		Nodes: nodes,
		Queues: []test_utils.TestQueueBasic{
			{
				Name:               "queue-0",
				DeservedGPUs:       float64(params.Queue0DeservedGPUs),
				GPUOverQuotaWeight: 0,
			},
			{
				Name:               "queue-1",
				DeservedGPUs:       float64(params.Queue1DeservedGPUs),
				GPUOverQuotaWeight: 0,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{},
		},
	}
}
