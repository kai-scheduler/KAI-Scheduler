// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"cmp"
	"fmt"
	"sort"

	"golang.org/x/exp/slices"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
)

const (
	idleGpuFilterName               = "AccumulatedIdleGpus"
	nonAccumulatedScenarioBaseError = "accumulatedIdleGpus requires all the filters scenarios using the same instance to be based on the same scenario with accumulation of potential victims. "
	requiredResourcesDiffError      = "The pending task %s didn't appear in the scenario given at the AccumulatedIdleGpus ctor"
	recordedVictimsDiffError        = "The recorded victims should remain the same between the different scenario filtering. %d cache hits, pre update recorded tasks seen %d, post update recorded tasks seen %d"
	potentialVictimsDiffError       = "The list of potential victims for the current scenario should contain the previous list of potential victims. Only %d out %d tasks are the contained in the current scenario"
)

type AccumulatedIdleGpus struct {
	// requiredGpusSorted contains the amount of gpus required by scenario pending task,
	// sorted from the largest to the smallest.
	requiredGpusSorted []float64

	nodesNameToIdleGpus map[string]float64
	// maxFreeGpuNodesSorted is a slice of nodeNames with a requiredGPUs number of nodes with the most idle gpus
	maxFreeGpuNodesSorted []string

	// pendingTasksInState holds a reference for all scenario pending tasks given by the scenarios to the
	// NewIdleGpusFilter or Filter function.
	// This value is used for assertions run on the scenarios given in the Filter function.
	pendingTasksInState map[common_info.PodID]bool
	// recordedVictimsInCache holds a reference for all recorded victim tasks given by the scenarios to the
	// NewIdleGpusFilter or Filter function.
	// This helps us to calculate "gpus freed by victim task eviction" only once for each task,
	// even if multiple scenarios reference to a single task as a victim.
	recordedVictimsInCache map[common_info.PodID]bool
	// potentialVictimsInCache holds a reference for all potential victim tasks given by the scenarios to the
	// NewIdleGpusFilter or Filter function.
	// This helps us to calculate "gpus freed by victim task eviction" only once for each task,
	// even if multiple scenarios reference to a single task as a victim.
	potentialVictimsInCache map[common_info.PodID]bool

	// We have a separate map for each type of task to help with scenario accumulated build assertions.
}

type matchingState struct {
	nodesToVirtuallyAllocatedGpus map[string]float64
}

func NewIdleGpusFilter(
	scenario *scenario.ByNodeScenario, nodeInfosMap map[string]*node_info.NodeInfo) *AccumulatedIdleGpus {
	idleGpusMap, relevantNodesSorted := createGpuMap(nodeInfosMap, len(scenario.PendingTasks()))

	filter := &AccumulatedIdleGpus{
		nodesNameToIdleGpus:     idleGpusMap,
		maxFreeGpuNodesSorted:   relevantNodesSorted,
		pendingTasksInState:     make(map[common_info.PodID]bool),
		recordedVictimsInCache:  make(map[common_info.PodID]bool),
		potentialVictimsInCache: make(map[common_info.PodID]bool),
	}
	err := filter.updateStateWithScenario(scenario, true)
	if err != nil {
		return nil
	}

	return filter
}

func (ig *AccumulatedIdleGpus) Name() string {
	return idleGpuFilterName
}

func (ig *AccumulatedIdleGpus) Filter(scenario *scenario.ByNodeScenario) (bool, error) {
	err := ig.updateStateWithScenario(scenario, false)
	if err != nil {
		return false, err
	}

	numOfRelevantNodes := len(ig.maxFreeGpuNodesSorted)
	matchState := matchingState{
		nodesToVirtuallyAllocatedGpus: make(map[string]float64, numOfRelevantNodes),
	}

	for _, currentRequiredGpus := range ig.requiredGpusSorted {
		if currentRequiredGpus == 0 {
			continue
		}

		taskAllocatable := ig.matchRelevantNodeToTask(currentRequiredGpus, matchState)
		if !taskAllocatable {
			return false, nil
		}
	}

	return true, nil
}

func (ig *AccumulatedIdleGpus) updateStateWithScenario(scenario *scenario.ByNodeScenario, isFirstScenario bool) error {
	err := ig.updateRequiredResources(scenario, isFirstScenario)
	if err != nil {
		return err
	}

	preUpdateRecordedVictimsCacheSize := len(ig.recordedVictimsInCache)
	recordedVictimsCacheHits := ig.updateVictimList(scenario.RecordedVictimsTasks(), ig.recordedVictimsInCache)
	if err = ig.assertRecordedVictimsState(isFirstScenario, len(ig.recordedVictimsInCache), preUpdateRecordedVictimsCacheSize, recordedVictimsCacheHits); err != nil {
		return err
	}

	preUpdatePotentialVictimsCacheSize := len(ig.potentialVictimsInCache)
	potentialVictimsCacheHits := ig.updateVictimList(scenario.PotentialVictimsTasks(), ig.potentialVictimsInCache)
	if err = ig.assertPotentialVictimsState(potentialVictimsCacheHits, preUpdatePotentialVictimsCacheSize); err != nil {
		return err
	}
	return nil
}

func (ig *AccumulatedIdleGpus) updateVictimList(
	victimTasks []*pod_info.PodInfo, relevantCacheData map[common_info.PodID]bool,
) int {
	numOfCacheHits := 0

	var minIdleGpusRelevant string
	if len(ig.maxFreeGpuNodesSorted) == 0 {
		minIdleGpusRelevant = ""
	} else {
		minIdleGpusRelevant = ig.maxFreeGpuNodesSorted[len(ig.maxFreeGpuNodesSorted)-1]
	}

	for _, task := range victimTasks {
		if task.NodeName == "" {
			continue
		}
		if relevantCacheData[task.UID] {
			numOfCacheHits += 1
			continue
		}
		minIdleGpusRelevant = ig.updateWithVictim(task, minIdleGpusRelevant, relevantCacheData)
	}
	return numOfCacheHits
}

func (ig *AccumulatedIdleGpus) assertRecordedVictimsState(
	isFirstScenario bool, recordedVictimsCacheSize int,
	preUpdatePotentialVictimsCacheSize int, recordedVictimsCacheHits int,
) error {
	if isFirstScenario ||
		(preUpdatePotentialVictimsCacheSize == recordedVictimsCacheHits &&
			recordedVictimsCacheHits == recordedVictimsCacheSize) {
		return nil
	}
	return fmt.Errorf(nonAccumulatedScenarioBaseError+recordedVictimsDiffError, recordedVictimsCacheHits,
		preUpdatePotentialVictimsCacheSize, recordedVictimsCacheSize)
}

func (ig *AccumulatedIdleGpus) assertPotentialVictimsState(
	potentialVictimsCacheHits int, preUpdatePotentialVictimsCacheSize int) error {
	if preUpdatePotentialVictimsCacheSize == potentialVictimsCacheHits {
		return nil
	}
	return fmt.Errorf(nonAccumulatedScenarioBaseError+potentialVictimsDiffError, potentialVictimsCacheHits,
		preUpdatePotentialVictimsCacheSize)
}

func (ig *AccumulatedIdleGpus) updateRequiredResources(scenario *scenario.ByNodeScenario, firstScenarioSet bool) error {
	if !firstScenarioSet {
		if err := ig.assertRequiredResourcesInCache(scenario); err != nil {
			return err
		}
	}

	var requiredResources []float64
	for _, pod := range scenario.PendingTasks() {
		requiredResources = append(requiredResources, pod.ResReq.GPUs())
		ig.pendingTasksInState[pod.UID] = true
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(requiredResources)))
	ig.requiredGpusSorted = requiredResources
	return nil
}

func (ig *AccumulatedIdleGpus) assertRequiredResourcesInCache(scenario *scenario.ByNodeScenario) error {
	for _, pod := range scenario.PendingTasks() {
		if !ig.pendingTasksInState[pod.UID] {
			return fmt.Errorf(nonAccumulatedScenarioBaseError+requiredResourcesDiffError, pod.UID)
		}
	}
	return nil
}

func (ig *AccumulatedIdleGpus) updateWithVictim(
	task *pod_info.PodInfo, minIdleGpusRelevant string,
	relevantCacheData map[common_info.PodID]bool,
) string {
	relevantCacheData[task.UID] = true
	if len(ig.nodesNameToIdleGpus) == 0 {
		return ""
	}

	prevMinRelevantValue := ig.nodesNameToIdleGpus[minIdleGpusRelevant]
	ig.nodesNameToIdleGpus[task.NodeName] += task.AcceptedResource.GPUs()

	if ig.nodesNameToIdleGpus[task.NodeName] > prevMinRelevantValue {
		ig.maxFreeGpuNodesSorted = orderedInsert(ig.maxFreeGpuNodesSorted, task.NodeName,
			true, func(i, j string) int {
				return cmp.Compare(ig.nodesNameToIdleGpus[j], ig.nodesNameToIdleGpus[i])
			})
		minIdleGpusRelevant = ig.maxFreeGpuNodesSorted[len(ig.maxFreeGpuNodesSorted)-1]
	}
	return minIdleGpusRelevant
}

// matchRelevantNodeToTask tries to find a node in which there are enough free gpus to accommodate the pending task's gpus.
func (ig *AccumulatedIdleGpus) matchRelevantNodeToTask(pendingTaskGpus float64, filterMatchState matchingState) bool {
	// Try to allocate this task's gpus only on the N most "gpu free" nodes. N = len(scenario.pendingTasks)
	//  If we cannot find "allocation" in these N nodes, we won't find any allocation for this task in the current scenario.
	for _, currentNode := range ig.maxFreeGpuNodesSorted {
		// The nodes and pendingResources are sorted by the number of free gpus.
		// If the currentNode doesn't have enough free gpus for the current task, no other node has enough free gpus.
		nodeIdleGpus := ig.nodesNameToIdleGpus[currentNode]
		if nodeIdleGpus < pendingTaskGpus {
			return false
		}

		// If this isn't the first pendingTask, then we need to take into account previously "matched" scenario pending tasks.
		//  filterMatchState saves the amount of previously matched gpus per node.
		//  <Currently amount of free gpus per node> = ig.nodesNameToIdleGpus[currentNode] - <matched gpus per job>
		previouslyVirtuallyAllocatedGpus := filterMatchState.nodesToVirtuallyAllocatedGpus[currentNode]
		if nodeIdleGpus-previouslyVirtuallyAllocatedGpus < pendingTaskGpus {
			continue
		} else {
			filterMatchState.nodesToVirtuallyAllocatedGpus[currentNode] += pendingTaskGpus
			return true
		}
	}
	return false
}

func createGpuMap(nodeInfosMap map[string]*node_info.NodeInfo, relevantNodesLen int) (map[string]float64, []string) {
	idleGpusMap := map[string]float64{}
	relevantNodesSorted := make([]string, 0)
	minRelevantValue := float64(-1)

	for _, nodeInfo := range nodeInfosMap {
		nodeIdleGpus, _ := nodeInfo.GetSumOfIdleGPUs()
		nodeReleasingGpus, _ := nodeInfo.GetSumOfReleasingGPUs()
		idleGpusMap[nodeInfo.Name] = nodeIdleGpus + nodeReleasingGpus

		if idleGpusMap[nodeInfo.Name] > minRelevantValue || len(relevantNodesSorted) < relevantNodesLen {
			replace := relevantNodesLen <= len(relevantNodesSorted)
			relevantNodesSorted = orderedInsert(relevantNodesSorted, nodeInfo.Name, replace,
				func(i, j string) int {
					return cmp.Compare(idleGpusMap[j], idleGpusMap[i])
				})
			minRelevantValue = idleGpusMap[relevantNodesSorted[len(relevantNodesSorted)-1]]
		}
	}

	return idleGpusMap, relevantNodesSorted
}

func orderedInsert[T cmp.Ordered](ts []T, t T, replace bool, cmp func(T, T) int) []T {
	i, _ := slices.BinarySearchFunc(ts, t, cmp) // find slot

	if updatedTS, found := updateLocationIfTAlreadyExists(ts, t, i); found {
		return updatedTS
	}

	if replace {
		return insertWithoutIncreasingListSize(ts, t, i)
	} else {
		return slices.Insert(ts, i, t)
	}
}

func insertWithoutIncreasingListSize[T cmp.Ordered](ts []T, t T, i int) []T {
	if i == len(ts)-1 {
		ts[i] = t
		return ts
	}
	for j := len(ts) - 1; j > i; j-- {
		ts[j] = ts[j-1]
	}
	ts[i] = t
	return ts
}

func updateLocationIfTAlreadyExists[T cmp.Ordered](ts []T, t T, i int) ([]T, bool) {
	for j := range len(ts) {
		if ts[j] == t {
			if j == i {
				return ts, true
			}
			for k := j; k > i; k-- {
				ts[k] = ts[k-1]
			}
			ts[i] = t
			return ts, true
		}
	}
	return nil, false
}
