// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestManySingleGPUJobsSchedulingCycleActions(t *testing.T) {
	actions := manySingleGPUJobsSchedulingCycleActions()
	actionNames := make([]string, 0, len(actions))
	for _, action := range actions {
		actionNames = append(actionNames, string(action.Name()))
	}

	expected := []string{"allocate", "consolidation", "reclaim", "preempt", "stalegangeviction"}
	if !reflect.DeepEqual(actionNames, expected) {
		t.Fatalf("scheduling cycle actions = %v, want %v", actionNames, expected)
	}
}

type manySingleGPUJobsReclaimParams struct {
	NumNodes          int
	GPUsPerNode       int
	RunningJobCount   int
	PendingJobCount   int
	ReclaimMinRuntime *metav1.Duration
}

const (
	manySingleGPURunningQueueName = "many-single-gpu-running-queue"
	manySingleGPUPendingQueueName = "many-single-gpu-pending-queue"
	manySingleGPURunningJobPrefix = "many-single-gpu-running-job-"
	manySingleGPUPendingJobPrefix = "many-single-gpu-pending-job-"
)

func TestManySingleGPUJobsReclaimTopology(t *testing.T) {
	defer gock.Off()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	params := manySingleGPUJobsReclaimParamsWithMinRuntime(10)
	ssn := test_utils.BuildSession(buildManySingleGPUJobsReclaimTopology(params), ctrl)
	if actual := ssn.ClusterInfo.Queues[common_info.QueueID(manySingleGPURunningQueueName)].ReclaimMinRuntime; actual == nil || *actual != *params.ReclaimMinRuntime {
		t.Fatalf("expected running queue reclaim min-runtime %v, got %v", params.ReclaimMinRuntime, actual)
	}

	onJobSolutionStartCalls := 0
	ssn.AddOnJobSolutionStartFn(func() {
		onJobSolutionStartCalls++
	})

	expectedTasks := params.NumNodes * params.GPUsPerNode
	actions := manySingleGPUJobsSchedulingCycleActions()
	actions[0].Execute(ssn)
	if fitErrorTasks := countManySingleGPUFitErrorTasks(ssn); fitErrorTasks != expectedTasks {
		t.Fatalf("expected %d tasks with fit errors after allocate, got %d", expectedTasks, fitErrorTasks)
	}
	for _, action := range actions[1:] {
		action.Execute(ssn)
	}

	if onJobSolutionStartCalls == 0 {
		t.Fatal("expected reclaim to attempt solving pending jobs")
	}

	runningJobs := 0
	pendingJobs := 0
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		switch {
		case strings.HasPrefix(job.Name, manySingleGPURunningJobPrefix):
			runningJobs++
			if len(job.PodStatusIndex[pod_status.Releasing]) != 1 {
				t.Fatalf("expected running job %q to have one releasing task", job.Name)
			}
		case strings.HasPrefix(job.Name, manySingleGPUPendingJobPrefix):
			pendingJobs++
			if len(job.PodStatusIndex[pod_status.Pipelined]) != 1 {
				t.Fatalf("expected pending job %q to have one pipelined task", job.Name)
			}
		}
	}

	if runningJobs != expectedTasks {
		t.Fatalf("expected %d reclaimed running jobs, got %d", expectedTasks, runningJobs)
	}
	if pendingJobs != expectedTasks {
		t.Fatalf("expected %d pipelined pending jobs, got %d", expectedTasks, pendingJobs)
	}
}

func defaultManySingleGPUJobsReclaimParams(numNodes int) manySingleGPUJobsReclaimParams {
	const gpusPerNode = 8
	jobCount := numNodes * gpusPerNode
	return manySingleGPUJobsReclaimParams{
		NumNodes:        numNodes,
		GPUsPerNode:     gpusPerNode,
		RunningJobCount: jobCount,
		PendingJobCount: jobCount,
	}
}

func manySingleGPUJobsReclaimParamsWithMinRuntime(numNodes int) manySingleGPUJobsReclaimParams {
	params := defaultManySingleGPUJobsReclaimParams(numNodes)
	params.ReclaimMinRuntime = &metav1.Duration{Duration: 30 * time.Second}
	return params
}

func buildManySingleGPUJobsReclaimTopology(
	params manySingleGPUJobsReclaimParams,
) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic, params.NumNodes)
	for i := 0; i < params.NumNodes; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{GPUs: params.GPUsPerNode}
	}

	jobs := make([]*jobs_fake.TestJobBasic, 0, params.RunningJobCount+params.PendingJobCount)
	for i := 0; i < params.RunningJobCount; i++ {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("%s%d", manySingleGPURunningJobPrefix, i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           manySingleGPURunningQueueName,
			Tasks: []*tasks_fake.TestTaskBasic{{
				NodeName: fmt.Sprintf("node%d", i%params.NumNodes),
				State:    pod_status.Running,
			}},
		})
	}
	for i := 0; i < params.PendingJobCount; i++ {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("%s%d", manySingleGPUPendingJobPrefix, i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           manySingleGPUPendingQueueName,
			Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
		})
	}

	return test_utils.TestTopologyBasic{
		Name:  "many single GPU jobs reclaim benchmark",
		Nodes: nodes,
		Jobs:  jobs,
		Queues: []test_utils.TestQueueBasic{
			{Name: manySingleGPURunningQueueName, DeservedGPUs: 0, GPUOverQuotaWeight: 0, ReclaimMinRuntime: params.ReclaimMinRuntime},
			{Name: manySingleGPUPendingQueueName, DeservedGPUs: float64(params.PendingJobCount), GPUOverQuotaWeight: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
			NumberOfCacheEvictions:  params.RunningJobCount,
			NumberOfPipelineActions: params.PendingJobCount,
		}},
	}
}
