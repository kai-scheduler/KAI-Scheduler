// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

type k8sPod struct {
	podInfo  ksf.PodInfo
	nodeInfo *k8sframework.NodeInfo
}

func (ssn *Session) terminatingPodsMatterForAffinity(pod *v1.Pod) bool {
	t := ssn.terminating()
	return len(t.k8sPods) > 0 && (t.hasRequiredAntiAffinity || hasRequiredPodAffinityTerms(pod))
}

// removeFromAffinityState subtracts the terminating pods from a copy of the pod's affinity counts.
// Rerunning the prefilter could return Skip, which the predicates plugin keeps for the whole
// session (possibly a bug).
func (ssn *Session) removeFromAffinityState(pod *v1.Pod) (restore func()) {
	extensions := ssn.podAffinityExtensions()
	if extensions == nil {
		return func() {}
	}
	t := ssn.terminating()
	state := ssn.GetSessionStateForResource(pod.UID)
	derived, found := t.affinityStates[pod.UID]
	if !found {
		derived = state.Clone()
		for _, terminating := range t.k8sPods {
			// Fails when the prefilter was skipped: the pod has no affinity counts to adjust.
			status := extensions.RemovePod(context.Background(), derived, pod, terminating.podInfo, terminating.nodeInfo)
			if !status.IsSuccess() {
				derived = state
				break
			}
		}
		t.affinityStates[pod.UID] = derived
	}
	ssn.k8sResourceStateCache.Store(pod.UID, derived)
	return func() { ssn.k8sResourceStateCache.Store(pod.UID, state) }
}

func (ssn *Session) dropAffinityState(uid types.UID) {
	if ssn.terminatingPods != nil {
		delete(ssn.terminatingPods.affinityStates, uid)
	}
}

func (ssn *Session) podAffinityExtensions() ksf.PreFilterExtensions {
	if ssn.Cache == nil || ssn.InternalK8sPlugins() == nil {
		return nil
	}
	if plugin, ok := ssn.InternalK8sPlugins().PodAffinity.(ksf.PreFilterPlugin); ok {
		return plugin.PreFilterExtensions()
	}
	return nil
}

func hasRequiredPodAffinityTerms(pod *v1.Pod) bool {
	affinity := pod.Spec.Affinity
	if affinity == nil {
		return false
	}
	return (affinity.PodAffinity != nil && len(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0) ||
		(affinity.PodAntiAffinity != nil && len(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0)
}
