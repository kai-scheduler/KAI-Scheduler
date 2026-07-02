// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodelocalgreedy

import (
	"sort"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type nodeLocalGreedyGenerator struct {
	solveCtx                         *solvers.SolveContext
	generateVictimsQueue             solvers.GenerateVictimsQueue
	builder                          *solvers.PodAccumulatedScenarioBuilder
	scenarios                        []*scenario.ByNodeScenario
	shouldAdvanceAccumulatedScenario bool
}

func NewNodeLocalGreedyGenerator(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
	solveCtx, generateVictimsQueue, ok := solvers.ValidateScenarioGeneratorContext(ctx)
	if !ok {
		return nil
	}
	return &nodeLocalGreedyGenerator{
		solveCtx:             solveCtx,
		generateVictimsQueue: generateVictimsQueue,
	}
}

func (g *nodeLocalGreedyGenerator) Name() string {
	return constants.GeneratorNodeLocalGreedy
}

func (g *nodeLocalGreedyGenerator) Next() api.ScenarioInfo {
	if !g.ensureBuilder() {
		return nil
	}
	for {
		if sn := g.popScenario(); sn != nil {
			return sn
		}
		accumulated := g.nextValidAccumulatedScenario()
		if accumulated == nil {
			return nil
		}
		g.scenarios = nodeLocalScenarios(g.solveCtx.Session, accumulated)
	}
}

func (g *nodeLocalGreedyGenerator) ensureBuilder() bool {
	if g.builder != nil {
		return true
	}
	victimsQueue := g.generateVictimsQueue()
	if victimsQueue == nil {
		return false
	}
	g.builder = solvers.NewPodAccumulatedScenarioBuilder(
		g.solveCtx.Session,
		g.solveCtx.PartialPendingJob,
		g.solveCtx.RecordedVictimsJobs,
		victimsQueue,
		g.solveCtx.FeasibleNodes,
	)
	return true
}

func (g *nodeLocalGreedyGenerator) popScenario() *scenario.ByNodeScenario {
	if len(g.scenarios) == 0 {
		return nil
	}
	sn := g.scenarios[0]
	g.scenarios = g.scenarios[1:]
	return sn
}

func (g *nodeLocalGreedyGenerator) nextValidAccumulatedScenario() *scenario.ByNodeScenario {
	if g.shouldAdvanceAccumulatedScenario {
		return g.builder.GetNextAccumulatedScenario()
	}
	g.shouldAdvanceAccumulatedScenario = true

	return g.builder.GetValidAccumulatedScenario()
}

func nodeLocalScenarios(session *framework.Session, base *scenario.ByNodeScenario) []*scenario.ByNodeScenario {
	if base == nil {
		return nil
	}
	if len(base.PotentialVictimsTasks()) == 0 {
		if len(base.RecordedVictimsTasks()) == 0 {
			return nil
		}
		return []*scenario.ByNodeScenario{base}
	}

	var scenarios []*scenario.ByNodeScenario
	seen := map[string]struct{}{}
	for _, nodeName := range nodeNamesOfJob(base.LatestPotentialVictim()) {
		victimTasks := base.VictimsTasksFromNodes([]string{nodeName})
		if len(victimTasks) == 0 {
			continue
		}
		key := victimUIDSetKey(victimTasks)
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		sn := scenario.NewByNodeScenario(
			session,
			base.GetPreemptor(),
			base.PendingTasks(),
			nil,
			base.RecordedVictimsJobs(),
		)
		addPotentialVictimsGroupedByJob(sn, victimTasks)
		scenarios = append(scenarios, sn)
	}
	return scenarios
}

func nodeNamesOfJob(job *podgroup_info.PodGroupInfo) []string {
	if job == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, task := range job.GetAllPodsMap() {
		if task.NodeName == "" {
			continue
		}
		seen[task.NodeName] = struct{}{}
	}
	nodeNames := make([]string, 0, len(seen))
	for nodeName := range seen {
		nodeNames = append(nodeNames, nodeName)
	}
	sort.Strings(nodeNames)
	return nodeNames
}

func victimUIDSetKey(tasks []*pod_info.PodInfo) string {
	uids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		uids = append(uids, string(task.UID))
	}
	sort.Strings(uids)
	return strings.Join(uids, "\x00")
}

func addPotentialVictimsGroupedByJob(sn *scenario.ByNodeScenario, tasks []*pod_info.PodInfo) {
	groupedTasks := map[common_info.PodGroupID][]*pod_info.PodInfo{}
	var jobOrder []common_info.PodGroupID
	for _, task := range tasks {
		if _, found := groupedTasks[task.Job]; !found {
			jobOrder = append(jobOrder, task.Job)
		}
		groupedTasks[task.Job] = append(groupedTasks[task.Job], task)
	}
	for _, jobID := range jobOrder {
		sn.AddPotentialVictimsTasks(groupedTasks[jobID])
	}
}
