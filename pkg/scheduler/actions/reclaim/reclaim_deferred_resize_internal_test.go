// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

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

func TestIsEligibleForDeferredResizeUsesCanReclaimResourcesWithoutPendingPods(t *testing.T) {
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

	canReclaim := false
	var gotJob *podgroup_info.PodGroupInfo
	ssn.AddCanReclaimResourcesFn(func(job *podgroup_info.PodGroupInfo) bool {
		gotJob = job
		return canReclaim
	})

	require.False(t, isEligibleForDeferredResize(ssn)(reclaimer, []*pod_info.PodInfo{resizing}))
	require.Equal(t, reclaimer.UID, gotJob.UID)
	require.Zero(t, gotJob.GetNumPendingTasks(), "pending pods' demand must not count")
	require.Contains(t, gotJob.GetAllPodsMap(), resizing.UID)

	canReclaim = true
	require.True(t, isEligibleForDeferredResize(ssn)(reclaimer, []*pod_info.PodInfo{resizing}))
}

func TestDeferredResizeSolverStartsJobSolution(t *testing.T) {
	ssn, reclaimer, _, _ := newVictimQueueTestSession()
	calls := 0
	ssn.AddOnJobSolutionStartFn(func() { calls++ })

	solver := newDeferredResizeSolver(ssn, reclaimer, nil)(&node_info.NodeInfo{Name: "node-0"})
	require.NotNil(t, solver)
	require.Equal(t, 1, calls)
}
