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
// and grows the set with exponentially increasing steps, clamped to the full candidate set.
//
// "Picking" a node selects every potential-victim *batch* that has any task on that node,
// and includes the batch's tasks across all nodes it spans. This preserves gang semantics:
// a job like q0_running_job-on-node0-and-node1 is evicted as a whole, not just its node0
// half — which the OLD per-node iteration in the solver enforced implicitly via
// VictimsTasksFromNodes.
//
// Sort order: per-node capacity ascending (prefer the smallest viable node so we minimize
// the number of victims actually evicted/pipelined). Ties broken by the index at which
// the node's first victim task appeared in the accumulator, so insertion order from the
// outer priority queue is preserved when capacities tie.

// victimBatch corresponds to one call into the accumulator's addNextPotentialVictims:
// for a non-elastic gang job that's all its tasks at once; for an elastic job that's
// the slice the accumulator chose to peel off on that pop. Treating batches as atomic
// matches what the OLD per-node iteration did via VictimsTasksFromNodes (which returned
// tasks from each victim-job-representative grouped under one node).
type victimBatch struct {
	representative *podgroup_info.PodGroupInfo
	tasks          []*pod_info.PodInfo
}

type subScenarioEmitter struct {
	session     *framework.Session
	base        *scenario.ByNodeScenario
	sortedNodes []string
	nodeBatches map[string][]int // node -> indexes into batches, in insertion order
	batches     []victimBatch
	nextK       int
	dK          int
}

func newSubScenarioEmitter(
	session *framework.Session, base *scenario.ByNodeScenario,
	baseNodes map[string]*node_info.NodeInfo,
) *subScenarioEmitter {
	pendingDemand, minPendingTask := pendingTaskGpuStats(base)
	recordedFreed := recordedFreedByNode(base)
	batches, nodeBatches, nodeFirstSeenAt := buildVictimBatches(base)
	nodeCap := nodeCapacities(session, batches, nodeBatches, recordedFreed)
	candidates := sortViableCandidates(nodeBatches, nodeCap, nodeFirstSeenAt, minPendingTask)
	baseline := baselineCapacity(baseNodes, nodeBatches, recordedFreed)

	remaining := pendingDemand - baseline
	if remaining < 0 {
		remaining = 0
	}
	minK := smallestKCovering(candidates, remaining, func(n string) float64 { return nodeCap[n] })

	return &subScenarioEmitter{
		session:     session,
		base:        base,
		sortedNodes: candidates,
		nodeBatches: nodeBatches,
		batches:     batches,
		nextK:       minK,
		dK:          1,
	}
}

// next emits the next sub-scenario, or nil when no more sub-scenarios are worth trying.
// Each call grows the picked-nodes prefix. Picking a node selects every victim batch
// with a task on that node and includes the batch's full task set across all nodes
// (gang-preserving).
func (sse *subScenarioEmitter) next() *scenario.ByNodeScenario {
	if sse.nextK < 0 || sse.nextK > len(sse.sortedNodes) {
		return nil
	}

	k := sse.nextK
	pickedBatches := map[int]bool{}
	for i := 0; i < k; i++ {
		for _, bi := range sse.nodeBatches[sse.sortedNodes[i]] {
			pickedBatches[bi] = true
		}
	}
	sse.advanceNextK(k)

	sub := scenario.NewByNodeScenario(
		sse.session,
		sse.base.GetPreemptor(),
		sse.base.GetPreemptor(),
		nil,
		sse.base.RecordedVictimsJobs(),
	)
	for bi := range pickedBatches {
		sub.AddPotentialVictimsTasks(sse.batches[bi].tasks)
	}
	return sub
}

func (sse *subScenarioEmitter) advanceNextK(currentK int) {
	if currentK >= len(sse.sortedNodes) {
		sse.nextK = len(sse.sortedNodes) + 1
		return
	}

	nextK := currentK + sse.dK
	if nextK > len(sse.sortedNodes) {
		nextK = len(sse.sortedNodes)
	}
	sse.nextK = nextK
	sse.dK *= 2
}

// recordedFreedByNode returns the per-node sum of GPUs that will be freed when the
// scenario's recorded victims (committed evictions from prior solver iterations) get
// evicted. The recorded set is the same across every sub-scenario, so its
// per-node contribution is a fixed input to capacity accounting.
func recordedFreedByNode(base *scenario.ByNodeScenario) map[string]float64 {
	out := map[string]float64{}
	for _, victim := range base.RecordedVictimsTasks() {
		if victim.NodeName == "" {
			continue
		}
		out[victim.NodeName] += victim.AcceptedResource.GetGpusQuota()
	}
	return out
}

// buildVictimBatches groups the scenario's potential-victim tasks into batches (one
// per accumulator-call boundary, identified by the per-call representative
// PodGroupInfo) and indexes them by node. Batches are appended in insertion order,
// so a node's batch list reflects the outer priority queue's pop order; the second
// return value records when each node first appeared, used for tiebreaking equal-
// capacity nodes.
func buildVictimBatches(base *scenario.ByNodeScenario) (
	batches []victimBatch,
	nodeBatches map[string][]int,
	nodeFirstSeenAt map[string]int,
) {
	batches = []victimBatch{}
	nodeBatches = map[string][]int{}
	nodeFirstSeenAt = map[string]int{}
	repToBatch := map[*podgroup_info.PodGroupInfo]int{}
	seenBatchOnNode := map[string]map[int]bool{}

	for idx, victim := range base.PotentialVictimsTasks() {
		rep := base.GetVictimJobRepresentativeById(victim)
		if rep == nil {
			continue
		}
		bi, ok := repToBatch[rep]
		if !ok {
			bi = len(batches)
			repToBatch[rep] = bi
			batches = append(batches, victimBatch{representative: rep})
		}
		batches[bi].tasks = append(batches[bi].tasks, victim)
		if victim.NodeName == "" {
			continue
		}
		if _, seen := nodeFirstSeenAt[victim.NodeName]; !seen {
			nodeFirstSeenAt[victim.NodeName] = idx
		}
		if seenBatchOnNode[victim.NodeName] == nil {
			seenBatchOnNode[victim.NodeName] = map[int]bool{}
		}
		if !seenBatchOnNode[victim.NodeName][bi] {
			seenBatchOnNode[victim.NodeName][bi] = true
			nodeBatches[victim.NodeName] = append(nodeBatches[victim.NodeName], bi)
		}
	}
	return
}

// nodeCapacities computes per-node "pickable" capacity: existing idle/releasing on
// the node + GPUs freed by evicting recorded victims on it + GPUs freed by evicting
// the batch tasks that land *on this node*. Tasks of those same batches that live on
// OTHER nodes contribute capacity to *those* nodes; we count only the local share
// here so the sort heuristic stays per-node.
func nodeCapacities(
	session *framework.Session,
	batches []victimBatch, nodeBatches map[string][]int,
	recordedFreed map[string]float64,
) map[string]float64 {
	out := map[string]float64{}
	for nodeName, batchIdxs := range nodeBatches {
		c := recordedFreed[nodeName]
		if node := session.ClusterInfo.Nodes[nodeName]; node != nil {
			c += nodeIdleOrReleasingGpus(node)
		}
		for _, bi := range batchIdxs {
			for _, t := range batches[bi].tasks {
				if t.NodeName == nodeName {
					c += t.AcceptedResource.GetGpusQuota()
				}
			}
		}
		out[nodeName] = c
	}
	return out
}

// sortViableCandidates filters out nodes whose post-eviction capacity is below the
// smallest pending-task GPU requirement (they can't host any pending task) and
// returns the survivors sorted ascending by capacity, with ties broken by
// insertion order so equal-capacity candidates are tried in queue order.
func sortViableCandidates(
	nodeBatches map[string][]int,
	nodeCap map[string]float64,
	nodeFirstSeenAt map[string]int,
	minPendingTask float64,
) []string {
	out := make([]string, 0, len(nodeBatches))
	for nodeName := range nodeBatches {
		if minPendingTask > 0 && nodeCap[nodeName] < minPendingTask {
			continue
		}
		out = append(out, nodeName)
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := nodeCap[out[i]], nodeCap[out[j]]
		if ci != cj {
			return ci < cj
		}
		return nodeFirstSeenAt[out[i]] < nodeFirstSeenAt[out[j]]
	})
	return out
}

// baselineCapacity sums capacity from feasibleNodes that aren't potential-bearing.
// Those nodes are already in the simulation's feasibleNodes set regardless of which
// K-prefix the emitter picks, so their contribution is a fixed credit toward
// pending demand.
func baselineCapacity(
	feasibleNodes map[string]*node_info.NodeInfo,
	nodeBatches map[string][]int,
	recordedFreed map[string]float64,
) float64 {
	out := 0.0
	for nodeName, node := range feasibleNodes {
		if _, hasBatch := nodeBatches[nodeName]; hasBatch {
			continue
		}
		if node != nil {
			out += nodeIdleOrReleasingGpus(node)
		}
		out += recordedFreed[nodeName]
	}
	return out
}

func pendingTaskGpuStats(s *scenario.ByNodeScenario) (totalDemand, minTask float64) {
	minTask = -1
	for _, t := range s.GetPreemptor().GetAllPodsMap() {
		req := t.ResReq.GetGpusQuota()
		totalDemand += req
		if minTask < 0 || req < minTask {
			minTask = req
		}
	}
	return totalDemand, minTask
}

// smallestKCovering returns the smallest K such that the sum of the top-K capacities
// (items must be pre-sorted) reaches demand. Returns 0 when demand is already non-
// positive, or len(items)+1 (out of range) when no K satisfies.
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
