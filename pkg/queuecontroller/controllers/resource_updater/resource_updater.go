// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_updater

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/resources"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/common"
)

type ResourceUpdater struct {
	client.Client
}

func (ru *ResourceUpdater) UpdateQueue(ctx context.Context, queue *v2.Queue) error {
	queue.Status.Requested = v1.ResourceList{}
	queue.Status.Allocated = v1.ResourceList{}
	queue.Status.AllocatedNonPreemptible = v1.ResourceList{}

	err := ru.sumChildQueueResources(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to update queue resources status: %v", err)
	}

	err = ru.sumPodGroupsResources(ctx, queue)
	if err != nil {
		return fmt.Errorf("failed to update queue resources status: %v", err)
	}

	return nil
}

func (ru *ResourceUpdater) sumChildQueueResources(ctx context.Context, queue *v2.Queue) error {
	children := v2.QueueList{}
	err := ru.Client.List(ctx, &children, client.MatchingFields{common.ParentQueueIndexName: queue.Name})
	if err != nil {
		return err
	}

	for _, q := range children.Items {
		if q.Spec.ParentQueue != queue.Name {
			continue
		}

		queue.Status.Allocated = resources.SumResources(q.Status.Allocated, queue.Status.Allocated)
		queue.Status.AllocatedNonPreemptible = resources.SumResources(q.Status.AllocatedNonPreemptible, queue.Status.AllocatedNonPreemptible)
		queue.Status.Requested = resources.SumResources(q.Status.Requested, queue.Status.Requested)

	}
	return nil
}

func (ru *ResourceUpdater) sumPodGroupsResources(ctx context.Context, queue *v2.Queue) error {
	queuePodGroups := v2alpha2.PodGroupList{}
	err := ru.Client.List(ctx, &queuePodGroups, client.MatchingFields{
		common.PodGroupQueueIndexName: queue.Name,
	})
	if err != nil {
		return err
	}

	for _, pg := range queuePodGroups.Items {
		queue.Status.Allocated = resources.SumResources(pg.Status.ResourcesStatus.Allocated, queue.Status.Allocated)
		queue.Status.AllocatedNonPreemptible = resources.SumResources(pg.Status.ResourcesStatus.AllocatedNonPreemptible,
			queue.Status.AllocatedNonPreemptible)
		queue.Status.Requested = resources.SumResources(pg.Status.ResourcesStatus.Requested, queue.Status.Requested)
	}

	return nil
}
