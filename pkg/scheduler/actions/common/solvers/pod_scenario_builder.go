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

// victimBatchSnapshot is one entry of the drained victim queue. Each entry
// captures the (job-representative, tasks-to-evict) pair that the old linear
// iteration would have produced from one addNextPotentialVictims pop, in the
// queue's priority order.
type victimBatchSnapshot struct {
	job   *podgroup_info.PodGroupInfo
	tasks []*pod_info.PodInfo
}

type PodAccumulatedScenarioBuilder struct {
	session         *framework.Session
	scenarioFilters []accumulated_scenario_filters.Interface

	pendingJob          *podgroup_info.PodGroupInfo
	tasksToAllocate     []*pod_info.PodInfo
	recordedVictimsJobs []*podgroup_info.PodGroupInfo

	// queueSnapshot is the victim queue drained in priority order at construction.
	// GetNextScenario indexes into this list when advancing the queue prefix.
	queueSnapshot []victimBatchSnapshot

	lastScenario *solverscenario.ByNodeScenario

	// feasibleNodes is the set of nodes the JobSolver gave us as feasible for the
	// preemptor (post FeasibleNodesForJob filter + recorded-victim nodes). The sub-
	// scenario emitter uses it to compute "baseline capacity" already available to
	// the simulation regardless of which potential victims it chooses.
	feasibleNodes map[string]*node_info.NodeInfo

	// Geometric search state. lastProbedSize is -1 before the first emission and
	// otherwise holds the prefix size of the most recently started probe. The next
	// probe's size is computed from it (0 → 1 → 2 → 4 → ... → len(queueSnapshot)).
	lastProbedSize int

	// subEmitter owns the active sub-scenario emission for the current outer state.
	// Each Get*Scenario call drains one sub-scenario from it; when it returns nil,
	// the builder advances to the next geometric prefix size.
	subEmitter *subScenarioEmitter
}

func NewPodAccumulatedScenarioBuilder(
	session *framework.Session, pendingJob *podgroup_info.PodGroupInfo, recordedVictimsJobs []*podgroup_info.PodGroupInfo,
	victimsJobsQueue *utils.JobsOrderByQueues, feasibleNodes map[string]*node_info.NodeInfo,
) *PodAccumulatedScenarioBuilder {

	tasksToAllocate := podgroup_info.GetTasksToAllocate(pendingJob, session.SubGroupOrderFn, session.TaskOrderFn, false)

	recordedVictimsTasks := make(map[common_info.PodID]*pod_info.PodInfo)
	for _, job := range recordedVictimsJobs {
		for podId, podInfo := range job.GetAllPodsMap() {
			recordedVictimsTasks[podId] = podInfo
		}
	}

	queueSnapshot := drainVictimQueue(session, victimsJobsQueue, recordedVictimsTasks)

	asb := &PodAccumulatedScenarioBuilder{
		session:             session,
		pendingJob:          pendingJob,
		tasksToAllocate:     tasksToAllocate,
		recordedVictimsJobs: recordedVictimsJobs,
		queueSnapshot:       queueSnapshot,
		feasibleNodes:       feasibleNodes,
		lastProbedSize:      -1,
	}
	if len(tasksToAllocate) != 0 {
		asb.rewindToSize(0) // initial state — no batches yet
	}
	return asb
}

// GetValidScenario returns the next scenario to solve, evaluating the current outer
// state without advancing the queue prefix. Used to obtain the first scenario in a
// pass.
func (asb *PodAccumulatedScenarioBuilder) GetValidScenario() *solverscenario.ByNodeScenario {
	if asb.lastScenario == nil {
		return nil
	}
	return asb.iterate(false)
}

// GetNextScenario advances to the next geometric prefix size (0, 1, 2, 4, 8, ...)
// before evaluating, returning the next scenario or nil when the schedule is
// exhausted. Used in the body of the caller's iteration loop after consuming each
// scenario.
func (asb *PodAccumulatedScenarioBuilder) GetNextScenario() *solverscenario.ByNodeScenario {
	if asb.lastScenario == nil {
		return nil
	}
	return asb.iterate(true)
}

// iterate is the driver behind GetValidScenario / GetNextScenario.
//
// Each outer "probe" picks a prefix size from the geometric schedule, rewinds
// lastScenario to that prefix, runs the accumulating filters, and (if the prefix
// is non-empty) hands off to a sub-emitter that walks K within the prefix.
//
// Geometric outer iteration converts the worst-case M-probe linear scan into a
// log(M) sequence, which matters for big-reclaimer / full-cluster reclaim attempts
// that exhaust the entire victim queue before failing. The disruption guarantee
// is "smallest power-of-two queue prefix that solves," which is at most 2× the
// smallest-prefix-that-solves the linear scan would have found.
func (asb *PodAccumulatedScenarioBuilder) iterate(advance bool) *solverscenario.ByNodeScenario {
	if asb.subEmitter != nil {
		if sub := asb.subEmitter.next(); sub != nil {
			return sub
		}
		asb.subEmitter = nil
	}

	if asb.lastProbedSize < 0 {
		if !advance {
			// GetValidScenario on a fresh builder: try probe 0 (recorded-only outer).
			// If the filters reject it, fall through to the advance loop so we try
			// progressively larger prefixes, matching the linear-scan behavior.
			if sub := asb.startProbe(0); sub != nil {
				return sub
			}
		} else {
			// GetNextScenario on a fresh builder: skip probe 0 (the caller is
			// asking for *the next* scenario, not the current one).
			asb.lastProbedSize = 0
		}
	}

	for {
		next := asb.nextGeometricSize()
		if next < 0 {
			return nil
		}
		if sub := asb.startProbe(next); sub != nil {
			return sub
		}
		// Probe rejected outright (invalid outer or empty emitter). Advance again.
	}
}

// startProbe rebuilds lastScenario to the given prefix size and either returns the
// recorded-only outer scenario (when there are no potential victims at this size)
// or the first sub-scenario from a fresh sub-emitter. Returns nil when the outer
// state is filtered out or the emitter exhausts immediately, signaling the caller
// to advance to the next probe size.
func (asb *PodAccumulatedScenarioBuilder) startProbe(size int) *solverscenario.ByNodeScenario {
	asb.rewindToSize(size)
	asb.lastProbedSize = size

	if !asb.outerScenarioValid() {
		return nil
	}

	if len(asb.lastScenario.PotentialVictimsTasks()) == 0 {
		return asb.lastScenario
	}

	asb.subEmitter = newSubScenarioEmitter(asb.session, asb.lastScenario, asb.feasibleNodes)
	if sub := asb.subEmitter.next(); sub != nil {
		return sub
	}
	asb.subEmitter = nil
	return nil
}

// nextGeometricSize returns the next prefix size to probe (0, 1, 2, 4, ..., M)
// or -1 when the schedule is exhausted. M is clamped to len(queueSnapshot).
func (asb *PodAccumulatedScenarioBuilder) nextGeometricSize() int {
	maxSize := len(asb.queueSnapshot)
	switch {
	case asb.lastProbedSize < 0:
		return 0
	case asb.lastProbedSize == 0:
		if maxSize == 0 {
			return -1
		}
		return 1
	}
	next := asb.lastProbedSize * 2
	if next > maxSize {
		if asb.lastProbedSize < maxSize {
			return maxSize
		}
		return -1
	}
	return next
}

// rewindToSize reconstructs lastScenario from a fresh ByNodeScenario plus the first
// `size` batches of the queue snapshot, and reseeds the accumulated filters so they
// match the new scenario's state. Filter constructors are cheap relative to a
// simulator probe, and reconstructing avoids any monotone-accumulation invariants
// the filters internally rely on.
func (asb *PodAccumulatedScenarioBuilder) rewindToSize(size int) {
	asb.lastScenario = solverscenario.NewByNodeScenario(
		asb.session, asb.pendingJob, asb.tasksToAllocate, nil, asb.recordedVictimsJobs,
	)
	for i := 0; i < size; i++ {
		asb.lastScenario.AddPotentialVictimsTasks(asb.queueSnapshot[i].tasks)
	}
	asb.scenarioFilters = buildScenarioFilters(asb.session, asb.lastScenario, asb.feasibleNodes)
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

func buildScenarioFilters(
	session *framework.Session, scenario *solverscenario.ByNodeScenario,
	feasibleNodes map[string]*node_info.NodeInfo,
) []accumulated_scenario_filters.Interface {
	var filters []accumulated_scenario_filters.Interface

	if f := node_affinities.NewNodeAffinitiesFilter(scenario, feasibleNodes, session); f != nil {
		filters = append(filters, f)
	}
	if f := idle_gpus_filter.NewTopologyAwareIdleGpusFilter(scenario, session.ClusterInfo.Nodes); f != nil {
		filters = append(filters, f)
	}
	if f := idle_gpus_filter.NewIdleGpusFilter(scenario, session.ClusterInfo.Nodes); f != nil {
		filters = append(filters, f)
	}
	return filters
}

// drainVictimQueue pops the queue in priority order, replicating the per-pop
// behavior of the legacy addNextPotentialVictims path (elastic-job push-back,
// recorded-victim collision skips) and accumulating the resulting (job, tasks)
// pairs as a static snapshot the builder can replay arbitrary prefixes of.
func drainVictimQueue(
	session *framework.Session,
	queue *utils.JobsOrderByQueues,
	recordedVictims map[common_info.PodID]*pod_info.PodInfo,
) []victimBatchSnapshot {
	snapshot := make([]victimBatchSnapshot, 0, queue.Len())
	for !queue.IsEmpty() {
		nextVictimJob := queue.PopNextJob()
		potentialVictimTasks, jobHasMoreTasks := podgroup_info.GetTasksToEvict(
			nextVictimJob, session.SubGroupOrderFn, session.TaskOrderFn,
		)

		// Jump over recorded victims: if any task in the chosen subset is a
		// recorded victim, push back a clone stripped of recorded tasks so the
		// next pop can re-pick from what remains.
		conflictsWithRecorded := false
		for _, task := range potentialVictimTasks {
			if _, ok := recordedVictims[task.UID]; ok {
				conflictsWithRecorded = true
				break
			}
		}
		if conflictsWithRecorded {
			var remaining []*pod_info.PodInfo
			for _, task := range nextVictimJob.GetAllPodsMap() {
				if _, ok := recordedVictims[task.UID]; !ok {
					remaining = append(remaining, task)
				}
			}
			if len(remaining) != 0 {
				queue.PushJob(nextVictimJob.CloneWithTasks(remaining))
			}
			continue
		}

		if jobHasMoreTasks {
			var remaining []*pod_info.PodInfo
			for _, task := range nextVictimJob.GetAllPodsMap() {
				if !slices.Contains(potentialVictimTasks, task) {
					remaining = append(remaining, task)
				}
			}
			queue.PushJob(nextVictimJob.CloneWithTasks(remaining))
		}

		snapshot = append(snapshot, victimBatchSnapshot{
			job:   nextVictimJob,
			tasks: potentialVictimTasks,
		})
	}
	return snapshot
}
