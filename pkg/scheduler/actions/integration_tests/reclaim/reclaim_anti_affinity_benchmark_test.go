// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type antiAffinityReclaimBenchmarkParams struct {
	nodes     int
	releasing bool
	symmetric bool
}

type antiAffinityReclaimOutcome struct {
	bound         int
	pipelined     int
	evictedGroups int
}

var antiAffinityReclaimBenchmarkNodes = flag.Int(
	"reclaim-anti-affinity-nodes", 100,
	"node count for BenchmarkReclaimAntiAffinityFullCycle; must be a positive multiple of 10",
)

func BenchmarkReclaimAntiAffinityFullCycle(b *testing.B) {
	nodes := *antiAffinityReclaimBenchmarkNodes
	if nodes < 10 || nodes%10 != 0 {
		b.Fatal("reclaim-anti-affinity-nodes must be a positive multiple of 10")
	}
	for _, releasing := range []bool{false, true} {
		for _, symmetric := range []bool{false, true} {
			params := antiAffinityReclaimBenchmarkParams{nodes: nodes, releasing: releasing, symmetric: symmetric}
			b.Run(fmt.Sprintf("%d-nodes/releasing-%t/symmetric-%t", nodes, releasing, symmetric), func(b *testing.B) {
				benchmarkReclaimAntiAffinityFullCycle(b, params)
			})
		}
	}
}

func benchmarkReclaimAntiAffinityFullCycle(b *testing.B, params antiAffinityReclaimBenchmarkParams) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	topology := antiAffinityReclaimBenchmarkTopology(params)
	var totals antiAffinityReclaimOutcome
	var actionTime time.Duration
	b.ReportAllocs()
	for b.Loop() {
		controller := gomock.NewController(b)
		ssn := test_utils.BuildSession(topology, controller)
		start := time.Now()
		allocate.New().Execute(ssn)
		reclaim.New().Execute(ssn)
		actionTime += time.Since(start)
		outcome := antiAffinityReclaimBenchmarkOutcome(ssn, params)
		if outcome.bound+outcome.pipelined != params.nodes/10 {
			b.Fatalf("expected %d placements, got %+v", params.nodes/10, outcome)
		}
		totals.bound += outcome.bound
		totals.pipelined += outcome.pipelined
		totals.evictedGroups += outcome.evictedGroups
		controller.Finish()
	}
	b.ReportMetric(float64(actionTime.Nanoseconds())/float64(b.N), "actions_ns/op")
	b.ReportMetric(float64(totals.bound)/float64(b.N), "bound/op")
	b.ReportMetric(float64(totals.pipelined)/float64(b.N), "pipelined/op")
	b.ReportMetric(float64(totals.evictedGroups)/float64(b.N), "evicted_groups/op")
}

func TestReclaimAntiAffinityBenchmarkOutcomes(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	for _, releasing := range []bool{false, true} {
		for _, symmetric := range []bool{false, true} {
			t.Run(fmt.Sprintf("releasing-%t/symmetric-%t", releasing, symmetric), func(t *testing.T) {
				params := antiAffinityReclaimBenchmarkParams{nodes: 10, releasing: releasing, symmetric: symmetric}
				ssn := test_utils.BuildSession(antiAffinityReclaimBenchmarkTopology(params), gomock.NewController(t))
				allocate.New().Execute(ssn)
				reclaim.New().Execute(ssn)
				expected := antiAffinityReclaimOutcome{pipelined: 1}
				if !releasing {
					expected.evictedGroups = 1
				}
				require.Equal(t, expected, antiAffinityReclaimBenchmarkOutcome(ssn, params))
			})
		}
	}
}

func antiAffinityReclaimBenchmarkTopology(params antiAffinityReclaimBenchmarkParams) test_utils.TestTopologyBasic {
	const hostname = "kubernetes.io/hostname"
	incomingCount := params.nodes / 10
	topology := test_utils.TestTopologyBasic{
		Name:  "reclaim with idle GPUs and required pod anti-affinity",
		Nodes: make(map[string]nodes_fake.TestNodeBasic, params.nodes),
		Queues: []test_utils.TestQueueBasic{
			{Name: "victims", DeservedGPUs: 0, GPUOverQuotaWeight: 1},
			{Name: "reclaimers", DeservedGPUs: float64(incomingCount * 2), GPUOverQuotaWeight: 1},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
			NumberOfCacheBinds:      incomingCount,
			NumberOfCacheEvictions:  params.nodes,
			NumberOfPipelineActions: incomingCount,
		}},
	}
	selector := map[string]string{"anti-affinity-group": "benchmark"}
	if params.symmetric {
		selector = map[string]string{"role": "incoming"}
	}
	for i := 0; i < params.nodes; i++ {
		name := fmt.Sprintf("node-%06d", i)
		topology.Nodes[name] = nodes_fake.TestNodeBasic{GPUs: 8, Labels: map[string]string{hostname: name, tasks_fake.NodeAffinityKey: name}}
		task := &tasks_fake.TestTaskBasic{
			State:             pod_status.Running,
			NodeName:          name,
			NodeAffinityNames: []string{name},
			PodAffinityLabels: map[string]string{"anti-affinity-group": "benchmark", "role": "victim"},
		}
		if params.releasing && i < incomingCount {
			task.State = pod_status.Releasing
		}
		if params.symmetric {
			task.PodAntiAffinitySelector = selector
			task.PodAntiAffinityTopologyKey = hostname
		}
		topology.Jobs = append(topology.Jobs, &jobs_fake.TestJobBasic{
			Name: fmt.Sprintf("victim-%06d", i), QueueName: "victims",
			RequiredGPUsPerTask: 4, Priority: constants.PriorityTrainNumber,
			Tasks: []*tasks_fake.TestTaskBasic{task},
		})
	}
	for i := 0; i < incomingCount; i++ {
		topology.Jobs = append(topology.Jobs, &jobs_fake.TestJobBasic{
			Name: fmt.Sprintf("reclaimer-%06d", i), QueueName: "reclaimers",
			RequiredGPUsPerTask: 2, Priority: constants.PriorityTrainNumber,
			Tasks: []*tasks_fake.TestTaskBasic{{
				State:                      pod_status.Pending,
				PodAffinityLabels:          map[string]string{"anti-affinity-group": "benchmark", "role": "incoming"},
				PodAntiAffinitySelector:    selector,
				PodAntiAffinityTopologyKey: hostname,
			}},
		})
	}
	return topology
}

func antiAffinityReclaimBenchmarkOutcome(ssn *framework.Session, params antiAffinityReclaimBenchmarkParams) antiAffinityReclaimOutcome {
	outcome := antiAffinityReclaimOutcome{}
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if strings.HasPrefix(job.Name, "reclaimer-") {
			outcome.bound += len(job.PodStatusIndex[pod_status.Binding]) + len(job.PodStatusIndex[pod_status.Allocated])
			outcome.pipelined += len(job.PodStatusIndex[pod_status.Pipelined])
		} else if len(job.PodStatusIndex[pod_status.Releasing]) > 0 {
			outcome.evictedGroups++
		}
	}
	if params.releasing {
		outcome.evictedGroups -= params.nodes / 10
	}
	return outcome
}
