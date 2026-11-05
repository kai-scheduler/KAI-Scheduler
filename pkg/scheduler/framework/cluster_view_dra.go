// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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
	}
	if k8sPlugins := ssn.InternalK8sPlugins(); k8sPlugins != nil && k8sPlugins.SessionDRAManager != nil {
		overlay.claims, _ = k8sPlugins.SessionDRAManager.ResourceClaims().(*k8s_utils.SessionClaimTracker)
	}
	if terminating.isEmpty() || overlay.claims == nil || ssn.ClusterInfo == nil {
		return overlay
	}

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

// isFreedWithTerminatingPods reports whether the claim is reserved only for terminating pods, matching
// the eviction path that deallocates a claim once no consumer is left.
func (o *draOverlay) isFreedWithTerminatingPods(claim *resourceapi.ResourceClaim) bool {
	if claim.Status.Allocation == nil || len(claim.Status.ReservedFor) == 0 {
		return false
	}
	for _, consumer := range claim.Status.ReservedFor {
		if consumer.APIGroup != "" || consumer.Resource != "pods" || !o.terminating.uids.Has(consumer.UID) {
			return false
		}
	}
	return true
}

func (o *draOverlay) allocatedState(pod *v1.Pod, future bool) (*structured.AllocatedState, error) {
	if o.claims == nil {
		return nil, fmt.Errorf("DRA claim tracker is not initialized")
	}
	state, err := o.claims.GatherAllocatedState()
	if err != nil || !future || o.releasing.Len() == 0 {
		return state, err
	}
	ownClaims := podClaimKeys(pod)
	for device := range o.releasing {
		if o.isFreeInFuture(device, ownClaims) {
			state.AllocatedDevices.Delete(device)
		}
	}
	return state, nil
}

// isFreeInFuture reports whether every claim holding the device is freed with the terminating pods.
// The pod's own claims keep their devices: its allocated claims hold them from its other claims.
func (o *draOverlay) isFreeInFuture(device structured.DeviceID, ownClaims sets.Set[string]) bool {
	for _, holder := range o.claims.DeviceHolders(device) {
		if holder.InFlight || !o.isFreedWithTerminatingPods(holder.Claim) ||
			ownClaims.Has(claimKey(holder.Claim.Namespace, holder.Claim.Name)) {
			return false
		}
	}
	return true
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
