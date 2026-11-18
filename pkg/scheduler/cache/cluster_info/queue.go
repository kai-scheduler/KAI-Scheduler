// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"fmt"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
)

const (
	defaultQueueName = "default"
)

func (c *ClusterInfo) getDefaultParentQueue() *queue_info.QueueInfo {
	queue := &enginev2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:              defaultQueueName,
			CreationTimestamp: metav1.Now(),
		},
		Spec: enginev2.QueueSpec{
			Resources: &enginev2.QueueResources{
				GPU: enginev2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
				CPU: enginev2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
				Memory: enginev2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
			},
		},
	}
	return queue_info.NewQueueInfo(queue)
}

func (c *ClusterInfo) snapshotQueues() (map[common_info.QueueID]*queue_info.QueueInfo, error) {
	queues, err := c.dataLister.ListQueues()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error listing queues: %w", err))
		return nil, err
	}

	result := map[common_info.QueueID]*queue_info.QueueInfo{}
	if c.fairnessLevelType == FullFairness {
		for _, queue := range queues {
			queueInfo := queue_info.NewQueueInfo(queue)
			result[queueInfo.UID] = queueInfo
		}
	} else if c.fairnessLevelType == ProjectLevelFairness {
		defaultParentQueue := c.getDefaultParentQueue()
		result[defaultParentQueue.UID] = defaultParentQueue

		for _, queue := range queues {
			if len(queue.Spec.ParentQueue) > 0 {
				queue.Spec.ParentQueue = defaultQueueName
				queueInfo := queue_info.NewQueueInfo(queue)
				result[queueInfo.UID] = queueInfo
			}
		}
	}

	return result, nil
}

func (c *ClusterInfo) snapshotQueueResourceUsage() (*queue_info.ClusterUsage, error) {
	if !c.collectUsageData {
		return nil, nil
	}

	return c.dataLister.ListResourceUsage()
}

// UpdateQueueHierarchy iterates over a map containing multiple levels of queue hierarchies, and updates queues with
// child queues where relevant
func UpdateQueueHierarchy(queues map[common_info.QueueID]*queue_info.QueueInfo) {
	removeQueueParentCycles(queues)
	updateQueueChildren(queues)
	cleanQueueOrphans(queues)
}

// removeQueueParentCycles deletes queues whose chain of parent queues never reaches a root queue, because it loops
// back on itself (for example a queue that is its own parent, or two queues that are each other's parent). Code that
// walks from a queue up to its root would otherwise never finish. Queues below a removed queue lose their parent and
// are then removed as orphans.
func removeQueueParentCycles(queues map[common_info.QueueID]*queue_info.QueueInfo) {
	var cyclic []common_info.QueueID
	for queueId := range queues {
		if parentChainHasCycle(queues, queueId) {
			cyclic = append(cyclic, queueId)
		}
	}
	for _, queueId := range cyclic {
		log.InfraLogger.V(2).Warnf("Found queue %s in a parent queue cycle, deleting it", queueId)
		delete(queues, queueId)
	}
}

func parentChainHasCycle(queues map[common_info.QueueID]*queue_info.QueueInfo, queueId common_info.QueueID) bool {
	visited := map[common_info.QueueID]struct{}{}
	for current := queueId; current != ""; {
		if _, seen := visited[current]; seen {
			return true
		}
		visited[current] = struct{}{}

		queue, found := queues[current]
		if !found {
			return false
		}
		current = queue.ParentQueue
	}
	return false
}

func updateQueueChildren(queues map[common_info.QueueID]*queue_info.QueueInfo) {
	for queueId, queue := range queues {
		if queue.ParentQueue != "" {
			if parent, found := queues[queue.ParentQueue]; found {
				parent.AddChildQueue(queueId)
			}
		}
	}
}

func cleanQueueOrphans(queues map[common_info.QueueID]*queue_info.QueueInfo) {
	for queueId, queue := range queues {
		if queue.ParentQueue != "" {
			if _, found := queues[queue.ParentQueue]; !found {
				log.InfraLogger.V(2).Warnf("Found orphan queue %s with missing parent %s, deleting it and all children",
					queueId, queue.ParentQueue)
				deleteQueueAndChildren(queues, queueId)
			}
		}
	}
}

func deleteQueueAndChildren(queues map[common_info.QueueID]*queue_info.QueueInfo, queueID common_info.QueueID) {
	queue, found := queues[queueID]
	if !found {
		log.InfraLogger.V(2).Warnf("Could not find queue %s in queues map when deleting children", queueID)
		return
	}

	log.InfraLogger.V(6).Do(func() {
		log.InfraLogger.Infof("Deleting queue %s and children %s", queueID, queue.ChildQueues)
	})
	for _, child := range queue.ChildQueues {
		deleteQueueAndChildren(queues, child)
	}
	delete(queues, queueID)
}
