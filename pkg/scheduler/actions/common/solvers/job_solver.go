// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
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
) *JobSolver {
	return &JobSolver{
		feasibleNodes:        feasibleNodes,
		solutionValidator:    solutionValidator,
		generateVictimsQueue: generateVictimsQueue,
		actionType:           action,
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
	state := solvingState{}
	originalNumActiveTasks := pendingJob.GetNumActiveUsedTasks()

	tasksToAllocate := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
	n := len(tasksToAllocate)
	if n == 0 {
		return false, nil, calcVictimNames(state.recordedVictimsTasks)
	}

	maxSolvedK := s.searchMaxSolvableK(ssn, &state, pendingJob, tasksToAllocate)
	if maxSolvedK == 0 {
		return false, nil, calcVictimNames(state.recordedVictimsTasks)
	}

	result := s.probeAtK(ssn, &state, pendingJob, tasksToAllocate, n)
	if result == nil || !result.solved {
		return false, nil, calcVictimNames(state.recordedVictimsTasks)
	}

	numActiveTasks := pendingJob.GetNumActiveUsedTasks()
	jobSolved := pendingJob.IsGangSatisfied()
	if originalNumActiveTasks >= numActiveTasks {
		jobSolved = false
	}

	log.InfraLogger.V(4).Infof(
		"Scenario solved for %d tasks to allocate for %s. Victims: %s",
		n, pendingJob.Name, victimPrintingStruct{result.victimsTasks})
	return jobSolved, result.statement, calcVictimNames(result.victimsTasks)
}

// searchMaxSolvableK returns the largest k in [0, n] for which a probe at k succeeds.
// Each probe is discarded before returning, so session state is clean on return.
// Successful probes update hints in state for use by subsequent probes.
// Complexity: O(log n) probes — exponential doubling to locate a failing k (or reach n),
// then binary search between the last success and first failure.
func (s *JobSolver) searchMaxSolvableK(
	ssn *framework.Session,
	state *solvingState,
	pendingJob *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo,
) int {
	n := len(tasksToAllocate)
	if n == 0 {
		return 0
	}

	lo := 0
	var hi int
	k := 1
	for {
		if !s.tryProbeAndDiscard(ssn, state, pendingJob, tasksToAllocate, k) {
			hi = k
			break
		}
		lo = k
		if k == n {
			return n
		}
		k *= 2
		if k > n {
			k = n
		}
	}

	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if s.tryProbeAndDiscard(ssn, state, pendingJob, tasksToAllocate, mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// tryProbeAndDiscard probes at k and always discards the resulting statement so the session
// is left clean. On success, hints are written to state; returns whether the probe succeeded.
func (s *JobSolver) tryProbeAndDiscard(
	ssn *framework.Session,
	state *solvingState,
	pendingJob *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo,
	k int,
) bool {
	result := s.probeAtK(ssn, state, pendingJob, tasksToAllocate, k)
	if result == nil || !result.solved {
		log.InfraLogger.V(5).Infof("No solution found for %d tasks out of %d tasks to allocate for %s",
			k, len(tasksToAllocate), pendingJob.Name)
		return false
	}
	log.InfraLogger.V(5).Infof(
		"Scenario probed for %d tasks out of %d tasks to allocate for %s. Victims: %s",
		k, len(tasksToAllocate), pendingJob.Name, victimPrintingStruct{result.victimsTasks})
	state.recordedVictimsTasks = result.victimsTasks
	state.recordedVictimsJobs = result.victimJobs
	if result.statement != nil {
		result.statement.Discard()
	}
	return true
}

func (s *JobSolver) probeAtK(
	ssn *framework.Session,
	state *solvingState,
	pendingJob *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo,
	k int,
) *solutionResult {
	pendingTasks := tasksToAllocate[:k]
	partialPendingJob := getPartialJobRepresentative(pendingJob, pendingTasks)
	return s.solvePartialJob(ssn, state, partialPendingJob)
}

func (s *JobSolver) solvePartialJob(ssn *framework.Session, state *solvingState, partialPendingJob *podgroup_info.PodGroupInfo) *solutionResult {
	feasibleNodeMap := map[string]*node_info.NodeInfo{}
	for _, node := range s.feasibleNodes {
		feasibleNodeMap[node.Name] = node
	}
	for _, task := range state.recordedVictimsTasks {
		node := ssn.ClusterInfo.Nodes[task.NodeName]
		feasibleNodeMap[task.NodeName] = node
	}

	scenarioBuilder := NewPodAccumulatedScenarioBuilder(
		ssn, partialPendingJob, state.recordedVictimsJobs, s.generateVictimsQueue(), feasibleNodeMap)

	for scenarioToSolve := scenarioBuilder.GetValidScenario(); scenarioToSolve != nil; scenarioToSolve =
		scenarioBuilder.GetNextScenario() {
		scenarioSolver := newByPodSolver(feasibleNodeMap, s.solutionValidator, ssn.AllowConsolidatingReclaim(),
			s.actionType)

		log.InfraLogger.V(5).Infof("Trying to solve scenario: %s", scenarioToSolve)
		metrics.IncScenarioSimulatedByAction()

		result := scenarioSolver.solve(ssn, scenarioToSolve)
		if result.solved {
			return result
		}
	}

	return nil
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
