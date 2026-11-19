// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info/data_lister"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
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

func TestSnapshotQueuesProjectLevelFairnessKeepsListedQueuesUntouched(t *testing.T) {
	listedQueues := []*enginev2.Queue{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "department"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "project"},
			Spec:       enginev2.QueueSpec{ParentQueue: "department"},
		},
	}

	controller := gomock.NewController(t)
	lister := data_lister.NewMockDataLister(controller)
	lister.EXPECT().ListQueues().Return(listedQueues, nil)
	clusterInfo := &ClusterInfo{dataLister: lister, fairnessLevelType: ProjectLevelFairness}

	queues, err := clusterInfo.snapshotQueues()

	assert.NoError(t, err)
	// In project-level fairness every project hangs under the synthetic default queue.
	assert.Equal(t, common_info.QueueID(defaultQueueName), queues["project"].ParentQueue)
	// The listed queues are shared with the informer cache and must not be modified by the snapshot.
	assert.Equal(t, "department", listedQueues[1].Spec.ParentQueue)
}
