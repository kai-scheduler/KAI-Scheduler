// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	schedapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/numa"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// These tests verify bookkeeping and rollback/discard correctness in complicated simulations.

// scenarioFixture wires up the following scenario: one node, two NUMA single-numa-node zones
// (4 cpu allocatable, 1 cpu available — each already holds a 3-cpu Guaranteed victim), and one
// pending 3-cpu Guaranteed preemptor that fits neither zone until a victim is evicted.
type scenarioFixture struct {
	ssn          *framework.Session
	stmt         *framework.Statement
	node         *node_info.NodeInfo
	preemptorJob *podgroup_info.PodGroupInfo
	victim0      *pod_info.PodInfo
	victim1      *pod_info.PodInfo
	preemptor    *pod_info.PodInfo
	allVictims   []*pod_info.PodInfo
	nodes        []*node_info.NodeInfo
}

func newScenarioFixture(t *testing.T) *scenarioFixture {
	t.Helper()
	vectorMap := resource_info.NewResourceVectorMap()

	jobs := []*jobs_fake.TestJobBasic{
		{Name: "victim0", QueueName: "q", RequiredCPUsPerTask: 3,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0"}}},
		{Name: "victim1", QueueName: "q", RequiredCPUsPerTask: 3,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0"}}},
		{Name: "preemptor", QueueName: "q", RequiredCPUsPerTask: 3,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}}},
	}
	jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
	nodesInfoMap := nodes_fake.BuildNodesInfoMap(
		map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 16000, CPUMemory: 16e9}},
		tasksToNodeMap, nil, vectorMap)
	node := nodesInfoMap["node0"]

	node.NumaTopology = &node_info.NumaTopology{
		Policy:    node_info.TopologyPolicySingleNUMANode,
		Scope:     node_info.TopologyScopePod,
		Zones:     []*node_info.NumaZone{cpuZone("node-0", "4", "1"), cpuZone("node-1", "4", "1")},
		Resources: sets.New[v1.ResourceName]("cpu"),
	}

	victim0 := singleTask(jobsInfoMap["victim0"])
	victim1 := singleTask(jobsInfoMap["victim1"])
	preemptor := singleTask(jobsInfoMap["preemptor"])

	// The plugin handles Guaranteed pods; the fakes build Burstable pods, so mark QoS explicitly.
	for _, task := range []*pod_info.PodInfo{victim0, victim1, preemptor} {
		task.Pod.Status.QOSClass = v1.PodQOSGuaranteed
	}
	// Victims carry observed placement records: OnSessionOpen seeds each task's NUMAPlacement from
	// them and reconstructs zone Available (4 allocatable - 3 observed = 1 per zone).
	setObservedPlacement(victim0, "node-0", "3")
	setObservedPlacement(victim1, "node-1", "3")

	ssn := &framework.Session{
		ClusterInfo: &schedapi.ClusterInfo{PodGroupInfos: jobsInfoMap, Nodes: nodesInfoMap},
	}
	require.NoError(t, ssn.InitNodeScoringPool()) // OrderedNodesByTask needs the scoring pool
	numa.New(framework.PluginArguments{}).OnSessionOpen(ssn)

	return &scenarioFixture{
		ssn:          ssn,
		stmt:         ssn.Statement(),
		node:         node,
		preemptorJob: jobsInfoMap["preemptor"],
		victim0:      victim0,
		victim1:      victim1,
		preemptor:    preemptor,
		allVictims:   []*pod_info.PodInfo{victim0, victim1},
		nodes:        []*node_info.NodeInfo{node},
	}
}

func (f *scenarioFixture) zone(i int) int64 {
	q := f.node.NumaTopology.Zones[i].Available["cpu"]
	return q.Value()
}

// runScenario simulates the scenario by evicting the victims and allocating the preemptor.
func (f *scenarioFixture) runScenario(t *testing.T) {
	t.Helper()
	require.NoError(t, common.EvictAllPreemptees(f.ssn, f.allVictims, f.preemptorJob, f.stmt, framework.Preempt))
	require.Equal(t, int64(4), f.zone(0), "evicting both victims frees zone 0")
	require.Equal(t, int64(4), f.zone(1), "evicting both victims frees zone 1")

	require.True(t, common.AllocateJob(f.ssn, f.stmt, f.nodes, f.preemptorJob, true), "preemptor pipelines onto freed zone 0")
	// Re-pipeline the evicted victim jobs. victim0 re-homes to the now-only-free zone 1;
	// victim1 cannot fit and stays evicted — this asymmetry is what the simple hand-trace misses.
	common.AllocateJob(f.ssn, f.stmt, f.nodes, f.ssn.ClusterInfo.PodGroupInfos["victim0"], true)
	common.AllocateJob(f.ssn, f.stmt, f.nodes, f.ssn.ClusterInfo.PodGroupInfos["victim1"], true)

	// Mid-scenario (before any unwind): the real allocateTaskToNode stamp must have re-homed
	// victim0 to the free zone 1 so the dedup did NOT restore+recharge its stale zone-0 placement
	// onto the preemptor's zone. Otherwise the zone is over-committed and the solver would treat an
	// infeasible scenario as feasible.
	require.GreaterOrEqual(t, f.zone(0), int64(0), "zone 0 over-committed mid-scenario (victim re-charged the preemptor's zone)")
	require.GreaterOrEqual(t, f.zone(1), int64(0), "zone 1 over-committed mid-scenario")
	require.Equal(t, []int{1}, f.victim0.NUMAPlacement.ZoneIndices(), "victim0 re-homes to the free zone 1, not its stale zone 0")
}

// TestConsolidationDiscardRestoresLedger validates NUMA resource correctness after discard/rollback, verifying
// that the resources are restored correctly.
func TestConsolidationDiscardRestoresLedger(t *testing.T) {
	t.Run("Discard", func(t *testing.T) {
		f := newScenarioFixture(t)
		before0, before1 := f.zone(0), f.zone(1)

		f.runScenario(t)
		f.stmt.Discard() // matches by_pod_solver.handleScenarioSolution's reject path

		assert.Equal(t, before0, f.zone(0), "zone 0 Available after Discard must equal pre-scenario value")
		assert.Equal(t, before1, f.zone(1), "zone 1 Available after Discard must equal pre-scenario value")
	})

	t.Run("RollbackThenDiscard", func(t *testing.T) {
		f := newScenarioFixture(t)
		before0, before1 := f.zone(0), f.zone(1)
		checkpoint := f.stmt.Checkpoint()

		f.runScenario(t)
		require.NoError(t, f.stmt.Rollback(checkpoint)) // matches by_pod_solver.solve's no-solution path
		f.stmt.Discard()

		assert.Equal(t, before0, f.zone(0), "zone 0 Available after Rollback+Discard must equal pre-scenario value")
		assert.Equal(t, before1, f.zone(1), "zone 1 Available after Rollback+Discard must equal pre-scenario value")
	})
}
