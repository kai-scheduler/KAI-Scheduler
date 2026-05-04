// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	solverscenario "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// accumulatingGenerator wraps the legacy PodAccumulatedScenarioBuilder
// and emits, for each surviving accumulation step, the same set of
// scenarios that today's byPodSolver iterates: one Scenario per host
// node of the just-added victim job (per-node subset), followed by one
// Scenario over the full accumulated set.
//
// Phase 2 contract: emit-by-emit equivalent to PodAccumulatedScenarioBuilder
// + byPodSolver's per-node loop + the full-set fallback. Filter behavior
// is inherited from the wrapped builder.
//
// Phases 5 and 7 will fold accumulation into v2 directly and remove the
// dependency on the parent solvers package.
type accumulatingGenerator struct {
	preemptor *podgroup_info.PodGroupInfo
	pending   []*pod_info.PodInfo

	builder *solvers.PodAccumulatedScenarioBuilder

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
	builder := solvers.NewPodAccumulatedScenarioBuilder(
		ssn, pendingJob, recordedVictimsJobs, victimsQueue, feasibleNodes,
	)
	return &accumulatingGenerator{
		preemptor: pendingJob,
		pending:   pending,
		builder:   builder,
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

func (g *accumulatingGenerator) advance() bool {
	var s *solverscenario.ByNodeScenario
	if !g.started {
		s = g.builder.GetValidScenario()
		g.started = true
	} else {
		s = g.builder.GetNextScenario()
	}
	if s == nil {
		return false
	}
	g.pendingEmissions = g.buildEmissions(s)
	return true
}

func (g *accumulatingGenerator) buildEmissions(s *solverscenario.ByNodeScenario) []Scenario {
	recorded := s.RecordedVictimsTasks()
	latest := s.LatestPotentialVictim()

	// Initial step: no potentials accumulated yet. Only emit if there
	// are recorded victims to simulate against (matches today's
	// "hasRecordedVictimsForSimulation" branch in byPodSolver.solve).
	if latest == nil {
		if len(recorded) == 0 {
			return nil
		}
		return []Scenario{g.scenarioWithVictims(append([]*pod_info.PodInfo(nil), recorded...))}
	}

	nodes := sortedHostNodes(latest)
	emissions := make([]Scenario, 0, len(nodes)+1)

	// Per-node subset for each host node of the latest victim job:
	// recorded ∪ all accumulated victims on that node.
	for _, node := range nodes {
		victimsOnNode := s.VictimsTasksFromNodes([]string{node})
		emissions = append(emissions, g.scenarioWithVictims(joinTasks(recorded, victimsOnNode)))
	}

	// Full-set fallback: recorded ∪ every accumulated potential victim.
	all := s.PotentialVictimsTasks()
	if len(all) > 0 {
		emissions = append(emissions, g.scenarioWithVictims(joinTasks(recorded, all)))
	}

	return emissions
}

func (g *accumulatingGenerator) scenarioWithVictims(victims []*pod_info.PodInfo) Scenario {
	return Scenario{
		Preemptor: g.preemptor,
		Pending:   g.pending,
		Victims:   victims,
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
