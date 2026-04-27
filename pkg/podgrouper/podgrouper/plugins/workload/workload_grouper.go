// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package workload implements the translation layer described in
// docs/developer/designs/k8s-workload-api/README.md: it overrides podgroup
// metadata produced by the top-owner plugin when a Pod declares
// spec.workloadRef pointing at an upstream scheduling.k8s.io/v1alpha1
// Workload resource (KEP-4671).
package workload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	pgconstants "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
)

// ErrWorkloadNotFound is returned when a Pod's spec.workloadRef references a
// Workload that does not exist. Callers should treat this as a soft failure —
// the Pod stays pending until the Workload is created and the watcher enqueues
// the Pod for reconciliation. See section 4 of the design.
var ErrWorkloadNotFound = errors.New("workload not found")

// ErrWorkloadPodGroupNotFound is returned when the Workload exists but does not declare
// the PodGroup name referenced by the Pod's spec.workloadRef.podGroup.
var ErrWorkloadPodGroupNotFound = errors.New("workload podGroup not found")

// ApplyOverride layers Workload-derived metadata on top of the base metadata
// produced by the top-owner plugin, per section 3 of the design. It is a no-op
// when:
//   - the Pod has no spec.workloadRef, or
//   - the Pod or its top owner is annotated with
//     constants.WorkloadIgnoreAnnotationKey=true.
//
// Otherwise it resolves the Workload through the supplied client.Reader, picks
// the referenced Workload.Spec.PodGroups entry, and overrides Name,
// MinAvailable, SubGroups, and (when the Workload itself carries them as
// labels/annotations) Queue / PriorityClassName / Preemptibility / Topology.
// Labels and annotations are merged with Workload values taking precedence on
// collision.
//
// reader must be non-nil and backed by the same cache that drives the
// controller's Workload watch (typically the manager's cached client). Using a
// separate informer factory's lister introduces a race where the controller's
// watch fires Reconcile before the lister has processed the same Workload
// UPDATE, so ApplyOverride reads stale labels and the PodGroup never updates.
// Callers that haven't detected the upstream API on the cluster must skip
// ApplyOverride entirely rather than passing a sentinel reader.
func ApplyOverride(
	ctx context.Context,
	base *podgroup.Metadata,
	pod *corev1.Pod,
	topOwner *unstructured.Unstructured,
	reader client.Reader,
) (*podgroup.Metadata, error) {
	if shouldSkip(base, pod, topOwner) {
		return base, nil
	}
	ref := pod.Spec.WorkloadRef

	wl := &schedulingv1alpha1.Workload{}
	err := reader.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: ref.Name}, wl)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", ErrWorkloadNotFound, pod.Namespace, ref.Name)
		}
		return nil, fmt.Errorf("failed to get workload %s/%s: %w", pod.Namespace, ref.Name, err)
	}

	wlPodGroup, ok := findWorkloadPodGroup(wl, ref.PodGroup)
	if !ok {
		return nil, fmt.Errorf("%w: workload=%s/%s podGroup=%s",
			ErrWorkloadPodGroupNotFound, pod.Namespace, ref.Name, ref.PodGroup)
	}

	merged := base.DeepCopy()
	merged.Name = generatePodGroupName(ref.Name, ref.PodGroup, ref.PodGroupReplicaKey, wlPodGroup.Policy)
	merged.MinAvailable = generateMinAvailable(wlPodGroup.Policy)
	// SubGroups are owned by the Workload dispatch and ignored until
	// the upstream API grows sub-group support
	merged.SubGroups = nil

	if merged.Labels == nil {
		merged.Labels = map[string]string{}
	}
	maps.Copy(merged.Labels, wl.Labels)
	if merged.Annotations == nil {
		merged.Annotations = map[string]string{}
	}
	maps.Copy(merged.Annotations, wl.Annotations)

	if v, ok := wl.Labels[commonconstants.DefaultQueueLabel]; ok && v != "" {
		merged.Queue = v
	}
	if v, ok := wl.Labels[pgconstants.PriorityLabelKey]; ok && v != "" {
		merged.PriorityClassName = v
	}
	if v, ok := wl.Labels[pgconstants.PreemptibilityLabelKey]; ok && v != "" {
		preemptibility, err := v2alpha2.ParsePreemptibility(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse preemptibility %s from workload %s/%s: %w", v, wl.Namespace, wl.Name, err)
		}
		merged.Preemptibility = preemptibility
	}
	if v, ok := wl.Annotations[pgconstants.TopologyKey]; ok && v != "" {
		merged.Topology = v
	}
	if v, ok := wl.Annotations[pgconstants.TopologyRequiredPlacementKey]; ok && v != "" {
		merged.RequiredTopologyLevel = v
	}
	if v, ok := wl.Annotations[pgconstants.TopologyPreferredPlacementKey]; ok && v != "" {
		merged.PreferredTopologyLevel = v
	}

	return merged, nil
}

func shouldSkip(base *podgroup.Metadata, pod *corev1.Pod, topOwner *unstructured.Unstructured) bool {
	return base == nil || pod == nil || pod.Spec.WorkloadRef == nil || isIgnored(pod, topOwner)
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

func findWorkloadPodGroup(wl *schedulingv1alpha1.Workload, name string) (schedulingv1alpha1.PodGroup, bool) {
	for _, pg := range wl.Spec.PodGroups {
		if pg.Name == name {
			return pg, true
		}
	}
	return schedulingv1alpha1.PodGroup{}, false
}

func generatePodGroupName(workload, podGroup, replicaKey string, policy schedulingv1alpha1.PodGroupPolicy) string {
	full := fmt.Sprintf("%s-%s", workload, podGroup)
	if policy.Gang != nil && replicaKey != "" {
		full = fmt.Sprintf("%s-%s-%s", workload, podGroup, replicaKey)
	}
	if len(full) <= validation.DNS1123SubdomainMaxLength {
		return full
	}
	return truncateWithHash(full, validation.DNS1123SubdomainMaxLength)
}

// truncateWithHash shrinks name to fit max chars by appending a deterministic
// SHA-256 suffix. The trimmed prefix is stripped of trailing '-' / '.' so the
// result remains a valid DNS-1123 subdomain. 40-bit hash gives ~2^20 birthday
// resistance among inputs that share the truncation prefix — overkill for the
// (Workload, podGroup, replicaKey) cardinality this naming serves.
func truncateWithHash(name string, max int) string {
	const hashLen = 10
	sum := sha256.Sum256([]byte(name))
	suffix := "-" + hex.EncodeToString(sum[:])[:hashLen]
	prefix := strings.TrimRight(name[:max-len(suffix)], "-.")
	return prefix + suffix
}

func generateMinAvailable(policy schedulingv1alpha1.PodGroupPolicy) int32 {
	if policy.Gang != nil {
		return policy.Gang.MinCount
	}
	// Basic policy (or unset, which the apiserver validates against): behave
	// as a standard non-gang group.
	return 1
}
