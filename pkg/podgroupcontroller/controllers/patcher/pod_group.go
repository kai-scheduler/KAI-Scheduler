// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package patcher

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/metadata"
)

func ShouldUpdatePodGroupStatus(
	podGroup *v2alpha2.PodGroup, podGroupMetadata *metadata.PodGroupMetadata,
) bool {
	updatedStatus := getStatusWithMetadata(podGroupMetadata, podGroup.Status)
	return !reflect.DeepEqual(&podGroup.Status, updatedStatus)
}

func UpdatePodGroupStatus(
	ctx context.Context, podGroup *v2alpha2.PodGroup, podGroupMetadata *metadata.PodGroupMetadata,
	kubeClient client.Client,
) error {
	updatedStatus := getStatusWithMetadata(podGroupMetadata, podGroup.Status)

	return kubeClient.Status().Patch(
		ctx,
		&v2alpha2.PodGroup{
			TypeMeta:   podGroup.TypeMeta,
			ObjectMeta: podGroup.ObjectMeta,
			Status:     *updatedStatus,
		}, client.MergeFrom(podGroup))
}

func getStatusWithMetadata(
	metaData *metadata.PodGroupMetadata, originalStatus v2alpha2.PodGroupStatus,
) *v2alpha2.PodGroupStatus {
	updatedStatus := originalStatus.DeepCopy()

	updatedStatus.ResourcesStatus.Requested = metaData.Requested
	updatedStatus.ResourcesStatus.Allocated = metaData.Allocated
	if !metaData.Preemptible {
		updatedStatus.ResourcesStatus.AllocatedNonPreemptible = metaData.Allocated
	}

	return updatedStatus
}
