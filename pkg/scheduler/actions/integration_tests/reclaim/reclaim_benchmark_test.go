// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"
	"testing"

	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

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

func init() {
	test_utils.InitTestingInfrastructure()
}

func TestUnschedulableDistributedReclaimTopology(t *testing.T) {
	defer gock.Off()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	params := defaultUnschedulableDistributedReclaimParams(10)
	topology := buildUnschedulableDistributedReclaimTopology(params)

	ssn := test_utils.BuildSession(topology, ctrl)
	onJobSolutionStartCalls := 0
	ssn.AddOnJobSolutionStartFn(func() {
		onJobSolutionStartCalls++
	})

	action := reclaim.New()
	action.Execute(ssn)

	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(unschedulableDistributedJobName)]
	if job == nil {
		t.Fatalf("expected distributed job %q in session", unschedulableDistributedJobName)
	}

	if onJobSolutionStartCalls == 0 {
		t.Fatalf("expected reclaim to attempt solving for the distributed job")
	}

	if len(job.PodStatusIndex[pod_status.Pending]) != params.PodsPerDistributedJob {
		t.Fatalf("expected %d pending distributed-job tasks, got %d",
			params.PodsPerDistributedJob, len(job.PodStatusIndex[pod_status.Pending]))
	}

	for _, clusterJob := range ssn.ClusterInfo.PodGroupInfos {
		if len(clusterJob.PodStatusIndex[pod_status.Releasing]) != 0 {
			t.Fatalf("expected no committed reclaimees, found %d releasing tasks on job %q",
				len(clusterJob.PodStatusIndex[pod_status.Releasing]), clusterJob.Name)
		}
		if len(clusterJob.PodStatusIndex[pod_status.Pipelined]) != 0 {
			t.Fatalf("expected no pipelined tasks after failed reclaim, found %d on job %q",
				len(clusterJob.PodStatusIndex[pod_status.Pipelined]), clusterJob.Name)
		}
	}
}

type unschedulableDistributedReclaimParams struct {
	NumNodes                int
	GPUsPerNode             int
	PodsPerDistributedJob   int
	RunningJobsPerNode      int
	Queue0DeservedGPUs      int
	Queue1DeservedGPUs      int
	NumberOfCacheBinds      int
	NumberOfCacheEvictions  int
	NumberOfPipelineActions int
}

const (
	unschedulableDistributedJobName = "unschedulable-distributed-job"
)

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
		NumberOfCacheEvictions:  numNodes * 10,
		NumberOfPipelineActions: numNodes * 10,
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

func defaultUnschedulableDistributedReclaimParams(numNodes int) unschedulableDistributedReclaimParams {
	return unschedulableDistributedReclaimParams{
		NumNodes:                numNodes,
		GPUsPerNode:             8,
		PodsPerDistributedJob:   10,
		RunningJobsPerNode:      8,
		Queue0DeservedGPUs:      (numNodes * 8) - (10 * 8) + 1,
		Queue1DeservedGPUs:      10 * 8,
		NumberOfCacheBinds:      0,
		NumberOfCacheEvictions:  0,
		NumberOfPipelineActions: 0,
	}
}

func buildUnschedulableDistributedReclaimTopology(
	params unschedulableDistributedReclaimParams,
) test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name:  "unschedulable distributed reclaim benchmark",
		Nodes: buildUnschedulableDistributedReclaimNodes(params),
		Jobs:  buildUnschedulableDistributedReclaimJobs(params),
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

func buildUnschedulableDistributedReclaimNodes(
	params unschedulableDistributedReclaimParams,
) map[string]nodes_fake.TestNodeBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic, params.NumNodes)
	for i := 0; i < params.NumNodes; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: params.GPUsPerNode,
		}
	}
	return nodes
}

func buildUnschedulableDistributedReclaimJobs(
	params unschedulableDistributedReclaimParams,
) []*jobs_fake.TestJobBasic {
	runningJobCount := params.NumNodes * params.RunningJobsPerNode
	jobs := make([]*jobs_fake.TestJobBasic, 0, runningJobCount+1)
	for i := 0; i < runningJobCount; i++ {
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

	distributedJob := &jobs_fake.TestJobBasic{
		Name:                unschedulableDistributedJobName,
		RequiredGPUsPerTask: float64(params.GPUsPerNode),
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue-1",
		Tasks:               make([]*tasks_fake.TestTaskBasic, params.PodsPerDistributedJob),
	}
	for i := 0; i < params.PodsPerDistributedJob; i++ {
		distributedJob.Tasks[i] = &tasks_fake.TestTaskBasic{
			State: pod_status.Pending,
		}
	}

	jobs = append(jobs, distributedJob)
	return jobs
}
