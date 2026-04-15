// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type SolveStats struct {
	JobName            string
	StartTime          time.Time
	TaskIncrements     int
	ScenariosFiltered  int
	ScenariosSimulated int
	FilterDurations    map[string]time.Duration
	FilterRejects      map[string]int
	VictimJobsPopped   int
	NodesTestedTotal   int
	SimulationCalls    int
	SimulationDuration time.Duration
	EvictionCalls      int
	EvictionDuration   time.Duration
	ValidatorCalls     int
	ValidatorDuration  time.Duration
	FeasibleNodesCount int
	VictimQueueSize    int
	Solved             bool
}

func newSolveStats(jobName string, feasibleNodesCount int) *SolveStats {
	return &SolveStats{
		JobName:            jobName,
		StartTime:          time.Now(),
		FeasibleNodesCount: feasibleNodesCount,
		FilterDurations:    make(map[string]time.Duration),
		FilterRejects:      make(map[string]int),
	}
}

func (s *SolveStats) log() {
	duration := time.Since(s.StartTime)
	log.InfraLogger.V(3).Infof(
		"Solve stats for %s: solved=%v dur=%v incr=%d "+
			"scenarios(filt=%d sim=%d) %s "+
			"victimsPopped=%d sim(calls=%d dur=%v) nodes=%d "+
			"evict(calls=%d dur=%v) validator(calls=%d dur=%v) "+
			"input(feasNodes=%d victimQueue=%d)",
		s.JobName, s.Solved, duration, s.TaskIncrements,
		s.ScenariosFiltered, s.ScenariosSimulated, s.filterSummary(),
		s.VictimJobsPopped, s.SimulationCalls, s.SimulationDuration, s.NodesTestedTotal,
		s.EvictionCalls, s.EvictionDuration, s.ValidatorCalls, s.ValidatorDuration,
		s.FeasibleNodesCount, s.VictimQueueSize,
	)
}

func (s *SolveStats) filterSummary() string {
	names := make([]string, 0, len(s.FilterDurations))
	for name := range s.FilterDurations {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s:%v/%drej", name, s.FilterDurations[name], s.FilterRejects[name]))
	}
	if len(parts) == 0 {
		return "filters[]"
	}
	return fmt.Sprintf("filters[%s]", strings.Join(parts, " "))
}
