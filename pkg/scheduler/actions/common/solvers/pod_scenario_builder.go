// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"golang.org/x/exp/slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/accumulated_scenario_filters"
	idle_gpus_filter "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/accumulated_scenario_filters/idle_gpus"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/accumulated_scenario_filters/node_affinities"
	solverscenario "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

type PodAccumulatedScenarioBuilder struct {
	session         *framework.Session
	scenarioFilters []accumulated_scenario_filters.Interface

	lastScenario     *solverscenario.ByNodeScenario
	victimsJobsQueue *utils.JobsOrderByQueues

	recordedVictimsTasks map[common_info.PodID]*pod_info.PodInfo
}

func NewPodAccumulatedScenarioBuilder(
	session *framework.Session, pendingJob *podgroup_info.PodGroupInfo, recordedVictimsJobs []*podgroup_info.PodGroupInfo,
	victimsJobsQueue *utils.JobsOrderByQueues, feasibleNodes map[string]*node_info.NodeInfo,
) *PodAccumulatedScenarioBuilder {

	var scenario *solverscenario.ByNodeScenario = nil
	recordedVictimsTasks := make(map[common_info.PodID]*pod_info.PodInfo)
	tasksToAllocate := podgroup_info.GetTasksToAllocate(pendingJob, session.SubGroupOrderFn, session.TaskOrderFn, false)
	if len(tasksToAllocate) != 0 {
		scenario = solverscenario.NewByNodeScenario(session, pendingJob, tasksToAllocate, nil, recordedVictimsJobs)
		for _, job := range recordedVictimsJobs {
			for podId, podInfo := range job.GetAllPodsMap() {
				recordedVictimsTasks[podId] = podInfo
			}
		}
	}

	var scenarioFilters []accumulated_scenario_filters.Interface

	// Filter scenario if it has any pods with node affinities that cannot be satisfied by the available nodes for allocation
	nodeSelectorFilter := node_affinities.NewNodeAffinitiesFilter(scenario, feasibleNodes, session)
	if nodeSelectorFilter != nil {
		scenarioFilters = append(scenarioFilters, nodeSelectorFilter)
	}

	// Basic topology-aware gpu capacity filter
	topologyAwareFilter := idle_gpus_filter.NewTopologyAwareIdleGpusFilter(scenario, session.ClusterInfo.Nodes)
	if topologyAwareFilter != nil {
		scenarioFilters = append(scenarioFilters, topologyAwareFilter)
	}

	// Full cluster-level idle GPUs filter
	idleGpusScenarioFilter := idle_gpus_filter.NewIdleGpusFilter(scenario, session.ClusterInfo.Nodes)
	if idleGpusScenarioFilter != nil {
		scenarioFilters = append(scenarioFilters, idleGpusScenarioFilter)
	}

	return &PodAccumulatedScenarioBuilder{
		session:              session,
		victimsJobsQueue:     victimsJobsQueue,
		recordedVictimsTasks: recordedVictimsTasks,
		lastScenario:         scenario,
		scenarioFilters:      scenarioFilters,
	}
}

func (asb *PodAccumulatedScenarioBuilder) GetNextScenario() *solverscenario.ByNodeScenario {
	if asb.victimsJobsQueue.IsEmpty() {
		return nil
	}

	addedPotentialVictims := asb.addNextPotentialVictims()
	if !addedPotentialVictims {
		return asb.GetNextScenario()
	}

	return asb.GetValidScenario()
}

func (asb *PodAccumulatedScenarioBuilder) addNextPotentialVictims() bool {
	nextVictimJob := asb.victimsJobsQueue.PopNextJob()

	potentialVictimTasks, jobHasMoreTasks := podgroup_info.GetTasksToEvict(
		nextVictimJob, asb.session.SubGroupOrderFn, asb.session.TaskOrderFn,
	)

	// Jump over recorded victims in potential victims generation
	for _, potentialVictimTask := range potentialVictimTasks {
		if _, ok := asb.recordedVictimsTasks[potentialVictimTask.UID]; ok {
			// If any of the tasks of the victim job are recorded victims
			// we still want to evaluate the job again if there are tasks
			// that are not recorded victims yet, like elastic jobs
			var remainingTasks []*pod_info.PodInfo
			for _, task := range nextVictimJob.GetAllPodsMap() {
				if _, ok := asb.recordedVictimsTasks[task.UID]; !ok {
					remainingTasks = append(remainingTasks, task)
				}
			}
			if len(remainingTasks) != 0 {
				jobToPush := nextVictimJob.CloneWithTasks(remainingTasks)
				asb.victimsJobsQueue.PushJob(jobToPush)
			}
			return false
		}
	}

	if jobHasMoreTasks {
		var remainingTasks []*pod_info.PodInfo
		for _, task := range nextVictimJob.GetAllPodsMap() {
			if !slices.Contains(potentialVictimTasks, task) {
				remainingTasks = append(remainingTasks, task)
			}
		}

		jobToPush := nextVictimJob.CloneWithTasks(remainingTasks)
		asb.victimsJobsQueue.PushJob(jobToPush)
	}

	if asb.lastScenario != nil {
		asb.lastScenario.AddPotentialVictimsTasks(potentialVictimTasks)
	}
	return true
}

func (asb *PodAccumulatedScenarioBuilder) GetValidScenario() *solverscenario.ByNodeScenario {
	if isValid, failedFilterName := asb.isScenarioValid(); !isValid {
		log.InfraLogger.V(5).Infof("Filtered by %s for scenario: %s", failedFilterName, asb.lastScenario)
		metrics.IncScenarioFilteredByAction()

		return asb.GetNextScenario()
	}
	return asb.lastScenario
}

func (asb *PodAccumulatedScenarioBuilder) isScenarioValid() (bool, string) {
	for _, filter := range asb.scenarioFilters {
		validScenario, err := filter.Filter(asb.lastScenario)
		if err != nil {
			log.InfraLogger.Errorf("Failed to run the filter %s with the error %v. scenario: %s", filter.Name(), err,
				asb.lastScenario)
			// Even if the filter fails, we can still use the scenario - we just might run more simulations the necessary
			continue
		}
		if !validScenario {
			return false, filter.Name()
		}
	}
	return true, ""
}
