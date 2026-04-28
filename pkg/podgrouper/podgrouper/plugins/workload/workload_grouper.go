// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package workload translates upstream scheduling.k8s.io/v1alpha1 Workloads (KEP-4671) into KAI PodGroup metadata.
// See docs/developer/designs/k8s-workload-api/README.md.
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
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/common"
	pgconstants "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
)

// ErrWorkloadNotFound — soft failure: the Pod stays pending until the watcher enqueues it.
var ErrWorkloadNotFound = errors.New("workload not found")

var ErrWorkloadPodGroupNotFound = errors.New("workload podGroup not found")

// ApplyOverride layers Workload metadata onto base. reader must share the cache that drives the
// Workload watch — a separate lister races with the watcher and stales reads on UPDATE.
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
		if common.PriorityClassExists(ctx, reader, v) {
			merged.PriorityClassName = v
		} else {
			log.FromContext(ctx).V(1).Info(
				"Workload priorityClassName label references unknown PriorityClass; keeping base value",
				"workload", fmt.Sprintf("%s/%s", wl.Namespace, wl.Name),
				"priorityClassName", v,
				"baseValue", base.PriorityClassName,
			)
		}
	}
	if v, ok := wl.Labels[pgconstants.PreemptibilityLabelKey]; ok && v != "" {
		if preemptibility, err := v2alpha2.ParsePreemptibility(v); err == nil {
			merged.Preemptibility = preemptibility
		} else {
			log.FromContext(ctx).V(1).Info(
				"Workload preemptibility label is invalid; keeping base value",
				"workload", fmt.Sprintf("%s/%s", wl.Namespace, wl.Name),
				"preemptibility", v,
				"error", err.Error(),
			)
		}
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

// truncateWithHash shrinks name to max chars and keeps the result a valid DNS-1123 subdomain.
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
	return 1
}
