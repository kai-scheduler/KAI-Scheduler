// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"strings"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

type GenerateVictimsQueue func() *utils.JobsOrderByQueues

type JobSolver struct {
	feasibleNodes        []*node_info.NodeInfo
	solutionValidator    SolutionValidator
	generateVictimsQueue GenerateVictimsQueue
	actionType           framework.ActionType
	actionBudget         *ActionSearchBudget
}

type solvingState struct {
	recordedVictimsJobs  []*podgroup_info.PodGroupInfo
	recordedVictimsTasks []*pod_info.PodInfo
}

func NewJobsSolver(
	feasibleNodes []*node_info.NodeInfo,
	solutionValidator SolutionValidator,
	generateVictimsQueue GenerateVictimsQueue,
	action framework.ActionType,
	actionBudget ...*ActionSearchBudget,
) *JobSolver {
	var budget *ActionSearchBudget
	if len(actionBudget) > 0 {
		budget = actionBudget[0]
	}
	if budget == nil {
		budget = newUnlimitedActionSearchBudget(action)
	}
	return &JobSolver{
		feasibleNodes:        feasibleNodes,
		solutionValidator:    solutionValidator,
		generateVictimsQueue: generateVictimsQueue,
		actionType:           action,
		actionBudget:         budget,
	}
}

func (s *JobSolver) ensureActionBudget() *ActionSearchBudget {
	if s.actionBudget == nil {
		s.actionBudget = newUnlimitedActionSearchBudget(s.actionType)
	}
	return s.actionBudget
}

func newUnlimitedActionSearchBudget(action framework.ActionType) *ActionSearchBudget {
	now := time.Now
	return &ActionSearchBudget{
		action:    action,
		startedAt: now(),
		now:       now,
	}
}

// Solve attempts to find a feasible allocation for all of pendingJob's pending tasks,
// evicting tasks from other jobs as victims when necessary. It operates with all-or-nothing
// semantics: either the full set of pending tasks is scheduled, or no allocation is produced.
//
// Returns:
//   - solved: true when every pending task was allocated and pendingJob is gang-satisfied.
//   - statement: on success, a live Statement holding the speculative allocations and victim
//     evictions; the caller is responsible for Commit or Discard. nil on failure.
//   - victimTaskNames: formatted "<namespace>/<name>" strings of the victim tasks, for logging.
//
// Session state is mutated only on success (to reflect the speculative operations in the
// returned statement) and is left unchanged on failure.
func (s *JobSolver) Solve(
	ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo) (bool, *framework.Statement, []string) {
	solved, statement, victimTaskNames, _ := s.SolveWithResult(ssn, pendingJob)
	return solved, statement, victimTaskNames
}

// SolveWithResult attempts to solve pendingJob and returns a structured search result
// describing why the scenario search stopped.
func (s *JobSolver) SolveWithResult(
	ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo,
) (solved bool, statement *framework.Statement, victimTaskNames []string, searchResult *SearchResult) {
	defer func() {
		if searchResult != nil {
			metrics.IncScenarioSearchJobs(
				s.actionType, searchResult.scenarioSearchMetricResult(), searchResult.ReducedBudget(),
			)
		}
	}()

	originalNumActiveTasks := pendingJob.GetNumActiveUsedTasks()

	tasksToAllocate := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
	n := len(tasksToAllocate)
	if n == 0 {
		searchResult := terminalSearchResult(SearchResultGeneratorsExhausted, false, false)
		searchResult.metricResult = string(SearchResultNotAttempted)
		return false, nil, nil, searchResult
	}

	actionBudget := s.ensureActionBudget()
	jobBudget := actionBudget.BeginJob()
	if actionBudget.Exhausted() {
		return false, nil, nil, terminalSearchResult(SearchResultNotAttempted, false, false)
	}

	if s.generateVictimsQueue == nil {
		return false, nil, nil, terminalSearchResult(SearchResultNoGenerator, jobBudget.ReducedBudget(), false)
	}
	registrations := applicableScenarioGeneratorRegistrations(ssn, s.actionType)
	if len(registrations) == 0 {
		return false, nil, nil, terminalSearchResult(SearchResultNoGenerator, jobBudget.ReducedBudget(), false)
	}

	enteredSearch := false
	var lastVictimTasks []*pod_info.PodInfo
	var lastResult *SearchResult
	for _, registration := range registrations {
		state := solvingState{}
		generatorBudget := jobBudget.BeginGenerator(registration.Name)
		result := s.solvePendingJobWithGenerator(
			ssn, &state, pendingJob, tasksToAllocate, jobBudget, registration, generatorBudget,
		)
		enteredSearch = enteredSearch || searchResultEntered(result) || resultSolved(result)
		lastVictimTasks = state.recordedVictimsTasks
		lastResult = result

		if resultSolved(result) {
			solution := result.solution
			numActiveTasks := pendingJob.GetNumActiveUsedTasks()
			jobSolved := pendingJob.IsGangSatisfied()
			if originalNumActiveTasks >= numActiveTasks {
				jobSolved = false
			}

			log.InfraLogger.V(4).Infof(
				"Scenario solved for %d tasks to allocate for %s. Victims: %s",
				n, pendingJob.Name, victimPrintingStruct{solution.victimsTasks})
			return jobSolved, solution.statement, calcVictimNames(solution.victimsTasks), result
		}

		if shouldStopSearch(result) {
			preserveEnteredSearch(result, enteredSearch)
			return false, nil, calcVictimNames(lastVictimTasks), result
		}
	}

	if lastResult == nil {
		lastResult = terminalSearchResult(SearchResultGeneratorsExhausted, jobBudget.ReducedBudget(), enteredSearch)
	}
	preserveEnteredSearch(lastResult, enteredSearch)
	return false, nil, calcVictimNames(lastVictimTasks), lastResult
}

func (s *JobSolver) solvePendingJobWithGenerator(
	ssn *framework.Session,
	state *solvingState,
	pendingJob *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo,
	jobBudget *jobSearchBudget,
	registration framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
) *SearchResult {
	n := len(tasksToAllocate)
	enteredSearch := false
	if n > 1 {
		maxSolvedK, searchResult := s.searchMaxSolvableK(
			ssn, state, pendingJob, tasksToAllocate, jobBudget, registration, generatorBudget,
		)
		enteredSearch = searchResultEntered(searchResult) || maxSolvedK > 0
		if maxSolvedK == 0 {
			if searchResult == nil {
				searchResult = terminalSearchResult(SearchResultGeneratorsExhausted, jobBudget.ReducedBudget(), false)
			}
			preserveEnteredSearch(searchResult, enteredSearch)
			return searchResult
		}
	}

	result := s.probeAtK(ssn, state, pendingJob, tasksToAllocate, n, jobBudget, registration, generatorBudget)
	if !resultSolved(result) {
		preserveEnteredSearch(result, enteredSearch)
	}
	return result
}

// searchMaxSolvableK returns the largest k in [0, n) for which a probe at k succeeds.
// Each probe is discarded before returning, so session state is clean on return.
// Successful probes update hints in state for use by subsequent probes.
// Complexity: O(log n) probes — exponential doubling to locate a failing k (or reach n),
// then binary search between the last success and first failure.
func (s *JobSolver) searchMaxSolvableK(
	ssn *framework.Session,
	state *solvingState,
	pendingJob *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo,
	jobBudget *jobSearchBudget,
	registration framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
) (int, *SearchResult) {
	n := len(tasksToAllocate)
	if n <= 1 {
		return 0, nil
	}

	return searchMaxSolvableK(n, func(k int) *SearchResult {
		return s.tryProbeAndDiscard(
			ssn, state, pendingJob, tasksToAllocate, k, jobBudget, registration, generatorBudget,
		)
	})
}

func searchMaxSolvableK(n int, probe func(k int) *SearchResult) (int, *SearchResult) {
	lo := 0
	hi := n
	var lastUnsolvedResult *SearchResult
	enteredSearch := false
	k := 1
	for k < n {
		result := probe(k)
		enteredSearch = enteredSearch || searchResultEntered(result) || resultSolved(result)
		if shouldStopSearch(result) {
			preserveEnteredSearch(result, enteredSearch)
			return 0, result
		}
		if !resultSolved(result) {
			lastUnsolvedResult = result
			hi = k
			break
		}
		lo = k
		k *= 2
		if k >= n {
			hi = n
			break
		}
	}

	for hi-lo > 1 {
		mid := (lo + hi) / 2
		result := probe(mid)
		enteredSearch = enteredSearch || searchResultEntered(result) || resultSolved(result)
		if shouldStopSearch(result) {
			preserveEnteredSearch(result, enteredSearch)
			return 0, result
		}
		if resultSolved(result) {
			lo = mid
		} else {
			lastUnsolvedResult = result
			hi = mid
		}
	}
	return lo, lastUnsolvedResult
}

// tryProbeAndDiscard probes at k and always discards a solved statement so the session
// is left clean. On success, hints are written to state.
func (s *JobSolver) tryProbeAndDiscard(
	ssn *framework.Session,
	state *solvingState,
	pendingJob *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo,
	k int,
	jobBudget *jobSearchBudget,
	registration framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
) *SearchResult {
	result := s.probeAtK(ssn, state, pendingJob, tasksToAllocate, k, jobBudget, registration, generatorBudget)
	if !resultSolved(result) {
		log.InfraLogger.V(5).Infof("No solution found for %d tasks out of %d tasks to allocate for %s",
			k, len(tasksToAllocate), pendingJob.Name)
		return result
	}
	solution := result.solution
	log.InfraLogger.V(5).Infof(
		"Scenario probed for %d tasks out of %d tasks to allocate for %s. Victims: %s",
		k, len(tasksToAllocate), pendingJob.Name, victimPrintingStruct{solution.victimsTasks})
	state.recordedVictimsTasks = solution.victimsTasks
	state.recordedVictimsJobs = solution.victimJobs
	if solution.statement != nil {
		solution.statement.Discard()
	}
	return result
}

func (s *JobSolver) probeAtK(
	ssn *framework.Session,
	state *solvingState,
	pendingJob *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo,
	k int,
	jobBudget *jobSearchBudget,
	registration framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
) *SearchResult {
	pendingTasks := tasksToAllocate[:k]
	partialPendingJob := getPartialJobRepresentative(pendingJob, pendingTasks)
	return s.solvePartialJob(ssn, state, partialPendingJob, jobBudget, registration, generatorBudget, k)
}

func (s *JobSolver) solvePartialJob(
	ssn *framework.Session, state *solvingState, partialPendingJob *podgroup_info.PodGroupInfo,
	jobBudget *jobSearchBudget, registration framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget, probeK int,
) *SearchResult {
	actionBudget := s.ensureActionBudget()
	if jobBudget == nil {
		jobBudget = actionBudget.BeginJob()
	}

	feasibleNodeMap := map[string]*node_info.NodeInfo{}
	for _, node := range s.feasibleNodes {
		feasibleNodeMap[node.Name] = node
	}
	for _, task := range state.recordedVictimsTasks {
		node := ssn.ClusterInfo.Nodes[task.NodeName]
		feasibleNodeMap[task.NodeName] = node
	}

	solveCtx := &SolveContext{
		Session:              ssn,
		ActionType:           s.actionType,
		PartialPendingJob:    partialPendingJob,
		RecordedVictimsJobs:  state.recordedVictimsJobs,
		RecordedVictimsTasks: state.recordedVictimsTasks,
		GenerateVictimsQueue: s.generateVictimsQueue,
		FeasibleNodes:        feasibleNodeMap,
		ProbeK:               probeK,
	}
	portfolio := newSingleGeneratorScenarioPortfolio(solveCtx, jobBudget, registration, generatorBudget)

	for {
		if actionBudget.Exhausted() {
			metrics.IncScenarioSearchActionBudgetExhausted(s.actionType)
			return terminalSearchResult(
				SearchResultDeadlineExhausted, jobBudget.ReducedBudget(), portfolio.enteredSearch,
			)
		}
		if jobBudget.Remaining() <= 0 {
			return terminalSearchResult(
				SearchResultDeadlineExhausted, jobBudget.ReducedBudget(), portfolio.enteredSearch,
			)
		}
		scenarioToSolve := portfolio.Next()
		if actionBudget.Exhausted() {
			metrics.IncScenarioSearchActionBudgetExhausted(s.actionType)
			return terminalSearchResult(
				SearchResultDeadlineExhausted, jobBudget.ReducedBudget(), portfolio.enteredSearch,
			)
		}
		if jobBudget.Remaining() <= 0 {
			return terminalSearchResult(
				SearchResultDeadlineExhausted, jobBudget.ReducedBudget(), portfolio.enteredSearch,
			)
		}
		if scenarioToSolve == nil {
			break
		}
		generatorName := portfolio.CurrentGeneratorName()
		validatorRejected := false
		scenarioSolver := newByPodSolver(feasibleNodeMap, s.solutionValidatorWithMetrics(generatorName, &validatorRejected),
			ssn.AllowConsolidatingReclaim(),
			s.actionType)

		log.InfraLogger.V(5).Infof("Trying to solve scenario: %s", scenarioToSolve)
		metrics.IncScenarioSimulatedByAction()
		metrics.IncScenarioSearchScenario(s.actionType, generatorName, "simulated")

		result := scenarioSolver.solve(ssn, scenarioToSolve)
		attemptResult := scenarioSearchResultUnsolved
		if validatorRejected {
			attemptResult = scenarioSearchResultValidatorRejected
		}
		if result.solved {
			portfolio.ObserveCurrentAttempt(string(SearchResultSolved))
			return solvedSearchResult(result, jobBudget.ReducedBudget())
		}
		portfolio.ObserveCurrentAttempt(attemptResult)
	}

	return terminalSearchResult(portfolio.StopReason(), jobBudget.ReducedBudget(), portfolio.enteredSearch)
}

func (s *JobSolver) solutionValidatorWithMetrics(generator string, rejected *bool) SolutionValidator {
	if s.solutionValidator == nil {
		return nil
	}
	return func(scenario api.ScenarioInfo) bool {
		valid := s.solutionValidator(scenario)
		if !valid {
			if rejected != nil {
				*rejected = true
			}
			metrics.IncScenarioSearchScenario(s.actionType, generator, "validator_rejected")
		}
		return valid
	}
}

func searchResultEntered(result *SearchResult) bool {
	return result != nil && result.EnteredSearch()
}

func preserveEnteredSearch(result *SearchResult, enteredSearch bool) {
	if result != nil && enteredSearch {
		result.enteredSearch = true
	}
}

func shouldStopSearch(result *SearchResult) bool {
	switch result.Reason() {
	case SearchResultDeadlineExhausted, SearchResultNotAttempted, SearchResultNoGenerator:
		return true
	default:
		return false
	}
}

func resultSolved(result *SearchResult) bool {
	return result != nil && result.Reason() == SearchResultSolved &&
		result.solution != nil && result.solution.solved
}

func getPartialJobRepresentative(
	job *podgroup_info.PodGroupInfo, pendingTasks []*pod_info.PodInfo) *podgroup_info.PodGroupInfo {
	representativeTasks := append(job.GetAllAllocatedPods(), pendingTasks...)
	jobRepresentative := job.CloneWithTasks(representativeTasks)

	adjustSubGroupsMinAvailable(jobRepresentative)
	adjustSubGroupsMinSubGroup(jobRepresentative.RootSubGroupSet)

	return jobRepresentative
}

// adjustSubGroupsMinAvailable adjusts the minAvailable of the subGroups of the job representative to the number of tasks in the job representative.
// This is done to ensure that the job representative has the correct minAvailable for each subGroup,
// taking into account that the representative is a PARTIAL clone of the original job.
func adjustSubGroupsMinAvailable(jobRepresentative *podgroup_info.PodGroupInfo) {
	subGroupsPodCount := map[string]int{}
	for _, pendingTask := range jobRepresentative.GetAllPodsMap() {
		if _, found := jobRepresentative.GetAllPodSets()[pendingTask.SubGroupName]; found {
			subGroupsPodCount[pendingTask.SubGroupName] += 1
		} else {
			subGroupsPodCount[podgroup_info.DefaultSubGroup] += 1
		}
	}
	for subGroupName, podCount := range subGroupsPodCount {
		subGroup, found := jobRepresentative.GetAllPodSets()[subGroupName]
		if !found {
			log.InfraLogger.V(2).Warnf("Couldn't find SubGroup with name %s for job %s",
				subGroupName, jobRepresentative.NamespacedName,
			)
			continue
		}
		minAvailable := min(subGroup.GetMinAvailable(), int32(podCount))
		subGroup.SetMinAvailable(minAvailable)
	}
}

// adjustSubGroupsMinSubGroup recursively walks the SubGroupSet tree and sets each node's
// minSubGroup to the number of direct members that have tasks in the partial clone.
// This mirrors the minAvailable adjustment done on PodSets: the clone must only require
// what it actually contains, so that gang-satisfaction checks work correctly on the partial job.
// Returns true if this node contains any tasks.
func adjustSubGroupsMinSubGroup(sgs *subgroup_info.SubGroupSet) bool {
	nonEmptyMembers := int32(0)
	for _, podSet := range sgs.GetDirectPodSets() {
		if len(podSet.GetPodInfos()) > 0 {
			nonEmptyMembers++
		}
	}
	for _, subGroupSet := range sgs.GetDirectSubgroupsSets() {
		if adjustSubGroupsMinSubGroup(subGroupSet) {
			nonEmptyMembers++
		}
	}
	if minSubGroup := sgs.GetMinSubGroup(); minSubGroup != nil {
		minSubGroup := min(*minSubGroup, nonEmptyMembers)
		sgs.SetMinSubGroup(&minSubGroup)
	}
	return nonEmptyMembers > 0
}

func calcVictimNames(victimsTasks []*pod_info.PodInfo) []string {
	var names []string
	for _, victimTask := range victimsTasks {
		names = append(names,
			fmt.Sprintf("<%s/%s>", victimTask.Namespace, victimTask.Name))
	}
	return names
}

type victimPrintingStruct struct {
	victims []*pod_info.PodInfo
}

func (v victimPrintingStruct) String() string {
	if len(v.victims) == 0 {
		return ""
	}
	stringBuilder := strings.Builder{}

	stringBuilder.WriteString(v.victims[0].Namespace)
	stringBuilder.WriteString("/")
	stringBuilder.WriteString(v.victims[0].Name)

	for _, victimTask := range v.victims[1:] {
		stringBuilder.WriteString(", ")
		stringBuilder.WriteString(victimTask.Namespace)
		stringBuilder.WriteString("/")
		stringBuilder.WriteString(victimTask.Name)
	}

	return stringBuilder.String()
}
