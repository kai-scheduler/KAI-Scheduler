// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// subScenarioEmitter generates sub-scenarios from a "potentially feasible" outer scenario
// produced by PodAccumulatedScenarioBuilder. Each sub-scenario is a subset of the outer's
// potential victims, picked by node so the simulation only sees nodes that can plausibly
// host a pending task. The emitter starts at the smallest top-K of victim-bearing nodes
// whose cumulative post-eviction capacity (added to baseline) covers total pending demand,
// and grows the set by one node each subsequent call.
//
// "Picking" a node selects every potential-victim *job* that has any task on that node,
// and includes the job's tasks across all nodes it spans. This preserves gang semantics:
// a job like q0_running_job-on-node0-and-node1 is evicted as a whole, not just its node0
// half — which the OLD per-node iteration in the solver enforced implicitly via
// VictimsTasksFromNodes.
//
// Sort order: per-node capacity descending. Ties broken by the index at which the node's
// first victim task appeared in the accumulator, so insertion order from the outer
// priority queue is preserved when capacities tie (relevant for tests that assert which
// of two equal-capacity victim jobs gets evicted).
// victimUnit corresponds to one call into the accumulator's addNextPotentialVictims:
// for a non-elastic gang job that's all its tasks at once, for an elastic job that's
// the slice the accumulator chose to peel off on that pop. Treating units as atomic
// matches what the OLD per-node iteration did via VictimsTasksFromNodes (which returned
// tasks from each victim-job-representative grouped under one node).
type victimUnit struct {
	representative *podgroup_info.PodGroupInfo
	tasks          []*pod_info.PodInfo
}

type subScenarioEmitter struct {
	session       *framework.Session
	base          *scenario.ByNodeScenario
	sortedNodes   []string
	nodeUnits     map[string][]int // node -> indexes into units, in insertion order
	units         []victimUnit
	pendingDemand float64
	nextK         int
}

func newSubScenarioEmitter(
	session *framework.Session, base *scenario.ByNodeScenario,
	feasibleNodes map[string]*node_info.NodeInfo,
) *subScenarioEmitter {
	pendingDemand, minPendingTask := pendingTaskGpuStats(base)

	recordedFreedPerNode := map[string]float64{}
	for _, victim := range base.RecordedVictimsTasks() {
		if victim.NodeName == "" {
			continue
		}
		recordedFreedPerNode[victim.NodeName] += victim.AcceptedGpuRequirement.GetGpusQuota()
	}

	// Build the unit list (one entry per accumulator-call boundary, identified by the
	// scenario's per-call representative PodGroupInfo) and a per-node index of unit IDs
	// that touch each node. Insertion order tracks the outer priority queue so equal-
	// capacity node ties break consistently.
	units := []victimUnit{}
	repToUnit := map[*podgroup_info.PodGroupInfo]int{}
	nodeUnits := map[string][]int{}
	seenUnitOnNode := map[string]map[int]bool{}
	nodeFirstSeenAt := map[string]int{}
	for idx, victim := range base.PotentialVictimsTasks() {
		rep := base.GetVictimJobRepresentativeById(victim)
		if rep == nil {
			continue
		}
		unitIdx, ok := repToUnit[rep]
		if !ok {
			unitIdx = len(units)
			repToUnit[rep] = unitIdx
			units = append(units, victimUnit{representative: rep})
		}
		units[unitIdx].tasks = append(units[unitIdx].tasks, victim)
		if victim.NodeName == "" {
			continue
		}
		if _, seen := nodeFirstSeenAt[victim.NodeName]; !seen {
			nodeFirstSeenAt[victim.NodeName] = idx
		}
		if seenUnitOnNode[victim.NodeName] == nil {
			seenUnitOnNode[victim.NodeName] = map[int]bool{}
		}
		if !seenUnitOnNode[victim.NodeName][unitIdx] {
			seenUnitOnNode[victim.NodeName][unitIdx] = true
			nodeUnits[victim.NodeName] = append(nodeUnits[victim.NodeName], unitIdx)
		}
	}

	// Per-node "pickable" capacity: existing idle/releasing + recorded eviction on this
	// node + the GPU of victim tasks ON this node from units touching this node. Tasks
	// of those units on OTHER nodes contribute capacity to *those* nodes when those
	// nodes are also picked or already in the simulation's feasibleNodes set; we count
	// only the per-node contribution here to keep the sort heuristic local.
	nodeCap := map[string]float64{}
	for nodeName, unitIdxs := range nodeUnits {
		c := recordedFreedPerNode[nodeName]
		if node := session.ClusterInfo.Nodes[nodeName]; node != nil {
			c += nodeIdleOrReleasingGpus(node)
		}
		for _, ui := range unitIdxs {
			for _, t := range units[ui].tasks {
				if t.NodeName == nodeName {
					c += t.AcceptedGpuRequirement.GetGpusQuota()
				}
			}
		}
		nodeCap[nodeName] = c
	}

	candidates := make([]string, 0, len(nodeUnits))
	for nodeName := range nodeUnits {
		if minPendingTask > 0 && nodeCap[nodeName] < minPendingTask {
			continue
		}
		candidates = append(candidates, nodeName)
	}
	// Sort ascending by capacity (prefer the smallest viable node so we minimize the
	// number of victims actually evicted/pipelined; consolidation tests rely on this).
	// Ties broken by insertion order from the outer priority queue, so equal-capacity
	// candidates are tried in the order the accumulator surfaced them.
	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := nodeCap[candidates[i]], nodeCap[candidates[j]]
		if ci != cj {
			return ci < cj
		}
		return nodeFirstSeenAt[candidates[i]] < nodeFirstSeenAt[candidates[j]]
	})

	// Baseline: nodes the simulation will see regardless of which potential victims we
	// pick — feasibleNodes minus the potential-bearing ones (whose contribution is
	// counted in candidates / picked above).
	baseline := 0.0
	for nodeName, node := range feasibleNodes {
		if _, inPotential := nodeUnits[nodeName]; inPotential {
			continue
		}
		if node != nil {
			baseline += nodeIdleOrReleasingGpus(node)
		}
		baseline += recordedFreedPerNode[nodeName]
	}

	remaining := pendingDemand - baseline
	if remaining < 0 {
		remaining = 0
	}
	minK := smallestKCovering(candidates, remaining, func(n string) float64 { return nodeCap[n] })

	return &subScenarioEmitter{
		session:       session,
		base:          base,
		sortedNodes:   candidates,
		nodeUnits:     nodeUnits,
		units:         units,
		pendingDemand: pendingDemand,
		nextK:         minK,
	}
}

// next emits the next sub-scenario, or nil when no more sub-scenarios are worth trying.
// Each call grows the picked-nodes prefix by one. Picking a node selects every victim
// job with a task on that node and includes the job's full task set across all nodes
// (gang-preserving).
func (sse *subScenarioEmitter) next() *scenario.ByNodeScenario {
	if sse.nextK < 0 || sse.nextK > len(sse.sortedNodes) {
		return nil
	}

	pickedUnits := map[int]bool{}
	for i := 0; i < sse.nextK; i++ {
		for _, ui := range sse.nodeUnits[sse.sortedNodes[i]] {
			pickedUnits[ui] = true
		}
	}
	sse.nextK++

	sub := scenario.NewByNodeScenario(
		sse.session,
		sse.base.GetPreemptor(),
		sse.base.PendingTasks(),
		nil,
		sse.base.RecordedVictimsJobs(),
	)
	for ui := range pickedUnits {
		sub.AddPotentialVictimsTasks(sse.units[ui].tasks)
	}
	return sub
}

func pendingTaskGpuStats(s *scenario.ByNodeScenario) (totalDemand, minTask float64) {
	minTask = -1
	for _, t := range s.PendingTasks() {
		req := t.GpuRequirement.GetGpusQuota()
		totalDemand += req
		if minTask < 0 || req < minTask {
			minTask = req
		}
	}
	return totalDemand, minTask
}

// smallestKCovering returns the smallest K such that the sum of the top-K capacities
// (items must be pre-sorted descending by capacity) reaches demand. Returns 0 when
// demand is already non-positive, or len(items)+1 (out of range) when no K satisfies.
func smallestKCovering[T any](items []T, demand float64, capacityOf func(T) float64) int {
	if demand <= 0 {
		return 0
	}
	cumulative := 0.0
	for i, item := range items {
		cumulative += capacityOf(item)
		if cumulative >= demand {
			return i + 1
		}
	}
	return len(items) + 1
}
