// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
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
	FailureMode           unschedulableDistributedReclaimFailureMode
}

type unschedulableDistributedReclaimFailureMode int

const (
	unschedulableDistributedReclaimProportion unschedulableDistributedReclaimFailureMode = iota
	unschedulableDistributedReclaimAntiAffinity
	unschedulableDistributedReclaimTopology
)

const (
	unschedulableDistributedJobName = "unschedulable-distributed-job"
	unschedulableDistributedRackKey = "benchmark.kai.scheduler/rack"
)

func BenchmarkReclaimUnschedulableDistributedJob_10Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 10)
}

func BenchmarkReclaimUnschedulableDistributedJob_50Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 50)
}

func BenchmarkReclaimUnschedulableDistributedJob_100Node(b *testing.B) {
	benchmarkReclaimUnschedulableDistributedJob(b, 100)
}

func BenchmarkReclaimUnschedulableDistributedJob_AntiAffinity100Node(b *testing.B) {
	params := defaultUnschedulableDistributedReclaimBenchmarkParams(100)
	params.Queue0DeservedGPUs = (params.NumNodes * params.GPUsPerNode) -
		(params.PodsPerDistributedJob * params.GPUsPerNode)
	params.FailureMode = unschedulableDistributedReclaimAntiAffinity
	benchmarkReclaimUnschedulableDistributedJobWithParams(b, params)
}

func BenchmarkReclaimUnschedulableDistributedJob_Topology100Node(b *testing.B) {
	params := defaultUnschedulableDistributedReclaimBenchmarkParams(100)
	params.Queue0DeservedGPUs = (params.NumNodes * params.GPUsPerNode) -
		(params.PodsPerDistributedJob * params.GPUsPerNode)
	params.FailureMode = unschedulableDistributedReclaimTopology
	benchmarkReclaimUnschedulableDistributedJobWithParams(b, params)
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
	benchmarkReclaimUnschedulableDistributedJobWithParams(
		b, defaultUnschedulableDistributedReclaimBenchmarkParams(numNodes),
	)
}

func benchmarkReclaimUnschedulableDistributedJobWithParams(
	b *testing.B, params unschedulableDistributedReclaimBenchmarkParams,
) {
	defer gock.Off()

	test_utils.InitTestingInfrastructure()
	topology := buildUnschedulableDistributedReclaimBenchmarkTopology(params)
	action := reclaim.New()

	before := reclaimScenarioStateTotals(b, "simulated", "duplicate")

	for b.Loop() {
		controller := NewController(b)
		ssn := test_utils.BuildSession(topology, controller)
		action.Execute(ssn)
		controller.Finish()
	}

	after := reclaimScenarioStateTotals(b, "simulated", "duplicate")
	iterations := float64(b.N)
	b.ReportMetric((after["simulated"]-before["simulated"])/iterations, "simulated/op")
	b.ReportMetric((after["duplicate"]-before["duplicate"])/iterations, "duplicate/op")
}

func TestReclaimSkipsDuplicateScenariosBeforeSimulation(t *testing.T) {
	defer gock.Off()

	test_utils.InitTestingInfrastructure()
	topology := buildUnschedulableDistributedReclaimBenchmarkTopology(
		defaultUnschedulableDistributedReclaimBenchmarkParams(50),
	)
	action := reclaim.New()
	duplicateBefore := reclaimScenarioStateTotals(t, "duplicate")["duplicate"]

	controller := NewController(t)
	ssn := test_utils.BuildSession(topology, controller)
	action.Execute(ssn)
	controller.Finish()

	require.Greater(t, reclaimScenarioStateTotals(t, "duplicate")["duplicate"], duplicateBefore)
}

func reclaimScenarioStateTotals(tb testing.TB, states ...string) map[string]float64 {
	tb.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(tb, err)

	totals := make(map[string]float64, len(states))
	for _, state := range states {
		totals[state] = 0
	}
	for _, family := range families {
		if family.GetName() != "scenario_search_scenarios_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["action"] != "reclaim" {
				continue
			}
			if _, wanted := totals[labels["state"]]; wanted {
				totals[labels["state"]] += metric.GetCounter().GetValue()
			}
		}
	}
	return totals
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
		node := nodes_fake.TestNodeBasic{
			GPUs: params.GPUsPerNode,
		}
		if params.FailureMode == unschedulableDistributedReclaimAntiAffinity ||
			params.FailureMode == unschedulableDistributedReclaimTopology {
			node.Labels = map[string]string{
				unschedulableDistributedRackKey: fmt.Sprintf("rack-%d", i%unschedulableDistributedRackCount(params)),
			}
		}
		nodes[fmt.Sprintf("node%d", i)] = node
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

	distributedJob := &jobs_fake.TestJobBasic{
		Name:                unschedulableDistributedJobName,
		RequiredGPUsPerTask: 8,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue-1",
		Tasks:               make([]*tasks_fake.TestTaskBasic, params.PodsPerDistributedJob),
	}
	if params.FailureMode == unschedulableDistributedReclaimTopology {
		distributedJob.RootSubGroupSet = subgroup_info.NewSubGroupSet(
			subgroup_info.RootSubGroupSetName,
			&topology_info.TopologyConstraintInfo{
				Topology:      "benchmark-topology",
				RequiredLevel: unschedulableDistributedRackKey,
			},
		)
	}
	for i := 0; i < params.PodsPerDistributedJob; i++ {
		task := &tasks_fake.TestTaskBasic{State: pod_status.Pending}
		if params.FailureMode == unschedulableDistributedReclaimAntiAffinity {
			task.PodAffinityLabels = map[string]string{
				"benchmark.kai.scheduler/job": "distributed-reclaimer",
			}
			task.PodAntiAffinityTopologyKey = unschedulableDistributedRackKey
		}
		distributedJob.Tasks[i] = task
	}

	jobs = append(jobs, distributedJob)

	return test_utils.TestTopologyBasic{
		Name:       "unschedulable distributed reclaim benchmark",
		Jobs:       jobs,
		Nodes:      nodes,
		Topologies: buildUnschedulableDistributedReclaimTopologies(params),
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

func unschedulableDistributedRackCount(params unschedulableDistributedReclaimBenchmarkParams) int {
	switch params.FailureMode {
	case unschedulableDistributedReclaimAntiAffinity:
		return params.PodsPerDistributedJob - 1
	case unschedulableDistributedReclaimTopology:
		return params.PodsPerDistributedJob + 2
	default:
		return 0
	}
}

func buildUnschedulableDistributedReclaimTopologies(
	params unschedulableDistributedReclaimBenchmarkParams,
) []*kaiv1alpha1.Topology {
	if params.FailureMode != unschedulableDistributedReclaimTopology {
		return nil
	}
	return []*kaiv1alpha1.Topology{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "benchmark-topology"},
			Spec: kaiv1alpha1.TopologySpec{
				Levels: []kaiv1alpha1.TopologyLevel{
					{NodeLabel: unschedulableDistributedRackKey},
				},
			},
		},
	}
}
