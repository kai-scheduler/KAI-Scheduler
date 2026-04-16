// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"
	"testing"
	"time"

	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestReclaimLargeJobsTest(t *testing.T) {
	defer gock.Off()

	t.Skipf("Skip ReclaimLargeJobsTest by default")

	for _, testParams := range []VeryLargeJobReclaimParams{
		{
			NumNodes:                10,
			GPUsPerNode:             8,
			NumJobs:                 80,
			GPUsPerTask:             1,
			VeryLargeJobGPUsPerTask: 8,
			VeryLargeJobTasks:       6,
			Queue0DeservedGPUs:      0,
			Queue1DeservedGPUs:      3000,
			NumberOfCacheBinds:      48,
			NumberOfCacheEvictions:  48,
			NumberOfPipelineActions: 48,
		},
		{
			NumNodes:                50,
			GPUsPerNode:             8,
			NumJobs:                 400,
			GPUsPerTask:             1,
			VeryLargeJobGPUsPerTask: 8,
			VeryLargeJobTasks:       25,
			Queue0DeservedGPUs:      0,
			Queue1DeservedGPUs:      3000,
			NumberOfCacheBinds:      200,
			NumberOfCacheEvictions:  200,
			NumberOfPipelineActions: 200,
		},
		{
			NumNodes:                100,
			GPUsPerNode:             8,
			NumJobs:                 800,
			GPUsPerTask:             1,
			VeryLargeJobGPUsPerTask: 8,
			VeryLargeJobTasks:       60,
			Queue0DeservedGPUs:      0,
			Queue1DeservedGPUs:      3000,
			NumberOfCacheBinds:      480,
			NumberOfCacheEvictions:  480,
			NumberOfPipelineActions: 480,
		},
		{
			NumNodes:                200,
			GPUsPerNode:             8,
			NumJobs:                 1600,
			GPUsPerTask:             1,
			VeryLargeJobGPUsPerTask: 8,
			VeryLargeJobTasks:       120,
			Queue0DeservedGPUs:      0,
			Queue1DeservedGPUs:      3000,
			NumberOfCacheBinds:      120 * 8,
			NumberOfCacheEvictions:  120 * 8,
			NumberOfPipelineActions: 120 * 8,
		},
	} {
		startTime := time.Now()
		integration_tests_utils.RunTests(t, veryLargeJobReclaim(testParams))
		t.Logf("Test for %d nodes and %d jobs took %s", testParams.NumNodes, testParams.NumJobs, time.Since(startTime))
	}
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

func veryLargeJobReclaim(params VeryLargeJobReclaimParams) []integration_tests_utils.TestTopologyMetadata {
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

	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name:  "very large job reclaim",
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
			},
		},
	}
}
