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

func TestUpdateQueueHierarchyRemovesParentCycles(t *testing.T) {
	tests := []struct {
		name         string
		queues       map[common_info.QueueID]string // queue -> parent queue
		wantRemained []common_info.QueueID
	}{
		{
			name: "queue that is its own parent",
			queues: map[common_info.QueueID]string{
				"self":  "self",
				"root":  "",
				"child": "root",
			},
			wantRemained: []common_info.QueueID{"root", "child"},
		},
		{
			name: "two queues that are each other's parent",
			queues: map[common_info.QueueID]string{
				"a":     "b",
				"b":     "a",
				"root":  "",
				"child": "root",
			},
			wantRemained: []common_info.QueueID{"root", "child"},
		},
		{
			name: "queues below a cycle are removed with it",
			queues: map[common_info.QueueID]string{
				"a":          "b",
				"b":          "a",
				"below-a":    "a",
				"below-leaf": "below-a",
				"root":       "",
				"child":      "root",
			},
			wantRemained: []common_info.QueueID{"root", "child"},
		},
		{
			name: "longer cycle",
			queues: map[common_info.QueueID]string{
				"a":    "b",
				"b":    "c",
				"c":    "a",
				"root": "",
			},
			wantRemained: []common_info.QueueID{"root"},
		},
		{
			name: "valid hierarchy is untouched",
			queues: map[common_info.QueueID]string{
				"root":  "",
				"mid":   "root",
				"leaf1": "mid",
				"leaf2": "mid",
			},
			wantRemained: []common_info.QueueID{"root", "mid", "leaf1", "leaf2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queues := map[common_info.QueueID]*queue_info.QueueInfo{}
			for id, parent := range tt.queues {
				queues[id] = &queue_info.QueueInfo{UID: id, Name: string(id), ParentQueue: common_info.QueueID(parent)}
			}

			UpdateQueueHierarchy(queues)

			remained := make([]common_info.QueueID, 0, len(queues))
			for id := range queues {
				remained = append(remained, id)
			}
			assert.ElementsMatch(t, tt.wantRemained, remained)

			// Scheduler plugins walk ParentQueue links until they run out of parents, so
			// every remaining queue must reach a root within len(queues) steps.
			for id := range queues {
				current, steps := id, 0
				for queue, found := queues[current]; found && queue.ParentQueue != ""; queue, found = queues[current] {
					current = queue.ParentQueue
					steps++
					if !assert.LessOrEqual(t, steps, len(queues), "queue %s never reaches a root queue", id) {
						break
					}
				}
			}
		})
	}
}
