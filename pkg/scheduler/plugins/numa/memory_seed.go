// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"encoding/json"
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	commonpod "github.com/kai-scheduler/KAI-scheduler/pkg/common/pod"
	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func (pp *numaPlugin) managedMemoryResources(node *node_info.NodeInfo) sets.Set[v1.ResourceName] {
	resources := sets.New[v1.ResourceName]()
	for name := range node.NumaTopology.Resources {
		if !commonresources.IsMemoryResource(name) {
			continue
		}
		if pp.ignoreList.Has(name) && !pp.observedMemoryByNode[node.Name].Has(name) {
			continue
		}
		resources.Insert(name)
	}
	return resources
}

func requestsMemory(pod *v1.Pod, resources sets.Set[v1.ResourceName]) bool {
	if !commonpod.IsGuaranteed(pod) {
		return false
	}
	for _, container := range longRunningContainers(pod) {
		for name, amount := range container.Resources.Requests {
			if resources.Has(name) && amount.Sign() > 0 {
				return true
			}
		}
	}
	return false
}

func memoryOwner(task *pod_info.PodInfo) types.UID {
	if task.Pod != nil && task.Pod.UID != "" {
		return task.Pod.UID
	}
	return types.UID(task.UID)
}

type memoryGroupSource int

const (
	memoryGroupsUnknown memoryGroupSource = iota
	memoryGroupsPredicted
	memoryGroupsObserved
)

func resolveMemoryGroups(ssn *framework.Session, pod *v1.Pod) ([]schedulingv1alpha2.NUMAMemoryGroupPlacement, memoryGroupSource) {
	if raw, exists := pod.Annotations[commonconstants.NumaMemoryGroupsObserved]; exists {
		groups, complete := parseMemoryGroups(raw)
		if !complete {
			return nil, memoryGroupsUnknown
		}
		return groups, memoryGroupsObserved
	}
	if request := ssn.ClusterInfo.BindRequests.GetBindRequestForPod(pod); request != nil &&
		request.BindRequest.Spec.PredictedNUMAMemoryGroups != nil {
		return request.BindRequest.Spec.PredictedNUMAMemoryGroups, memoryGroupsPredicted
	}
	if raw, exists := pod.Annotations[commonconstants.NumaMemoryGroupsPredicted]; exists {
		groups, complete := parseMemoryGroups(raw)
		if !complete {
			return nil, memoryGroupsUnknown
		}
		return groups, memoryGroupsPredicted
	}
	return nil, memoryGroupsUnknown
}

func parseMemoryGroups(raw string) ([]schedulingv1alpha2.NUMAMemoryGroupPlacement, bool) {
	var groups []schedulingv1alpha2.NUMAMemoryGroupPlacement
	if err := json.Unmarshal([]byte(raw), &groups); err != nil || groups == nil {
		return nil, false
	}
	return groups, true
}

func memoryGroupsFromRecord(record []schedulingv1alpha2.NUMAMemoryGroupPlacement, topo *node_info.NumaTopology) ([]pod_info.MemoryGroupPlacement, error) {
	amounts := map[pod_info.NUMAMask]v1.ResourceList{}
	for _, group := range record {
		indices := make([]int, 0, len(group.MemoryNodes))
		for _, zone := range group.MemoryNodes {
			index, exists := topo.ZoneIndexByID(zone)
			if !exists {
				return nil, fmt.Errorf("unknown memory zone %q", zone)
			}
			indices = append(indices, index)
		}
		mask, err := pod_info.NewNUMAMask(indices)
		if err != nil || mask == 0 || len(group.Amount) == 0 {
			return nil, fmt.Errorf("invalid memory group %v", group.MemoryNodes)
		}
		if amounts[mask] == nil {
			amounts[mask] = v1.ResourceList{}
		}
		for name, quantity := range group.Amount {
			_, representable := quantity.AsInt64()
			if !commonresources.IsMemoryResource(name) || !topo.Resources.Has(name) || quantity.Sign() < 0 || !representable {
				return nil, fmt.Errorf("invalid memory amount %s=%s", name, quantity.String())
			}
			amount := amounts[mask][name]
			amount.Add(quantity)
			if _, representable := amount.AsInt64(); !representable {
				return nil, fmt.Errorf("memory amount exceeds int64 for %s", name)
			}
			amounts[mask][name] = amount
		}
	}
	groups := make([]pod_info.MemoryGroupPlacement, 0, len(amounts))
	for mask, amount := range amounts {
		groups = append(groups, pod_info.MemoryGroupPlacement{Mask: mask, Amount: amount})
	}
	sort.Slice(groups, func(first, second int) bool { return groups[first].Mask < groups[second].Mask })
	return groups, nil
}

type seededMemoryPlacement struct {
	groups []pod_info.MemoryGroupPlacement
	source memoryGroupSource
}

func seedMemoryPlacement(ssn *framework.Session, task *pod_info.PodInfo, topo *node_info.NumaTopology) seededMemoryPlacement {
	record, source := resolveMemoryGroups(ssn, task.Pod)
	result := seededMemoryPlacement{source: source}
	if source == memoryGroupsUnknown {
		return result
	}
	groups, err := memoryGroupsFromRecord(record, topo)
	if err != nil {
		result.source = memoryGroupsUnknown
		log.InfraLogger.V(4).Infof("numa: invalid memory groups for pod <%s/%s>: %v", task.Namespace, task.Name, err)
		return result
	}
	result.groups = groups
	return result
}

func applySeededMemoryPlacement(task *pod_info.PodInfo, placement seededMemoryPlacement) {
	task.NUMAPlacement.MemoryGroups = placement.groups
}

func (pp *numaPlugin) seedMemoryGroups(ssn *framework.Session) {
	pp.observedMemoryByNode = map[string]sets.Set[v1.ResourceName]{}
	placements := map[types.UID]seededMemoryPlacement{}
	resourcesByNode := map[string]sets.Set[v1.ResourceName]{}
	for _, node := range ssn.ClusterInfo.Nodes {
		topo := node.NumaTopology
		if topo == nil || !isScoredPolicy(topo.Policy) || len(topo.Zones) > pod_info.MaxNUMAZones {
			continue
		}
		state := &node_info.MemoryGroupState{
			Status: node_info.MemoryGroupsKnown,
			Groups: map[pod_info.NUMAMask]*node_info.MemoryGroup{},
		}
		topo.MemoryGroups = state
		pp.observedMemoryByNode[node.Name] = sets.New[v1.ResourceName]()
		resources := sets.New[v1.ResourceName]()
		for name := range topo.Resources {
			if commonresources.IsMemoryResource(name) {
				resources.Insert(name)
			}
		}
		resourcesByNode[node.Name] = resources
		for _, task := range node.PodInfos {
			if !pod_status.IsActiveUsedStatus(task.Status) || !requestsMemory(task.Pod, resources) {
				continue
			}
			owner := memoryOwner(task)
			placement := seedMemoryPlacement(ssn, task, topo)
			if placement.source != memoryGroupsUnknown {
				if err := state.ValidateOwner(topo, owner, placement.groups); err != nil {
					placement.groups = nil
					placement.source = memoryGroupsUnknown
					log.InfraLogger.V(4).Infof("numa: invalid memory groups for pod <%s/%s>: %v", task.Namespace, task.Name, err)
				} else {
					state.ApplyOwner(owner, placement.groups)
				}
			}
			placements[owner] = placement
			applySeededMemoryPlacement(task, placement)
			if placement.source == memoryGroupsUnknown {
				state.Status = node_info.MemoryGroupsUnknown
				continue
			}
			if placement.source == memoryGroupsObserved {
				for _, group := range placement.groups {
					for name := range group.Amount {
						pp.observedMemoryByNode[node.Name].Insert(name)
					}
				}
			}
		}
		if state.Status == node_info.MemoryGroupsUnknown {
			state.Groups = map[pod_info.NUMAMask]*node_info.MemoryGroup{}
			log.InfraLogger.V(6).Do(func() {
				log.InfraLogger.Infof("numa: memory group state unknown on node <%s>", node.Name)
			})
		}
	}
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		for _, task := range job.GetAllPodsMap() {
			node := ssn.ClusterInfo.Nodes[task.NodeName]
			if node == nil || node.NumaTopology == nil || !isScoredPolicy(node.NumaTopology.Policy) || task.Pod == nil {
				continue
			}
			if !requestsMemory(task.Pod, resourcesByNode[node.Name]) {
				applySeededMemoryPlacement(task, seededMemoryPlacement{})
				continue
			}
			placement, found := placements[memoryOwner(task)]
			if !found {
				placement = seedMemoryPlacement(ssn, task, node.NumaTopology)
			}
			applySeededMemoryPlacement(task, placement)
		}
	}
}
