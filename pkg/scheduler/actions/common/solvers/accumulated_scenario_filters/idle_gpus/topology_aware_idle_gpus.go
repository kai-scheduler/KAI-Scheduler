// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"sort"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
)

const (
	topologyAwareIdleGpuFilterName = "TopologyAwareIdleGpus"
)

type TopologyDomainKey struct {
	topologyName string
	level        string // Level within the topology (e.g., "rack", "zone")
	value        string // Specific domain value (e.g., "rack-1", "zone-a")
}

// constraintGroupKey identifies a unique (topology, level) pair used to group subgroups and domains.
type constraintGroupKey struct {
	topology string
	level    string
}

// TopologyAwareIdleGpus validates scenarios considering topology constraints
type TopologyAwareIdleGpus struct {
	domainCapacity        map[TopologyDomainKey]float64
	subgroupsWithRequired []*subgroup_info.SubGroupSet
	nodeToTopologyDomains map[string][]TopologyDomainKey
	// domainsByConstraint holds domain keys grouped by (topology, level), sorted descending by
	// idle GPU capacity. The sort order is maintained incrementally as victims are added.
	domainsByConstraint map[constraintGroupKey][]TopologyDomainKey
	// processedVictims tracks victim UIDs that have already had their freed GPUs applied to
	// domainCapacity, preventing double-counting when the same victim appears across calls.
	processedVictims map[common_info.PodID]bool
}

func NewTopologyAwareIdleGpusFilter(
	scenario *scenario.ByNodeScenario,
	nodeInfosMap map[string]*node_info.NodeInfo,
) *TopologyAwareIdleGpus {
	subgroupsWithRequired := extractRequiredTopologyConstraints(scenario)
	if len(subgroupsWithRequired) == 0 {
		return nil
	}

	domainCapacity, nodeToTopologyDomains, domainsByConstraint := buildDomainCapacity(nodeInfosMap, subgroupsWithRequired)

	return &TopologyAwareIdleGpus{
		domainCapacity:        domainCapacity,
		subgroupsWithRequired: subgroupsWithRequired,
		nodeToTopologyDomains: nodeToTopologyDomains,
		domainsByConstraint:   domainsByConstraint,
		processedVictims:      make(map[common_info.PodID]bool),
	}
}

func (taf *TopologyAwareIdleGpus) Name() string {
	return topologyAwareIdleGpuFilterName
}

func (taf *TopologyAwareIdleGpus) Filter(scenario *scenario.ByNodeScenario) (bool, error) {
	taf.updateDomainCapacityWithVictims(scenario)
	return taf.requiredTopologyCapacityExists(), nil
}

// updateDomainCapacityWithVictims updates domain capacities when new victims are added.
// After each capacity increase it repositions the domain in the pre-sorted slice so the
// slice stays ordered without a full re-sort.
func (taf *TopologyAwareIdleGpus) updateDomainCapacityWithVictims(scenario *scenario.ByNodeScenario) {
	taf.applyVictimTasks(scenario.RecordedVictimsTasks())
	taf.applyVictimTasks(scenario.PotentialVictimsTasks())
}

func (taf *TopologyAwareIdleGpus) applyVictimTasks(victimTasks []*pod_info.PodInfo) {
	iterateNewVictims(victimTasks, taf.processedVictims, func(victimTask *pod_info.PodInfo) {
		freedGpus := victimTask.AcceptedResource.GPUs() + float64(victimTask.AcceptedResource.GetDraGpusCount())
		if domains, ok := taf.nodeToTopologyDomains[victimTask.NodeName]; ok {
			for _, domainKey := range domains {
				taf.domainCapacity[domainKey] += freedGpus
				cgKey := constraintGroupKey{topology: domainKey.topologyName, level: domainKey.level}
				repositionDomainAfterIncrease(taf.domainsByConstraint[cgKey], domainKey, taf.domainCapacity)
			}
		}
	})
}

// repositionDomainAfterIncrease moves domain leftward within the descending-sorted slice to
// restore sorted order after its capacity increased. The underlying array is modified in place;
// no allocation is needed.
func repositionDomainAfterIncrease(
	domains []TopologyDomainKey,
	domain TopologyDomainKey,
	capacity map[TopologyDomainKey]float64,
) {
	newCapacity := capacity[domain]

	// Find current position.
	currentPos := -1
	for i, d := range domains {
		if d == domain {
			currentPos = i
			break
		}
	}
	if currentPos <= 0 {
		return // Already at the front or not found.
	}

	// Capacity increased, so the domain can only move left (toward index 0).
	// Walk left until the previous domain has at least as much capacity.
	newPos := currentPos
	for newPos > 0 && capacity[domains[newPos-1]] < newCapacity {
		newPos--
	}
	if newPos == currentPos {
		return
	}

	// Shift elements [newPos..currentPos-1] one step to the right, then place domain at newPos.
	shiftElementLeft(domains, currentPos, newPos)
}

// requiredTopologyCapacityExists groups subgroups by constraint, sorts subgroup requirements
// descending, then greedily matches each group against the pre-sorted domain list using virtual
// allocation. The domain list is used directly (no copy, no re-sort).
func (taf *TopologyAwareIdleGpus) requiredTopologyCapacityExists() bool {
	gpusByConstraint := taf.groupSubgroupGpusByConstraint()

	for key, gpuRequirements := range gpusByConstraint {
		sort.Slice(gpuRequirements, func(i, j int) bool {
			return gpuRequirements[i] > gpuRequirements[j]
		})

		if !taf.matchSubgroupsToDomains(gpuRequirements, taf.domainsByConstraint[key]) {
			return false
		}
	}
	return true
}

func (taf *TopologyAwareIdleGpus) groupSubgroupGpusByConstraint() map[constraintGroupKey][]float64 {
	gpusByConstraint := make(map[constraintGroupKey][]float64)
	for _, subgroup := range taf.subgroupsWithRequired {
		constraint := subgroup.GetTopologyConstraint()
		if constraint == nil || constraint.RequiredLevel == "" {
			continue
		}
		key := constraintGroupKey{topology: constraint.Topology, level: constraint.RequiredLevel}
		gpusByConstraint[key] = append(gpusByConstraint[key], sumGpuRequirements(subgroup))
	}
	return gpusByConstraint
}

// matchSubgroupsToDomains greedily assigns each subgroup (sorted largest-first) to the domain
// with the most available idle GPUs, using virtual allocation to prevent double-counting.
// Domains must be pre-sorted by total idle capacity descending to enable early termination.
func (taf *TopologyAwareIdleGpus) matchSubgroupsToDomains(
	gpuRequirements []float64, domains []TopologyDomainKey,
) bool {
	return greedyMatchRequirements(gpuRequirements, domains, func(domain TopologyDomainKey) float64 {
		return taf.domainCapacity[domain]
	})
}

func extractRequiredTopologyConstraints(scenario *scenario.ByNodeScenario) []*subgroup_info.SubGroupSet {
	if scenario == nil {
		return nil
	}
	pendingJob := scenario.GetPreemptor()
	if pendingJob == nil || pendingJob.RootSubGroupSet == nil {
		return nil
	}
	return getSubgroupsWithRequiredConstraints(pendingJob.RootSubGroupSet, nil)
}

func getSubgroupsWithRequiredConstraints(
	jobSubGroup *subgroup_info.SubGroupSet,
	out []*subgroup_info.SubGroupSet,
) []*subgroup_info.SubGroupSet {
	if jobSubGroup == nil {
		return out
	}
	if jobSubGroup.GetTopologyConstraint() != nil && len(jobSubGroup.GetTopologyConstraint().RequiredLevel) > 0 {
		out = append(out, jobSubGroup)
	}
	for _, childGroup := range jobSubGroup.GetChildGroups() {
		out = getSubgroupsWithRequiredConstraints(childGroup, out)
	}
	return out
}

func buildDomainCapacity(
	nodeInfosMap map[string]*node_info.NodeInfo,
	subgroupsWithRequired []*subgroup_info.SubGroupSet,
) (map[TopologyDomainKey]float64, map[string][]TopologyDomainKey, map[constraintGroupKey][]TopologyDomainKey) {
	domainCapacity := make(map[TopologyDomainKey]float64)
	nodeToTopologyDomains := make(map[string][]TopologyDomainKey)

	// Deduplicate constraint keys across subgroups so each node's GPUs are counted once per domain.
	constraintKeys := make(map[constraintGroupKey]bool)
	for _, subgroup := range subgroupsWithRequired {
		constraint := subgroup.GetTopologyConstraint()
		if constraint != nil && len(constraint.RequiredLevel) > 0 {
			constraintKeys[constraintGroupKey{topology: constraint.Topology, level: constraint.RequiredLevel}] = true
		}
	}

	for _, nodeInfo := range nodeInfosMap {
		totalIdleGpus := nodeIdleOrReleasingGpuCapacity(nodeInfo)

		for cgKey := range constraintKeys {
			domainValue := nodeInfo.Node.Labels[cgKey.level]
			if len(domainValue) == 0 {
				continue
			}
			key := TopologyDomainKey{
				topologyName: cgKey.topology,
				level:        cgKey.level,
				value:        domainValue,
			}
			domainCapacity[key] += totalIdleGpus
			nodeToTopologyDomains[nodeInfo.Name] = append(nodeToTopologyDomains[nodeInfo.Name], key)
		}
	}

	// Build domainsByConstraint from the registered domain keys, then sort each list by
	// capacity descending so it is ready for greedy matching without any per-call sort.
	domainsByConstraint := make(map[constraintGroupKey][]TopologyDomainKey)
	for key := range domainCapacity {
		cgKey := constraintGroupKey{topology: key.topologyName, level: key.level}
		domainsByConstraint[cgKey] = append(domainsByConstraint[cgKey], key)
	}
	for _, domains := range domainsByConstraint {
		sort.Slice(domains, func(i, j int) bool {
			return domainCapacity[domains[i]] > domainCapacity[domains[j]]
		})
	}

	return domainCapacity, nodeToTopologyDomains, domainsByConstraint
}

func sumGpuRequirements(subgroup *subgroup_info.SubGroupSet) float64 {
	podSets := subgroup.GetAllPodSets()
	totalGpusNeeded := 0.0
	for _, podSet := range podSets {
		for _, task := range podSet.GetPodInfos() {
			if task.ResReq != nil {
				totalGpusNeeded += task.ResReq.GPUs() + float64(task.ResReq.GetDraGpusCount())
			}
		}
	}
	return totalGpusNeeded
}
