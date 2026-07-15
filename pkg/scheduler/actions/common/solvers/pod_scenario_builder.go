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

	// feasibleNodes is the set of nodes the JobSolver gave us as feasible for the
	// preemptor (post FeasibleNodesForJob filter + recorded-victim nodes). The sub-
	// scenario emitter uses it to compute "baseline capacity" already available to
	// the simulation regardless of which potential victims it chooses.
	feasibleNodes map[string]*node_info.NodeInfo

	// subEmitter, when non-nil, owns the active sub-scenario emission for the current
	// outer state. Each Get*Scenario call drains one sub-scenario from it; when it
	// returns nil, outer accumulation resumes.
	subEmitter *subScenarioEmitter
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
		feasibleNodes:        feasibleNodes,
	}
}

// GetValidScenario returns the next scenario to solve, evaluating the current outer
// state without advancing the victim queue. Used to obtain the first scenario in a
// pass.
func (asb *PodAccumulatedScenarioBuilder) GetValidScenario() *solverscenario.ByNodeScenario {
	return asb.iterate(false)
}

// GetNextScenario advances the victim queue by one before evaluating, returning the
// next scenario or nil when the queue is exhausted. Used in the body of the
// caller's iteration loop after consuming each scenario.
func (asb *PodAccumulatedScenarioBuilder) GetNextScenario() *solverscenario.ByNodeScenario {
	return asb.iterate(true)
}

// GetValidAccumulatedScenario returns the current filter-valid accumulated outer
// scenario without expanding it into sub-scenarios.
func (asb *PodAccumulatedScenarioBuilder) GetValidAccumulatedScenario() *solverscenario.ByNodeScenario {
	return asb.iterateAccumulated(false)
}

// GetNextAccumulatedScenario advances the victim queue and returns the next
// filter-valid accumulated outer scenario without expanding it into sub-scenarios.
func (asb *PodAccumulatedScenarioBuilder) GetNextAccumulatedScenario() *solverscenario.ByNodeScenario {
	return asb.iterateAccumulated(true)
}

// iterate is the unified driver behind GetValidScenario / GetNextScenario.
//
// The pipeline runs as a single loop with three exit points:
//  1. An active sub-emitter yields a sub-scenario — return it.
//  2. The outer scenario is valid and has no potential victims — return it
//     unmodified (the recorded-victims-only case).
//  3. The victim queue is exhausted — return nil.
//
// Otherwise the loop either pops the next victim, runs the accumulating filters
// (skipping rejected states), or constructs a fresh sub-emitter for a newly-valid
// outer state. advanceFirst controls whether the first pass starts by popping a
// victim or by evaluating the current state as-is.
func (asb *PodAccumulatedScenarioBuilder) iterate(advanceFirst bool) *solverscenario.ByNodeScenario {
	if asb.lastScenario == nil {
		return nil
	}
	needAdvance := advanceFirst
	for {
		if sub := asb.nextFromSubEmitter(); sub != nil {
			return sub
		}
		if needAdvance {
			if asb.victimsJobsQueue.IsEmpty() {
				return nil
			}
			if !asb.addNextPotentialVictims() {
				continue
			}
		}
		needAdvance = true

		if !asb.outerScenarioValid() {
			continue
		}
		if len(asb.lastScenario.PotentialVictimsTasks()) == 0 {
			return asb.lastScenario
		}
		asb.subEmitter = newSubScenarioEmitter(asb.session, asb.lastScenario, asb.feasibleNodes)
	}
}

func (asb *PodAccumulatedScenarioBuilder) iterateAccumulated(advanceFirst bool) *solverscenario.ByNodeScenario {
	if asb.lastScenario == nil {
		return nil
	}
	needAdvance := advanceFirst
	for {
		if needAdvance {
			if asb.victimsJobsQueue.IsEmpty() {
				return nil
			}
			if !asb.addNextPotentialVictims() {
				continue
			}
		}
		needAdvance = true

		if !asb.outerScenarioValid() {
			continue
		}
		return asb.lastScenario
	}
}

// nextFromSubEmitter drains the active sub-scenario emitter (if any) by one. Returns
// nil and clears the emitter when it is exhausted, so callers fall through to outer
// accumulation.
func (asb *PodAccumulatedScenarioBuilder) nextFromSubEmitter() *solverscenario.ByNodeScenario {
	if asb.subEmitter == nil {
		return nil
	}
	if sub := asb.subEmitter.next(); sub != nil {
		return sub
	}
	asb.subEmitter = nil
	return nil
}

// outerScenarioValid runs the accumulating filters against the current outer
// scenario, logging and counting the rejection on failure for observability.
func (asb *PodAccumulatedScenarioBuilder) outerScenarioValid() bool {
	isValid, failedFilterName := asb.isScenarioValid()
	if !isValid {
		log.InfraLogger.V(5).Infof("Filtered by %s for scenario: %s", failedFilterName, asb.lastScenario)
		metrics.IncScenarioFilteredByAction()
	}
	return isValid
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
