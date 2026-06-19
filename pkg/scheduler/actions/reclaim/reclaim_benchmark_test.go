// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"fmt"
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
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

// TestReclaimDeduplicatesEquivalentScenarios verifies that the scenario dedup cache
// actually skips duplicate candidates before simulation. It uses a topology where the
// distributed job needs fewer nodes than are available, so multiple outer accumulation
// orderings produce the same K-prefix sub-scenarios — confirming dedup fires.
func TestReclaimDeduplicatesEquivalentScenarios(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()

	// 20 nodes, distributed job needs 5 nodes (40 GPUs).
	// queue-0 deserved = 121 GPUs → reclaimable = 160 - 121 = 39 GPUs < 40 needed → unschedulable.
	// With 20 nodes available but only ~5 needed, multiple accumulation orderings produce
	// the same K-prefix sub-scenarios — exactly the duplicate pattern from #1719.
	const numNodes = 20
	const gpusPerNode = 8
	const distributedJobTasks = 5
	const distributedJobGPUsPerTask = gpusPerNode

	nodes := make(map[string]nodes_fake.TestNodeBasic, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{GPUs: gpusPerNode}
	}

	totalRunning := numNodes * gpusPerNode
	jobs := make([]*jobs_fake.TestJobBasic, 0, totalRunning+1)
	for i := 0; i < totalRunning; i++ {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("running-job-%d", i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue-0",
			Tasks: []*tasks_fake.TestTaskBasic{
				{NodeName: fmt.Sprintf("node%d", i%numNodes), State: pod_status.Running},
			},
		})
	}

	distributedTasks := make([]*tasks_fake.TestTaskBasic, distributedJobTasks)
	for i := range distributedTasks {
		distributedTasks[i] = &tasks_fake.TestTaskBasic{State: pod_status.Pending}
	}
	jobs = append(jobs, &jobs_fake.TestJobBasic{
		Name:                "dedup-test-distributed-job",
		RequiredGPUsPerTask: float64(distributedJobGPUsPerTask),
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue-1",
		Tasks:               distributedTasks,
	})

	topology := test_utils.TestTopologyBasic{
		Name:  "dedup test topology",
		Jobs:  jobs,
		Nodes: nodes,
		Queues: []test_utils.TestQueueBasic{
			// queue-0 deserved = 121 → only 39 GPUs reclaimable, job needs 40 → unschedulable
			{Name: "queue-0", DeservedGPUs: float64(numNodes*gpusPerNode - distributedJobTasks*distributedJobGPUsPerTask + 1), GPUOverQuotaWeight: 0},
			{Name: "queue-1", DeservedGPUs: float64(distributedJobTasks * distributedJobGPUsPerTask), GPUOverQuotaWeight: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}

	controller := NewController(t)
	defer controller.Finish()

	before := testutil.ToFloat64(metrics.ScenariosDedupedByAction().WithLabelValues("reclaim"))

	ssn := test_utils.BuildSession(topology, controller)
	metrics.SetCurrentAction("reclaim")
	action := reclaim.New()
	action.Execute(ssn)

	after := testutil.ToFloat64(metrics.ScenariosDedupedByAction().WithLabelValues("reclaim"))
	deduped := after - before
	if deduped == 0 {
		t.Errorf("expected scenarios_deduped_by_action to increase, got delta=0 — deduplication is not working")
	}
	t.Logf("scenarios_deduped_by_action delta = %.0f", deduped)
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
