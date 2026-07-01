// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type largeJobReclaimParams struct {
	TopologyName            string
	PendingJobName          string
	NumNodes                int
	GPUsPerNode             int
	NumRunningJobs          int
	RunningJobGPUsPerTask   float64
	PendingJobGPUsPerTask   float64
	PendingJobTasks         int
	Queue0DeservedGPUs      int
	Queue1DeservedGPUs      int
	NumberOfCacheBinds      int
	NumberOfCacheEvictions  int
	NumberOfPipelineActions int
}

func buildLargeJobReclaimTopology(params largeJobReclaimParams) test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name:  params.TopologyName,
		Nodes: buildLargeJobReclaimNodes(params),
		Jobs:  buildLargeJobReclaimJobs(params),
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

func buildLargeJobReclaimNodes(params largeJobReclaimParams) map[string]nodes_fake.TestNodeBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic, params.NumNodes)
	for i := 0; i < params.NumNodes; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: params.GPUsPerNode,
		}
	}
	return nodes
}

func buildLargeJobReclaimJobs(params largeJobReclaimParams) []*jobs_fake.TestJobBasic {
	jobs := make([]*jobs_fake.TestJobBasic, 0, params.NumRunningJobs+1)
	for i := 0; i < params.NumRunningJobs; i++ {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("running-job-%d", i),
			RequiredGPUsPerTask: params.RunningJobGPUsPerTask,
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

	pendingJob := &jobs_fake.TestJobBasic{
		Name:                params.PendingJobName,
		RequiredGPUsPerTask: params.PendingJobGPUsPerTask,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue-1",
		Tasks:               make([]*tasks_fake.TestTaskBasic, params.PendingJobTasks),
	}
	for i := 0; i < params.PendingJobTasks; i++ {
		pendingJob.Tasks[i] = &tasks_fake.TestTaskBasic{
			State: pod_status.Pending,
		}
	}

	jobs = append(jobs, pendingJob)
	return jobs
}
