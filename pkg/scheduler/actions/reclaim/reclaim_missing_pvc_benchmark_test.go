// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type reclaimMissingPVCBenchmarkParams struct {
	NodeCount             int
	RunningJobCount       int
	MissingPVCJobCount    int
	NodeCPUCores          float64
	OccupiedQueueCPUQuota float64
}

var scaleTestCPURequests = []float64{
	0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.25, 2.5, 2.75,
	3, 3.25, 3.5, 3.75, 4, 4.25, 4.5, 4.75, 5,
}

func TestMissingPVCReclaimTopologyHasVolumeBindingFailure(t *testing.T) {
	defer gock.Off()

	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := buildMissingPVCReclaimBenchmarkTopology(reclaimMissingPVCBenchmarkParams{
		NodeCount:             3,
		RunningJobCount:       6,
		MissingPVCJobCount:    1,
		NodeCPUCores:          8,
		OccupiedQueueCPUQuota: 1000,
	})
	ssn := test_utils.BuildSession(topology, controller)
	onJobSolutionStartCalls := 0
	ssn.AddOnJobSolutionStartFn(func() {
		onJobSolutionStartCalls++
	})

	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("missing-pvc-job-0")]
	require.NotNil(t, job)
	task := job.GetAllPodsMap()[common_info.PodID("missing-pvc-job-0-0")]

	err := ssn.PrePredicateFn(task, job)
	require.Error(t, err)
	require.Contains(t, err.Error(), `persistentvolumeclaim "busybox-missing-pvc" not found`)

	reclaim.New().Execute(ssn)
	require.Zero(t, onJobSolutionStartCalls)
	require.Equal(t, pod_status.Pending, task.Status)
	require.Contains(t, job.TasksFitErrors[task.UID].Error(), `persistentvolumeclaim "busybox-missing-pvc" not found`)
	require.NotEmpty(t, job.JobFitErrors)
	require.Contains(t, job.JobFitErrors[0].DetailedMessage(), "Resources were not found for pod runai-reclaim/missing-pvc-job-0-0 due to:")
}

func BenchmarkReclaimWithMissingPVCJobs(b *testing.B) {
	defer gock.Off()

	test_utils.InitTestingInfrastructure()
	topology := buildMissingPVCReclaimBenchmarkTopology(reclaimMissingPVCBenchmarkParams{
		NodeCount:             1500,
		RunningJobCount:       7370,
		MissingPVCJobCount:    3,
		NodeCPUCores:          80,
		OccupiedQueueCPUQuota: 500000,
	})
	reclaimAction := reclaim.New()

	b.ReportAllocs()
	b.ResetTimer()
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		controller := NewController(b)
		ssn := test_utils.BuildSession(topology, controller)

		b.StartTimer()
		reclaimAction.Execute(ssn)
		b.StopTimer()

		controller.Finish()
	}
}

func buildMissingPVCReclaimBenchmarkTopology(params reclaimMissingPVCBenchmarkParams) test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name:   "reclaim scale with missing PVC jobs",
		Nodes:  buildCPUNodes(params.NodeCount, params.NodeCPUCores),
		Jobs:   buildMissingPVCReclaimBenchmarkJobs(params),
		Queues: buildMissingPVCReclaimBenchmarkQueues(params.OccupiedQueueCPUQuota),
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{},
		},
	}
}

func buildCPUNodes(nodeCount int, cpuCores float64) map[string]nodes_fake.TestNodeBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[cpuNodeName(i)] = nodes_fake.TestNodeBasic{
			CPUMillis: cpuCores,
		}
	}
	return nodes
}

func buildMissingPVCReclaimBenchmarkJobs(
	params reclaimMissingPVCBenchmarkParams,
) []*jobs_fake.TestJobBasic {
	jobs := make([]*jobs_fake.TestJobBasic, 0, params.RunningJobCount+params.MissingPVCJobCount)
	jobs = append(jobs, buildOccupiedQueueCPUJobs(params.NodeCount, params.RunningJobCount)...)
	jobs = append(jobs, buildPendingMissingPVCJobs(params.MissingPVCJobCount)...)
	return jobs
}

func buildOccupiedQueueCPUJobs(nodeCount, jobCount int) []*jobs_fake.TestJobBasic {
	jobs := make([]*jobs_fake.TestJobBasic, 0, jobCount)
	for i := 0; i < jobCount; i++ {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("occupied-cpu-job-%04d", i),
			Namespace:           "runai-test",
			QueueName:           "test-cpu",
			Priority:            constants.PriorityTrainNumber,
			RequiredCPUsPerTask: scaleTestCPURequests[i%len(scaleTestCPURequests)],
			Tasks: []*tasks_fake.TestTaskBasic{
				{
					NodeName: cpuNodeName(i % nodeCount),
					State:    pod_status.Running,
				},
			},
		})
	}
	return jobs
}

func buildPendingMissingPVCJobs(jobCount int) []*jobs_fake.TestJobBasic {
	jobs := make([]*jobs_fake.TestJobBasic, 0, jobCount)
	for i := 0; i < jobCount; i++ {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("missing-pvc-job-%d", i),
			Namespace:           "runai-reclaim",
			QueueName:           "reclaim-cpu",
			Priority:            constants.PriorityTrainNumber,
			RequiredCPUsPerTask: 0.5,
			Tasks: []*tasks_fake.TestTaskBasic{
				{
					State:                      pod_status.Pending,
					PersistentVolumeClaimNames: []string{"busybox-missing-pvc"},
				},
			},
		})
	}
	return jobs
}

func buildMissingPVCReclaimBenchmarkQueues(occupiedQueueCPUQuota float64) []test_utils.TestQueueBasic {
	return []test_utils.TestQueueBasic{
		{
			Name:           "test-cpu",
			Priority:       ptr.To(100),
			DeservedGPUs:   0,
			DeservedCPUs:   ptr.To(occupiedQueueCPUQuota),
			MaxAllowedGPUs: 0,
		},
		{
			Name:           "reclaim-cpu",
			Priority:       ptr.To(210),
			DeservedGPUs:   0,
			DeservedCPUs:   ptr.To(common_info.NoMaxAllowedResource),
			MaxAllowedGPUs: 0,
		},
	}
}

func cpuNodeName(index int) string {
	return fmt.Sprintf("cpu-node-%04d", index)
}
