// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package minmember parses the kai.scheduler/batch-min-member annotation, which lets users override the minimal number
// of members a podgroup or a subgroup requires. The default applied when the annotation is absent is workload specific
// and stays with the grouper.
package minmember

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
)

// Parse parses a min-member annotation value set on source, which names the annotated object for error reporting.
func Parse(value, sourceName string) (int32, error) {
	minMember, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s annotation on %s: %w", constants.MinMemberOverrideKey, sourceName, err)
	}
	if minMember < 1 {
		return 0, fmt.Errorf("invalid %s annotation on %s: value %d must be >= 1",
			constants.MinMemberOverrideKey, sourceName, minMember)
	}

	return int32(minMember), nil
}

// FromAnnotations returns the min-member override of obj, or fallback when the annotation is absent.
func FromAnnotations(obj metav1.Object, source string, fallback int32) (int32, error) {
	value, found := obj.GetAnnotations()[constants.MinMemberOverrideKey]
	if !found {
		return fallback, nil
	}

	return Parse(value, source)
}
