// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package fullnodefirst implements the FullNodeFirst reclaim scenario generator.
// For a pending whole-node gang (every task fills an entire node, possibly of
// different sizes), it scans nodes once and proposes freeing one reclaimable node
// per task, giving the solver a node-scoped scenario in near-constant time instead
// of searching victim combinations. It only proposes; the simulator and validator
// remain authoritative.
package fullnodefirst

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// sizeDemand is how many whole nodes of one task size the gang still needs.
type sizeDemand struct {
	req   resource_info.ResourceVector
	count int
}

type fullNodeFirstGenerator struct {
	solveCtx *solvers.SolveContext

	initialized   bool
	inert         bool
	pendingTasks  []*pod_info.PodInfo
	demand        []sizeDemand
	nodeNames     []string
	recordedNodes map[string]struct{}
	cursor        int
	recordedDone  bool
}

// NewFullNodeFirstGenerator builds the generator, or nil if the context isn't a reclaim context it can serve.
func NewFullNodeFirstGenerator(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
	solveCtx, _, ok := solvers.ValidateScenarioGeneratorContext(ctx)
	if !ok {
		return nil
	}
	return &fullNodeFirstGenerator{solveCtx: solveCtx}
}

func (g *fullNodeFirstGenerator) Name() string {
	return constants.GeneratorFullNodeFirst
}

// Next returns one scenario per call (one reclaimable node per task), or untyped nil when exhausted.
func (g *fullNodeFirstGenerator) Next() api.ScenarioInfo {
	if !g.initialized {
		g.initialized = true
		g.init()
	}
	if g.inert {
		return nil
	}
	if g.totalDemand() == 0 {
		if g.recordedDone {
			return nil
		}
		g.recordedDone = true
		if sn := g.recordedOnlyScenario(); sn != nil {
			return sn
		}
		return nil
	}

	for g.cursor < len(g.nodeNames) {
		need := g.cloneDemand()
		window := make([]string, 0, remainingCount(need))
		victims := map[string][]*podgroup_info.PodGroupInfo{}
		for g.cursor < len(g.nodeNames) && remainingCount(need) > 0 {
			name := g.nodeNames[g.cursor]
			g.cursor++
			if jobs, idx, ok := g.matchNode(name, need); ok {
				window = append(window, name)
				victims[name] = jobs
				need[idx].count--
			}
		}
		if remainingCount(need) > 0 {
			return nil
		}
		return g.newScenario(window, victims)
	}
	return nil
}

// --- Initialization and per-size node demand ---

// init builds the per-size node demand and the node list; it stays inert unless every
// pending task is a whole-node request (a non-empty resource vector).
func (g *fullNodeFirstGenerator) init() {
	ssn := g.solveCtx.Session
	pendingTasks := podgroup_info.GetTasksToAllocate(
		g.solveCtx.PartialPendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, podgroup_info.SimulatedTaskAllocation)
	demand, ok := buildSizeDemand(pendingTasks)
	if !ok {
		g.inert = true
		return
	}
	g.pendingTasks = pendingTasks
	g.demand = demand
	g.reduceByRecordedFreedNodes()
	g.nodeNames = make([]string, 0, len(ssn.ClusterInfo.Nodes))
	for name := range ssn.ClusterInfo.Nodes {
		g.nodeNames = append(g.nodeNames, name)
	}
}

// buildSizeDemand groups the gang's tasks by resource request; it fails if there are
// no tasks or any task requests nothing (so it cannot fill a node).
func buildSizeDemand(tasks []*pod_info.PodInfo) ([]sizeDemand, bool) {
	if len(tasks) == 0 {
		return nil, false
	}
	var demand []sizeDemand
	for _, task := range tasks {
		if task.ResReqVector.IsZero() {
			return nil, false
		}
		matched := false
		for i := range demand {
			if demand[i].req.Equal(task.ResReqVector) {
				demand[i].count++
				matched = true
				break
			}
		}
		if !matched {
			demand = append(demand, sizeDemand{req: task.ResReqVector, count: 1})
		}
	}
	return demand, true
}

// reduceByRecordedFreedNodes records nodes already freed by recorded victims and
// decrements the matching size's demand, so those nodes are neither re-proposed nor
// double-counted across incremental gang probes.
func (g *fullNodeFirstGenerator) reduceByRecordedFreedNodes() {
	recoveredByNode := map[string]resource_info.ResourceVector{}
	for _, job := range g.solveCtx.RecordedVictimsJobs {
		for _, victim := range job.GetAllPodsMap() {
			if victim.NodeName == "" {
				continue
			}
			recovered := recoveredByNode[victim.NodeName]
			recovered.Add(victim.ResReqVector)
			recoveredByNode[victim.NodeName] = recovered
		}
	}

	ssn := g.solveCtx.Session
	g.recordedNodes = map[string]struct{}{}
	for nodeName, recovered := range recoveredByNode {
		node, found := ssn.ClusterInfo.Nodes[nodeName]
		if !found {
			continue
		}
		available := node.IdleVector.Clone()
		available.Add(recovered)
		for i := range g.demand {
			if g.demand[i].count == 0 {
				continue
			}
			if needsEntireNode(g.demand[i].req, node.AllocatableVector) && g.demand[i].req.LessEqual(available) {
				g.demand[i].count--
				g.recordedNodes[nodeName] = struct{}{}
				break
			}
		}
	}
}

func (g *fullNodeFirstGenerator) totalDemand() int {
	return remainingCount(g.demand)
}

func remainingCount(demand []sizeDemand) int {
	total := 0
	for _, d := range demand {
		total += d.count
	}
	return total
}

func (g *fullNodeFirstGenerator) cloneDemand() []sizeDemand {
	clone := make([]sizeDemand, len(g.demand))
	copy(clone, g.demand)
	return clone
}

// --- Node matching ---

// matchNode assigns the node to the first demanded size it fills and can reclaim for,
// returning that size's victim jobs and demand index.
func (g *fullNodeFirstGenerator) matchNode(name string, need []sizeDemand) ([]*podgroup_info.PodGroupInfo, int, bool) {
	if _, recorded := g.recordedNodes[name]; recorded {
		return nil, 0, false
	}
	node, found := g.solveCtx.Session.ClusterInfo.Nodes[name]
	if !found {
		return nil, 0, false
	}
	for i := range need {
		if need[i].count == 0 {
			continue
		}
		req := need[i].req
		// Skip nodes too small for this size.
		if !req.LessEqual(node.AllocatableVector) {
			continue
		}
		// Skip nodes this size does not fully fill (not a whole-node task here).
		if !needsEntireNode(req, node.AllocatableVector) {
			continue
		}
		// Skip nodes that already fit this size on idle (no reclaim needed).
		if req.LessEqual(node.IdleVector) {
			continue
		}
		if jobs, ok := g.reclaimableFor(name, node, req); ok {
			return jobs, i, true
		}
	}
	return nil, 0, false
}

// needsEntireNode reports whether the task exactly fills at least one of the node's resources.
func needsEntireNode(taskReq, allocatable resource_info.ResourceVector) bool {
	for i := range taskReq {
		if capacity := allocatable.Get(i); capacity > 0 && taskReq[i] == capacity {
			return true
		}
	}
	return false
}

// reclaimableFor returns the node's reclaimable victim jobs when evicting them (plus
// idle) covers req. Reclaimability reuses ssn.ReclaimVictimFilter.
func (g *fullNodeFirstGenerator) reclaimableFor(
	name string, node *node_info.NodeInfo, req resource_info.ResourceVector,
) ([]*podgroup_info.PodGroupInfo, bool) {
	ssn := g.solveCtx.Session
	reclaimer := g.solveCtx.PartialPendingJob
	reclaimableOnNode := node.IdleVector.Clone()
	var jobs []*podgroup_info.PodGroupInfo
	seen := map[common_info.PodGroupID]struct{}{}
	for _, task := range node.PodInfos {
		// PodGroupInfos is keyed by the namespaced id (NewPodGroupID), which is how the
		// cache normalizes a task's job; node.PodInfos may still carry the bare pod-group
		// annotation, so build the namespaced key. Fall back to the raw id in case it is
		// already normalized.
		job, ok := ssn.ClusterInfo.PodGroupInfos[common_info.NewPodGroupID(task.Namespace, string(task.Job))]
		if !ok {
			job, ok = ssn.ClusterInfo.PodGroupInfos[task.Job]
		}
		// Skip tasks whose job is missing from the cache.
		if !ok {
			continue
		}
		// Skip the reclaimer's own queue; it is never its own victim.
		if job.Queue == reclaimer.Queue {
			continue
		}
		if _, done := seen[job.UID]; done {
			continue
		}
		if !ssn.ReclaimVictimFilter(reclaimer, job) {
			continue
		}
		seen[job.UID] = struct{}{}
		evictable, _ := podgroup_info.GetTasksToEvict(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn)
		onThisNode := false
		for _, victim := range evictable {
			if victim.NodeName == name {
				reclaimableOnNode.Add(victim.ResReqVector)
				onThisNode = true
			}
		}
		if onThisNode {
			jobs = append(jobs, job)
		}
	}
	if !req.LessEqual(reclaimableOnNode) {
		return nil, false
	}
	return jobs, true
}

// --- Scenario construction ---

// newScenario builds a scenario evicting the chosen nodes' whole reclaimable gangs plus recorded victims.
func (g *fullNodeFirstGenerator) newScenario(
	nodes []string, victimsByNode map[string][]*podgroup_info.PodGroupInfo,
) *scenario.ByNodeScenario {
	sn := scenario.NewByNodeScenario(
		g.solveCtx.Session, g.solveCtx.PartialPendingJob, g.pendingTasks, nil, g.solveCtx.RecordedVictimsJobs)

	ssn := g.solveCtx.Session
	added := map[common_info.PodGroupID]struct{}{}
	for _, name := range nodes {
		for _, job := range victimsByNode[name] {
			if _, ok := added[job.UID]; ok {
				continue
			}
			added[job.UID] = struct{}{}
			tasks, _ := podgroup_info.GetTasksToEvict(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn)
			sn.AddPotentialVictimsTasks(tasks)
		}
	}
	if len(added) == 0 {
		return nil
	}
	return sn
}

// recordedOnlyScenario builds a scenario carrying only the recorded victims (which already free enough nodes).
func (g *fullNodeFirstGenerator) recordedOnlyScenario() *scenario.ByNodeScenario {
	if len(g.solveCtx.RecordedVictimsJobs) == 0 {
		return nil
	}
	return scenario.NewByNodeScenario(
		g.solveCtx.Session, g.solveCtx.PartialPendingJob, g.pendingTasks, nil, g.solveCtx.RecordedVictimsJobs)
}
