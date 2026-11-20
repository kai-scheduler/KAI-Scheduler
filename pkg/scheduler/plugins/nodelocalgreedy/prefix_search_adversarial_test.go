// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodelocalgreedy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

func TestEarlierPrefixStockRunsBeforeLaterVictim(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4"},
		"node-b": {gpus: 1, cpu: "4"},
		"node-c": {gpus: 1, cpu: "4"},
	})
	pending := addPrefixPending(t, ssn, 1, 70, "1", "100m")
	_, fairTasks := prefixRunningJob(t, ssn, 71, "100m", "1", "node-a", "node-b")
	_, laterTasks := prefixRunningJob(t, ssn, 72, "100m", "1", "node-c")

	prefixFair := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil, nil)
	prefixFair.AddPotentialVictimsTasks(fairTasks[:1])
	prefixFair.AddPotentialVictimsTasks(fairTasks[1:])

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		ProbeK:            1,
	}}
	first := generator.scenariosForPrefix(prefixFair)
	pairAt := scenarioIndex(first, fairTasks)
	require.GreaterOrEqual(t, pairAt, 0, "the fair pair is emitted with its own prefix")
	require.False(t, scenarioListHasPods(first, laterTasks), "a later victim is not part of this prefix")

	prefixLater := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil, nil)
	prefixLater.AddPotentialVictimsTasks(fairTasks[:1])
	prefixLater.AddPotentialVictimsTasks(fairTasks[1:])
	prefixLater.AddPotentialVictimsTasks(laterTasks)
	second := generator.scenariosForPrefix(prefixLater)
	require.True(t, scenarioListHasPods(second, laterTasks), "the later victim is emitted only from the later prefix")
}

func TestCPUPruneDoesNotDropBatchCoveredByRecordedVictims(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4"},
		"node-b": {gpus: 1, cpu: "8"},
	})
	pending := addPrefixPending(t, ssn, 1, 80, "1", "4")
	recorded, recordedTasks := prefixRunningJob(t, ssn, 81, "2", "0", "node-a")
	_, fairTasks := prefixRunningJob(t, ssn, 82, "500m", "1", "node-a", "node-b")
	require.NoError(t, ssn.ClusterInfo.Nodes["node-a"].AddTask(recordedTasks[0]))
	require.NoError(t, ssn.ClusterInfo.Nodes["node-a"].AddTask(fairTasks[0]))
	require.NoError(t, ssn.ClusterInfo.Nodes["node-b"].AddTask(fairTasks[1]))

	base := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil,
		[]*podgroup_info.PodGroupInfo{recorded})
	base.AddPotentialVictimsTasks(fairTasks[:1])
	base.AddPotentialVictimsTasks(fairTasks[1:])

	need := cpuOf(base.PendingTasks())
	idleA := ssn.ClusterInfo.Nodes["node-a"].IdleVector.Get(resource_info.CPUIndex)
	releasingA := ssn.ClusterInfo.Nodes["node-a"].ReleasingVector.Get(resource_info.CPUIndex)
	potentialA := cpuOf(fairTasks[:1])
	recordedCPU := cpuOf(recordedTasks)
	require.GreaterOrEqual(t, idleA+releasingA+potentialA+recordedCPU+1e-3, need)
	require.Less(t, idleA+potentialA+1e-3, need)

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		ProbeK:            1,
	}}
	emitted := generator.scenariosForPrefix(base)
	require.GreaterOrEqual(t, scenarioIndex(emitted, fairTasks[:1]), 0,
		"node-a batch stays; recorded CPU is evicted with the scenario")
}

func TestCPUPruneDoesNotDropBatchCoveredByReleasingCPU(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4"},
		"node-b": {gpus: 1, cpu: "8"},
	})
	pending := addPrefixPending(t, ssn, 1, 90, "1", "3")
	_, releasingTasks := prefixRunningJob(t, ssn, 91, "3", "0", "node-a")
	releasingTasks[0].Status = pod_status.Releasing
	_, fairTasks := prefixRunningJob(t, ssn, 92, "500m", "1", "node-a", "node-b")
	require.NoError(t, ssn.ClusterInfo.Nodes["node-a"].AddTask(releasingTasks[0]))
	require.NoError(t, ssn.ClusterInfo.Nodes["node-a"].AddTask(fairTasks[0]))
	require.NoError(t, ssn.ClusterInfo.Nodes["node-b"].AddTask(fairTasks[1]))

	base := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil, nil)
	base.AddPotentialVictimsTasks(fairTasks[:1])
	base.AddPotentialVictimsTasks(fairTasks[1:])

	need := cpuOf(base.PendingTasks())
	nodeA := ssn.ClusterInfo.Nodes["node-a"]
	idleA := nodeA.IdleVector.Get(resource_info.CPUIndex)
	releasingA := nodeA.ReleasingVector.Get(resource_info.CPUIndex)
	potentialA := cpuOf(fairTasks[:1])
	require.GreaterOrEqual(t, idleA+releasingA+potentialA+1e-3, need)
	require.Less(t, idleA+potentialA+1e-3, need)

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		ProbeK:            1,
	}}
	emitted := generator.scenariosForPrefix(base)
	require.GreaterOrEqual(t, scenarioIndex(emitted, fairTasks[:1]), 0,
		"node-a batch stays; allocation fits idle plus releasing CPU")
}

func TestPartialProbeDoesNotEmitDomainBatchBeforeStock(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4", domain: "z"},
		"node-b": {gpus: 1, cpu: "4", domain: "z"},
		"node-c": {gpus: 1, cpu: "4", domain: "other"},
	})
	pending := addPrefixPending(t, ssn, 2, 100, "1", "100m")
	pending.RootSubGroupSet = subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
		&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: prefixDomainLabel})
	recorded, recordedTasks := prefixRunningJob(t, ssn, 101, "100m", "1", "node-c")
	_, tasksA := prefixRunningJob(t, ssn, 102, "100m", "1", "node-a")
	_, tasksB := prefixRunningJob(t, ssn, 103, "100m", "1", "node-b")
	_, tasksC := prefixRunningJob(t, ssn, 104, "100m", "1", "node-c")

	base := scenario.NewByNodeScenario(ssn, pending, podsOf(pending)[:1], nil,
		[]*podgroup_info.PodGroupInfo{recorded})
	base.AddPotentialVictimsTasks(tasksC)
	base.AddPotentialVictimsTasks(tasksA)
	base.AddPotentialVictimsTasks(tasksB)

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		ProbeK:            1,
	}}
	require.False(t, generator.isSinglePodPlacement(len(base.PendingTasks())))
	stock := nodeLocalScenarios(ssn, base)
	emitted := generator.scenariosForPrefix(base)

	require.NotEmpty(t, stock)
	require.NotEmpty(t, emitted)
	require.True(t, sameVictimSet(emitted[0], stock[0]))
	require.ElementsMatch(t, podNames(recordedTasks), podNames(emitted[0].RecordedVictimsTasks()))
}

func TestDomainBatchesSkippedWhenRequiredLevelsDiffer(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4", domain: "a"},
		"node-b": {gpus: 1, cpu: "4", domain: "b"},
	})
	pending := addPrefixPending(t, ssn, 1, 120, "1", "100m")
	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
		&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: prefixDomainLabel})
	child := subgroup_info.NewSubGroupSet("child",
		&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "example.com/other"})
	root.AddSubGroup(child)
	pending.RootSubGroupSet = root
	_, tasks := prefixRunningJob(t, ssn, 121, "100m", "1", "node-a", "node-b")
	base := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil, nil)
	base.AddPotentialVictimsTasks(tasks[:1])
	base.AddPotentialVictimsTasks(tasks[1:])

	require.Empty(t, domainBatchScenarios(ssn, base))
}

func TestGatedSiblingIsNotASinglePodPlacement(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{"node-a": {gpus: 1, cpu: "4"}})
	job := addPrefixPending(t, ssn, 2, 110, "1", "100m")
	setOneTaskStatus(job, pod_status.Gated)
	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: job,
		ProbeK:            1,
	}}
	require.False(t, generator.isSinglePodPlacement(1))
}

func TestBindingAndPipelinedSiblingsAreNotUnscheduledWork(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{"node-a": {gpus: 1, cpu: "4"}})
	bindingJob := addPrefixPending(t, ssn, 2, 111, "1", "100m")
	setOneTaskStatus(bindingJob, pod_status.Binding)
	pipelinedJob := addPrefixPending(t, ssn, 2, 112, "1", "100m")
	setOneTaskStatus(pipelinedJob, pod_status.Pipelined)

	bindingGen := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session: ssn, PartialPendingJob: bindingJob, ProbeK: 1,
	}}
	pipelinedGen := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session: ssn, PartialPendingJob: pipelinedJob, ProbeK: 1,
	}}
	require.True(t, bindingGen.isSinglePodPlacement(1))
	require.True(t, pipelinedGen.isSinglePodPlacement(1))
}

func cpuOf(tasks []*pod_info.PodInfo) float64 {
	total := 0.0
	for _, task := range tasks {
		if task != nil {
			total += task.ResReqVector.Get(resource_info.CPUIndex)
		}
	}
	return total
}

func scenarioListHasPods(scenarios []*scenario.ByNodeScenario, pods []*pod_info.PodInfo) bool {
	return scenarioIndex(scenarios, pods) >= 0
}

func scenarioIndex(scenarios []*scenario.ByNodeScenario, pods []*pod_info.PodInfo) int {
	want := podNames(pods)
	for i, sn := range scenarios {
		if sn == nil {
			continue
		}
		if equalStrings(podNames(sn.PotentialVictimsTasks()), want) {
			return i
		}
	}
	return -1
}

func sameVictimSet(left, right *scenario.ByNodeScenario) bool {
	if left == nil || right == nil {
		return false
	}
	return equalStrings(podNames(left.PotentialVictimsTasks()), podNames(right.PotentialVictimsTasks())) &&
		equalStrings(podNames(left.RecordedVictimsTasks()), podNames(right.RecordedVictimsTasks()))
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
