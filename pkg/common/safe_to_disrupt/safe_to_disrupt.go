// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package safe_to_disrupt reads the kai.scheduler/safe-to-disrupt annotation, with which a workload says
// whether the scheduler may evict its pods for reasons other than fairness or priority (today, consolidation).
package safe_to_disrupt

import "strconv"

const AnnotationKey = "kai.scheduler/safe-to-disrupt"

// Parse parses an annotation value, "true" or "false"; anything strconv.ParseBool rejects is an error.
func Parse(value string) (bool, error) {
	return strconv.ParseBool(value)
}

// FromAnnotations reports whether the annotations allow disruption: true unless the annotation is false.
func FromAnnotations(annotations map[string]string) bool {
	value, found := annotations[AnnotationKey]
	if !found {
		return true
	}
	safe, err := Parse(value)
	return err != nil || safe
}
