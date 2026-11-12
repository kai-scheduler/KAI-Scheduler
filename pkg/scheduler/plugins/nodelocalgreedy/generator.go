// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodelocalgreedy

import (
	"sort"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/api/constants"
)

type nodeLocalGreedyGenerator struct {
	solveCtx                         *solvers.SolveContext
	generateVictimsQueue             solvers.GenerateVictimsQueue
	builder                          *solvers.PodAccumulatedScenarioBuilder
	scenarios                        []*scenario.ByNodeScenario
	shouldAdvanceAccumulatedScenario bool
	placementKnown                   bool
	narrowPrefix                     bool
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
		// The builder mutates one scenario as victims accumulate. This prefix,
		// including its stock superset, is queued before the next victim is added.
		g.scenarios = g.scenariosForPrefix(accumulated)
	}
}

func (g *nodeLocalGreedyGenerator) scenariosForPrefix(accumulated *scenario.ByNodeScenario) []*scenario.ByNodeScenario {
	if !g.placementKnown {
		g.placementKnown = true
		g.narrowPrefix = g.isSinglePodPlacement(len(accumulated.PendingTasks()))
	}
	stock := nodeLocalScenarios(g.solveCtx.Session, accumulated)
	// A partial probe of a larger job stays on the stock stream. Domain batches
	// are a single-pod optimization and must not run ahead of that stream.
	if !g.narrowPrefix {
		return stock
	}
	narrow := batchesTouchingNodeScenarios(g.solveCtx.Session, accumulated)
	head := append(narrow, domainBatchScenarios(g.solveCtx.Session, accumulated)...)
	if len(head) == 0 {
		return stock
	}
	// Narrower batches of this prefix, then its stock superset, before the next victim.
	return appendUniqueThenStock(head, stock)
}

// appendUniqueThenStock keeps the first copy of a victim set from head, then
// every stock scenario whose set was not already queued. Stock order is unchanged.
func appendUniqueThenStock(head, stock []*scenario.ByNodeScenario) []*scenario.ByNodeScenario {
	seen := map[string]struct{}{}
	out := make([]*scenario.ByNodeScenario, 0, len(head)+len(stock))
	for _, sn := range head {
		if sn == nil {
			continue
		}
		key := scenarioVictimKey(sn)
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sn)
	}
	for _, sn := range stock {
		if sn == nil {
			continue
		}
		key := scenarioVictimKey(sn)
		if _, found := seen[key]; found {
			continue
		}
		out = append(out, sn)
	}
	return out
}

// scenarioVictimKey distinguishes a domain subset that drops out-of-domain
// recorded victims from the stock scenario with the same potential victims.
func scenarioVictimKey(sn *scenario.ByNodeScenario) string {
	if sn == nil {
		return ""
	}
	return victimUIDSetKey(sn.PotentialVictimsTasks()) + "\x01" + victimUIDSetKey(sn.RecordedVictimsTasks())
}

// isSinglePodPlacement is true only when this probe places the job's single
// unscheduled pod. A k=1 probe of a larger job, including one whose other pod
// is still gated, stays on the stock stream. Binding and pipelined pods already
// have a placement and are not unscheduled work.
func (g *nodeLocalGreedyGenerator) isSinglePodPlacement(scenarioPending int) bool {
	if scenarioPending != 1 || g.solveCtx == nil || g.solveCtx.ProbeK != 1 {
		return false
	}
	job := g.solveCtx.PartialPendingJob
	session := g.solveCtx.Session
	if job == nil || session == nil || session.ClusterInfo == nil || session.ClusterInfo.PodGroupInfos == nil {
		return false
	}
	original := session.ClusterInfo.PodGroupInfos[job.UID]
	if original == nil {
		return false
	}
	unscheduled := 0
	for _, task := range original.GetAllPodsMap() {
		if task != nil && isUnscheduledWork(task.Status) {
			unscheduled++
		}
	}
	return unscheduled == 1
}

// isUnscheduledWork is a pod with no placement yet. Pending has not been
// scheduled. Gated is still unschedulable. Allocated, binding, pipelined,
// bound, and running pods already have a host decision.
func isUnscheduledWork(status pod_status.PodStatus) bool {
	return status == pod_status.Pending || status == pod_status.Gated
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

// batchesTouchingNodeScenarios is the current prefix restricted to batches that
// have a task on each node of the latest victim. Omitted when that set is the
// stock VictimsTasksFromNodes set, so single-batch jobs stay unchanged.
func batchesTouchingNodeScenarios(session *framework.Session, base *scenario.ByNodeScenario) []*scenario.ByNodeScenario {
	if base == nil || len(base.PotentialVictimsTasks()) == 0 {
		return nil
	}
	var scenarios []*scenario.ByNodeScenario
	seen := map[string]struct{}{}
	for _, nodeName := range nodeNamesOfJob(base.LatestPotentialVictim()) {
		narrowTasks := tasksForBatchesTouchingNode(base, nodeName)
		if len(narrowTasks) == 0 {
			continue
		}
		stockTasks := base.VictimsTasksFromNodes([]string{nodeName})
		if victimUIDSetKey(narrowTasks) == victimUIDSetKey(stockTasks) {
			continue
		}
		key := victimUIDSetKey(narrowTasks)
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
		addPotentialVictimsGroupedByJob(sn, narrowTasks)
		scenarios = append(scenarios, sn)
	}
	return scenarios
}

// tasksForBatchesTouchingNode keeps accumulator order. A batch is included whole
// when any of its tasks is on nodeName.
func tasksForBatchesTouchingNode(base *scenario.ByNodeScenario, nodeName string) []*pod_info.PodInfo {
	touching := map[*podgroup_info.PodGroupInfo]struct{}{}
	for _, task := range base.PotentialVictimsTasks() {
		if task == nil || task.NodeName != nodeName {
			continue
		}
		rep := base.GetVictimJobRepresentativeById(task)
		if rep == nil {
			continue
		}
		touching[rep] = struct{}{}
	}
	if len(touching) == 0 {
		return nil
	}
	tasks := make([]*pod_info.PodInfo, 0, len(base.PotentialVictimsTasks()))
	for _, task := range base.PotentialVictimsTasks() {
		if task == nil {
			continue
		}
		rep := base.GetVictimJobRepresentativeById(task)
		if _, ok := touching[rep]; !ok {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// domainBatchScenarios returns one scenario per required-topology domain of the
// latest victim. Each scenario keeps every batch that touches that domain, and
// nothing from other domains in this prefix. No scenario is returned when the
// job has no required level or the set is the whole prefix.
func domainBatchScenarios(session *framework.Session, base *scenario.ByNodeScenario) []*scenario.ByNodeScenario {
	if session == nil || base == nil || len(base.PotentialVictimsTasks()) == 0 {
		return nil
	}
	level := requiredTopologyLevel(base.GetPreemptor())
	if level == "" {
		return nil
	}
	domainSet := map[string]struct{}{}
	for _, nodeName := range nodeNamesOfJob(base.LatestPotentialVictim()) {
		if value := nodeLabel(session, nodeName, level); value != "" {
			domainSet[value] = struct{}{}
		}
	}
	if len(domainSet) == 0 {
		return nil
	}
	domains := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	whole := victimUIDSetKey(base.PotentialVictimsTasks())
	allRecorded := victimUIDSetKey(tasksOfJobs(base.RecordedVictimsJobs()))
	var scenarios []*scenario.ByNodeScenario
	seen := map[string]struct{}{}
	for _, domain := range domains {
		tasks := tasksForBatchesTouchingDomain(session, base, level, domain)
		if len(tasks) == 0 {
			continue
		}
		recorded := recordedJobsInDomain(session, base.RecordedVictimsJobs(), level, domain)
		key := victimUIDSetKey(tasks) + "\x01" + victimUIDSetKey(tasksOfJobs(recorded))
		// Identical to the full prefix, so the stock scenarios already cover it.
		if victimUIDSetKey(tasks) == whole && victimUIDSetKey(tasksOfJobs(recorded)) == allRecorded {
			continue
		}
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		sn := scenario.NewByNodeScenario(
			session,
			base.GetPreemptor(),
			base.PendingTasks(),
			nil,
			recorded,
		)
		addPotentialVictimsGroupedByJob(sn, tasks)
		scenarios = append(scenarios, sn)
	}
	return scenarios
}

// recordedJobsInDomain keeps a recorded victim job whole when any of its tasks
// sit in the domain. Jobs entirely outside the domain are left for the stock fallback.
func recordedJobsInDomain(
	session *framework.Session, jobs []*podgroup_info.PodGroupInfo, level, domain string,
) []*podgroup_info.PodGroupInfo {
	if len(jobs) == 0 {
		return nil
	}
	out := make([]*podgroup_info.PodGroupInfo, 0, len(jobs))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		for _, task := range job.GetAllPodsMap() {
			if task != nil && nodeLabel(session, task.NodeName, level) == domain {
				out = append(out, job)
				break
			}
		}
	}
	return out
}

func tasksOfJobs(jobs []*podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	var tasks []*pod_info.PodInfo
	for _, job := range jobs {
		if job == nil {
			continue
		}
		for _, task := range job.GetAllPodsMap() {
			if task != nil {
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

// requiredTopologyLevel is set only when the job has one required level.
// Different levels on subgroups or pod sets are left to the stock scenarios.
func requiredTopologyLevel(job *podgroup_info.PodGroupInfo) string {
	if job == nil || job.RootSubGroupSet == nil {
		return ""
	}
	levels := map[string]struct{}{}
	collectRequiredLevels(job.RootSubGroupSet, levels)
	if len(levels) != 1 {
		return ""
	}
	for level := range levels {
		return level
	}
	return ""
}

func collectRequiredLevels(sgs *subgroup_info.SubGroupSet, levels map[string]struct{}) {
	if sgs == nil {
		return
	}
	if constraint := sgs.GetTopologyConstraint(); constraint != nil && constraint.RequiredLevel != "" {
		levels[constraint.RequiredLevel] = struct{}{}
	}
	for _, child := range sgs.GetDirectSubgroupsSets() {
		collectRequiredLevels(child, levels)
	}
	for _, podSet := range sgs.GetDirectPodSets() {
		if constraint := podSet.GetTopologyConstraint(); constraint != nil && constraint.RequiredLevel != "" {
			levels[constraint.RequiredLevel] = struct{}{}
		}
	}
}

func nodeLabel(session *framework.Session, nodeName, level string) string {
	if session == nil || session.ClusterInfo == nil || session.ClusterInfo.Nodes == nil {
		return ""
	}
	node := session.ClusterInfo.Nodes[nodeName]
	if node == nil || node.Node == nil {
		return ""
	}
	return node.Node.Labels[level]
}

func tasksForBatchesTouchingDomain(
	session *framework.Session, base *scenario.ByNodeScenario, level, domain string,
) []*pod_info.PodInfo {
	touching := map[*podgroup_info.PodGroupInfo]struct{}{}
	for _, task := range base.PotentialVictimsTasks() {
		if task == nil || nodeLabel(session, task.NodeName, level) != domain {
			continue
		}
		rep := base.GetVictimJobRepresentativeById(task)
		if rep == nil {
			continue
		}
		touching[rep] = struct{}{}
	}
	if len(touching) == 0 {
		return nil
	}
	tasks := make([]*pod_info.PodInfo, 0, len(base.PotentialVictimsTasks()))
	for _, task := range base.PotentialVictimsTasks() {
		if task == nil {
			continue
		}
		rep := base.GetVictimJobRepresentativeById(task)
		if _, ok := touching[rep]; !ok {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks
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
