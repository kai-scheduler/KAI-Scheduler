// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type SolutionValidator func(scenario api.ScenarioInfo) bool

type simulationVictims struct {
	preemptedVictims []*pod_info.PodInfo
	pipelinedVictims []*pod_info.PodInfo
}

func newCalculatedVictimsStruct() *simulationVictims {
	return &simulationVictims{
		preemptedVictims: make([]*pod_info.PodInfo, 0),
		pipelinedVictims: make([]*pod_info.PodInfo, 0),
	}
}

type solutionResult struct {
	solved       bool
	victimsTasks []*pod_info.PodInfo
	victimJobs   []*podgroup_info.PodGroupInfo
	statement    *framework.Statement
}

type byPodSolver struct {
	feasibleNodes             map[string]*node_info.NodeInfo
	amountOfNewPreemptorTasks int
	solutionValidator         SolutionValidator
	allowVictimConsolidation  bool
	actionType                framework.ActionType
}

func newByPodSolver(
	feasibleNodes map[string]*node_info.NodeInfo,
	amountOfNewPreemptorTasks int,
	checkVictims SolutionValidator,
	allowVictimConsolidation bool,
	action framework.ActionType,
) *byPodSolver {
	return &byPodSolver{
		feasibleNodes:             feasibleNodes,
		amountOfNewPreemptorTasks: amountOfNewPreemptorTasks,
		solutionValidator:         checkVictims,
		allowVictimConsolidation:  allowVictimConsolidation,
		actionType:                action,
	}
}

func (s *byPodSolver) solve(
	session *framework.Session, scenario *scenario.ByNodeScenario,
) *solutionResult {
	statement := session.Statement()
	pendingJob := scenario.GetPreemptor()
	nextTask := scenario.PendingTasks()[len(scenario.PendingTasks())-1]

	if err := common.EvictAllPreemptees(session, scenario.RecordedVictimsTasks(), pendingJob, statement, s.actionType); err != nil {
		return handleSolveError(pendingJob, nextTask, err, statement)
	}

	var result *solutionResult
	var err error

	latestPotentialVictims := scenario.LatestPotentialVictims()
	if latestPotentialVictims == nil {
		if hasRecordedVictimsForSimulation(scenario) {
			log.InfraLogger.V(6).Infof("Trying to solve scenario with previously calculated victims only")
			result = s.runSimulation(session, scenario, statement, scenario.RecordedVictimsTasks())
		}
	} else {
		potentialVictimNodes := getNodesOfJobs(latestPotentialVictims)
		if s.amountOfNewPreemptorTasks <= 1 {
			result, err = s.solveFromSingleNodePreemption(session, scenario, statement, potentialVictimNodes)
		} else {
			result, err = s.solveOnPotentialNodes(session, scenario, statement, potentialVictimNodes)
		}
		if err != nil {
			return handleSolveError(pendingJob, nextTask, err, statement)
		}
	}

	if result != nil {
		return result
	}
	statement.Discard()
	return &solutionResult{false, nil, nil, nil}
}

func (s *byPodSolver) runSimulation(
	session *framework.Session, scenario *scenario.ByNodeScenario, statement *framework.Statement,
	victimTasks []*pod_info.PodInfo) *solutionResult {
	pendingJob := scenario.GetPreemptor()
	nextTaskToFindAllocation := scenario.PendingTasks()[len(scenario.PendingTasks())-1]

	successfulSimulation, solutionVictims, err :=
		s.tryScenarioWithEvictedVictims(session, scenario, statement, victimTasks)

	if err != nil {
		return handleSolveError(pendingJob, nextTaskToFindAllocation, err, statement)
	}
	if successfulSimulation {
		return s.handleScenarioSolution(scenario, statement, solutionVictims)
	}
	return nil
}

// solveFromSingleNodePreemption tries to solve the scenario by preempting the least amount of potential victims possible, while looking for the best cleanup of a single node.
// We can assume that cleaning a single node is enough only if we know that the preemptor job has only one more task then the previous scenario we solved.
// We try to remove potential victims from each node separately, and see if the simulation will succeed.
func (s *byPodSolver) solveFromSingleNodePreemption(ssn *framework.Session, scenario *scenario.ByNodeScenario,
	statement *framework.Statement, potentialVictimNodeNames []string) (*solutionResult, error) {
	for _, node := range potentialVictimNodeNames {
		log.InfraLogger.V(6).Infof("Trying to solve scenario with potential victims from node: %s", node)
		if result, err := s.solveOnPotentialNodes(ssn, scenario, statement, []string{node}); err != nil || result != nil {
			return result, err
		}
	}
	return nil, nil
}

// solveOnPotentialNodes tries to solve the scenario by preempting all the potential victims from the given nodes, and running a simulation
func (s *byPodSolver) solveOnPotentialNodes(ssn *framework.Session, scenario *scenario.ByNodeScenario,
	statement *framework.Statement, potentialVictimNodeNames []string) (*solutionResult, error) {
	checkpoint, potentialVictimsTasks, err := s.evictPotentialVictimsFromNodes(ssn, scenario, statement, potentialVictimNodeNames...)
	if err != nil {
		return nil, err
	}
	newFeasibleNodes := s.updateFeasibleNodes(ssn, potentialVictimsTasks)

	victimTasks := getVictimTasks(scenario.RecordedVictimsTasks(), potentialVictimsTasks)
	if result := s.runSimulation(ssn, scenario, statement, victimTasks); result != nil {
		return result, nil
	}

	s.feasibleNodesRollback(newFeasibleNodes)
	if err = statement.Rollback(*checkpoint); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *byPodSolver) feasibleNodesRollback(newFeasibleNodes map[string]bool) {
	for potentialNodeFeasibleNode := range newFeasibleNodes {
		delete(s.feasibleNodes, potentialNodeFeasibleNode)
	}
}

func (s *byPodSolver) updateFeasibleNodes(ssn *framework.Session, victimTasks []*pod_info.PodInfo) map[string]bool {
	newFeasibleNodes := map[string]bool{}
	for _, potentialVictimTasks := range victimTasks {
		_, found := s.feasibleNodes[potentialVictimTasks.NodeName]
		if !found {
			newFeasibleNodes[potentialVictimTasks.NodeName] = true
		}
		s.feasibleNodes[potentialVictimTasks.NodeName] = ssn.ClusterInfo.Nodes[potentialVictimTasks.NodeName]
	}
	return newFeasibleNodes
}

func (s *byPodSolver) evictPotentialVictimsFromNodes(
	session *framework.Session, scenario *scenario.ByNodeScenario, statement *framework.Statement, nodeToTest ...string,
) (*framework.Checkpoint, []*pod_info.PodInfo, error) {
	recordedVictimsCheckpoint := statement.Checkpoint()
	pendingJob := scenario.GetPreemptor()

	potentialVictimsTasks := scenario.VictimsTasksFromNodes(nodeToTest)
	if err := common.EvictAllPreemptees(session, potentialVictimsTasks, pendingJob, statement, s.actionType); err != nil {
		return nil, nil, err
	}
	return &recordedVictimsCheckpoint, potentialVictimsTasks, nil
}

func (s *byPodSolver) handleScenarioSolution(
	scenario *scenario.ByNodeScenario, statement *framework.Statement, solutionVictims *simulationVictims,
) *solutionResult {
	victimsTasks := make([]*pod_info.PodInfo, len(solutionVictims.preemptedVictims))
	for i := 0; i < len(solutionVictims.preemptedVictims); i++ {
		victimsTasks[i] = solutionVictims.preemptedVictims[i]
	}
	if !s.allowVictimConsolidation {
		victimsTasks = append(victimsTasks, solutionVictims.pipelinedVictims...)
	}
	actualVictimJobs := getVictimJobsFromVictimTasks(victimsTasks, scenario)

	if s.solutionValidator != nil {
		validSolution := s.solutionValidator(scenario)
		if !validSolution {
			statement.Discard()
			return &solutionResult{false, nil, nil, nil}
		}
	}

	if s.allowVictimConsolidation {
		victimsTasks = append(victimsTasks, solutionVictims.pipelinedVictims...)
		actualVictimJobs = getVictimJobsFromVictimTasks(victimsTasks, scenario)
	}

	return &solutionResult{true, victimsTasks, actualVictimJobs, statement}
}

func getNodesOfJobs(pj []*podgroup_info.PodGroupInfo) []string {
	if pj == nil {
		return []string{}
	}

	pjNodeNames := map[string]string{}
	for _, job := range pj {
		for _, latestPotentialVictimTask := range job.GetAllPodsMap() {
			pjNodeNames[latestPotentialVictimTask.NodeName] = latestPotentialVictimTask.NodeName
		}
	}

	return maps.Keys(pjNodeNames)
}

func (s *byPodSolver) tryScenarioWithEvictedVictims(ssn *framework.Session, scenario *scenario.ByNodeScenario,
	statement *framework.Statement, victimTasks []*pod_info.PodInfo) (bool, *simulationVictims, error) {
	pendingJob := scenario.GetPreemptor()

	nodes := maps.Values(s.feasibleNodes)
	jobsToAllocate := common.GetJobsToAllocate(ssn, victimTasks, pendingJob)
	isSuccessfulAllocations, _ :=
		common.TryToVirtuallyAllocatePreemptorAndGetVictims(ssn, statement, nodes, pendingJob,
			jobsToAllocate, victimTasks)

	if !isSuccessfulAllocations {
		return false, nil, nil
	} else {
		actualVictims := newCalculatedVictimsStruct()
		for _, victimTask := range victimTasks {
			if victimTask.Status == pod_status.Releasing {
				actualVictims.preemptedVictims = append(actualVictims.preemptedVictims, victimTask)
			} else if victimTask.Status == pod_status.Pipelined {
				actualVictims.pipelinedVictims = append(actualVictims.pipelinedVictims, victimTask)
			}
		}
		return isSuccessfulAllocations, actualVictims, nil
	}
}

func getVictimJobsFromVictimTasks(
	actualVictimsTasks []*pod_info.PodInfo, scenario *scenario.ByNodeScenario) []*podgroup_info.PodGroupInfo {
	actualVictimJobs := extractJobsFromTasks(actualVictimsTasks, scenario)

	victimJobsAsList := make([]*podgroup_info.PodGroupInfo, 0)
	for _, victimJobsSameJobBase := range actualVictimJobs {
		victimJobsAsList = append(victimJobsAsList, victimJobsSameJobBase...)
	}
	return victimJobsAsList
}

func extractJobsFromTasks(
	tasks []*pod_info.PodInfo, scenario *scenario.ByNodeScenario) map[common_info.PodGroupID][]*podgroup_info.PodGroupInfo {
	jobs := map[common_info.PodGroupID][]*podgroup_info.PodGroupInfo{}
	for _, task := range tasks {
		jobAlreadyExists := false
		if possibleDuplicates, ok := jobs[task.Job]; ok {
			for _, possibleDuplicate := range possibleDuplicates {
				for _, podInfo := range possibleDuplicate.GetAllPodsMap() {
					if podInfo.UID == task.UID {
						jobAlreadyExists = true
						break
					}
				}
				if jobAlreadyExists {
					break
				}
			}
		}
		if !jobAlreadyExists {
			matchingJob := scenario.GetVictimJobRepresentativeById(task)
			jobs[matchingJob.UID] = append(jobs[matchingJob.UID], matchingJob)
		}
	}
	return jobs
}

func getVictimTasks(recordedVictimsTasks []*pod_info.PodInfo, potentialVictimsTasks []*pod_info.PodInfo) []*pod_info.PodInfo {
	victimTasks := make([]*pod_info.PodInfo, len(recordedVictimsTasks)+len(potentialVictimsTasks))
	copy(victimTasks, recordedVictimsTasks)
	copy(victimTasks[len(recordedVictimsTasks):], potentialVictimsTasks)
	return victimTasks
}

func handleSolveError(pendingJob *podgroup_info.PodGroupInfo, nextTaskToFindAllocation *pod_info.PodInfo, err error,
	statement *framework.Statement,
) *solutionResult {
	log.InfraLogger.V(6).Infof("Could not attempt to allocate over victims for pending job <%s/%s> <%v> "+
		"while simulation pod allocation %v due to error: %v",
		pendingJob.Namespace, pendingJob.Name, pendingJob.GetAliveTasksRequestedGPUs(), nextTaskToFindAllocation,
		err)
	statement.Discard()
	return &solutionResult{false, nil, nil, nil}
}

func hasRecordedVictimsForSimulation(scenario *scenario.ByNodeScenario) bool {
	return len(scenario.RecordedVictimsTasks()) > 0
}
