// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"strconv"
	"testing"

	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// In every topology below each pod has two GPUs and each node is full but for one GPU, so a pending
// two-GPU pod fits nowhere, and no rearrangement of the running pods (consolidation) frees two GPUs on a
// node. The fair share, capped by demand, gives every queue its demand, so only steady fair-share
// reclaim finds victims.

const steadyLowPriority = constants.PriorityTrainNumber - 10

func TestReclaimSteadyFairShareIntegrationTest(t *testing.T) {
	defer gock.Off()

	integration_tests_utils.RunTests(t, getReclaimSteadyFairShareTestsMetadata())
}

func getReclaimSteadyFairShareTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	steadyOn := map[string]string{"steadyFairShareReclaim": "true"}
	equalWeights := map[string]float64{"queue-a": 1, "queue-b": 1, "queue-c": 1}

	// 15 GPUs: steady fair shares of 5. queue-a holds 10, queue-c 2.
	fragmented := func() []*jobs_fake.TestJobBasic {
		return []*jobs_fake.TestJobBasic{
			steadyRunning("a-low", "queue-a", steadyLowPriority, "node0"),
			steadyRunning("a-1", "queue-a", constants.PriorityTrainNumber, "node0"),
			steadyRunning("a-2", "queue-a", constants.PriorityTrainNumber, "node1"),
			steadyRunning("c-1", "queue-c", constants.PriorityTrainNumber, "node1"),
			steadyRunning("a-3", "queue-a", constants.PriorityTrainNumber, "node2"),
			steadyRunning("a-4", "queue-a", constants.PriorityTrainNumber, "node2"),
			steadyPending("c-pending", "queue-c"),
		}
	}

	// 21 GPUs: steady fair shares of 7. With its pending job queue-c (6) ends at 8, 1.14 of its steady
	// fair share, while queue-a goes from 12 to 10, 1.43 of its.
	overshoot := func() []*jobs_fake.TestJobBasic {
		return []*jobs_fake.TestJobBasic{
			steadyRunning("a-low", "queue-a", steadyLowPriority, "node0"),
			steadyRunning("a-1", "queue-a", constants.PriorityTrainNumber, "node0"),
			steadyRunning("a-2", "queue-a", constants.PriorityTrainNumber, "node0"),
			steadyRunning("a-3", "queue-a", constants.PriorityTrainNumber, "node1"),
			steadyRunning("a-4", "queue-a", constants.PriorityTrainNumber, "node1"),
			steadyRunning("c-1", "queue-c", constants.PriorityTrainNumber, "node1"),
			steadyRunning("a-5", "queue-a", constants.PriorityTrainNumber, "node2"),
			steadyRunning("c-2", "queue-c", constants.PriorityTrainNumber, "node2"),
			steadyRunning("c-3", "queue-c", constants.PriorityTrainNumber, "node2"),
			steadyPending("c-pending", "queue-c"),
		}
	}
	overshootExpected := func(reclaimed bool) map[string]test_utils.TestExpectedResultBasic {
		expected := map[string]test_utils.TestExpectedResultBasic{
			"a-low": steadyRunningOn("node0"), "a-1": steadyRunningOn("node0"), "a-2": steadyRunningOn("node0"),
			"a-3": steadyRunningOn("node1"), "a-4": steadyRunningOn("node1"), "c-1": steadyRunningOn("node1"),
			"a-5": steadyRunningOn("node2"), "c-2": steadyRunningOn("node2"), "c-3": steadyRunningOn("node2"),
			"c-pending": steadyPendingResult(),
		}
		if reclaimed {
			expected["a-low"], expected["c-pending"] = steadyPendingResult(), steadyRunningOn("node0")
		}
		return expected
	}

	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name:   "steady fair-share reclaim disabled: a queue below its weighted share waits while free GPUs are fragmented",
				Jobs:   fragmented(),
				Nodes:  steadyNodes(3, 5),
				Queues: steadyWeightedQueues(equalWeights),
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"a-low": steadyRunningOn("node0"), "a-1": steadyRunningOn("node0"),
					"a-2": steadyRunningOn("node1"), "c-1": steadyRunningOn("node1"),
					"a-3": steadyRunningOn("node2"), "a-4": steadyRunningOn("node2"),
					"c-pending": steadyPendingResult(),
				},
				Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name:   "steady fair-share reclaim: a queue below its steady fair share reclaims the lowest priority job of a queue above its",
				Jobs:   fragmented(),
				Nodes:  steadyNodes(3, 5),
				Queues: steadyWeightedQueues(equalWeights),
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"a-low": steadyPendingResult(), "a-1": steadyRunningOn("node0"),
					"a-2": steadyRunningOn("node1"), "c-1": steadyRunningOn("node1"),
					"a-3": steadyRunningOn("node2"), "a-4": steadyRunningOn("node2"),
					"c-pending": steadyRunningOn("node0"),
				},
				Mocks: steadyMocks(1, steadyOn),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				// 15 GPUs, steady fair shares of 5: queue-a holds 8, 3 above its. Among jobs of equal
				// priority the newest is evicted first: the a-big gang, whose 4 GPUs exceed the 3, then
				// a-small, before a-mid on another node.
				Name: "steady fair-share reclaim: skips a victim that would take its queue below its steady fair share and evicts a smaller one",
				Jobs: []*jobs_fake.TestJobBasic{
					steadyAged(steadyRunning("a-big", "queue-a", constants.PriorityTrainNumber, "node0", "node1"), 1),
					steadyAged(steadyRunning("a-small", "queue-a", constants.PriorityTrainNumber, "node0"), 30),
					steadyRunning("b-1", "queue-b", constants.PriorityTrainNumber, "node1"),
					steadyAged(steadyRunning("a-mid", "queue-a", constants.PriorityTrainNumber, "node2"), 60),
					steadyRunning("c-1", "queue-c", constants.PriorityTrainNumber, "node2"),
					steadyPending("c-pending", "queue-c"),
				},
				Nodes:  steadyNodes(3, 5),
				Queues: steadyWeightedQueues(equalWeights),
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"a-big": {GPUsRequired: 4, Status: pod_status.Running}, "a-small": steadyPendingResult(),
					"b-1": steadyRunningOn("node1"), "a-mid": steadyRunningOn("node2"), "c-1": steadyRunningOn("node2"),
					"c-pending": steadyRunningOn("node0"),
				},
				Mocks: steadyMocks(1, steadyOn),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				// 15 GPUs, steady fair shares of 5: queue-a holds 6, 1 above its, in jobs of 2.
				Name: "steady fair-share reclaim: takes nothing from a queue whose every job is larger than its surplus",
				Jobs: []*jobs_fake.TestJobBasic{
					steadyRunning("a-1", "queue-a", constants.PriorityTrainNumber, "node0"),
					steadyRunning("a-2", "queue-a", constants.PriorityTrainNumber, "node0"),
					steadyRunning("a-3", "queue-a", constants.PriorityTrainNumber, "node1"),
					steadyRunning("b-1", "queue-b", constants.PriorityTrainNumber, "node1"),
					steadyRunning("b-2", "queue-b", constants.PriorityTrainNumber, "node2"),
					steadyRunning("c-1", "queue-c", constants.PriorityTrainNumber, "node2"),
					steadyPending("c-pending", "queue-c"),
				},
				Nodes:  steadyNodes(3, 5),
				Queues: steadyWeightedQueues(equalWeights),
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"a-1": steadyRunningOn("node0"), "a-2": steadyRunningOn("node0"),
					"a-3": steadyRunningOn("node1"), "b-1": steadyRunningOn("node1"),
					"b-2": steadyRunningOn("node2"), "c-1": steadyRunningOn("node2"),
					"c-pending": steadyPendingResult(),
				},
				Mocks: steadyMocks(0, steadyOn),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name:               "steady fair-share reclaim: the reclaimer may end above its steady fair share while less saturated than the victim's queue",
				Jobs:               overshoot(),
				Nodes:              steadyNodes(3, 7),
				Queues:             steadyWeightedQueues(equalWeights),
				JobExpectedResults: overshootExpected(true),
				Mocks:              steadyMocks(1, steadyOn),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				// 1.14 * 1.3 is not below 1.43.
				Name:               "steady fair-share reclaim: the reclaimer's saturation is multiplied by relcaimerSaturationMultiplier",
				Jobs:               overshoot(),
				Nodes:              steadyNodes(3, 7),
				Queues:             steadyWeightedQueues(equalWeights),
				JobExpectedResults: overshootExpected(false),
				Mocks: steadyMocks(0, map[string]string{
					"steadyFairShareReclaim": "true", "relcaimerSaturationMultiplier": "1.3",
				}),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				// 20 GPUs, weights 2:1:1: steady fair shares of 10, 5 and 5. queue-r and queue-v hold 8 each.
				// After the reclaim both have a pending job of 2 GPUs, and by share of the cluster queue-v's
				// (6 + 2) is below queue-r's (8 + 2): without ordering queues below their steady fair share
				// first, queue-v's job would take back the freed GPUs and the two would trade them.
				Name: "steady fair-share reclaim: what is reclaimed goes to the reclaimer and is not reclaimed back",
				Jobs: []*jobs_fake.TestJobBasic{
					steadyRunning("v-low", "queue-v", steadyLowPriority, "node0"),
					steadyRunning("v-1", "queue-v", constants.PriorityTrainNumber, "node0"),
					steadyRunning("v-2", "queue-v", constants.PriorityTrainNumber, "node1"),
					steadyRunning("r-1", "queue-r", constants.PriorityTrainNumber, "node1"),
					steadyRunning("v-3", "queue-v", constants.PriorityTrainNumber, "node2"),
					steadyRunning("r-2", "queue-r", constants.PriorityTrainNumber, "node2"),
					steadyRunning("r-3", "queue-r", constants.PriorityTrainNumber, "node3"),
					steadyRunning("r-4", "queue-r", constants.PriorityTrainNumber, "node3"),
					steadyPending("r-pending", "queue-r"),
				},
				Nodes:  steadyNodes(4, 5),
				Queues: steadyWeightedQueues(map[string]float64{"queue-r": 2, "queue-v": 1, "queue-d": 1}),
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"v-low": steadyPendingResult(), "v-1": steadyRunningOn("node0"),
					"v-2": steadyRunningOn("node1"), "r-1": steadyRunningOn("node1"),
					"v-3": steadyRunningOn("node2"), "r-2": steadyRunningOn("node2"),
					"r-3": steadyRunningOn("node3"), "r-4": steadyRunningOn("node3"),
					"r-pending": steadyRunningOn("node0"),
				},
				Mocks: steadyMocks(1, steadyOn),
			},
		},
	}
}

// steadyRunning is a running job with a two-GPU pod on each of nodes.
func steadyRunning(name, queue string, priority int32, nodes ...string) *jobs_fake.TestJobBasic {
	job := &jobs_fake.TestJobBasic{Name: name, QueueName: queue, RequiredGPUsPerTask: 2, Priority: priority}
	for _, node := range nodes {
		job.Tasks = append(job.Tasks, &tasks_fake.TestTaskBasic{NodeName: node, State: pod_status.Running})
	}
	return job
}

func steadyPending(name, queue string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		QueueName:           queue,
		RequiredGPUsPerTask: 2,
		Priority:            constants.PriorityTrainNumber,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
	}
}

func steadyAged(job *jobs_fake.TestJobBasic, ageInMinutes int) *jobs_fake.TestJobBasic {
	job.JobAgeInMinutes = ageInMinutes
	return job
}

func steadyNodes(count, gpus int) map[string]nodes_fake.TestNodeBasic {
	nodes := map[string]nodes_fake.TestNodeBasic{}
	for i := range count {
		nodes["node"+strconv.Itoa(i)] = nodes_fake.TestNodeBasic{GPUs: gpus}
	}
	return nodes
}

func steadyWeightedQueues(weights map[string]float64) []test_utils.TestQueueBasic {
	var queues []test_utils.TestQueueBasic
	for name, weight := range weights {
		queues = append(queues, test_utils.TestQueueBasic{Name: name, GPUOverQuotaWeight: weight})
	}
	return queues
}

func steadyRunningOn(node string) test_utils.TestExpectedResultBasic {
	return test_utils.TestExpectedResultBasic{NodeName: node, GPUsRequired: 2, Status: pod_status.Running}
}

func steadyPendingResult() test_utils.TestExpectedResultBasic {
	return test_utils.TestExpectedResultBasic{GPUsRequired: 2, Status: pod_status.Pending}
}

func steadyMocks(reclaims int, proportionArguments map[string]string) *test_utils.TestMock {
	return &test_utils.TestMock{
		CacheRequirements: &test_utils.CacheMocking{
			NumberOfCacheBinds:      reclaims,
			NumberOfCacheEvictions:  reclaims,
			NumberOfPipelineActions: reclaims,
		},
		SchedulerConf: schedulerConfigWithProportionArguments(proportionArguments),
	}
}

func schedulerConfigWithProportionArguments(arguments map[string]string) *conf.SchedulerConfiguration {
	config, err := conf_util.GetDefaultSchedulerConf()
	if err != nil {
		panic(err)
	}
	config.ScenarioSearchBudgets = nil
	for i, plugin := range config.Tiers[0].Plugins {
		if plugin.Name == "proportion" {
			config.Tiers[0].Plugins[i].Arguments = arguments
		}
	}
	return config
}
