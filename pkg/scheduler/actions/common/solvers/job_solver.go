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

func (s *JobSolver) Solve(
	ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo) (bool, *framework.Statement, []string) {
	state := solvingState{}
	originalNumActiveTasks := pendingJob.GetNumActiveUsedTasks()

	var statement *framework.Statement
	var pendingTasks []*pod_info.PodInfo
	tasksToAllocate := podgroup_info.GetTasksToAllocate(pendingJob, ssn.PodSetOrderFn, ssn.TaskOrderFn, false)
	for _, nextTaskToSolve := range tasksToAllocate {
		nextTasksToSolve := []*pod_info.PodInfo{nextTaskToSolve}
		pendingTasks = append(pendingTasks, nextTasksToSolve...)
		satisfactorySolution := len(pendingTasks) == len(tasksToAllocate)
		partialPendingJob := getPartialJobRepresentative(pendingJob, pendingTasks)

		result := s.solvePartialJob(ssn, &state, partialPendingJob)
		if result == nil || !result.solved {
			log.InfraLogger.V(5).Infof("No solution found for %d tasks out of %d tasks to allocate for %s",
				len(pendingTasks), len(tasksToAllocate), pendingJob.Name)
			break
		}

		if !satisfactorySolution && result.statement != nil {
			result.statement.Discard()
		}

		statement = result.statement
		state.recordedVictimsTasks = result.victimsTasks
		state.recordedVictimsJobs = result.victimJobs

		log.InfraLogger.V(4).Infof(
			"Scenario solved for %d tasks out of %d tasks to allocate for %s. Victims: %s",
			len(pendingTasks), len(tasksToAllocate), pendingJob.Name,
			victimPrintingStruct{result.victimsTasks})
	}

	numActiveTasks := pendingJob.GetNumActiveUsedTasks()
	jobSolved := pendingJob.IsGangSatisfied()
	if originalNumActiveTasks >= numActiveTasks {
		jobSolved = false
	}

	return jobSolved, statement, calcVictimNames(state.recordedVictimsTasks)
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
	jobRepresentative := job.CloneWithTasks(pendingTasks)
	subGroupsMinAvailable := map[string]int{}
	for _, pendingTask := range pendingTasks {
		if _, found := jobRepresentative.GetSubGroups()[pendingTask.SubGroupName]; found {
			subGroupsMinAvailable[pendingTask.SubGroupName] += 1
		} else {
			subGroupsMinAvailable[podgroup_info.DefaultSubGroup] += 1
		}
	}
	for subGroupName, minAvailable := range subGroupsMinAvailable {
		subGroup, found := jobRepresentative.GetSubGroups()[subGroupName]
		if !found {
			log.InfraLogger.V(2).Warnf("Couldn't find SubGroup with name %s for job %s",
				subGroupName, job.NamespacedName,
			)
			continue
		}
		subGroup.SetMinAvailable(int32(minAvailable))
	}

	return jobRepresentative
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
