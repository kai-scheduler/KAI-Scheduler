// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_info

import (
	"testing"
	"time"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

func TestNewQueueInfo(t *testing.T) {
	tests := []struct {
		name     string
		queue    *enginev2.Queue
		expected QueueInfo
	}{
		{
			name: "queue with display name",
			queue: &enginev2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "queue",
				},
				Spec: enginev2.QueueSpec{
					DisplayName: "display-queue",
					ParentQueue: "",
					Resources:   nil,
					Priority:    nil,
				},
			},
			expected: QueueInfo{
				UID:               "queue",
				Name:              "display-queue",
				ParentQueue:       "",
				ChildQueues:       []common_info.QueueID{},
				Resources:         QueueQuota{},
				Priority:          100,
				CreationTimestamp: metav1.Time{},
			},
		},
		{
			name: "queue without display name",
			queue: &enginev2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "queue",
				},
				Spec: enginev2.QueueSpec{
					DisplayName: "",
					ParentQueue: "",
					Resources:   nil,
					Priority:    nil,
				},
			},
			expected: QueueInfo{
				UID:               "queue",
				Name:              "queue",
				ParentQueue:       "",
				ChildQueues:       []common_info.QueueID{},
				Resources:         QueueQuota{},
				Priority:          100,
				CreationTimestamp: metav1.Time{},
			},
		},
		{
			name: "queue with priority",
			queue: &enginev2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "queue",
				},
				Spec: enginev2.QueueSpec{
					DisplayName: "",
					ParentQueue: "",
					Resources:   nil,
					Priority:    pointer.Int(6),
				},
			},
			expected: QueueInfo{
				UID:               "queue",
				Name:              "queue",
				ParentQueue:       "",
				ChildQueues:       []common_info.QueueID{},
				Resources:         QueueQuota{},
				Priority:          6,
				CreationTimestamp: metav1.Time{},
			},
		},
		{
			name: "queue with min-runtime",
			queue: &enginev2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "queue",
				},
				Spec: enginev2.QueueSpec{
					DisplayName:       "",
					ParentQueue:       "",
					Resources:         nil,
					PreemptMinRuntime: &metav1.Duration{Duration: 10 * time.Minute},
					ReclaimMinRuntime: &metav1.Duration{Duration: 10 * time.Minute},
				},
			},
			expected: QueueInfo{
				UID:               "queue",
				Name:              "queue",
				ParentQueue:       "",
				ChildQueues:       []common_info.QueueID{},
				Resources:         QueueQuota{},
				Priority:          100,
				CreationTimestamp: metav1.Time{},
				PreemptMinRuntime: &metav1.Duration{Duration: 10 * time.Minute},
				ReclaimMinRuntime: &metav1.Duration{Duration: 10 * time.Minute},
			},
		},
		{
			name: "queue with parent",
			queue: &enginev2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "queue",
				},
				Spec: enginev2.QueueSpec{
					DisplayName: "",
					ParentQueue: "daddy",
					Resources:   nil,
					Priority:    nil,
				},
			},
			expected: QueueInfo{
				UID:               "queue",
				Name:              "queue",
				ParentQueue:       "daddy",
				ChildQueues:       []common_info.QueueID{},
				Resources:         QueueQuota{},
				Priority:          100,
				CreationTimestamp: metav1.Time{},
			},
		},
		{
			name: "queue with resources",
			queue: &enginev2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "queue",
				},
				Spec: enginev2.QueueSpec{
					DisplayName: "",
					ParentQueue: "daddy",
					Resources: &enginev2.QueueResources{
						GPU: enginev2.QueueResource{
							Quota:           1,
							OverQuotaWeight: 2,
							Limit:           3,
						},
						CPU: enginev2.QueueResource{
							Quota:           4,
							OverQuotaWeight: 5,
							Limit:           6,
						},
						Memory: enginev2.QueueResource{
							Quota:           7,
							OverQuotaWeight: 8,
							Limit:           9,
						},
					},
					Priority: nil,
				},
			},
			expected: QueueInfo{
				UID:         "queue",
				Name:        "queue",
				ParentQueue: "daddy",
				ChildQueues: []common_info.QueueID{},
				Resources: QueueQuota{
					GPU: ResourceQuota{
						Quota:           1,
						OverQuotaWeight: 2,
						Limit:           3,
					},
					CPU: ResourceQuota{
						Quota:           4,
						OverQuotaWeight: 5,
						Limit:           6,
					},
					Memory: ResourceQuota{
						Quota:           7,
						OverQuotaWeight: 8,
						Limit:           9,
					},
				},
				Priority:          100,
				CreationTimestamp: metav1.Time{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queueInfo := NewQueueInfo(tt.queue)
			assert.DeepEqual(t, tt.expected, *queueInfo)
		})
	}
}
