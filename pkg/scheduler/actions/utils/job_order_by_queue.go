// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/scheduler_util"
)

// queueNode represents a node in the queue hierarchy tree.
// Each node contains either child queue nodes (for non-leaf queues) or jobs (for leaf queues).
// This structure supports n-level queue hierarchies.
type queueNode struct {
	queue        *queue_info.QueueInfo
	children     *scheduler_util.PriorityQueue // Contains *queueNode (for non-leaf) or *PodGroupInfo (for leaf)
	needsReorder bool
	parent       *queueNode // nil for root-level nodes
	isLeaf       bool       // true if children contains jobs, false if it contains queue nodes
}

// JobsOrderByQueues manages job ordering across an n-level queue hierarchy.
// The hierarchy is represented as a tree of queueNode objects.
type JobsOrderByQueues struct {
	ssn     *framework.Session
	options JobsOrderInitOptions

	rootNodes  *scheduler_util.PriorityQueue      // Top-level queue nodes (nodes with no parent)
	queueNodes map[common_info.QueueID]*queueNode // All queue nodes by ID for quick lookup

	poppedJobsByQueue map[common_info.QueueID][]*podgroup_info.PodGroupInfo
}

func NewJobsOrderByQueues(ssn *framework.Session, options JobsOrderInitOptions) JobsOrderByQueues {
	return JobsOrderByQueues{
		ssn:               ssn,
		options:           options,
		queueNodes:        map[common_info.QueueID]*queueNode{},
		poppedJobsByQueue: map[common_info.QueueID][]*podgroup_info.PodGroupInfo{},
	}
}

func (jo *JobsOrderByQueues) IsEmpty() bool {
	return jo.rootNodes == nil || jo.rootNodes.Empty()
}

func (jo *JobsOrderByQueues) Len() int {
	count := 0
	for _, node := range jo.queueNodes {
		if node.isLeaf {
			count += node.children.Len()
		}
	}
	return count
}

func (jo *JobsOrderByQueues) PopNextJob() *podgroup_info.PodGroupInfo {
	if jo.IsEmpty() {
		log.InfraLogger.V(7).Infof("No active queues")
		return nil
	}

	log.InfraLogger.V(7).Infof("PopNextJob: rootNodes.Len()=%d, queueNodes count=%d",
		jo.rootNodes.Len(), len(jo.queueNodes))

	// Traverse down the tree to find the best leaf node
	leafNode := jo.traverseToLeaf(jo.rootNodes)
	if leafNode == nil {
		log.InfraLogger.V(7).Warnf("PopNextJob: traverseToLeaf returned nil")
		return nil
	}

	// Pop the job from the leaf
	job := leafNode.children.Pop().(*podgroup_info.PodGroupInfo)

	if jo.options.VictimQueue {
		jo.poppedJobsByQueue[leafNode.queue.UID] = append(jo.poppedJobsByQueue[leafNode.queue.UID], job)
	}

	// Handle cleanup and bubble up needsReorder
	jo.handlePopFromNode(leafNode)

	log.InfraLogger.V(7).Infof("Popped job: %v", job.Name)
	return job
}

func (jo *JobsOrderByQueues) PushJob(job *podgroup_info.PodGroupInfo) {
	leafQueueInfo := jo.ssn.ClusterInfo.Queues[job.Queue]

	if !leafQueueInfo.IsLeafQueue() {
		log.InfraLogger.V(7).Warnf("PushJob: job <%v> is targeting a non-leaf queue <%v>", job.Name, leafQueueInfo.Name)
		return
	}

	// Check if leaf node already exists
	leafNode, found := jo.queueNodes[job.Queue]
	needsLinking := !found

	if needsLinking {
		leafNode = jo.createLeafNode(leafQueueInfo)
		jo.queueNodes[job.Queue] = leafNode
	}

	// Push job first (before linking) so ordering comparisons have valid data
	leafNode.children.Push(job)

	// Only link the tree if this is a new node
	if needsLinking {
		jo.ensureAncestorChainForPush(leafNode, leafQueueInfo)
	}

	// Mark ancestors for reordering
	jo.markAncestorsForReorder(leafNode)

	log.InfraLogger.V(7).Infof("Pushed job: %v for queue %v", job.Name, leafQueueInfo.Name)
}

// isRootQueue returns true if the queue has no parent (is at the root level).
func (jo *JobsOrderByQueues) isRootQueue(queue *queue_info.QueueInfo) bool {
	return queue.ParentQueue == ""
}

// ensureRootNodesInitialized ensures the rootNodes priority queue is initialized.
func (jo *JobsOrderByQueues) ensureRootNodesInitialized() {
	if jo.rootNodes == nil {
		jo.rootNodes = scheduler_util.NewPriorityQueue(
			jo.buildNodeOrderFn(jo.options.VictimQueue),
			scheduler_util.QueueCapacityInfinite)
	}
}

// ensureAncestorChainForPush creates all ancestor nodes for a node up to the root.
// Used by PushJob to build the tree dynamically.
func (jo *JobsOrderByQueues) ensureAncestorChainForPush(childNode *queueNode, childQueue *queue_info.QueueInfo) {
	if jo.isRootQueue(childQueue) {
		// Child is at root level - add to rootNodes if not already there.
		// The parent==nil check prevents duplicate additions: if parent is already set,
		// this node was already added to rootNodes in a previous call.
		if childNode.parent == nil {
			jo.ensureRootNodesInitialized()
			jo.rootNodes.Push(childNode)
		}
		return
	}

	parentQueueInfo, parentExists := jo.ssn.ClusterInfo.Queues[childQueue.ParentQueue]
	if !parentExists {
		log.InfraLogger.V(7).Warnf("Queue's parent doesn't exist. Queue: <%v>, Parent: <%v>",
			childQueue.Name, childQueue.ParentQueue)
		return
	}

	// Get or create parent node
	parentNode := jo.queueNodes[parentQueueInfo.UID]
	parentNodeIsNew := parentNode == nil
	if parentNodeIsNew {
		parentNode = jo.createNonLeafNode(parentQueueInfo)
		jo.queueNodes[parentQueueInfo.UID] = parentNode
	}

	// Link child to parent if not already linked
	if childNode.parent == nil {
		childNode.parent = parentNode
		parentNode.children.Push(childNode)
	}

	// Only recurse up the ancestor chain if we created a new parent node.
	// If the parent already existed, it's already linked to its ancestors.
	if parentNodeIsNew {
		jo.ensureAncestorChainForPush(parentNode, parentQueueInfo)
	}
}

// traverseToLeaf recursively traverses from a priority queue of nodes down to the best leaf node.
func (jo *JobsOrderByQueues) traverseToLeaf(pq *scheduler_util.PriorityQueue) *queueNode {
	node := jo.getNextNode(pq)
	if node == nil {
		return nil
	}

	if node.isLeaf {
		return node
	}

	// Recurse into children
	return jo.traverseToLeaf(node.children)
}

// getNextNode retrieves the next node from a priority queue, handling reordering as needed.
func (jo *JobsOrderByQueues) getNextNode(pq *scheduler_util.PriorityQueue) *queueNode {
	if pq.Empty() {
		return nil
	}

	node := pq.Peek().(*queueNode)

	if node.needsReorder {
		pq.Fix(0)
		node.needsReorder = false
		return jo.getNextNode(pq)
	}

	if node.children.Empty() {
		// This should never happen as we prune empty nodes from the tree on handlePopFromNode.
		log.InfraLogger.V(7).Warnf("Queue node <%v> is active but has no children", node.queue.Name)
		return nil
	}

	log.InfraLogger.V(7).Infof("Selected queue: %v (isLeaf=%v)", node.queue.Name, node.isLeaf)
	return node
}

// handlePopFromNode handles cleanup after popping a job from a leaf node.
// If the node becomes empty, it's removed from its parent. Otherwise, ancestors are marked for reorder.
func (jo *JobsOrderByQueues) handlePopFromNode(node *queueNode) {
	if node.children.Len() == 0 {
		// Remove this node from its parent
		jo.removeNodeFromParent(node)
		delete(jo.queueNodes, node.queue.UID)

		// If parent is now empty, recursively clean up
		if node.parent != nil {
			jo.handlePopFromNode(node.parent)
		}
		return
	}

	jo.markAncestorsForReorder(node)
}

// removeNodeFromParent removes a node from its parent's children priority queue.
// this assumes the node requested for removal is at the top of its parent's priority queue.
func (jo *JobsOrderByQueues) removeNodeFromParent(node *queueNode) {
	if node.parent != nil {
		node.parent.children.Pop()
	} else {
		jo.rootNodes.Pop()
	}
}

// markAncestorsForReorder marks all ancestors of a node (including itself) as needing reorder.
func (jo *JobsOrderByQueues) markAncestorsForReorder(node *queueNode) {
	for current := node; current != nil; current = current.parent {
		current.needsReorder = true
	}
}

// createLeafNode creates a new leaf node that will contain jobs.
func (jo *JobsOrderByQueues) createLeafNode(queue *queue_info.QueueInfo) *queueNode {
	return &queueNode{
		queue: queue,
		children: scheduler_util.NewPriorityQueue(func(l, r interface{}) bool {
			if jo.options.VictimQueue {
				return !jo.ssn.JobOrderFn(l, r)
			}
			return jo.ssn.JobOrderFn(l, r)
		}, jo.options.MaxJobsQueueDepth),
		isLeaf: true,
	}
}

// createNonLeafNode creates a new non-leaf node that will contain child queue nodes.
func (jo *JobsOrderByQueues) createNonLeafNode(queue *queue_info.QueueInfo) *queueNode {
	return &queueNode{
		queue: queue,
		children: scheduler_util.NewPriorityQueue(
			jo.buildNodeOrderFn(jo.options.VictimQueue),
			scheduler_util.QueueCapacityInfinite,
		),
		isLeaf: false,
	}
}

// buildNodeOrderFn creates a comparison function for ordering queue nodes.
// It compares nodes based on their best descendant job (recursively for non-leaf nodes).
func (jo *JobsOrderByQueues) buildNodeOrderFn(reverseOrder bool) func(interface{}, interface{}) bool {
	return func(l, r interface{}) bool {
		lNode := l.(*queueNode)
		rNode := r.(*queueNode)

		if lNode.children.Empty() {
			log.InfraLogger.V(7).Infof("Queue node %v has no children", lNode.queue.Name)
			return !reverseOrder
		}

		if rNode.children.Empty() {
			log.InfraLogger.V(7).Infof("Queue node %v has no children", rNode.queue.Name)
			return reverseOrder
		}

		// Get the best job from each subtree for comparison
		lPending, lVictims := jo.getBestJobFromNode(lNode)
		rPending, rVictims := jo.getBestJobFromNode(rNode)

		result := jo.ssn.QueueOrderFn(lNode.queue, rNode.queue, lPending, rPending, lVictims, rVictims)
		if reverseOrder {
			return !result
		}
		return result
	}
}

// getBestJobFromNode recursively finds the best job in a node's subtree.
// Returns (pending, victims) where exactly one is populated based on VictimQueue option.
func (jo *JobsOrderByQueues) getBestJobFromNode(node *queueNode) (*podgroup_info.PodGroupInfo, []*podgroup_info.PodGroupInfo) {
	if node.isLeaf {
		return jo.extractJobsForComparison(node.queue.UID, node)
	}

	// For non-leaf nodes, get the best child and recurse
	bestChild := node.children.Peek().(*queueNode)
	return jo.getBestJobFromNode(bestChild)
}

// extractJobsForComparison extracts the pending job or victims from a leaf node for comparison.
func (jo *JobsOrderByQueues) extractJobsForComparison(
	queueID common_info.QueueID,
	node *queueNode,
) (*podgroup_info.PodGroupInfo, []*podgroup_info.PodGroupInfo) {
	if node.children.Empty() {
		log.InfraLogger.V(7).Warnf("extractJobsForComparison: node %v has no children", node.queue.Name)
		return nil, nil
	}

	if jo.options.VictimQueue {
		return nil, jo.getVictimsForQueue(queueID, node)
	}

	pending := node.children.Peek().(*podgroup_info.PodGroupInfo)
	return pending, nil
}

// getVictimsForQueue returns all popped jobs plus the next job in queue for victim ordering.
func (jo *JobsOrderByQueues) getVictimsForQueue(queueID common_info.QueueID, node *queueNode) []*podgroup_info.PodGroupInfo {
	victims := jo.poppedJobsByQueue[queueID]
	if node.children.Empty() {
		log.InfraLogger.V(7).Warnf("getVictimsForQueue: node %v has no children", node.queue.Name)
		return victims
	}
	nextJob := node.children.Peek().(*podgroup_info.PodGroupInfo)
	return append(victims, nextJob)
}
