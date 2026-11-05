// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"

	v1 "k8s.io/api/core/v1"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

type k8sPod struct {
	podInfo  ksf.PodInfo
	nodeInfo *k8sframework.NodeInfo
}

// mayChangeAffinity reports whether removing the terminating pods can change the pod's inter-pod
// affinity result. Affinity is counted per topology domain, so it looks past the node.
func (ssn *Session) mayChangeAffinity(pod *v1.Pod) bool {
	t := ssn.terminating()
	return len(t.k8sPods) > 0 && (t.hasRequiredAntiAffinity || hasRequiredPodAffinityTerms(pod))
}

// removeFromAffinityState swaps the pod's prefilter state for a copy without the terminating pods in
// its inter-pod affinity counts, as kube-scheduler does for preemption victims. Rerunning the
// prefilter would be costlier, and a Skip it returns would stick for the rest of the session.
func (ssn *Session) removeFromAffinityState(pod *v1.Pod) (restore func()) {
	extensions := ssn.podAffinityExtensions()
	if extensions == nil {
		return func() {}
	}
	state := ssn.GetSessionStateForResource(pod.UID)
	derived := state.Clone()
	for _, terminating := range ssn.terminating().k8sPods {
		// Fails when the prefilter was skipped: the pod has no affinity counts to adjust.
		status := extensions.RemovePod(context.Background(), derived, pod, terminating.podInfo, terminating.nodeInfo)
		if !status.IsSuccess() {
			return func() {}
		}
	}
	ssn.k8sResourceStateCache.Store(pod.UID, derived)
	return func() { ssn.k8sResourceStateCache.Store(pod.UID, state) }
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
