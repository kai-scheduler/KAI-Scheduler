// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
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
	feasibleNodes            map[string]*node_info.NodeInfo
	solutionValidator        SolutionValidator
	allowVictimConsolidation bool
	actionType               framework.ActionType
}

func newByPodSolver(
	feasibleNodes map[string]*node_info.NodeInfo,
	checkVictims SolutionValidator,
	allowVictimConsolidation bool,
	action framework.ActionType,
) *byPodSolver {
	return &byPodSolver{
		feasibleNodes:            feasibleNodes,
		solutionValidator:        checkVictims,
		allowVictimConsolidation: allowVictimConsolidation,
		actionType:               action,
	}
}

func (s *byPodSolver) solve(
	session *framework.Session, scenario *scenario.ByNodeScenario,
) *solutionResult {
	statement := session.Statement()

	pendingJob := scenario.GetPreemptor()
	nextTaskToFindAllocation := scenario.PendingTasks()[len(scenario.PendingTasks())-1]
	latestPotentialVictim := scenario.LatestPotentialVictim()

	err := common.EvictAllPreemptees(session, scenario.RecordedVictimsTasks(), pendingJob, statement, s.actionType)
	if err != nil {
		return handleSolveError(pendingJob, nextTaskToFindAllocation, err, statement)
	}

	if latestPotentialVictim == nil {
		if hasRecordedVictimsForSimulation(scenario) {
			log.InfraLogger.V(6).Infof("Trying to solve scenario with priviously calculated victims only")
			result := s.runSimulation(session, scenario, statement, scenario.RecordedVictimsTasks())
			if result != nil {
				return result
			}
		}
	} else {
		potentialVictimNodes := getNodesOfJob(latestPotentialVictim)
		result, err := s.solveOnPotentialNodes(session, scenario, statement, potentialVictimNodes)
		if err != nil {
			return handleSolveError(pendingJob, nextTaskToFindAllocation, err, statement)
		}
		if result != nil {
			return result
		}
	}

	statement.Discard() // No solution for scenario
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

func (s *byPodSolver) solveOnPotentialNodes(ssn *framework.Session, scenario *scenario.ByNodeScenario,
	statement *framework.Statement, potentialVictimNodeNames []string) (*solutionResult, error) {
	for _, nodeToTest := range potentialVictimNodeNames {
		log.InfraLogger.V(6).Infof(
			"Trying to solve scenario with potantial victims from node: %s", nodeToTest)

		nodeVictimsEvictionCheckpoint, potentialVictimsTasks, err :=
			s.evictPotentialVictimsFromNode(ssn, scenario, statement, nodeToTest)
		if err != nil {
			return nil, err
		}
		newFeasibleNodes := s.updateFeasibleNodes(ssn, potentialVictimsTasks)

		victimTasks := getVictimTasks(scenario.RecordedVictimsTasks(), potentialVictimsTasks)
		result := s.runSimulation(ssn, scenario, statement, victimTasks)
		if result != nil {
			return result, nil
		}

		s.feasibleNodesRollback(newFeasibleNodes)
		if err = statement.Rollback(*nodeVictimsEvictionCheckpoint); err != nil {
			return nil, err
		}
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

func (s *byPodSolver) evictPotentialVictimsFromNode(
	session *framework.Session, scenario *scenario.ByNodeScenario, statement *framework.Statement, nodeToTest string,
) (*framework.Checkpoint, []*pod_info.PodInfo, error) {
	recordedVictimsCheckpoint := statement.Checkpoint()
	pendingJob := scenario.GetPreemptor()

	potentialVictimsTasks := scenario.VictimsTasksFromNodes([]string{nodeToTest})
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

func getNodesOfJob(pj *podgroup_info.PodGroupInfo) []string {
	if pj == nil {
		return []string{}
	}

	pjNodeNames := map[string]string{}
	for _, latestPotentialVictimTask := range pj.GetAllPodsMap() {
		pjNodeNames[latestPotentialVictimTask.NodeName] = latestPotentialVictimTask.NodeName
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
