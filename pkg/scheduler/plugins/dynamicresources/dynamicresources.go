// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	schedulingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/resources"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

const (
	defaultMaxCelCacheEntries = 10
	maxCelCacheEntriesKey     = "maxCelCacheEntries"
)

type draPlugin struct {
	enabled  bool
	manager  k8sframework.SharedDRAManager
	celCache *cel.Cache
}

// +kubebuilder:rbac:groups="resource.k8s.io",resources=deviceclasses;resourceslices;resourceclaims,verbs=get;list;watch

func New(pluginArgs map[string]string) framework.Plugin {
	maxCelCacheEntries := defaultMaxCelCacheEntries
	if entries, found := pluginArgs[maxCelCacheEntriesKey]; found {
		value, err := strconv.Atoi(entries)
		if err != nil {
			log.InfraLogger.V(2).Warnf("Failed to parse %s as int: %v, err: %v.\n Using default value of: %d",
				maxCelCacheEntriesKey, entries, err, defaultMaxCelCacheEntries)
		} else {
			maxCelCacheEntries = value
		}
	}

	features := k8s_utils.GetK8sFeatures()
	return &draPlugin{
		enabled:  features.EnableDynamicResourceAllocation,
		celCache: cel.NewCache(maxCelCacheEntries, cel.Features{EnableConsumableCapacity: features.EnableConsumableCapacity}),
	}
}

func (drap *draPlugin) Name() string {
	return "dynamicresources"
}

func (drap *draPlugin) OnSessionOpen(ssn *framework.Session) {
	fwork := ssn.InternalK8sPlugins().FrameworkHandle

	drap.manager = fwork.SharedDRAManager()

	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   drap.allocateHandlerFn(ssn),
		DeallocateFunc: drap.deallocateHandlerFn(ssn),
	})

	drap.assumePendingClaims(ssn)

	ssn.AddPrePredicateFn(drap.preFilter)
}

func (drap *draPlugin) assumePendingClaims(ssn *framework.Session) {
	for _, podGroup := range ssn.PodGroupInfos {
		for _, pod := range podGroup.PodInfos {
			if pod.BindRequest == nil {
				continue
			}
			for _, claim := range pod.BindRequest.BindRequest.Spec.ResourceClaimAllocations {
				err := drap.assumePendingClaim(&claim, pod.Pod)
				if err != nil {
					log.InfraLogger.Errorf("Failed to assume pending claim %s for pod %s/%s: %v", claim.Name, pod.Namespace, pod.Name, err)
				}
			}
		}
	}
}

func (drap *draPlugin) assumePendingClaim(claim *schedulingv1alpha2.ResourceClaimAllocation, pod *v1.Pod) error {
	claimName := ""
	for _, podClaim := range pod.Spec.ResourceClaims {
		if podClaim.Name == claim.Name {
			var err error
			claimName, err = resources.GetResourceClaimName(pod, &podClaim)
			if err != nil {
				return fmt.Errorf("failed to get claim name for pod %s/%s: %v", pod.Namespace, pod.Name, err)
			}
			break
		}
	}

	if claimName == "" {
		return fmt.Errorf("claim reference %s from bind request not found in pod %s/%s, pod's claims: %v",
			claim.Name, pod.Namespace, pod.Name, pod.Spec.ResourceClaims)
	}

	claimObject, err := drap.manager.ResourceClaims().Get(pod.Namespace, claimName)
	if err != nil {
		return fmt.Errorf("failed to get resource claim %s/%s: %v", pod.Namespace, claim.Name, err)
	}

	if claimObject.Status.Allocation != nil {
		return nil // Claim is already allocated, no need to assume
	}

	updatedClaim := claimObject.DeepCopy()
	resources.UpsertReservedFor(updatedClaim, pod)
	updatedClaim.Status.Allocation = claim.Allocation

	return drap.manager.ResourceClaims().SignalClaimPendingAllocation(updatedClaim.UID, updatedClaim)
}

func (drap *draPlugin) preFilter(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo) error {
	pod := task.Pod
	if !drap.enabled && len(pod.Spec.ResourceClaims) > 0 {
		var resourceClaimNames []string
		for _, claim := range pod.Spec.ResourceClaims {
			resourceClaimNames = append(resourceClaimNames, claim.Name)
		}
		return fmt.Errorf("pod %s/%s cannot be scheduled, it references resource claims <%v> "+
			"while dynamic resource allocation feature is not enabled in cluster",
			task.Namespace, task.Name, strings.Join(resourceClaimNames, ", "))
	}

	for _, podClaim := range pod.Spec.ResourceClaims {
		claimName, err := resources.GetResourceClaimName(pod, &podClaim)
		if err != nil {
			return err
		}

		claim, err := drap.manager.ResourceClaims().Get(pod.Namespace, claimName)
		if err != nil {
			return fmt.Errorf("failed to get resource claim %s/%s: %v",
				pod.Namespace, claimName, err)
		}
		if len(claim.Status.ReservedFor) >= resourceapi.ResourceClaimReservedForMaxSize {
			return fmt.Errorf("resource claim %s/%s has reached its maximum number of consumers (%d)",
				pod.Namespace, claimName, resourceapi.ResourceClaimReservedForMaxSize)
		}
	}

	return nil
}

func (drap *draPlugin) allocateHandlerFn(ssn *framework.Session) func(event *framework.Event) {
	// Assuming this pod already passed Filter for this Node -
	// we can add it to the reservedFor list and also call allocator if needed.
	return func(event *framework.Event) {
		pod := event.Task.Pod
		nodeName := event.Task.NodeName
		node := ssn.Nodes[nodeName].Node

		for _, podClaim := range pod.Spec.ResourceClaims {
			// TODO: support resource claim template
			err := drap.allocateResourceClaim(event.Task, &podClaim, node)
			if err != nil {
				log.InfraLogger.Errorf("Failed to allocate resource claim %s for pod %s/%s: %v", podClaim.Name, pod.Namespace, pod.Name, err)
				continue
			}
		}
	}
}

func (drap *draPlugin) deallocateHandlerFn(_ *framework.Session) func(event *framework.Event) {
	return func(event *framework.Event) {
		pod := event.Task.Pod

		for _, podClaim := range pod.Spec.ResourceClaims {
			err := drap.deallocateResourceClaim(event.Task, &podClaim)
			if err != nil {
				log.InfraLogger.Errorf("Failed to deallocate resource claim %s for pod %s/%s: %v", podClaim.Name, pod.Namespace, pod.Name, err)
				continue
			}
		}
	}
}

func (drap *draPlugin) OnSessionClose(_ *framework.Session) {}

func (drap *draPlugin) allocateResourceClaim(task *pod_info.PodInfo, podClaim *v1.PodResourceClaim, node *v1.Node) error {
	claimName, err := resources.GetResourceClaimName(task.Pod, podClaim)
	if err != nil {
		return err
	}

	originalClaim, err := drap.manager.ResourceClaims().Get(task.Namespace, claimName)
	if err != nil {
		return fmt.Errorf("failed to get resource claim %s/%s: %v", task.Namespace, claimName, err)
	}

	claim := originalClaim.DeepCopy() // Modifying the original object will cause the manager to think there were no updates

	resources.UpsertReservedFor(claim, task.Pod)

	if claim.Status.Allocation == nil {
		allocatedState, err := drap.manager.ResourceClaims().GatherAllocatedState()
		if err != nil {
			return fmt.Errorf("failed to list all allocated devices: %v", err)
		}

		resourceSlices, err := drap.manager.ResourceSlices().ListWithDeviceTaintRules()
		if err != nil {
			return fmt.Errorf("failed to list all resource slices: %v", err)
		}

		allocator, err := structured.NewAllocator(
			context.Background(), structured.Features{},
			*allocatedState,
			drap.manager.DeviceClasses(),
			resourceSlices,
			drap.celCache,
		)

		if err != nil {
			return fmt.Errorf("failed to create allocator: %v", err)
		}

		result, err := allocator.Allocate(context.Background(), node, []*resourceapi.ResourceClaim{claim})
		if err != nil || result == nil {
			return fmt.Errorf("failed to allocate resources: %v", err)
		}

		claim.Status.Allocation = &result[0]
	}

	err = drap.manager.ResourceClaims().AssumeClaimAfterAPICall(claim)
	if err != nil {
		return fmt.Errorf("failed to update resource claim %s/%s: %v", task.Namespace, claimName, err)
	}

	task.ResourceClaimInfo = append(task.ResourceClaimInfo, schedulingv1alpha2.ResourceClaimAllocation{
		Name:       podClaim.Name,
		Allocation: claim.Status.Allocation.DeepCopy(),
	})

	return nil
}

func (drap *draPlugin) deallocateResourceClaim(task *pod_info.PodInfo, podClaim *v1.PodResourceClaim) error {
	claimName, err := resources.GetResourceClaimName(task.Pod, podClaim)
	if err != nil {
		return err
	}

	originalClaim, err := drap.manager.ResourceClaims().Get(task.Namespace, claimName)
	if err != nil {
		return fmt.Errorf("failed to get resource claim %s/%s: %v", task.Namespace, claimName, err)
	}

	claim := originalClaim.DeepCopy() // Modifying the original object will cause the manager to think there were no updates

	resources.RemoveReservedFor(claim, task.Pod)

	if len(claim.Status.ReservedFor) == 0 {
		claim.Status.Allocation = nil
	}

	err = drap.manager.ResourceClaims().AssumeClaimAfterAPICall(claim)
	if err != nil {
		return fmt.Errorf("failed to update resource claim %s/%s: %v", task.Namespace, claimName, err)
	}

	if task.ResourceClaimInfo != nil {
		for i, rc := range task.ResourceClaimInfo {
			if rc.Name == claimName {
				task.ResourceClaimInfo = append(task.ResourceClaimInfo[:i], task.ResourceClaimInfo[i+1:]...)
				break
			}
		}
	}

	return nil
}
