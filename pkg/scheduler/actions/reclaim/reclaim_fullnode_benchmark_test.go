// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"fmt"
	"math/rand"
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// fullNodeReclaimBenchmarkParams describes a whole-node reclaim fixture: a cluster
// packed with cross-node victim gangs where each node's GPUs are held by gangs from
// several priority tiers, plus one pending workload that needs a whole node. It is
// used to compare reclaim scenario generators (NodeLocalGreedy / MultiNodeGang /
// FullNodeFirst) as the cluster grows.
type fullNodeReclaimBenchmarkParams struct {
	NumNodes      int
	GPUsPerNode   int
	GangWidth     int // nodes each victim gang spans (1 GPU per node)
	PriorityTiers int // distinct victim priorities per node
	ReclaimerGPUs int // GPUs the pending workload requests (a whole node)
}

func defaultFullNodeReclaimParams(numNodes int) fullNodeReclaimBenchmarkParams {
	return fullNodeReclaimBenchmarkParams{
		NumNodes:      numNodes,
		GPUsPerNode:   8,
		GangWidth:     2,
		PriorityTiers: 8,
		ReclaimerGPUs: 8,
	}
}

func BenchmarkFullNodeReclaim_512Node(b *testing.B) {
	benchmarkFullNodeReclaim(b, defaultFullNodeReclaimParams(512))
}

func BenchmarkFullNodeReclaim_1024Node(b *testing.B) {
	benchmarkFullNodeReclaim(b, defaultFullNodeReclaimParams(1024))
}

func BenchmarkFullNodeReclaim_2048Node(b *testing.B) {
	benchmarkFullNodeReclaim(b, defaultFullNodeReclaimParams(2048))
}

func BenchmarkFullNodeReclaim_4096Node(b *testing.B) {
	benchmarkFullNodeReclaim(b, defaultFullNodeReclaimParams(4096))
}

// benchmarkFullNodeReclaim runs the reclaim action once per iteration for each
// scenario generator in isolation, reporting the number of simulated scenarios.
func benchmarkFullNodeReclaim(b *testing.B, params fullNodeReclaimBenchmarkParams) {
	for _, generator := range []string{"sg-nodelocalgreedy", "sg-multinodegang", "sg-fullnodefirst"} {
		b.Run(generator, func(b *testing.B) {
			defer gock.Off()
			test_utils.InitTestingInfrastructure()
			topology := buildFullNodeReclaimTopology(params, generator)
			action := reclaim.New()

			before := reclaimScenarioStateTotals(b, "simulated")
			for b.Loop() {
				b.StopTimer()
				controller := NewController(b)
				ssn := test_utils.BuildSession(topology, controller)
				b.StartTimer()

				action.Execute(ssn)

				b.StopTimer()
				controller.Finish()
				b.StartTimer()
			}
			after := reclaimScenarioStateTotals(b, "simulated")
			b.ReportMetric((after["simulated"]-before["simulated"])/float64(b.N), "simulated/op")
		})
	}
}

// buildFullNodeReclaimTopology fills every node with one victim task per priority
// tier (each from a gang spanning GangWidth scattered nodes), then adds a pending
// workload that needs a whole node. queue-0 (victims) deserves nothing so it is
// reclaimable; queue-1 (reclaimer) deserves the whole cluster.
func buildFullNodeReclaimTopology(params fullNodeReclaimBenchmarkParams, generator string) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic, params.NumNodes)
	for i := 0; i < params.NumNodes; i++ {
		nodes[fmt.Sprintf("node%d", i)] = nodes_fake.TestNodeBasic{GPUs: params.GPUsPerNode}
	}

	rng := rand.New(rand.NewSource(42))
	jobs := make([]*jobs_fake.TestJobBasic, 0, params.NumNodes+1)
	gangID := 0
	for tier := 0; tier < params.PriorityTiers; tier++ {
		perm := rng.Perm(params.NumNodes)
		priority := int32((tier + 1) * 10)
		for start := 0; start < params.NumNodes; start += params.GangWidth {
			end := start + params.GangWidth
			if end > params.NumNodes {
				end = params.NumNodes
			}
			tasks := make([]*tasks_fake.TestTaskBasic, 0, end-start)
			for i := start; i < end; i++ {
				tasks = append(tasks, &tasks_fake.TestTaskBasic{
					NodeName: fmt.Sprintf("node%d", perm[i]),
					State:    pod_status.Running,
				})
			}
			jobs = append(jobs, &jobs_fake.TestJobBasic{
				Name:                fmt.Sprintf("victim-%d", gangID),
				RequiredGPUsPerTask: 1,
				Priority:            priority,
				QueueName:           "queue-0",
				Tasks:               tasks,
			})
			gangID++
		}
	}

	jobs = append(jobs, &jobs_fake.TestJobBasic{
		Name:                "reclaimer",
		RequiredGPUsPerTask: float64(params.ReclaimerGPUs),
		Priority:            int32((params.PriorityTiers + 2) * 10),
		QueueName:           "queue-1",
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
	})

	totalGPUs := params.NumNodes * params.GPUsPerNode
	return test_utils.TestTopologyBasic{
		Name:  "full node reclaim",
		Jobs:  jobs,
		Nodes: nodes,
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue-0", DeservedGPUs: 0, GPUOverQuotaWeight: 0},
			{Name: "queue-1", DeservedGPUs: float64(totalGPUs), GPUOverQuotaWeight: 0},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions:  1 << 30,
				NumberOfPipelineActions: 1 << 30,
				NumberOfCacheBinds:      1 << 30,
			},
			SchedulerConf: singleGeneratorSchedulerConf(generator),
		},
	}
}

// singleGeneratorSchedulerConf returns the default scheduler config with exactly one
// scenario generator enabled, isolating NodeLocalGreedy, MultiNodeGang or FullNodeFirst.
func singleGeneratorSchedulerConf(generator string) *conf.SchedulerConfiguration {
	config, err := conf_util.GetDefaultSchedulerConf()
	if err != nil {
		panic(err)
	}
	config.ScenarioSearchBudgets = nil
	allGenerators := map[string]bool{
		"sg-nodelocalgreedy": true,
		"sg-multinodegang":   true,
		"sg-fullnodefirst":   true,
	}
	filtered := make([]conf.PluginOption, 0, len(config.Tiers[0].Plugins))
	inserted := false
	for _, plugin := range config.Tiers[0].Plugins {
		if allGenerators[plugin.Name] {
			if !inserted {
				filtered = append(filtered, conf.PluginOption{Name: generator})
				inserted = true
			}
			continue
		}
		filtered = append(filtered, plugin)
	}
	if !inserted {
		filtered = append(filtered, conf.PluginOption{Name: generator})
	}
	config.Tiers[0].Plugins = filtered
	return config
}

