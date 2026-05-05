// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"strings"

	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/v2"
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
	validator            v2.Validator
	generateVictimsQueue GenerateVictimsQueue
	actionType           framework.ActionType
}

// NewJobsSolver constructs a JobSolver. The validator is action-specific
// policy on top of the simulator's outcome; pass nil to skip validation.
// Actions that still hold legacy func(api.ScenarioInfo) bool validators
// can wrap them with v2.LegacyValidator at the call site.
func NewJobsSolver(
	feasibleNodes []*node_info.NodeInfo,
	validator v2.Validator,
	generateVictimsQueue GenerateVictimsQueue,
	action framework.ActionType,
) *JobSolver {
	return &JobSolver{
		feasibleNodes:        feasibleNodes,
		validator:            validator,
		generateVictimsQueue: generateVictimsQueue,
		actionType:           action,
	}
}

// Solve attempts to allocate every pending task of pendingJob in a single
// shot, evicting tasks from other jobs as needed.
//
// All-or-nothing: the simulator either fits the full gang on top of some
// victim set, or no allocation is produced. There is no per-task probing
// loop — gang-direct simulation removes the iter-1 lock-in where an early
// commitment to a poor victim choice could poison the search space.
//
// On success, returns a live Statement holding the speculative
// allocations and victim evictions; the caller is responsible for Commit
// or Discard. Session state is left unchanged on failure.
func (s *JobSolver) Solve(
	ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo) (bool, *framework.Statement, []string) {
	originalNumActiveTasks := pendingJob.GetNumActiveUsedTasks()

	tasksToAllocate := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
	if len(tasksToAllocate) == 0 {
		return false, nil, nil
	}

	feasibleNodeMap := map[string]*node_info.NodeInfo{}
	for _, node := range s.feasibleNodes {
		feasibleNodeMap[node.Name] = node
	}

	gen := v2.NewAccumulatingGenerator(
		ssn, pendingJob, nil, s.generateVictimsQueue(), feasibleNodeMap,
	)
	sim := newCountingSimulator(v2.NewSessionSimulator(ssn, maps.Values(feasibleNodeMap), s.actionType))

	_, result, ok := v2.Solve(gen, sim, s.validator)
	if !ok {
		return false, nil, nil
	}

	victimTasks := make([]*pod_info.PodInfo, 0, len(result.Preempted)+len(result.Pipelined))
	victimTasks = append(victimTasks, result.Preempted...)
	victimTasks = append(victimTasks, result.Pipelined...)

	jobSolved := pendingJob.IsGangSatisfied()
	if originalNumActiveTasks >= pendingJob.GetNumActiveUsedTasks() {
		jobSolved = false
	}

	log.InfraLogger.V(4).Infof(
		"Scenario solved for %d tasks to allocate for %s. Victims: %s",
		len(tasksToAllocate), pendingJob.Name, victimPrintingStruct{victimTasks})
	return jobSolved, result.Statement, calcVictimNames(victimTasks)
}

// countingSimulator wraps a v2.Simulator to bump the per-scenario
// "simulated" metric and emit the V(5) trace line that the legacy
// solver path produced once per scenario attempt.
type countingSimulator struct {
	inner v2.Simulator
}

func newCountingSimulator(inner v2.Simulator) v2.Simulator {
	return &countingSimulator{inner: inner}
}

func (c *countingSimulator) Simulate(scenario v2.Scenario) v2.SimulationResult {
	log.InfraLogger.V(5).Infof(
		"Trying to solve scenario: pending=%s victims=%s",
		taskNames(scenario.Pending), taskNames(scenario.Victims),
	)
	metrics.IncScenarioSimulatedByAction()
	return c.inner.Simulate(scenario)
}

func taskNames(tasks []*pod_info.PodInfo) string {
	if len(tasks) == 0 {
		return ""
	}
	b := strings.Builder{}
	b.WriteString(tasks[0].Namespace)
	b.WriteString("/")
	b.WriteString(tasks[0].Name)
	for _, t := range tasks[1:] {
		b.WriteString(", ")
		b.WriteString(t.Namespace)
		b.WriteString("/")
		b.WriteString(t.Name)
	}
	return b.String()
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
	return taskNames(v.victims)
}
