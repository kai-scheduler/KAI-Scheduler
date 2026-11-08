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
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
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
	deferredStock                    []*scenario.ByNodeScenario
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
			if len(g.deferredStock) > 0 {
				g.scenarios = g.deferredStock
				g.deferredStock = nil
				continue
			}
			return nil
		}
		// The builder mutates one scenario as victims accumulate. Scenarios for
		// this prefix are built before the next pop. Partial probes and
		// multi-task jobs keep the stock per-node split.
		g.scenarios = g.scenariosForPrefix(accumulated)
	}
}

func (g *nodeLocalGreedyGenerator) scenariosForPrefix(accumulated *scenario.ByNodeScenario) []*scenario.ByNodeScenario {
	if !g.placementKnown {
		g.placementKnown = true
		g.narrowPrefix = g.isSinglePodPlacement(len(accumulated.PendingTasks()))
	}
	stock := nodeLocalScenarios(g.solveCtx.Session, accumulated)
	var narrow []*scenario.ByNodeScenario
	if g.narrowPrefix {
		// Batches that touch the latest victim's node, whole. When this is a
		// proper subset, the stock superset is held until the queue is
		// exhausted so it still runs if no subset places the pod.
		narrow = batchesTouchingNodeScenarios(g.solveCtx.Session, accumulated)
	}
	if g.narrowPrefix && len(narrow) > 0 {
		g.deferredStock = append(g.deferredStock, stock...)
		// Skip a one-node subset that cannot cover the pending CPU request.
		// The held stock superset still runs if every subset fails.
		narrow = narrowScenariosWithCPU(g.solveCtx.Session, accumulated, narrow)
		head := append(narrow, domainBatchScenarios(g.solveCtx.Session, accumulated)...)
		if len(head) == 0 {
			return nil
		}
		return appendUniqueThenStock(head, nil)
	}
	// Required-topology jobs get one scenario per domain of the latest victim,
	// still using only batches from this prefix. Stock stays in its order.
	head := append(narrow, domainBatchScenarios(g.solveCtx.Session, accumulated)...)
	if len(head) == 0 {
		return stock
	}
	return appendUniqueThenStock(head, stock)
}

// narrowScenariosWithCPU drops a one-node subset whose victim CPU plus the
// node's idle CPU is below the pending pod's CPU request. Unknown CPU and
// multi-node subsets are kept.
func narrowScenariosWithCPU(
	session *framework.Session, base *scenario.ByNodeScenario, narrow []*scenario.ByNodeScenario,
) []*scenario.ByNodeScenario {
	if base == nil {
		return narrow
	}
	need := cpuMilli(base.PendingTasks())
	if need <= 0 {
		return narrow
	}
	kept := make([]*scenario.ByNodeScenario, 0, len(narrow))
	for _, sn := range narrow {
		if sn == nil || cpuCovered(session, sn, need) {
			kept = append(kept, sn)
		}
	}
	return kept
}

func cpuCovered(session *framework.Session, sn *scenario.ByNodeScenario, need float64) bool {
	tasks := sn.PotentialVictimsTasks()
	freed := cpuMilli(tasks)
	if freed <= 0 {
		return true
	}
	nodeName := ""
	for _, task := range tasks {
		if task == nil || task.NodeName == "" {
			continue
		}
		if nodeName == "" {
			nodeName = task.NodeName
			continue
		}
		if task.NodeName != nodeName {
			return true
		}
	}
	if nodeName == "" {
		return true
	}
	return nodeIdleCPU(session, nodeName)+freed+1e-3 >= need
}

func cpuMilli(tasks []*pod_info.PodInfo) float64 {
	total := 0.0
	for _, task := range tasks {
		if task == nil {
			continue
		}
		total += task.ResReqVector.Get(resource_info.CPUIndex)
	}
	return total
}

func nodeIdleCPU(session *framework.Session, nodeName string) float64 {
	if session == nil || session.ClusterInfo == nil || session.ClusterInfo.Nodes == nil {
		return 0
	}
	node := session.ClusterInfo.Nodes[nodeName]
	if node == nil {
		return 0
	}
	return node.IdleVector.Get(resource_info.CPUIndex)
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

// isSinglePodPlacement is true only for the probe that places the original job's
// one pending pod. A partial k=1 probe of a larger job also has one scenario task
// and ProbeK 1; that probe stays on the stock stream.
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
	pending := 0
	for _, task := range original.GetAllPodsMap() {
		if task != nil && task.Status == pod_status.Pending {
			pending++
		}
	}
	return pending == 1
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
	for len(g.scenarios) > 0 {
		sn := g.scenarios[0]
		g.scenarios = g.scenarios[1:]
		if sn == nil {
			continue
		}
		// A scenario that cannot hold the largest pending task on any one node
		// cannot place the pod. Later scenarios, including held stock, still run.
		if !g.canHostLargestTask(sn) {
			continue
		}
		return sn
	}
	return nil
}

// canHostLargestTask is false only when every pending task needs more GPUs
// than the best single node in the scenario can free. Idle capacity on a
// feasible node keeps the scenario.
func (g *nodeLocalGreedyGenerator) canHostLargestTask(sn *scenario.ByNodeScenario) bool {
	if sn == nil {
		return false
	}
	need := 0.0
	for _, task := range sn.PendingTasks() {
		if task == nil {
			continue
		}
		if q := task.GpuRequirement.GetGpusQuota(); q > need {
			need = q
		}
	}
	if need <= 0 {
		return true
	}
	if g.solveCtx != nil {
		for _, node := range g.solveCtx.FeasibleNodes {
			if node == nil {
				continue
			}
			idle, _ := node.GetSumOfIdleGPUs()
			releasing, _ := node.GetSumOfReleasingGPUs()
			if idle+releasing+1e-6 >= need {
				return true
			}
		}
	}
	freedByNode := map[string]float64{}
	add := func(tasks []*pod_info.PodInfo) {
		for _, task := range tasks {
			if task == nil || task.NodeName == "" {
				continue
			}
			freedByNode[task.NodeName] += taskGPUs(task)
		}
	}
	add(sn.PotentialVictimsTasks())
	add(sn.RecordedVictimsTasks())
	if len(freedByNode) == 0 {
		return true
	}
	best := 0.0
	for name, freed := range freedByNode {
		capacity := freed + nodeIdleGPUs(g.nodeSession(), name)
		if capacity > best {
			best = capacity
		}
	}
	return best+1e-6 >= need
}

func (g *nodeLocalGreedyGenerator) nodeSession() *framework.Session {
	if g.solveCtx == nil {
		return nil
	}
	return g.solveCtx.Session
}

func taskGPUs(task *pod_info.PodInfo) float64 {
	if task == nil {
		return 0
	}
	if q := task.AcceptedGpuRequirement.GetGpusQuota(); q > 0 {
		return q
	}
	return task.GpuRequirement.GetGpusQuota()
}

func nodeIdleGPUs(session *framework.Session, nodeName string) float64 {
	if session == nil || session.ClusterInfo == nil || session.ClusterInfo.Nodes == nil {
		return 0
	}
	node := session.ClusterInfo.Nodes[nodeName]
	if node == nil {
		return 0
	}
	idle, _ := node.GetSumOfIdleGPUs()
	releasing, _ := node.GetSumOfReleasingGPUs()
	return idle + releasing
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

func requiredTopologyLevel(job *podgroup_info.PodGroupInfo) string {
	if job == nil || job.RootSubGroupSet == nil {
		return ""
	}
	return firstRequiredLevel(job.RootSubGroupSet)
}

func firstRequiredLevel(sgs *subgroup_info.SubGroupSet) string {
	if sgs == nil {
		return ""
	}
	if constraint := sgs.GetTopologyConstraint(); constraint != nil && constraint.RequiredLevel != "" {
		return constraint.RequiredLevel
	}
	for _, child := range sgs.GetDirectSubgroupsSets() {
		if level := firstRequiredLevel(child); level != "" {
			return level
		}
	}
	for _, podSet := range sgs.GetDirectPodSets() {
		if constraint := podSet.GetTopologyConstraint(); constraint != nil && constraint.RequiredLevel != "" {
			return constraint.RequiredLevel
		}
	}
	return ""
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
