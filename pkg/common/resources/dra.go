// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
)

func GetResourceClaimName(pod *v1.Pod, podClaim *v1.PodResourceClaim) (string, error) {
	if podClaim.ResourceClaimName != nil {
		return *podClaim.ResourceClaimName, nil
	}
	if podClaim.ResourceClaimTemplateName != nil {
		for _, status := range pod.Status.ResourceClaimStatuses {
			if status.Name == podClaim.Name && status.ResourceClaimName != nil {
				return *status.ResourceClaimName, nil
			}
		}
	}
	return "", fmt.Errorf("no resource claim name found for pod %s/%s and claim reference %s",
		pod.Namespace, pod.Name, podClaim.Name)
}

func UpsertReservedFor(claim *resourceapi.ResourceClaim, pod *v1.Pod) {
	for _, ref := range claim.Status.ReservedFor {
		if ref.Name == pod.Name &&
			ref.UID == pod.UID &&
			ref.Resource == "pods" &&
			ref.APIGroup == "" {
			return
		}
	}

	claim.Status.ReservedFor = append(
		claim.Status.ReservedFor,
		resourceapi.ResourceClaimConsumerReference{
			APIGroup: "",
			Resource: "pods",
			Name:     pod.Name,
			UID:      pod.UID,
		},
	)
}

func RemoveReservedFor(claim *resourceapi.ResourceClaim, pod *v1.Pod) {
	newReservedFor := make([]resourceapi.ResourceClaimConsumerReference, 0, len(claim.Status.ReservedFor))
	for _, ref := range claim.Status.ReservedFor {
		if ref.Name == pod.Name &&
			ref.UID == pod.UID &&
			ref.Resource == "pods" &&
			ref.APIGroup == "" {
			continue
		}

		newReservedFor = append(newReservedFor, ref)
	}
	claim.Status.ReservedFor = newReservedFor
}
