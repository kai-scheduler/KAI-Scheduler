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

// solve evaluates a scenario's feasibility by simulating it: evicting its victims and simulating the allocation loop.
// This simulates allocation order, node sorting, predicates, and all relevant scheduling logic, so the scheduler will
// not perform evictions that will not result in successful allocation of the pending job. This also helps the scheduler
// avoid evictions which are not relevant to the specific scenario.
func (s *byPodSolver) solve(session *framework.Session, scenario *scenario.ByNodeScenario) *solutionResult {
	statement := session.Statement()

	pendingJob := scenario.GetPreemptor()
	nextTaskToFindAllocation := scenario.PendingTasks()[len(scenario.PendingTasks())-1]

	allVictims := getVictimTasks(scenario.RecordedVictimsTasks(), scenario.PotentialVictimsTasks())

	if len(allVictims) == 0 {
		statement.Discard()
		return &solutionResult{false, nil, nil, nil}
	}

	checkpoint := statement.Checkpoint()
	if err := common.EvictAllPreemptees(session, allVictims, pendingJob, statement, s.actionType); err != nil {
		return handleSolveError(pendingJob, nextTaskToFindAllocation, err, statement)
	}
	newFeasibleNodes := s.updateFeasibleNodes(session, allVictims)

	result := s.runSimulation(session, scenario, statement, allVictims, maps.Values(s.feasibleNodes))
	if result != nil {
		// Roll back on validator-rejected and error results too: the map is
		// shared across the probe's scenarios and must stay derived from the
		// solver's node set plus the recorded victims.
		if !result.solved {
			s.feasibleNodesRollback(newFeasibleNodes)
		}
		return result
	}

	s.feasibleNodesRollback(newFeasibleNodes)
	if err := statement.Rollback(checkpoint); err != nil {
		return handleSolveError(pendingJob, nextTaskToFindAllocation, err, statement)
	}
	statement.Discard()
	return &solutionResult{false, nil, nil, nil}
}

func (s *byPodSolver) runSimulation(
	session *framework.Session, scenario *scenario.ByNodeScenario, statement *framework.Statement,
	victimTasks []*pod_info.PodInfo, nodes []*node_info.NodeInfo) *solutionResult {
	successfulSimulation, solutionVictims :=
		s.tryScenarioWithEvictedVictims(session, scenario, statement, victimTasks, nodes)
	if successfulSimulation {
		return s.handleScenarioSolution(scenario, statement, solutionVictims)
	}
	return nil
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

func (s *byPodSolver) handleScenarioSolution(
	scenario *scenario.ByNodeScenario, statement *framework.Statement, solutionVictims *simulationVictims,
) *solutionResult {
	victimsTasks := make([]*pod_info.PodInfo, len(solutionVictims.preemptedVictims))
	copy(victimsTasks, solutionVictims.preemptedVictims)
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

func (s *byPodSolver) tryScenarioWithEvictedVictims(ssn *framework.Session, scenario *scenario.ByNodeScenario,
	statement *framework.Statement, victimTasks []*pod_info.PodInfo, nodes []*node_info.NodeInfo) (bool, *simulationVictims) {
	pendingJob := scenario.GetPreemptor()

	jobsToAllocate := common.GetJobsToAllocate(ssn, victimTasks, pendingJob)
	isSuccessfulAllocations, _ :=
		common.TryToVirtuallyAllocatePreemptorAndGetVictims(ssn, statement, nodes, pendingJob,
			jobsToAllocate, victimTasks)

	if !isSuccessfulAllocations {
		return false, nil
	}
	actualVictims := newCalculatedVictimsStruct()
	for _, victimTask := range victimTasks {
		switch victimTask.Status {
		case pod_status.Releasing:
			actualVictims.preemptedVictims = append(actualVictims.preemptedVictims, victimTask)
		case pod_status.Pipelined:
			actualVictims.pipelinedVictims = append(actualVictims.pipelinedVictims, victimTask)
		}
	}
	return isSuccessfulAllocations, actualVictims
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

func nodeIdleOrReleasingGpus(ni *node_info.NodeInfo) float64 {
	idle, _ := ni.GetSumOfIdleGPUs()
	releasing, _ := ni.GetSumOfReleasingGPUs()
	return idle + releasing
}
