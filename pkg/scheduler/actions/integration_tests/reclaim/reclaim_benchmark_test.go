// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"
	"testing"

	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func init() {
	test_utils.InitTestingInfrastructure()
}

func BenchmarkReclaimLargeJobs_10Node(b *testing.B) {
	benchmarkReclaimLargeJobs(b, 10)
}

func BenchmarkReclaimLargeJobs_50Node(b *testing.B) {
	benchmarkReclaimLargeJobs(b, 50)
}

func BenchmarkReclaimLargeJobs_100Node(b *testing.B) {
	benchmarkReclaimLargeJobs(b, 100)
}

func BenchmarkReclaimLargeJobs_200Node(b *testing.B) {
	benchmarkReclaimLargeJobs(b, 200)
}

func BenchmarkReclaimLargeJobs_500Node(b *testing.B) {
	benchmarkReclaimLargeJobs(b, 500)
}

func BenchmarkReclaimLargeJobs_1000Node(b *testing.B) {
	benchmarkReclaimLargeJobs(b, 1000)
}

type VeryLargeJobReclaimParams struct {
	NumNodes                int
	GPUsPerNode             int
	NumJobs                 int
	GPUsPerTask             int
	VeryLargeJobGPUsPerTask int
	VeryLargeJobTasks       int
	Queue0DeservedGPUs      int
	Queue1DeservedGPUs      int
	NumberOfCacheBinds      int
	NumberOfCacheEvictions  int
	NumberOfPipelineActions int
}

func benchmarkReclaimLargeJobs(b *testing.B, numNodes int) {
	defer gock.Off()

	params := VeryLargeJobReclaimParams{
		NumNodes:                numNodes,
		GPUsPerNode:             8,
		NumJobs:                 numNodes * 8,
		GPUsPerTask:             1,
		VeryLargeJobGPUsPerTask: 8,
		VeryLargeJobTasks:       numNodes / 10,
		Queue0DeservedGPUs:      0,
		Queue1DeservedGPUs:      numNodes * 8,
		NumberOfCacheBinds:      numNodes * 4,
		NumberOfCacheEvictions:  numNodes * 4,
		NumberOfPipelineActions: numNodes * 4,
	}

	topology := buildReclaimTopology(params)

	for b.Loop() {
		ctrl := gomock.NewController(b)
		ssn := test_utils.BuildSession(topology, ctrl)
		action := reclaim.New()
		action.Execute(ssn)
		ctrl.Finish()
	}
}

func buildReclaimTopology(params VeryLargeJobReclaimParams) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic)
	for i := 0; i < params.NumNodes; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: params.GPUsPerNode,
		}
	}

	jobs := make([]*jobs_fake.TestJobBasic, params.NumJobs)
	for i := 0; i < params.NumJobs; i++ {
		jobs[i] = &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("running-job-%d", i),
			RequiredGPUsPerTask: float64(params.GPUsPerTask),
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue-0",
			Tasks: []*tasks_fake.TestTaskBasic{
				{
					NodeName: fmt.Sprintf("node%d", i%params.NumNodes),
					State:    pod_status.Running,
				},
			},
		}
	}

	jobs = append(jobs, &jobs_fake.TestJobBasic{
		Name:                "very-large-job",
		RequiredGPUsPerTask: float64(params.VeryLargeJobGPUsPerTask),
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue-1",
		Tasks:               make([]*tasks_fake.TestTaskBasic, params.VeryLargeJobTasks),
	})

	for i := 0; i < params.VeryLargeJobTasks; i++ {
		jobs[params.NumJobs].Tasks[i] = &tasks_fake.TestTaskBasic{
			State: pod_status.Pending,
		}
	}

	return test_utils.TestTopologyBasic{
		Name:  "very large job reclaim benchmark",
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
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:      params.NumberOfCacheBinds,
				NumberOfCacheEvictions:  params.NumberOfCacheEvictions,
				NumberOfPipelineActions: params.NumberOfPipelineActions,
			},
		},
	}
}
