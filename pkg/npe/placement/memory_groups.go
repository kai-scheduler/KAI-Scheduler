// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package placement

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	commonpod "github.com/kai-scheduler/KAI-scheduler/pkg/common/pod"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

const incompleteMemoryGroups = "null"

// MemoryGroupsValue builds the pod's observed memory-group annotation from podResources allocations.
// Non-Guaranteed pods return "[]". Guaranteed pods return "" before allocations start, "null" for
// incomplete or invalid observations, or a complete group list. Previously observed pods never
// revert to "", preserving the override of persisted predictions when observations regress.
func MemoryGroupsValue(ctx context.Context, pod *v1.Pod, observed *podresourcesv1.PodResources, previouslyObserved bool) string {
	if !commonpod.IsGuaranteed(pod) {
		return "[]"
	}
	logger := log.FromContext(ctx).WithValues("pod", pod.Namespace+"/"+pod.Name, "podUID", pod.UID)
	expected := map[string]v1.Container{}
	for _, container := range pod.Spec.Containers {
		expected[container.Name] = container
	}
	for _, container := range pod.Spec.InitContainers {
		if restartable(container) {
			expected[container.Name] = container
		}
	}
	groups, represented, err := aggregateMemoryGroups(observed, expected)
	if err != nil {
		logger.Error(err, "Invalid NUMA memory-group observation; publishing unknown memory groups")
		return incompleteMemoryGroups
	}
	complete, started, reason := memoryObservationComplete(pod, represented, expected)
	if !complete {
		if len(groups) != 0 || started || previouslyObserved {
			if pod.Annotations[constants.NumaMemoryGroupsObserved] == incompleteMemoryGroups {
				logger = logger.V(2)
			}
			logger.Info("Incomplete NUMA memory-group observation; publishing unknown memory groups", "reason", reason, "previouslyObserved", previouslyObserved)
			return incompleteMemoryGroups
		}
		logger.V(2).Info("Waiting for NUMA memory-group allocations", "reason", reason)
		return ""
	}
	encoded, err := json.Marshal(groups)
	if err != nil {
		logger.Error(err, "Failed to marshal NUMA memory groups; publishing unknown memory groups")
		return incompleteMemoryGroups
	}
	return string(encoded)
}

func aggregateMemoryGroups(observed *podresourcesv1.PodResources, expected map[string]v1.Container) ([]v1alpha2.NUMAMemoryGroupPlacement, map[string]*podresourcesv1.ContainerResources, error) {
	groups := []v1alpha2.NUMAMemoryGroupPlacement{}
	byMask := map[string]int{}
	represented := map[string]*podresourcesv1.ContainerResources{}
	maxAmount := resource.NewQuantity(math.MaxInt64, resource.BinarySI)
	for _, container := range observed.GetContainers() {
		if _, exists := represented[container.GetName()]; exists {
			return nil, nil, fmt.Errorf("duplicate container %q", container.GetName())
		}
		if len(container.GetMemory()) > 0 {
			if _, exists := expected[container.GetName()]; !exists {
				return nil, nil, fmt.Errorf("unexpected memory allocation for %q", container.GetName())
			}
		}
		represented[container.GetName()] = container
		for _, block := range container.GetMemory() {
			nodes := map[int64]struct{}{}
			for _, node := range block.GetTopology().GetNodes() {
				if node == nil || node.GetID() < 0 {
					return nil, nil, fmt.Errorf("invalid memory NUMA node for container %q", container.GetName())
				}
				nodes[node.GetID()] = struct{}{}
			}
			if len(nodes) == 0 || block.GetSize() > math.MaxInt64 {
				return nil, nil, fmt.Errorf("invalid memory block topology or size for container %q: NUMA nodes=%d, size=%d", container.GetName(), len(nodes), block.GetSize())
			}
			ids := make([]int64, 0, len(nodes))
			for node := range nodes {
				ids = append(ids, node)
			}
			sort.Slice(ids, func(first, second int) bool { return ids[first] < ids[second] })
			zones := make([]string, len(ids))
			for index, node := range ids {
				zones[index] = zoneName(node)
			}
			key := strings.Join(zones, ",")
			index, exists := byMask[key]
			if !exists {
				index = len(groups)
				byMask[key] = index
				groups = append(groups, v1alpha2.NUMAMemoryGroupPlacement{MemoryNodes: zones, Amount: v1.ResourceList{}})
			}
			name := v1.ResourceName(memoryResourceName(block.GetMemoryType()))
			if !resources.IsMemoryResource(name) {
				return nil, nil, fmt.Errorf("unsupported memory resource %q for container %q", name, container.GetName())
			}
			quantity := groups[index].Amount[name]
			quantity.Add(*resource.NewQuantity(int64(block.GetSize()), resource.BinarySI))
			if quantity.Cmp(*maxAmount) > 0 {
				return nil, nil, fmt.Errorf("memory amount for resource %q on NUMA nodes %v overflows int64", name, zones)
			}
			groups[index].Amount[name] = quantity
		}
	}
	sort.Slice(groups, func(first, second int) bool {
		return strings.Join(groups[first].MemoryNodes, ",") < strings.Join(groups[second].MemoryNodes, ",")
	})
	return groups, represented, nil
}

func memoryObservationComplete(pod *v1.Pod, represented map[string]*podresourcesv1.ContainerResources, expected map[string]v1.Container) (bool, bool, string) {
	started := map[string]bool{}
	anyStarted := false
	for _, status := range pod.Status.ContainerStatuses {
		started[status.Name] = status.State.Running != nil || status.State.Terminated != nil
		anyStarted = anyStarted || started[status.Name]
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if _, exists := expected[status.Name]; !exists {
			continue
		}
		started[status.Name] = status.State.Running != nil || status.State.Terminated != nil
		anyStarted = anyStarted || started[status.Name]
	}
	for _, container := range expected {
		for name, amount := range container.Resources.Requests {
			if !resources.IsMemoryResource(name) || amount.Sign() <= 0 {
				continue
			}
			entry, exists := represented[container.Name]
			if !started[container.Name] {
				return false, anyStarted, fmt.Sprintf("container %q has not started", container.Name)
			}
			if !exists {
				return false, anyStarted, fmt.Sprintf("container %q is missing from podResources", container.Name)
			}
			found := false
			for _, block := range entry.GetMemory() {
				if memoryResourceName(block.GetMemoryType()) == string(name) {
					found = true
				}
			}
			if !found {
				return false, true, fmt.Sprintf("container %q has no podResources block for requested resource %q", container.Name, name)
			}
		}
	}
	return true, anyStarted, ""
}

func restartable(container v1.Container) bool {
	return container.RestartPolicy != nil && *container.RestartPolicy == v1.ContainerRestartPolicyAlways
}
