// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func TestOrderedVictimsQueueOnNodeKeepsOnlyJobsOnTheNode(t *testing.T) {
	ssn, reclaimer, onNode, otherNode := newVictimQueueTestSession()
	for _, task := range otherNode.GetAllPodsMap() {
		task.NodeName = "node-1"
	}
	ssn.AddReclaimVictimFilterFn(func(_, _ *podgroup_info.PodGroupInfo) bool { return true })

	queue := getOrderedVictimsQueueOnNode(ssn, reclaimer, "node-0")()
	require.Same(t, onNode, queue.PopNextJob())
	require.True(t, queue.IsEmpty(), "same-queue and other-node jobs are not victims")

	queue = getOrderedVictimsQueueOnNode(ssn, reclaimer, "node-1")()
	require.Same(t, otherNode, queue.PopNextJob())
	require.True(t, queue.IsEmpty())
}

func TestSolveDeferredResizeChecksCanReclaimResourcesWithoutPendingPods(t *testing.T) {
	ssn, reclaimer, _, _ := newVictimQueueTestSession()
	addTask := func(name, nodeName string, phase v1.PodPhase) *pod_info.PodInfo {
		pod := common_info.BuildPod("victim-queue-test", name, nodeName, phase,
			common_info.BuildResourceList("2", "1Gi"), nil, nil, nil)
		task := pod_info.NewTaskInfo(pod, resource_info.NewResourceVectorMap())
		task.Job = reclaimer.UID
		reclaimer.AddTaskInfo(task)
		return task
	}
	resizing := addTask("resizing-pod", "node-0", v1.PodRunning)
	addTask("pending-pod", "", v1.PodPending)
	require.Equal(t, 1, reclaimer.GetNumPendingTasks())
	node := &node_info.NodeInfo{Name: "node-0"}

	canReclaim := false
	var gotJob *podgroup_info.PodGroupInfo
	ssn.AddCanReclaimResourcesFn(func(job *podgroup_info.PodGroupInfo) bool {
		gotJob = job
		return canReclaim
	})
	solutionStarts := 0
	ssn.AddOnJobSolutionStartFn(func() { solutionStarts++ })

	stmt, result := solveDeferredResize(ssn, nil)(reclaimer, node, resizing)
	require.Nil(t, stmt)
	require.Nil(t, result, "the solver must not run")
	require.Zero(t, solutionStarts)
	require.Equal(t, reclaimer.UID, gotJob.UID)
	require.Zero(t, gotJob.GetNumPendingTasks(), "pending pods' demand must not count")
	require.Contains(t, gotJob.GetAllPodsMap(), resizing.UID)

	canReclaim = true
	stmt, result = solveDeferredResize(ssn, nil)(reclaimer, node, resizing)
	require.Nil(t, stmt)
	require.Equal(t, solvers.SearchResultNoGenerator, result.Reason(), "the solver ran")
	require.Equal(t, 1, solutionStarts)
}

// A job queued only for a deferred resize must not reach the pending path, where it would be
// recorded as a failed job with no demand and make every later job in its queue look harder.
func TestReclaimForPendingTasksSkipsJobWithoutPendingTasks(t *testing.T) {
	ssn, _, running, _ := newVictimQueueTestSession()
	require.Zero(t, running.GetNumPendingTasks())
	reclaimChecks := 0
	ssn.AddCanReclaimResourcesFn(func(*podgroup_info.PodGroupInfo) bool {
		reclaimChecks++
		return true
	})

	smallestFailedJobsByQueue := map[common_info.QueueID]*common.MinimalJobRepresentatives{}
	require.False(t, New().reclaimForPendingTasks(ssn, running, nil, smallestFailedJobsByQueue))
	require.Empty(t, smallestFailedJobsByQueue)
	require.Zero(t, reclaimChecks)
}
