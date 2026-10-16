// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package minmember parses min-member annotation values, which let users override the minimal number
// of members a podgroup or a subgroup requires. The default applied when the annotation is absent is
// workload specific and stays with the grouper.
// The standard annotation key is kai.scheduler/batch-min-member, but workload-specific groupers may
// use their own key via FromAnnotationsWithKey.
package minmember

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/api/podgrouper/constants"
)

// Parse parses a min-member annotation value set on source, which names the annotated object for error reporting.
func Parse(value, sourceName string) (int32, error) {
	return parseWithKey(value, sourceName, constants.MinMemberOverrideKey)
}

// FromAnnotations returns the min-member override of obj, or fallback when the annotation is absent.
// It reads the standard kai.scheduler/batch-min-member annotation key.
func FromAnnotations(obj metav1.Object, source string, fallback int32) (int32, error) {
	return FromAnnotationsWithKey(obj, source, constants.MinMemberOverrideKey, fallback)
}

// FromAnnotationsWithKey is like FromAnnotations but reads an arbitrary annotation key.
// Use this when a workload-specific grouper needs a different annotation than the standard batch key.
func FromAnnotationsWithKey(obj metav1.Object, source, annotationKey string, fallback int32) (int32, error) {
	value, found := obj.GetAnnotations()[annotationKey]
	if !found {
		return fallback, nil
	}

	return parseWithKey(value, source, annotationKey)
}

// parseWithKey parses and validates a raw annotation value, including the annotation key name in
// error messages for clarity.
func parseWithKey(value, sourceName, annotationKey string) (int32, error) {
	minMember, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s annotation on %s: %w", annotationKey, sourceName, err)
	}
	if minMember < 1 {
		return 0, fmt.Errorf("invalid %s annotation on %s: value %d must be >= 1",
			annotationKey, sourceName, minMember)
	}

	return int32(minMember), nil
}
