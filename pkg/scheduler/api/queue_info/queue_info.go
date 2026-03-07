/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_info

import (
	"golang.org/x/exp/slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enginev2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
)

type QueueInfo struct {
	UID               common_info.QueueID
	Name              string
	ParentQueue       common_info.QueueID
	ChildQueues       []common_info.QueueID
	Resources         QueueQuota
	ResourceUsage     QueueUsage
	Priority          int
	CreationTimestamp metav1.Time
	PreemptMinRuntime *metav1.Duration
	ReclaimMinRuntime *metav1.Duration
}

func NewQueueInfo(queue *enginev2.Queue) *QueueInfo {
	queueName := queue.Name
	if queue.Spec.DisplayName != "" {
		queueName = queue.Spec.DisplayName
	}

	priority := commonconstants.DefaultQueuePriority
	if queue.Spec.Priority != nil {
		priority = *queue.Spec.Priority
	}

	return &QueueInfo{
		UID:               common_info.QueueID(queue.Name),
		Name:              queueName,
		ParentQueue:       common_info.QueueID(queue.Spec.ParentQueue),
		ChildQueues:       []common_info.QueueID{},
		Resources:         getQueueQuota(*queue),
		Priority:          priority,
		CreationTimestamp: queue.CreationTimestamp,
		PreemptMinRuntime: queue.Spec.PreemptMinRuntime,
		ReclaimMinRuntime: queue.Spec.ReclaimMinRuntime,
	}
}

func (q *QueueInfo) IsLeafQueue() bool {
	return len(q.ChildQueues) == 0
}

func (q *QueueInfo) AddChildQueue(queue common_info.QueueID) {
	if slices.Contains(q.ChildQueues, queue) {
		return
	}
	q.ChildQueues = append(q.ChildQueues, queue)
}

func getQueueQuota(queue enginev2.Queue) QueueQuota {
	if queue.Spec.Resources == nil {
		return QueueQuota{}
	}

	return QueueQuota{
		GPU:    ResourceQuota(queue.Spec.Resources.GPU),
		CPU:    ResourceQuota(queue.Spec.Resources.CPU),
		Memory: ResourceQuota(queue.Spec.Resources.Memory),
	}
}
