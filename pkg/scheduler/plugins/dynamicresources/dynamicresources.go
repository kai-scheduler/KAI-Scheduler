// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	defaultMaxCelCacheEntries = 10
	maxCelCacheEntriesKey     = "maxCelCacheEntries"
)

type draPlugin struct {
	enabled       bool
	manager       k8sframework.SharedDRAManager
	celCache      *cel.Cache
	queueLabelKey string
}

// +kubebuilder:rbac:groups="resource.k8s.io",resources=deviceclasses;resourceslices;resourceclaims,verbs=get;list;watch

func New(pluginArgs framework.PluginArguments) framework.Plugin {
	maxCelCacheEntries, err := pluginArgs.GetInt(maxCelCacheEntriesKey, defaultMaxCelCacheEntries)
	if err != nil {
		log.InfraLogger.Warningf("Failed to parse %s as int: %v, err: %v.\n Using default value of: %d",
			maxCelCacheEntriesKey, err, defaultMaxCelCacheEntries)
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
	drap.queueLabelKey = ssn.SchedulerParams.QueueLabelKey

	k8sPlugins := ssn.InternalK8sPlugins()
	if k8sPlugins != nil && k8sPlugins.ResourceSliceTracker != nil {
		drap.manager = k8s_utils.NewSessionDRAManager(
			ssn.ClusterInfo.ResourceClaims,
			k8sPlugins.ResourceSliceTracker,
			k8sPlugins.InformerFactory,
		)
		k8sPlugins.SessionDRAManager = drap.manager
	}

	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   drap.allocateHandlerFn(ssn),
		DeallocateFunc: drap.deallocateHandlerFn(ssn),
	})

	drap.assumePendingClaims(ssn)

	ssn.AddPrePredicateFn(drap.preFilter)
	ssn.AddPredicateFn(drap.filter)
}

func (drap *draPlugin) assumePendingClaims(ssn *framework.Session) {
	for _, podGroup := range ssn.ClusterInfo.PodGroupInfos {
		for _, pod := range podGroup.GetAllPodsMap() {
			if pod.BindRequest == nil {
				continue
			}
			if pod.Pod.Status.Phase == v1.PodSucceeded || pod.Pod.Status.Phase == v1.PodFailed {
				// If the pod has already succeeded or failed, the resources are freed and we can skip assuming pending claims
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

func (drap *draPlugin) preFilter(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo) error {
	pod := task.Pod

	if len(pod.Spec.ResourceClaims) == 0 {
		return nil
	}

	if !drap.enabled {
		var resourceClaimNames []string
		for _, claim := range pod.Spec.ResourceClaims {
			resourceClaimNames = append(resourceClaimNames, claim.Name)
		}
		return fmt.Errorf("pod %s/%s cannot be scheduled, it references resource claims <%v> "+
			"while dynamic resource allocation feature is not enabled in cluster",
			task.Namespace, task.Name, strings.Join(resourceClaimNames, ", "))
	}

	if drap.manager == nil {
		return fmt.Errorf("pod %s/%s has resource claims but DRA manager is not initialized",
			task.Namespace, task.Name)
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

		if err := drap.validateSharedGpuClaimQueueLabel(job, &podClaim, claim); err != nil {
			return fmt.Errorf("pod %s/%s cannot be scheduled: %v", task.Namespace, task.Name, err)
		}

		if claim.Status.Allocation != nil {
			continue
		}

		for _, request := range claim.Spec.Devices.Requests {
			if request.Exactly == nil {
				continue
			}
			if _, err := drap.manager.DeviceClasses().Get(request.Exactly.DeviceClassName); err != nil {
				return fmt.Errorf("device class %s does not exist for claim %s/%s request %s",
					request.Exactly.DeviceClassName, pod.Namespace, claimName, request.Name)
			}
		}
	}

	return nil
}

func (drap *draPlugin) filter(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, nodeInfo *node_info.NodeInfo) error {
	pod := task.Pod
	if len(pod.Spec.ResourceClaims) == 0 || drap.manager == nil {
		return nil
	}

	var claimsToAllocate []*resourceapi.ResourceClaim
	for _, podClaim := range pod.Spec.ResourceClaims {
		claimName, err := resources.GetResourceClaimName(pod, &podClaim)
		if err != nil {
			return err
		}

		claim, err := drap.manager.ResourceClaims().Get(pod.Namespace, claimName)
		if err != nil {
			return fmt.Errorf("failed to get resource claim %s/%s: %v", pod.Namespace, claimName, err)
		}

		if claim.Status.Allocation != nil {
			if claim.Status.Allocation.NodeSelector != nil {
				ns, err := nodeaffinity.NewNodeSelector(claim.Status.Allocation.NodeSelector)
				if err != nil {
					return fmt.Errorf("failed to parse node selector for claim %s/%s: %v",
						pod.Namespace, claimName, err)
				}
				if !ns.Match(nodeInfo.Node) {
					return fmt.Errorf("claim %s/%s node selector does not match node %s",
						claim.Namespace, claim.Name, nodeInfo.Name)
				}
			}
			continue
		}

		claimsToAllocate = append(claimsToAllocate, claim)
	}

	if len(claimsToAllocate) == 0 {
		return nil
	}

	allocatedState, err := drap.manager.ResourceClaims().GatherAllocatedState()
	if err != nil {
		return fmt.Errorf("failed to gather allocated device state: %v", err)
	}

	slices, err := drap.manager.ResourceSlices().ListWithDeviceTaintRules()
	if err != nil {
		return fmt.Errorf("failed to list resource slices: %v", err)
	}

	allocator, err := structured.NewAllocator(
		context.Background(), structured.Features{},
		*allocatedState,
		drap.manager.DeviceClasses(),
		slices,
		drap.celCache,
	)
	if err != nil {
		return fmt.Errorf("failed to create allocator: %v", err)
	}

	results, err := allocator.Allocate(context.Background(), nodeInfo.Node, claimsToAllocate)
	if err != nil {
		return fmt.Errorf("cannot allocate DRA claims on node %s: %v", nodeInfo.Name, err)
	}
	if len(results) != len(claimsToAllocate) {
		return fmt.Errorf("cannot allocate all DRA claims on node %s", nodeInfo.Name)
	}

	return nil
}

// validateSharedGpuClaimQueueLabel validates that shared GPU DRA claims (non-template claims) have the correct queue label.
// Template claims are created per-pod and don't need queue validation.
// Shared GPU claims can be used by multiple pods and must have the correct queue label to be scheduled.
func (drap *draPlugin) validateSharedGpuClaimQueueLabel(
	job *podgroup_info.PodGroupInfo,
	podClaim *v1.PodResourceClaim,
	claim *resourceapi.ResourceClaim,
) error {
	if podClaim.ResourceClaimTemplateName != nil {
		return nil
	}

	if !resources.IsGpuResourceClaim(claim) {
		return nil
	}

	expectedQueue := string(job.Queue)
	claimQueueLabel := claim.Labels[drap.queueLabelKey]

	if claimQueueLabel == "" {
		return fmt.Errorf("DRA claim %s is a shared GPU claim but does not have a queue label (%s)",
			claim.Name, constants.DefaultQueueLabel)
	}

	if claimQueueLabel != expectedQueue {
		return fmt.Errorf("DRA claim %s is a shared GPU claim with wrong queue label (expected queue: %s, claim queue label: %s)",
			claim.Name, expectedQueue, claimQueueLabel)
	}

	return nil
}

func (drap *draPlugin) allocateHandlerFn(ssn *framework.Session) func(event *framework.Event) {
	// Assuming this pod already passed Filter for this Node -
	// we can add it to the reservedFor list and also call allocator if needed.
	return func(event *framework.Event) {
		pod := event.Task.Pod
		nodeName := event.Task.NodeName
		node := ssn.ClusterInfo.Nodes[nodeName].Node

		for _, podClaim := range pod.Spec.ResourceClaims {
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

	// If the claim info has already been allocated in the past (the deallocation was virtual), recover previous allocation data
	allocatedFromMemory := false
	if claimAllocationInfo, ok := task.ResourceClaimInfo[podClaim.Name]; ok && claimAllocationInfo.Allocation != nil {
		claim.Status.Allocation = claimAllocationInfo.Allocation.DeepCopy()
		allocatedFromMemory = true
	}

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
		if err != nil {
			return fmt.Errorf("failed to allocate resources: %v", err)

		}
		if result == nil {
			return fmt.Errorf("failed to allocate resources: no allocation result")
		}

		claim.Status.Allocation = &result[0]
	}

	err = drap.manager.ResourceClaims().AssumeClaimAfterAPICall(claim)
	if err != nil {
		return fmt.Errorf("failed to update resource claim %s/%s: %v", task.Namespace, claimName, err)
	}

	log.InfraLogger.V(6).Infof("Allocated claim <%s/%s>, devices <%s>, allocation data from podInfo: %t.", task.Namespace, claimName, getClaimDevicesString(claim), allocatedFromMemory)

	task.ResourceClaimInfo[podClaim.Name] = &schedulingv1alpha2.ResourceClaimAllocation{
		Name:       podClaim.Name,
		Allocation: claim.Status.Allocation.DeepCopy(),
	}

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

	devicesDeallocatedStr := getClaimDevicesString(claim)

	resources.RemoveReservedFor(claim, task.Pod)
	if len(claim.Status.ReservedFor) == 0 {
		claim.Status.Allocation = nil
	}

	err = drap.manager.ResourceClaims().AssumeClaimAfterAPICall(claim)
	if err != nil {
		return fmt.Errorf("failed to update resource claim %s/%s: %v", task.Namespace, claimName, err)
	}

	if task.ResourceClaimInfo != nil {
		if claimInfoInTask := task.ResourceClaimInfo[podClaim.Name]; claimInfoInTask != nil {
			claimInfoInTask.Allocation = claim.Status.Allocation
		}
	}

	log.InfraLogger.V(6).Infof("Deallocated claim <%s/%s>, devices <%s>.", task.Namespace, claimName, devicesDeallocatedStr)

	return nil
}

func getClaimDevicesString(claim *resourceapi.ResourceClaim) string {
	if claim.Status.Allocation == nil {
		return ""
	}
	devices := make([]string, 0, len(claim.Status.Allocation.Devices.Results))
	for _, device := range claim.Status.Allocation.Devices.Results {
		devices = append(devices, device.Device)
	}
	return strings.Join(devices, ", ")
}
