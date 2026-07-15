// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

// mergeNamespaceSelector merges matchExpressions from current into desired, preserving any
// entries in current whose key is not already present in desired (e.g. cloud-provider-injected rules).
func mergeNamespaceSelector(desired, current *metav1.LabelSelector) *metav1.LabelSelector {
	if current == nil {
		return desired
	}
	if desired == nil {
		return current
	}
	desiredKeys := map[string]bool{}
	for _, expr := range desired.MatchExpressions {
		desiredKeys[expr.Key] = true
	}
	for _, expr := range current.MatchExpressions {
		if !desiredKeys[expr.Key] {
			desired.MatchExpressions = append(desired.MatchExpressions, expr)
		}
	}
	return desired
}
