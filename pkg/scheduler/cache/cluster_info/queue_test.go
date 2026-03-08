// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
)

func TestUpdateQueueHierarchySetChildQueues(t *testing.T) {
	queues := map[common_info.QueueID]*queue_info.QueueInfo{
		"queue1": {
			Name:        "queue1",
			ParentQueue: "dep1",
		},
		"dep1": {
			Name:        "queue1",
			ParentQueue: "",
		},
		"queue2": {
			Name:        "queue2",
			ParentQueue: "dep2",
		},
		"queue3": {
			Name:        "queue3",
			ParentQueue: "dep2",
		},
		"dep2": {
			Name:        "dep2",
			ParentQueue: "",
		},
		"dep3": {
			Name:        "dep3",
			ParentQueue: "",
		},
		"orphan": {
			Name:        "orphan",
			ParentQueue: "unexisting",
		},
	}

	UpdateQueueHierarchy(queues)
	assert.Equal(t, 6, len(queues))
	assert.Equal(t, []common_info.QueueID{"queue1"}, queues["dep1"].ChildQueues)
	assert.ElementsMatch(t, []common_info.QueueID{"queue2", "queue3"}, queues["dep2"].ChildQueues)
	assert.Equal(t, []common_info.QueueID(nil), queues["dep3"].ChildQueues)
	_, foundOrphan := queues["orphan"]
	assert.False(t, foundOrphan)
}
