// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"flag"
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
)

var reclaimLargeJobSearchBudget = flag.String(
	"reclaim-large-job-search-budget",
	"",
	"scenario search job budget for BenchmarkReclaimLargeJobs; action uses the same budget and generators use half",
)

var reclaimLargeJobNodeLocalGreedyBudget = flag.String(
	"reclaim-large-job-node-local-greedy-budget",
	"",
	"optional NodeLocalGreedy generator budget override for BenchmarkReclaimLargeJobs",
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

func benchmarkReclaimLargeJobs(b *testing.B, numNodes int) {
	defer gock.Off()

	params := largeJobReclaimParams{
		TopologyName:            "very large job reclaim benchmark",
		PendingJobName:          "very-large-job",
		NumNodes:                numNodes,
		GPUsPerNode:             8,
		NumRunningJobs:          numNodes * 8,
		RunningJobGPUsPerTask:   1,
		PendingJobGPUsPerTask:   8,
		PendingJobTasks:         numNodes / 2,
		Queue0DeservedGPUs:      0,
		Queue1DeservedGPUs:      numNodes * 8,
		NumberOfCacheBinds:      numNodes * 4,
		NumberOfCacheEvictions:  numNodes * 10,
		NumberOfPipelineActions: numNodes * 10,
	}

	topology := buildLargeJobReclaimTopology(params)

	for b.Loop() {
		ctrl := gomock.NewController(b)
		ssn := test_utils.BuildSession(topology, ctrl)
		if budgets := reclaimLargeJobScenarioSearchBudgets(); budgets != nil {
			ssn.Config.ScenarioSearchBudgets = budgets
		}
		action := reclaim.New()
		action.Execute(ssn)
		assertVeryLargeJobReclaimed(b, ssn, params)
		ctrl.Finish()
	}
}

func reclaimLargeJobScenarioSearchBudgets() *kaiv1.ScenarioSearchBudgets {
	if *reclaimLargeJobSearchBudget == "" {
		return nil
	}
	jobBudget, err := time.ParseDuration(*reclaimLargeJobSearchBudget)
	if err != nil {
		panic(fmt.Sprintf("invalid reclaim-large-job-search-budget: %v", err))
	}
	generatorBudget := jobBudget / 2
	nodeLocalGreedyBudget := generatorBudget
	if *reclaimLargeJobNodeLocalGreedyBudget != "" {
		parsedNodeLocalGreedyBudget, err := time.ParseDuration(*reclaimLargeJobNodeLocalGreedyBudget)
		if err != nil {
			panic(fmt.Sprintf("invalid reclaim-large-job-node-local-greedy-budget: %v", err))
		}
		nodeLocalGreedyBudget = parsedNodeLocalGreedyBudget
	}
	return &kaiv1.ScenarioSearchBudgets{
		MaxActionSearchDuration: map[string]metav1.Duration{
			commonconstants.ActionDefault: {Duration: jobBudget},
			commonconstants.ActionReclaim: {Duration: jobBudget},
		},
		MaxJobSearchDuration: &metav1.Duration{Duration: jobBudget},
		MinJobSearchDuration: &metav1.Duration{},
		MaxGeneratorSearchDuration: map[string]metav1.Duration{
			commonconstants.ActionDefault:            {Duration: generatorBudget},
			commonconstants.GeneratorNodeLocalGreedy: {Duration: nodeLocalGreedyBudget},
			commonconstants.GeneratorMultiNodeGang:   {Duration: generatorBudget},
		},
	}
}

func assertVeryLargeJobReclaimed(b *testing.B, ssn *framework.Session, params largeJobReclaimParams) {
	b.Helper()

	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(params.PendingJobName)]
	if job == nil {
		b.Fatalf("expected %s in session", params.PendingJobName)
	}
	if pending := len(job.PodStatusIndex[pod_status.Pending]); pending != 0 {
		b.Fatalf("expected %s to have no pending tasks after reclaim, got %d", params.PendingJobName, pending)
	}
	if pipelined := len(job.PodStatusIndex[pod_status.Pipelined]); pipelined != params.PendingJobTasks {
		b.Fatalf("expected %s to pipeline %d tasks, got %d", params.PendingJobName, params.PendingJobTasks, pipelined)
	}

	releasingTasks := 0
	for _, clusterJob := range ssn.ClusterInfo.PodGroupInfos {
		releasingTasks += len(clusterJob.PodStatusIndex[pod_status.Releasing])
	}
	expectedReleasingTasks := int(float64(params.PendingJobTasks) *
		params.PendingJobGPUsPerTask / params.RunningJobGPUsPerTask)
	if releasingTasks != expectedReleasingTasks {
		b.Fatalf("expected %d victim tasks to be releasing after reclaim, got %d",
			expectedReleasingTasks, releasingTasks)
	}
}
