// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fullnodefirst_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/fullnodefirst"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const (
	reclaimerQueue = "reclaimer-queue"
	borrowerQueue  = "borrower-queue"
)

// drainScenarios exhausts the generator, asserting it terminates with an untyped nil interface.
func drainScenarios(t *testing.T, generator framework.ScenarioGenerator) []*scenario.ByNodeScenario {
	t.Helper()

	var scenarios []*scenario.ByNodeScenario
	for attempt := 0; attempt < 1000; attempt++ {
		sn := generator.Next()
		if sn == nil {
			return scenarios
		}
		byNodeScenario, ok := sn.(*scenario.ByNodeScenario)
		require.True(t, ok, "generator must only emit *ByNodeScenario")
		require.NotNil(t, byNodeScenario)
		scenarios = append(scenarios, byNodeScenario)
	}
	t.Fatal("generator did not terminate")
	return nil
}

func victimNodeNames(t *testing.T, sn *scenario.ByNodeScenario) map[string]int {
	t.Helper()

	nodes := map[string]int{}
	for _, task := range sn.PotentialVictimsTasks() {
		require.NotEmpty(t, task.NodeName, "victim must be placed on a node")
		nodes[task.NodeName]++
	}
	return nodes
}

func newGenerator(t *testing.T, ssn *framework.Session, pendingJobName string, recordedVictims ...*podgroup_info.PodGroupInfo) framework.ScenarioGenerator {
	t.Helper()
	return newGeneratorWithThreshold(t, ssn, pendingJobName, fullnodefirst.DefaultFullNodeThreshold, recordedVictims...)
}

func newGeneratorWithThreshold(t *testing.T, ssn *framework.Session, pendingJobName string, threshold float64,
	recordedVictims ...*podgroup_info.PodGroupInfo) framework.ScenarioGenerator {
	t.Helper()

	pendingJob := findJobByName(t, ssn, pendingJobName)
	generator := fullnodefirst.NewFullNodeFirstGeneratorFactory(threshold)(&solvers.SolveContext{
		Session:             ssn,
		ActionType:          framework.Reclaim,
		PartialPendingJob:   pendingJob,
		RecordedVictimsJobs: recordedVictims,
		GenerateVictimsQueue: func() *utils.JobsOrderByQueues {
			return utils.GetVictimsQueue(ssn, func(job *podgroup_info.PodGroupInfo) bool {
				return job.Queue != pendingJob.Queue
			})
		},
		FeasibleNodes: ssn.ClusterInfo.Nodes,
	})
	require.NotNil(t, generator)
	return generator
}

// TestEmitsOneScenarioPerNodeFreeingExactlyThatNode: a whole-node reclaimer on a packed cluster yields one scenario per node.
func TestEmitsOneScenarioPerNodeFreeingExactlyThatNode(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "full node first - symmetric",
		Jobs: []*jobs_fake.TestJobBasic{
			wholeNodeReclaimer("reclaimer", 2),
			borrower("bb-0", "node-0"),
			borrower("bb-1", "node-0"),
			borrower("bb-2", "node-1"),
			borrower("bb-3", "node-1"),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-0": {GPUs: 2},
			"node-1": {GPUs: 2},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: reclaimerQueue, DeservedGPUs: 4},
			{Name: borrowerQueue, DeservedGPUs: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, controller)

	scenarios := drainScenarios(t, newGenerator(t, ssn, "reclaimer"))

	require.Len(t, scenarios, 2, "one scenario per candidate node")
	freed := map[string]bool{}
	for _, sn := range scenarios {
		nodes := victimNodeNames(t, sn)
		require.Len(t, nodes, 1, "each scenario frees exactly one node")
		for nodeName, count := range nodes {
			require.Equal(t, 2, count, "victim set is exactly the node's two occupants")
			freed[nodeName] = true
		}
	}
	require.Equal(t, map[string]bool{"node-0": true, "node-1": true}, freed)
}

// TestRecordedVictimsSatisfyProbeEmitsRecordedOnlyScenario: recorded victims already free enough nodes.
func TestRecordedVictimsSatisfyProbeEmitsRecordedOnlyScenario(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "full node first - recorded victims",
		Jobs: []*jobs_fake.TestJobBasic{
			wholeNodeReclaimer("reclaimer", 2),
			borrower("bb-0", "node-0"),
			borrower("bb-1", "node-0"),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-0": {GPUs: 2},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: reclaimerQueue, DeservedGPUs: 4},
			{Name: borrowerQueue, DeservedGPUs: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, controller)

	recorded := []*podgroup_info.PodGroupInfo{
		findJobByName(t, ssn, "bb-0"),
		findJobByName(t, ssn, "bb-1"),
	}
	scenarios := drainScenarios(t, newGenerator(t, ssn, "reclaimer", recorded...))

	require.Len(t, scenarios, 1, "the single pending task is already covered by recorded victims")
	require.Empty(t, scenarios[0].PotentialVictimsTasks(), "recorded-only scenario adds no new victims")
	require.Len(t, scenarios[0].RecordedVictimsJobs(), 2)
}

// TestInertCases verifies the generator stays silent for probes it does not serve.
func TestInertCases(t *testing.T) {
	cases := []struct {
		name    string
		jobs    []*jobs_fake.TestJobBasic
		nodes   map[string]nodes_fake.TestNodeBasic
		pending string
	}{
		{
			name: "request exceeds node capacity",
			jobs: []*jobs_fake.TestJobBasic{
				wholeNodeReclaimer("reclaimer", 4),
				borrower("bb-0", "node-0"),
				borrower("bb-1", "node-0"),
			},
			nodes:   map[string]nodes_fake.TestNodeBasic{"node-0": {GPUs: 2}},
			pending: "reclaimer",
		},
		{
			name: "partial-node request is left to other generators",
			jobs: []*jobs_fake.TestJobBasic{
				wholeNodeReclaimer("reclaimer", 1),
				borrower("bb-0", "node-0"),
				borrower("bb-1", "node-0"),
			},
			nodes:   map[string]nodes_fake.TestNodeBasic{"node-0": {GPUs: 2}},
			pending: "reclaimer",
		},
		{
			name: "no reclaimable victims",
			jobs: []*jobs_fake.TestJobBasic{
				wholeNodeReclaimer("reclaimer", 2),
			},
			nodes:   map[string]nodes_fake.TestNodeBasic{"node-0": {GPUs: 2}},
			pending: "reclaimer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer gock.Off()
			test_utils.InitTestingInfrastructure()
			controller := gomock.NewController(t)
			defer controller.Finish()

			ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
				Name:  tc.name,
				Jobs:  tc.jobs,
				Nodes: tc.nodes,
				Queues: []test_utils.TestQueueBasic{
					{Name: reclaimerQueue, DeservedGPUs: 4},
					{Name: borrowerQueue, DeservedGPUs: 0},
				},
				Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			}, controller)

			require.Empty(t, drainScenarios(t, newGenerator(t, ssn, tc.pending)))
		})
	}
}

// TestServesCPUReclaim: a CPU reclaimer filling a CPU-packed node yields a node-freeing scenario, as it would for GPUs.
func TestServesCPUReclaim(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "full node first - cpu reclaim",
		Jobs: []*jobs_fake.TestJobBasic{
			cpuReclaimer("reclaimer", 4000),
			cpuBorrower("bb-0", "node-0", 2000),
			cpuBorrower("bb-1", "node-0", 2000),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-0": {CPUMillis: 4000, CPUMemory: 4000 * 1e6},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: reclaimerQueue, DeservedCPUs: test_utils.CreateFloat64Pointer(4000)},
			{Name: borrowerQueue, DeservedCPUs: test_utils.CreateFloat64Pointer(0)},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, controller)

	scenarios := drainScenarios(t, newGenerator(t, ssn, "reclaimer"))

	require.NotEmpty(t, scenarios, "FullNodeFirst must serve CPU reclaim, not only GPU")
	require.Equal(t, map[string]int{"node-0": 2}, victimNodeNames(t, scenarios[0]),
		"scenario frees node-0 by evicting both CPU borrowers")
}

// TestInertWhenATaskCannotFillAnyNode: a gang task that fills no available node leaves the whole gang to other generators.
func TestInertWhenATaskCannotFillAnyNode(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	reclaimer := &jobs_fake.TestJobBasic{
		Name:                "reclaimer",
		RequiredGPUsPerTask: 2,
		Priority:            constants.PriorityBuildNumber,
		QueueName:           reclaimerQueue,
		Tasks: []*tasks_fake.TestTaskBasic{
			{State: pod_status.Pending, RequiredGPUs: gpuPtr(2)},
			{State: pod_status.Pending, RequiredGPUs: gpuPtr(1)},
		},
	}
	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "full node first - task cannot fill any node",
		Jobs: []*jobs_fake.TestJobBasic{
			reclaimer,
			borrower("bb-0", "node-0"),
			borrower("bb-1", "node-0"),
			borrower("bb-2", "node-1"),
			borrower("bb-3", "node-1"),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-0": {GPUs: 2},
			"node-1": {GPUs: 2},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: reclaimerQueue, DeservedGPUs: 4},
			{Name: borrowerQueue, DeservedGPUs: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, controller)

	require.Empty(t, drainScenarios(t, newGenerator(t, ssn, "reclaimer")),
		"the 1-GPU task fills no 2-GPU node, so the gang is left to other generators")
}

// TestServesHeterogeneousWholeNodeGang: a gang mixing an 8-GPU and a 4-GPU whole-node task frees one correctly sized node each.
func TestServesHeterogeneousWholeNodeGang(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	reclaimer := &jobs_fake.TestJobBasic{
		Name:                "reclaimer",
		RequiredGPUsPerTask: 8,
		Priority:            constants.PriorityBuildNumber,
		QueueName:           reclaimerQueue,
		Tasks: []*tasks_fake.TestTaskBasic{
			{State: pod_status.Pending, RequiredGPUs: gpuPtr(8)},
			{State: pod_status.Pending, RequiredGPUs: gpuPtr(4)},
		},
	}
	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "full node first - heterogeneous whole-node gang",
		Jobs: []*jobs_fake.TestJobBasic{
			reclaimer,
			borrowerWithGPUs("fill-8", "node-8", 8),
			borrowerWithGPUs("fill-4", "node-4", 4),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-8": {GPUs: 8},
			"node-4": {GPUs: 4},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: reclaimerQueue, DeservedGPUs: 12},
			{Name: borrowerQueue, DeservedGPUs: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, controller)

	scenarios := drainScenarios(t, newGenerator(t, ssn, "reclaimer"))

	require.NotEmpty(t, scenarios, "heterogeneous whole-node gang must be served")
	freed := victimNodeNames(t, scenarios[0])
	require.Contains(t, freed, "node-8", "frees the 8-GPU node for the 8-GPU task")
	require.Contains(t, freed, "node-4", "frees the 4-GPU node for the 4-GPU task")
}

// TestRecordedMultiNodeVictimDoesNotOverCountFreedNodes: a victim spanning two nodes frees neither, so it must not count as freeing both.
func TestRecordedMultiNodeVictimDoesNotOverCountFreedNodes(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "full node first - multi-node recorded victim",
		Jobs: []*jobs_fake.TestJobBasic{
			wholeNodeReclaimer("reclaimer", 2),
			multiNodeBorrower("span", "node-0", "node-1"),
			borrower("fill-0", "node-0"),
			borrower("fill-1", "node-1"),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-0": {GPUs: 2},
			"node-1": {GPUs: 2},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: reclaimerQueue, DeservedGPUs: 4},
			{Name: borrowerQueue, DeservedGPUs: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, controller)

	recorded := []*podgroup_info.PodGroupInfo{findJobByName(t, ssn, "span")}
	scenarios := drainScenarios(t, newGenerator(t, ssn, "reclaimer", recorded...))

	require.NotEmpty(t, scenarios,
		"partial multi-node victim frees no whole node, so real node-freeing scenarios must still be emitted")
	for _, sn := range scenarios {
		require.NotEmpty(t, sn.PotentialVictimsTasks(),
			"each scenario must evict a node's occupants, not rely on the partial recorded victim")
	}
}

func wholeNodeReclaimer(name string, gpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: gpus,
		Priority:            constants.PriorityBuildNumber,
		QueueName:           reclaimerQueue,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
	}
}

func gpuPtr(n int64) *int64 { return &n }

func cpuReclaimer(name string, cpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredCPUsPerTask: cpus,
		Priority:            constants.PriorityBuildNumber,
		QueueName:           reclaimerQueue,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
	}
}

func cpuBorrower(name, nodeName string, cpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredCPUsPerTask: cpus,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           borrowerQueue,
		Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: nodeName, State: pod_status.Running}},
	}
}

func borrower(name, nodeName string) *jobs_fake.TestJobBasic {
	return borrowerWithGPUs(name, nodeName, 1)
}

func borrowerWithGPUs(name, nodeName string, gpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: gpus,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           borrowerQueue,
		Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: nodeName, State: pod_status.Running}},
	}
}

// multiNodeBorrower builds one borrower gang with a 1-GPU task on each node, spanning them without filling any.
func multiNodeBorrower(name string, nodeNames ...string) *jobs_fake.TestJobBasic {
	tasks := make([]*tasks_fake.TestTaskBasic, 0, len(nodeNames))
	for _, nodeName := range nodeNames {
		tasks = append(tasks, &tasks_fake.TestTaskBasic{NodeName: nodeName, State: pod_status.Running})
	}
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: 1,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           borrowerQueue,
		Tasks:               tasks,
	}
}

// TestServesWholeNodeWhenExactlyOneResourceMatches: a pod that fills the node on a single resource is served,
// whatever it requests of the other resources.
func TestServesWholeNodeWhenExactlyOneResourceMatches(t *testing.T) {
	const (
		nodeGPUs   = 8
		nodeMillis = 8000.0
		nodeMemory = 8e9
	)
	cases := []struct {
		name                    string
		gpus, millis, memoryReq float64
	}{
		{name: "gpu matches, cpu and memory below capacity", gpus: nodeGPUs, millis: 3000, memoryReq: 3e9},
		{name: "cpu matches, gpu and memory below capacity", gpus: 3, millis: nodeMillis, memoryReq: 3e9},
		{name: "memory matches, gpu and cpu below capacity", gpus: 3, millis: 3000, memoryReq: nodeMemory},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer gock.Off()
			test_utils.InitTestingInfrastructure()
			controller := gomock.NewController(t)
			defer controller.Finish()

			reclaimer := &jobs_fake.TestJobBasic{
				Name:                  "reclaimer",
				RequiredGPUsPerTask:   tc.gpus,
				RequiredCPUsPerTask:   tc.millis,
				RequiredMemoryPerTask: tc.memoryReq,
				Priority:              constants.PriorityBuildNumber,
				QueueName:             reclaimerQueue,
				Tasks:                 []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			}
			occupant := &jobs_fake.TestJobBasic{
				Name:                  "occupant",
				RequiredGPUsPerTask:   nodeGPUs,
				RequiredCPUsPerTask:   nodeMillis,
				RequiredMemoryPerTask: nodeMemory,
				Priority:              constants.PriorityTrainNumber,
				QueueName:             borrowerQueue,
				Tasks:                 []*tasks_fake.TestTaskBasic{{NodeName: "node-0", State: pod_status.Running}},
			}
			ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
				Name: "full node first - one matching resource",
				Jobs: []*jobs_fake.TestJobBasic{reclaimer, occupant},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node-0": {GPUs: nodeGPUs, CPUMillis: nodeMillis, CPUMemory: nodeMemory},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name: reclaimerQueue, DeservedGPUs: nodeGPUs,
						DeservedCPUs:   test_utils.CreateFloat64Pointer(nodeMillis),
						DeservedMemory: test_utils.CreateFloat64Pointer(nodeMemory),
					},
					{
						Name: borrowerQueue, DeservedGPUs: 0,
						DeservedCPUs:   test_utils.CreateFloat64Pointer(0),
						DeservedMemory: test_utils.CreateFloat64Pointer(0),
					},
				},
				Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			}, controller)

			scenarios := drainScenarios(t, newGenerator(t, ssn, "reclaimer"))

			require.Len(t, scenarios, 1, "a pod filling one resource of the node is a whole-node request")
			require.Equal(t, map[string]int{"node-0": 1}, victimNodeNames(t, scenarios[0]))
		})
	}
}

// TestThresholdControlsWhichTasksCountAsWholeNode: a task is whole-node when it requests at least threshold of the
// node's capacity on one resource, and must still fit the node on every resource.
func TestThresholdControlsWhichTasksCountAsWholeNode(t *testing.T) {
	const (
		nodeGPUs   = 8
		nodeMillis = 8000.0
		nodeMemory = 8e9
	)
	cases := []struct {
		name                    string
		threshold               float64
		gpus, millis, memoryReq float64
		served                  bool
	}{
		{name: "default requires the full node, 7 of 8 GPUs is left to other generators", threshold: 1.0, gpus: 7, millis: 3000, memoryReq: 3e9},
		{name: "7 of 8 GPUs meets 0.875", threshold: 0.875, gpus: 7, millis: 3000, memoryReq: 3e9, served: true},
		{name: "7 of 8 GPUs is below 0.9", threshold: 0.9, gpus: 7, millis: 3000, memoryReq: 3e9},
		{name: "cpu at the threshold", threshold: 0.75, gpus: 3, millis: 6000, memoryReq: 3e9, served: true},
		{name: "cpu just below the threshold", threshold: 0.75, gpus: 3, millis: 5999, memoryReq: 3e9},
		{name: "memory at the threshold", threshold: 0.75, gpus: 3, millis: 3000, memoryReq: 6e9, served: true},
		{name: "every resource below the threshold", threshold: 0.75, gpus: 5, millis: 5000, memoryReq: 5e9},
		{name: "full on one resource but over capacity on another", threshold: 0.75, gpus: 8, millis: 9000, memoryReq: 3e9},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer gock.Off()
			test_utils.InitTestingInfrastructure()
			controller := gomock.NewController(t)
			defer controller.Finish()

			reclaimer := &jobs_fake.TestJobBasic{
				Name:                  "reclaimer",
				RequiredGPUsPerTask:   tc.gpus,
				RequiredCPUsPerTask:   tc.millis,
				RequiredMemoryPerTask: tc.memoryReq,
				Priority:              constants.PriorityBuildNumber,
				QueueName:             reclaimerQueue,
				Tasks:                 []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			}
			occupant := &jobs_fake.TestJobBasic{
				Name:                  "occupant",
				RequiredGPUsPerTask:   nodeGPUs,
				RequiredCPUsPerTask:   nodeMillis,
				RequiredMemoryPerTask: nodeMemory,
				Priority:              constants.PriorityTrainNumber,
				QueueName:             borrowerQueue,
				Tasks:                 []*tasks_fake.TestTaskBasic{{NodeName: "node-0", State: pod_status.Running}},
			}
			ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
				Name: "full node first - threshold",
				Jobs: []*jobs_fake.TestJobBasic{reclaimer, occupant},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node-0": {GPUs: nodeGPUs, CPUMillis: nodeMillis, CPUMemory: nodeMemory},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name: reclaimerQueue, DeservedGPUs: nodeGPUs,
						DeservedCPUs:   test_utils.CreateFloat64Pointer(nodeMillis),
						DeservedMemory: test_utils.CreateFloat64Pointer(nodeMemory),
					},
					{
						Name: borrowerQueue, DeservedGPUs: 0,
						DeservedCPUs:   test_utils.CreateFloat64Pointer(0),
						DeservedMemory: test_utils.CreateFloat64Pointer(0),
					},
				},
				Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			}, controller)

			scenarios := drainScenarios(t, newGeneratorWithThreshold(t, ssn, "reclaimer", tc.threshold))

			if !tc.served {
				require.Empty(t, scenarios)
				return
			}
			require.Len(t, scenarios, 1)
			require.Equal(t, map[string]int{"node-0": 1}, victimNodeNames(t, scenarios[0]))
		})
	}
}

func findJobByName(t *testing.T, ssn *framework.Session, name string) *podgroup_info.PodGroupInfo {
	t.Helper()
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job.Name == name {
			return job
		}
	}
	t.Fatalf("job %q not found in session", name)
	return nil
}
