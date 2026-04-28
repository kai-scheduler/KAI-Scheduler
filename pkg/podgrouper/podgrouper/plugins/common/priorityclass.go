// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"

	schedulingv1 "k8s.io/api/scheduling/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PriorityClassExists returns false on empty/nil inputs and on any lookup error.
func PriorityClassExists(ctx context.Context, reader client.Reader, name string) bool {
	if name == "" || reader == nil {
		return false
	}

	pc := &schedulingv1.PriorityClass{}
	if err := reader.Get(ctx, client.ObjectKey{Name: name}, pc); err != nil {
		log.FromContext(ctx).V(1).Info("Failed to get priority class",
			"priorityClassName", name, "error", err.Error())
		return false
	}
	return true
}
