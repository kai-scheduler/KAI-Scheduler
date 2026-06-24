// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

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

func TestDefaultGeneratorPortfolioPreservesTopologyReclaimCoverage(t *testing.T) {
	defer gock.Off()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	params := defaultUnschedulableDistributedReclaimParams(10)
	topology := buildUnschedulableDistributedReclaimTopology(params)

	ssn := test_utils.BuildSession(topology, ctrl)
	assertDefaultScenarioGeneratorPortfolio(t, ssn)
	assertDefaultScenarioSearchBudgets(t, ssn)
	multiNodeGangEmissions := observeMultiNodeGangScenarios(t, ssn)

	action := reclaim.New()
	action.Execute(ssn)

	if *multiNodeGangEmissions == 0 {
		t.Fatalf("expected default reclaim scenario portfolio to reach %s", commonconstants.GeneratorMultiNodeGang)
	}

	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(unschedulableDistributedJobName)]
	if job == nil {
		t.Fatalf("expected distributed job %q in session", unschedulableDistributedJobName)
	}
	if len(job.PodStatusIndex[pod_status.Pending]) != params.PodsPerDistributedJob {
		t.Fatalf("expected %d pending distributed-job tasks, got %d",
			params.PodsPerDistributedJob, len(job.PodStatusIndex[pod_status.Pending]))
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
		Name:  "unschedulable distributed reclaim topology",
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

func assertDefaultScenarioGeneratorPortfolio(t *testing.T, ssn *framework.Session) {
	t.Helper()

	for _, expectedGenerator := range []string{
		commonconstants.GeneratorNodeLocalGreedy,
		commonconstants.GeneratorMultiNodeGang,
	} {
		foundGenerator := false
		for _, registration := range ssn.ScenarioGeneratorRegistrations {
			if registration.Name != expectedGenerator {
				continue
			}
			foundGenerator = true
			break
		}
		if !foundGenerator {
			t.Fatalf("expected default scenario generator plugins to register %q", expectedGenerator)
		}
	}
}

func observeMultiNodeGangScenarios(t *testing.T, ssn *framework.Session) *int {
	t.Helper()

	for index, registration := range ssn.ScenarioGeneratorRegistrations {
		if registration.Name != commonconstants.GeneratorMultiNodeGang {
			continue
		}
		emissions := 0
		originalFactory := registration.Factory
		ssn.ScenarioGeneratorRegistrations[index].Factory = func(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
			generator := originalFactory(ctx)
			if generator == nil {
				return nil
			}
			return &observedScenarioGenerator{
				ScenarioGenerator: generator,
				onScenario: func() {
					emissions++
				},
			}
		}
		return &emissions
	}
	t.Fatalf("expected default scenario generator plugins to register %q", commonconstants.GeneratorMultiNodeGang)
	return nil
}

func assertDefaultScenarioSearchBudgets(t *testing.T, ssn *framework.Session) {
	t.Helper()

	if ssn.Config == nil || ssn.Config.ScenarioSearchBudgets == nil {
		t.Fatalf("expected default scenario search budgets on session")
	}

	generatorBudgets := ssn.Config.ScenarioSearchBudgets.MaxGeneratorSearchDuration
	if got := generatorBudgets[commonconstants.GeneratorNodeLocalGreedy].Duration; got != 30*time.Second {
		t.Fatalf("expected default %s budget 30s, got %s",
			commonconstants.GeneratorNodeLocalGreedy, got)
	}
	if got := generatorBudgets[commonconstants.GeneratorMultiNodeGang].Duration; got != 2*time.Minute {
		t.Fatalf("expected default %s budget 2m, got %s",
			commonconstants.GeneratorMultiNodeGang, got)
	}
}

type observedScenarioGenerator struct {
	framework.ScenarioGenerator
	onScenario func()
}

func (g *observedScenarioGenerator) Next() api.ScenarioInfo {
	scenario := g.ScenarioGenerator.Next()
	if scenario != nil && g.onScenario != nil {
		g.onScenario()
	}
	return scenario
}
