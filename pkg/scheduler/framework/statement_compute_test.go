// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type statementComputeFixture struct {
	session *Session
	node    *node_info.NodeInfo
	victim  *pod_info.PodInfo
	pending *pod_info.PodInfo
}

type statementComputeLedger struct {
	used, allocated, releasing int64
}

func (f *statementComputeFixture) ledger() statementComputeLedger {
	return statementComputeLedger{
		used:      f.node.UsedSharedGPUsCompute["0"],
		allocated: f.node.AllocatedSharedGPUsCompute["0"],
		releasing: f.node.ReleasingSharedGPUsCompute["0"],
	}
}

// newStatementComputeFixture builds one GPU, group "0", fully charged by two running pods
// (0.7 and 0.3 compute) plus a pending 0.2-compute pod.
func newStatementComputeFixture(t *testing.T) *statementComputeFixture {
	t.Helper()
	vectorMap := resource_info.NewResourceVectorMap()
	jobs, tasksToNode, _ := jobs_fake.BuildJobsAndTasksMaps([]*jobs_fake.TestJobBasic{
		{
			Name: "other", QueueName: "q", RequiredGPUsPerTask: 0.5,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0", GPUGroups: []string{"0"}}},
		},
		{
			Name: "victim", QueueName: "q", RequiredGPUsPerTask: 0.3,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0", GPUGroups: []string{"0"}}},
		},
		{
			Name: "pending", QueueName: "q", RequiredGPUsPerTask: 0.2,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
		},
	}, vectorMap)

	computeByJob := map[string]int64{"other": 700, "victim": 300, "pending": 200}
	tasksByJob := map[string]*pod_info.PodInfo{}
	for _, job := range jobs {
		for _, task := range job.GetAllPodsMap() {
			task.GpuRequirement.SetGpuCompute(computeByJob[job.Name])
			tasksByJob[job.Name] = task
		}
	}

	nodes := nodes_fake.BuildNodesInfoMap(map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 1}}, tasksToNode, nil, vectorMap)
	fixture := &statementComputeFixture{
		session: &Session{ClusterInfo: &api.ClusterInfo{Nodes: nodes, PodGroupInfos: jobs}},
		node:    nodes["node0"],
		victim:  tasksByJob["victim"],
		pending: tasksByJob["pending"],
	}
	require.Equal(t, statementComputeLedger{used: 1000, allocated: 1000}, fixture.ledger())
	return fixture
}

func TestStatementGpuComputeRollback(t *testing.T) {
	t.Run("discard restores the ledger after evict and pipeline", func(t *testing.T) {
		f := newStatementComputeFixture(t)
		memoryBefore := f.node.UsedSharedGPUsMemory["0"]
		statement := f.session.Statement()

		require.NoError(t, statement.Evict(f.victim, "make room", eviction_info.EvictionMetadata{}))
		require.Equal(t, statementComputeLedger{used: 1000, allocated: 1000, releasing: 300}, f.ledger())

		f.pending.SetGPUGroupIDs([]string{"0"})
		require.NoError(t, statement.Pipeline(f.pending, "node0", false))
		require.Equal(t, statementComputeLedger{used: 1200, allocated: 1000, releasing: 100}, f.ledger())
		require.Equal(t, pod_status.Pipelined, f.pending.Status)

		statement.Discard()

		require.Equal(t, statementComputeLedger{used: 1000, allocated: 1000}, f.ledger())
		require.Equal(t, memoryBefore, f.node.UsedSharedGPUsMemory["0"])
		require.Equal(t, pod_status.Running, f.victim.Status)
		require.Equal(t, []string{"0"}, f.victim.GPUGroupIDs())
		require.Equal(t, pod_status.Pending, f.pending.Status)
	})

	t.Run("rollback to checkpoint keeps earlier operations", func(t *testing.T) {
		f := newStatementComputeFixture(t)
		statement := f.session.Statement()

		require.NoError(t, statement.Evict(f.victim, "make room", eviction_info.EvictionMetadata{}))
		checkpoint := statement.Checkpoint()
		f.pending.SetGPUGroupIDs([]string{"0"})
		require.NoError(t, statement.Pipeline(f.pending, "node0", false))

		require.NoError(t, statement.Rollback(checkpoint))
		require.Equal(t, statementComputeLedger{used: 1000, allocated: 1000, releasing: 300}, f.ledger())

		statement.Discard()
		require.Equal(t, statementComputeLedger{used: 1000, allocated: 1000}, f.ledger())
	})

	t.Run("pipelining the victim back to its GPU unevicts without double charge", func(t *testing.T) {
		f := newStatementComputeFixture(t)
		statement := f.session.Statement()

		require.NoError(t, statement.Evict(f.victim, "make room", eviction_info.EvictionMetadata{}))
		require.NoError(t, statement.Pipeline(f.victim, "node0", true))

		require.Equal(t, pod_status.Running, f.victim.Status)
		require.Equal(t, statementComputeLedger{used: 1000, allocated: 1000}, f.ledger())
	})
}

func TestStatementGpuComputeReleaseCompletion(t *testing.T) {
	f := newStatementComputeFixture(t)
	statement := f.session.Statement()
	require.NoError(t, statement.Evict(f.victim, "make room", eviction_info.EvictionMetadata{}))
	f.pending.SetGPUGroupIDs([]string{"0"})
	require.NoError(t, statement.Pipeline(f.pending, "node0", false))

	victimOnNode := f.node.PodInfos[pod_info.PodKey(f.victim.Pod)]
	require.NoError(t, f.node.RemoveTask(victimOnNode))
	require.Equal(t, statementComputeLedger{used: 900, allocated: 700, releasing: -200}, f.ledger())

	pendingOnNode := f.node.PodInfos[pod_info.PodKey(f.pending.Pod)]
	allocated := pendingOnNode.Clone()
	allocated.Status = pod_status.Allocated
	require.NoError(t, f.node.UpdateTask(allocated))
	require.Equal(t, statementComputeLedger{used: 900, allocated: 900}, f.ledger())
}
