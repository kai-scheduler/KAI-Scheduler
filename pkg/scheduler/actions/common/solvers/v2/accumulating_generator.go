// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"sort"

	"golang.org/x/exp/slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/accumulated_scenario_filters"
	idle_gpus_filter "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/accumulated_scenario_filters/idle_gpus"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/accumulated_scenario_filters/node_affinities"
	solverscenario "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

// accumulatingGenerator walks a victim queue, growing a single
// underlying ByNodeScenario one job at a time, and emits one Scenario
// per host node of the just-added victim job, followed by a full-set
// fallback covering every accumulated potential.
//
// The accumulation + filter logic is ported from the legacy
// PodAccumulatedScenarioBuilder. Filters use the existing
// accumulated_scenario_filters package against the underlying
// ByNodeScenario.
type accumulatingGenerator struct {
	ssn       *framework.Session
	preemptor *podgroup_info.PodGroupInfo
	pending   []*pod_info.PodInfo

	scenario             *solverscenario.ByNodeScenario
	filters              []accumulated_scenario_filters.Interface
	victimsQueue         *utils.JobsOrderByQueues
	recordedVictimsTasks map[common_info.PodID]*pod_info.PodInfo

	pendingEmissions []Scenario
	started          bool
}

// NewAccumulatingGenerator constructs a Generator that walks the given
// victims queue, accumulates potential victims, and emits scenarios in
// the same order as today's solver stack.
//
// pendingJob may be a partial-job representative when called from the
// outer gang loop (Phase 3). Phase 4 will collapse the gang loop and
// always pass the full preemptor.
func NewAccumulatingGenerator(
	ssn *framework.Session,
	pendingJob *podgroup_info.PodGroupInfo,
	recordedVictimsJobs []*podgroup_info.PodGroupInfo,
	victimsQueue *utils.JobsOrderByQueues,
	feasibleNodes map[string]*node_info.NodeInfo,
) Generator {
	pending := podgroup_info.GetTasksToAllocate(
		pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false,
	)

	var scenario *solverscenario.ByNodeScenario
	recorded := map[common_info.PodID]*pod_info.PodInfo{}
	if len(pending) > 0 {
		scenario = solverscenario.NewByNodeScenario(ssn, pendingJob, pending, nil, recordedVictimsJobs)
		for _, job := range recordedVictimsJobs {
			for id, podInfo := range job.GetAllPodsMap() {
				recorded[id] = podInfo
			}
		}
	}

	var filters []accumulated_scenario_filters.Interface
	if f := node_affinities.NewNodeAffinitiesFilter(scenario, feasibleNodes, ssn); f != nil {
		filters = append(filters, f)
	}
	if f := idle_gpus_filter.NewTopologyAwareIdleGpusFilter(scenario, ssn.ClusterInfo.Nodes); f != nil {
		filters = append(filters, f)
	}
	if f := idle_gpus_filter.NewIdleGpusFilter(scenario, ssn.ClusterInfo.Nodes); f != nil {
		filters = append(filters, f)
	}

	return &accumulatingGenerator{
		ssn:                  ssn,
		preemptor:            pendingJob,
		pending:              pending,
		scenario:             scenario,
		filters:              filters,
		victimsQueue:         victimsQueue,
		recordedVictimsTasks: recorded,
	}
}

func (g *accumulatingGenerator) Next() (Scenario, bool) {
	for {
		if len(g.pendingEmissions) > 0 {
			s := g.pendingEmissions[0]
			g.pendingEmissions = g.pendingEmissions[1:]
			return s, true
		}
		if !g.advance() {
			return Scenario{}, false
		}
	}
}

// advance moves the underlying scenario to its next valid state and
// queues that step's emissions. Returns false when the queue is
// exhausted.
func (g *accumulatingGenerator) advance() bool {
	if g.scenario == nil {
		return false
	}

	if !g.started {
		g.started = true
		if g.scenarioValid() {
			g.pendingEmissions = g.buildEmissions(g.scenario)
			if len(g.pendingEmissions) > 0 {
				return true
			}
		}
	}

	for !g.victimsQueue.IsEmpty() {
		if !g.addNextPotentialVictims() {
			continue
		}
		if !g.scenarioValid() {
			continue
		}
		g.pendingEmissions = g.buildEmissions(g.scenario)
		if len(g.pendingEmissions) > 0 {
			return true
		}
	}
	return false
}

func (g *accumulatingGenerator) scenarioValid() bool {
	for _, f := range g.filters {
		ok, err := f.Filter(g.scenario)
		if err != nil {
			log.InfraLogger.Errorf(
				"Failed to run the filter %s with the error %v. scenario: %s",
				f.Name(), err, g.scenario,
			)
			continue
		}
		if !ok {
			log.InfraLogger.V(5).Infof(
				"Filtered by %s for scenario: %s", f.Name(), g.scenario,
			)
			metrics.IncScenarioFilteredByAction()
			return false
		}
	}
	return true
}

// addNextPotentialVictims pops the next victim job from the queue and
// folds its evictable tasks into the accumulated scenario. If any of
// those tasks are already recorded victims, the step is skipped and
// the remaining (non-recorded) tasks are pushed back so they can be
// evaluated independently.
//
// Returns true when the scenario actually changed.
func (g *accumulatingGenerator) addNextPotentialVictims() bool {
	nextVictimJob := g.victimsQueue.PopNextJob()

	potentialTasks, jobHasMoreTasks := podgroup_info.GetTasksToEvict(
		nextVictimJob, g.ssn.SubGroupOrderFn, g.ssn.TaskOrderFn,
	)

	for _, t := range potentialTasks {
		if _, ok := g.recordedVictimsTasks[t.UID]; !ok {
			continue
		}
		// Elastic-job carve-out: tasks not yet recorded get re-queued
		// so they can be evaluated as a fresh step.
		var remaining []*pod_info.PodInfo
		for _, jobTask := range nextVictimJob.GetAllPodsMap() {
			if _, isRecorded := g.recordedVictimsTasks[jobTask.UID]; !isRecorded {
				remaining = append(remaining, jobTask)
			}
		}
		if len(remaining) > 0 {
			g.victimsQueue.PushJob(nextVictimJob.CloneWithTasks(remaining))
		}
		return false
	}

	if jobHasMoreTasks {
		var remaining []*pod_info.PodInfo
		for _, jobTask := range nextVictimJob.GetAllPodsMap() {
			if !slices.Contains(potentialTasks, jobTask) {
				remaining = append(remaining, jobTask)
			}
		}
		g.victimsQueue.PushJob(nextVictimJob.CloneWithTasks(remaining))
	}

	g.scenario.AddPotentialVictimsTasks(potentialTasks)
	return true
}

// buildEmissions yields, for the current accumulation step, scenarios
// of progressively-larger host-node coverage:
//
//  1. Per-node — recorded ∪ victims on a single host node of the latest
//     victim job. Cheapest fix when one node's victims suffice.
//  2. Pairs — recorded ∪ victims on (one prior host node + one latest
//     host node). Catches gang preemptors that need exactly two nodes
//     freed without dragging unrelated accumulated victims along.
//  3. Full set — recorded ∪ every accumulated potential. Required for
//     gangs spanning more than two host nodes; otherwise the upper
//     bound that always finds a solution if one exists in the
//     accumulated pool.
//
// All emissions share the same Candidates set: the full accumulated
// victim pool (recorded ∪ every potential added so far). That is what
// a legacy ScenarioInfo validator would have observed at this
// accumulation step, so passing it through preserves their semantics.
func (g *accumulatingGenerator) buildEmissions(s *solverscenario.ByNodeScenario) []Scenario {
	recorded := s.RecordedVictimsTasks()
	latest := s.LatestPotentialVictim()
	candidates := joinTasks(recorded, s.PotentialVictimsTasks())

	if latest == nil {
		if len(recorded) == 0 {
			return nil
		}
		return []Scenario{g.scenarioWith(append([]*pod_info.PodInfo(nil), recorded...), candidates)}
	}

	latestHosts := sortedHostNodes(latest)
	priorHosts := priorHostNodes(s, latestHosts)

	emissions := make([]Scenario, 0, len(latestHosts)+len(latestHosts)*len(priorHosts)+1)

	// (1) Per-node of the latest victim job's host nodes.
	for _, node := range latestHosts {
		victims := s.VictimsTasksFromNodes([]string{node})
		emissions = append(emissions, g.scenarioWith(joinTasks(recorded, victims), candidates))
	}
	// (2) Pairs: each prior host node combined with each latest host
	// node. Two-node gang preemptors are solved here without including
	// unrelated accumulated victims.
	for _, prior := range priorHosts {
		for _, node := range latestHosts {
			victims := s.VictimsTasksFromNodes([]string{prior, node})
			emissions = append(emissions, g.scenarioWith(joinTasks(recorded, victims), candidates))
		}
	}
	// (3) Full-set fallback.
	all := s.PotentialVictimsTasks()
	if len(all) > 0 {
		emissions = append(emissions, g.scenarioWith(joinTasks(recorded, all), candidates))
	}
	return emissions
}

// priorHostNodes returns the host nodes carrying any accumulated
// potential victim that does NOT live on one of the given exclude
// nodes. Used by buildEmissions to find pair candidates for the
// just-added victim job.
func priorHostNodes(s *solverscenario.ByNodeScenario, exclude []string) []string {
	excluded := make(map[string]struct{}, len(exclude))
	for _, n := range exclude {
		excluded[n] = struct{}{}
	}
	set := make(map[string]struct{})
	for _, t := range s.PotentialVictimsTasks() {
		if t.NodeName == "" {
			continue
		}
		if _, skip := excluded[t.NodeName]; skip {
			continue
		}
		set[t.NodeName] = struct{}{}
	}
	nodes := make([]string, 0, len(set))
	for n := range set {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	return nodes
}

func (g *accumulatingGenerator) scenarioWith(victims, candidates []*pod_info.PodInfo) Scenario {
	return Scenario{
		Preemptor:  g.preemptor,
		Pending:    g.pending,
		Victims:    victims,
		Candidates: candidates,
	}
}

func sortedHostNodes(job *podgroup_info.PodGroupInfo) []string {
	set := make(map[string]struct{})
	for _, p := range job.GetAllPodsMap() {
		if p.NodeName == "" {
			continue
		}
		set[p.NodeName] = struct{}{}
	}
	nodes := make([]string, 0, len(set))
	for n := range set {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	return nodes
}

func joinTasks(a, b []*pod_info.PodInfo) []*pod_info.PodInfo {
	out := make([]*pod_info.PodInfo, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
