// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import "sigs.k8s.io/controller-runtime/pkg/client"

type FieldInherit func(current, desired client.Object)

func mergeAnnotations(desiredAnnotations, currentAnnotations map[string]string) map[string]string {
	if desiredAnnotations == nil {
		desiredAnnotations = map[string]string{}
	}
	for currentAnnotationKey, currentAnnotationValue := range currentAnnotations {
		if _, isOverride := desiredAnnotations[currentAnnotationKey]; !isOverride {
			desiredAnnotations[currentAnnotationKey] = currentAnnotationValue
		}
	}
	return desiredAnnotations
}
