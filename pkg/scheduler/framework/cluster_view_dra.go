// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"slices"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/resourceclaim"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/kai-scheduler/api/utilities/resources"
)

// draOverlay adjusts the session's allocated DRA devices to a view. The devices of claims that
// only terminating pods hold, and that are freed when those pods exit, are free in the future view
// unless another live claim holds them.
type draOverlay struct {
	terminating *terminatingPods
	claims      *k8s_utils.SessionClaimTracker
	// releasing are the devices of claims freed with terminating pods when the session opened.
	releasing sets.Set[structured.DeviceID]
	// pinned are claims whose consumer has an in-flight bind and is not in ReservedFor yet.
	pinned sets.Set[string]
	// sharedPool is set when a releasing device is in a pool reachable from more than one node.
	sharedPool bool
}

type devicePool struct {
	driver string
	name   string
}

func (ssn *Session) draOverlay() *draOverlay {
	views := ssn.clusterViews()
	views.draOnce.Do(func() {
		views.dra = newDRAOverlay(ssn, views.terminating)
	})
	return views.dra
}

func newDRAOverlay(ssn *Session, terminating *terminatingPods) *draOverlay {
	overlay := &draOverlay{
		terminating: terminating,
		releasing:   sets.New[structured.DeviceID](),
		pinned:      sets.New[string](),
	}
	if ssn.Cache == nil {
		return overlay
	}
	if k8sPlugins := ssn.InternalK8sPlugins(); k8sPlugins != nil && k8sPlugins.SessionDRAManager != nil {
		overlay.claims, _ = k8sPlugins.SessionDRAManager.ResourceClaims().(*k8s_utils.SessionClaimTracker)
	}
	if terminating.isEmpty() || overlay.claims == nil || ssn.ClusterInfo == nil {
		return overlay
	}

	claimsByKey := map[string]*resourceapi.ResourceClaim{}
	for _, claim := range ssn.ClusterInfo.ResourceClaims {
		claimsByKey[claimKey(claim.Namespace, claim.Name)] = claim
	}
	overlay.pinned = pinnedClaims(ssn, claimsByKey)

	sharedPools := sets.New[devicePool]()
	for _, slice := range ssn.ClusterInfo.ResourceSlices {
		if ptr.Deref(slice.Spec.NodeName, "") == "" {
			sharedPools.Insert(devicePool{driver: slice.Spec.Driver, name: slice.Spec.Pool.Name})
		}
	}
	for _, claim := range ssn.ClusterInfo.ResourceClaims {
		if !overlay.isFreedWithTerminatingPods(claim) {
			continue
		}
		for _, device := range k8s_utils.DedicatedDevices(claim) {
			overlay.releasing.Insert(device)
			if sharedPools.Has(devicePool{driver: device.Driver.String(), name: device.Pool.String()}) {
				overlay.sharedPool = true
			}
		}
	}
	return overlay
}

// pinnedClaims returns the allocated claims of pods with an in-flight BindRequest that are not in
// the claim's ReservedFor yet: such a claim may look held only by terminating pods.
func pinnedClaims(ssn *Session, claimsByKey map[string]*resourceapi.ResourceClaim) sets.Set[string] {
	pinned := sets.New[string]()
	for _, podGroup := range ssn.ClusterInfo.PodGroupInfos {
		for _, task := range podGroup.GetAllPodsMap() {
			if task.BindRequest == nil || task.Pod.Status.Phase == v1.PodSucceeded ||
				task.Pod.Status.Phase == v1.PodFailed {
				continue
			}
			for _, allocation := range task.BindRequest.BindRequest.Spec.ResourceClaimAllocations {
				for _, podClaim := range task.Pod.Spec.ResourceClaims {
					if podClaim.Name != allocation.Name {
						continue
					}
					claimName, err := resources.GetResourceClaimName(task.Pod, &podClaim)
					if err != nil {
						continue
					}
					key := claimKey(task.Namespace, claimName)
					claim := claimsByKey[key]
					if claim != nil && claim.Status.Allocation != nil && !resourceclaim.IsReservedForPod(task.Pod, claim) {
						pinned.Insert(key)
					}
				}
			}
		}
	}
	return pinned
}

// isFreedWithTerminatingPods reports whether the claim's devices are freed once the terminating
// pods exit: it is reserved only for them, and the claim controller deallocates it (it carries the
// controller's finalizer) or deletes it with its owning pod. KAI's binder never adds the finalizer
// to standalone claims, which stay allocated after their last consumer leaves.
func (o *draOverlay) isFreedWithTerminatingPods(claim *resourceapi.ResourceClaim) bool {
	if claim.Status.Allocation == nil || len(claim.Status.ReservedFor) == 0 ||
		o.pinned.Has(claimKey(claim.Namespace, claim.Name)) {
		return false
	}
	for _, consumer := range claim.Status.ReservedFor {
		if consumer.APIGroup != "" || consumer.Resource != "pods" || !o.terminating.uids.Has(consumer.UID) {
			return false
		}
	}
	if slices.Contains(claim.Finalizers, resourceapi.Finalizer) {
		return true
	}
	owner := metav1.GetControllerOf(claim)
	return owner != nil && owner.APIVersion == "v1" && owner.Kind == "Pod" && o.terminating.uids.Has(owner.UID)
}

func (o *draOverlay) allocatedState(pod *v1.Pod, future bool) (*structured.AllocatedState, error) {
	if o.claims == nil {
		return nil, fmt.Errorf("DRA claim tracker is not initialized")
	}
	state, err := o.claims.GatherAllocatedState()
	if err != nil {
		return nil, err
	}
	if o.releasing.Len() == 0 {
		return state, nil
	}

	var ownClaims sets.Set[string]
	if future {
		ownClaims = podClaimKeys(pod)
	}
	// The tracker's allocated set is not reference counted, so the live holders decide, in both views.
	for device := range o.releasing {
		if o.isTaken(device, future, ownClaims) {
			state.AllocatedDevices.Insert(device)
		} else {
			state.AllocatedDevices.Delete(device)
		}
	}
	return state, nil
}

// isTaken reports whether a live claim keeps the device in the view. In the future view, claims
// freed with terminating pods do not, unless they are the pod's own claims: a pod's allocated
// claims keep their devices from its other claims.
func (o *draOverlay) isTaken(device structured.DeviceID, future bool, ownClaims sets.Set[string]) bool {
	for _, holder := range o.claims.DeviceHolders(device) {
		if !future || holder.InFlight || !o.isFreedWithTerminatingPods(holder.Claim) ||
			ownClaims.Has(claimKey(holder.Claim.Namespace, holder.Claim.Name)) {
			return true
		}
	}
	return false
}

func podClaimKeys(pod *v1.Pod) sets.Set[string] {
	keys := sets.New[string]()
	for _, podClaim := range pod.Spec.ResourceClaims {
		if claimName, err := resources.GetResourceClaimName(pod, &podClaim); err == nil {
			keys.Insert(claimKey(pod.Namespace, claimName))
		}
	}
	return keys
}

func claimKey(namespace, name string) string {
	return namespace + "/" + name
}
