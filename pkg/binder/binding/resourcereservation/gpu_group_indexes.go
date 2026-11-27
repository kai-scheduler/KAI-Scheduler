// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resourcereservation

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	"github.com/kai-scheduler/api/utilities/resources"
)

const (
	PodGPUGroupIndex         = "kai.gpuGroups"
	BindRequestGPUGroupIndex = "kai.selectedGPUGroupIDs"
)

// RegisterGPUGroupIndexes indexes the objects that can hold a GPU reservation.
func RegisterGPUGroupIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	if err := indexer.IndexField(ctx, &v1.Pod{}, PodGPUGroupIndex, PodGPUGroups); err != nil {
		return fmt.Errorf("index Pods by GPU group: %w", err)
	}
	if err := indexer.IndexField(ctx, &schedulingv1alpha2.BindRequest{}, BindRequestGPUGroupIndex, BindRequestGPUGroups); err != nil {
		return fmt.Errorf("index BindRequests by GPU group: %w", err)
	}
	return nil
}

// PodGPUGroups returns the Pod's primary GPU group and valid additional GPU groups.
func PodGPUGroups(obj client.Object) []string {
	labels := obj.GetLabels()
	var groups []string
	if group, ok := labels[constants.GPUGroup]; ok {
		groups = append(groups, group)
	}
	for key, group := range labels {
		expectedKey, _ := resources.GetMultiFractionGpuGroupLabel(group)
		if key == expectedKey {
			groups = append(groups, group)
		}
	}
	return groups
}

// BindRequestGPUGroups returns the fractional GPU groups selected for the bind.
func BindRequestGPUGroups(obj client.Object) []string {
	return obj.(*schedulingv1alpha2.BindRequest).Spec.SelectedFractionalGpuGroupIDs()
}
