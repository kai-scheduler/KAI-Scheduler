// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package childqueues_updater

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/queuecontroller/common"
)

type ChildQueuesUpdater struct {
	client.Client
}

func (ru *ChildQueuesUpdater) UpdateQueue(ctx context.Context, queue *v2.Queue) error {
	childrenQueue := &v2.QueueList{}
	err := ru.List(ctx, childrenQueue, client.MatchingFields{common.ParentQueueIndexName: queue.Name})
	if err != nil {
		return fmt.Errorf("failed to list child queues: %v", err)
	}

	var childrenQueueNames []string
	for _, childQueue := range childrenQueue.Items {
		if childQueue.Spec.ParentQueue != queue.Name {
			continue
		}

		childrenQueueNames = append(childrenQueueNames, childQueue.Name)
	}
	queue.Status.ChildQueues = childrenQueueNames

	return nil
}
