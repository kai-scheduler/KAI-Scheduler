// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"slices"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClaimOutlivesPod reports whether a claim stays allocated once the pod is deleted: a standalone
// claim without the claim controller's finalizer. A claim generated for the pod, or one the claim
// controller still tracks, is deallocated once the pod is gone.
func ClaimOutlivesPod(claim *resourceapi.ResourceClaim, pod *v1.Pod) bool {
	if slices.Contains(claim.Finalizers, resourceapi.Finalizer) {
		return false
	}
	owner := metav1.GetControllerOf(claim)
	return owner == nil || owner.Kind != "Pod" || owner.UID != pod.UID
}
