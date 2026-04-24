// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package workload implements the translation layer described in
// docs/developer/designs/k8s-workload-api/README.md: it overrides podgroup
// metadata produced by the top-owner plugin when a Pod declares
// spec.workloadRef pointing at an upstream scheduling.k8s.io/v1alpha1
// Workload resource (KEP-4671).
package workload

import (
	"errors"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	schedulingv1alpha1listers "k8s.io/client-go/listers/scheduling/v1alpha1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
)

// ErrWorkloadNotFound is returned when a Pod's spec.workloadRef references a
// Workload that does not exist. Callers should treat this as a soft failure —
// the Pod stays pending until the Workload is created and the watcher enqueues
// the Pod for reconciliation. See section 4 of the design.
var ErrWorkloadNotFound = errors.New("workload not found")

// ErrPodGroupNotFound is returned when the Workload exists but does not declare
// the PodGroup name referenced by the Pod's spec.workloadRef.podGroup.
var ErrPodGroupNotFound = errors.New("workload podGroup not found")

// ApplyOverride layers Workload-derived metadata on top of the base metadata
// produced by the top-owner plugin, per section 3 of the design. It is a no-op
// when:
//   - the Pod has no spec.workloadRef, or
//   - the Pod or its top owner is annotated with
//     constants.WorkloadIgnoreAnnotationKey=true.
//
// Otherwise it resolves the Workload through the supplied lister, picks the
// referenced Workload.Spec.PodGroups entry, and overrides Name, MinAvailable,
// SubGroups, and (when the Workload itself carries them as labels/annotations)
// Queue / PriorityClassName / Preemptibility / Topology. Labels and
// annotations are merged with Workload values taking precedence on collision.
func ApplyOverride(
	base *podgroup.Metadata,
	pod *corev1.Pod,
	topOwner *unstructured.Unstructured,
	workloads schedulingv1alpha1listers.WorkloadLister,
) (*podgroup.Metadata, error) {
	if base == nil || pod == nil {
		return base, nil
	}
	ref := pod.Spec.WorkloadRef
	if ref == nil {
		return base, nil
	}
	if isIgnored(pod, topOwner) {
		return base, nil
	}
	if workloads == nil {
		// Podgrouper was built without a lister — treat as unavailable.
		return base, nil
	}

	wl, err := workloads.Workloads(pod.Namespace).Get(ref.Name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", ErrWorkloadNotFound, pod.Namespace, ref.Name)
		}
		return nil, fmt.Errorf("failed to get workload %s/%s: %w", pod.Namespace, ref.Name, err)
	}

	wlPodGroup, ok := findPodGroup(wl, ref.PodGroup)
	if !ok {
		return nil, fmt.Errorf("%w: workload=%s/%s podGroup=%s",
			ErrPodGroupNotFound, pod.Namespace, ref.Name, ref.PodGroup)
	}

	// Never mutate the caller's metadata.
	merged := *base
	merged.Name = buildPodGroupName(ref.Name, ref.PodGroup, ref.PodGroupReplicaKey, wlPodGroup.Policy)
	merged.MinAvailable = minAvailableFromPolicy(wlPodGroup.Policy)
	// SubGroups are owned by the Workload dispatch and ignored until the
	// upstream API grows sub-group support (design section 3, "SubGroups: None").
	merged.SubGroups = nil

	merged.Labels = mergeStrings(base.Labels, wl.Labels)
	merged.Annotations = mergeStrings(base.Annotations, wl.Annotations)

	// Workload > Top Owner > Pod: only override scheduling config when the
	// Workload itself declares the KAI-specific label/annotation.
	if v, ok := wl.Labels[commonconstants.DefaultQueueLabel]; ok && v != "" {
		merged.Queue = v
	}
	if v, ok := wl.Labels["priorityClassName"]; ok && v != "" {
		merged.PriorityClassName = v
	}
	if v, ok := wl.Labels["kai.scheduler/preemptibility"]; ok && v != "" {
		// Upstream type parsing is done elsewhere; store verbatim — callers
		// already validate Preemptibility values.
		merged.Preemptibility = toPreemptibility(v, base.Preemptibility)
	}
	if v, ok := wl.Annotations["kai.scheduler/topology"]; ok && v != "" {
		merged.Topology = v
	}
	if v, ok := wl.Annotations["kai.scheduler/topology-required-placement"]; ok && v != "" {
		merged.RequiredTopologyLevel = v
	}
	if v, ok := wl.Annotations["kai.scheduler/topology-preferred-placement"]; ok && v != "" {
		merged.PreferredTopologyLevel = v
	}

	return &merged, nil
}

// IsSoftFailure reports whether err should leave the Pod pending without
// triggering a retry loop — currently the Workload- and PodGroup-not-found
// signals. See section 4 of the design.
func IsSoftFailure(err error) bool {
	return errors.Is(err, ErrWorkloadNotFound) || errors.Is(err, ErrPodGroupNotFound)
}

func isIgnored(pod *corev1.Pod, topOwner *unstructured.Unstructured) bool {
	if pod != nil && pod.Annotations[commonconstants.WorkloadIgnoreAnnotationKey] == "true" {
		return true
	}
	if topOwner != nil && topOwner.GetAnnotations()[commonconstants.WorkloadIgnoreAnnotationKey] == "true" {
		return true
	}
	return false
}

func findPodGroup(wl *schedulingv1alpha1.Workload, name string) (schedulingv1alpha1.PodGroup, bool) {
	for _, pg := range wl.Spec.PodGroups {
		if pg.Name == name {
			return pg, true
		}
	}
	return schedulingv1alpha1.PodGroup{}, false
}

// buildPodGroupName synthesizes the KAI PodGroup name for a Workload podGroup.
// Gang: one KAI PodGroup per (workload, podGroup, replicaKey). Basic: one per
// (workload, podGroup) — all replicaKeys share the same PodGroup with
// MinAvailable=1 (design section 2).
func buildPodGroupName(workload, podGroup, replicaKey string, policy schedulingv1alpha1.PodGroupPolicy) string {
	if policy.Gang != nil && replicaKey != "" {
		return fmt.Sprintf("%s-%s-%s", workload, podGroup, replicaKey)
	}
	return fmt.Sprintf("%s-%s", workload, podGroup)
}

func minAvailableFromPolicy(policy schedulingv1alpha1.PodGroupPolicy) int32 {
	if policy.Gang != nil {
		return policy.Gang.MinCount
	}
	// Basic policy (or unset, which the apiserver validates against): behave
	// as a standard non-gang group.
	return 1
}

func mergeStrings(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	maps.Copy(out, base)
	maps.Copy(out, overlay)
	return out
}

// toPreemptibility converts a raw string from a label to the typed enum.
// Unknown values keep the base value — the KAI admission webhook validates.
func toPreemptibility(raw string, base v2alpha2.Preemptibility) v2alpha2.Preemptibility {
	switch v2alpha2.Preemptibility(raw) {
	case v2alpha2.Preemptible:
		return v2alpha2.Preemptible
	case v2alpha2.NonPreemptible:
		return v2alpha2.NonPreemptible
	default:
		return base
	}
}
