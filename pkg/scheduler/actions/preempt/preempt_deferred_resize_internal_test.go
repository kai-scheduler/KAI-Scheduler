// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

func TestOrderedVictimsQueueOnNodeKeepsOnlyJobsOnTheNode(t *testing.T) {
	ssn, preemptor, onNode, otherNode := newPreemptVictimQueueTestSession()
	for _, task := range otherNode.GetAllPodsMap() {
		task.NodeName = "node-1"
	}
	ssn.ClusterInfo.Nodes["node-1"] = nil
	ssn.AddPreemptVictimFilterFn(func(_, _ *podgroup_info.PodGroupInfo) bool { return true })

	queue := getOrderedVictimsQueueOnNode(ssn, preemptor, "node-0")()
	require.Same(t, onNode, queue.PopNextJob())
	require.True(t, queue.IsEmpty())

	queue = getOrderedVictimsQueueOnNode(ssn, preemptor, "node-1")()
	require.Same(t, otherNode, queue.PopNextJob())
	require.True(t, queue.IsEmpty())
}

func TestOrderedVictimsQueueOnNodeKeepsPreemptRules(t *testing.T) {
	ssn, preemptor, onNode, filtered := newPreemptVictimQueueTestSession()
	filtered.Priority = preemptor.Priority
	ssn.AddPreemptVictimFilterFn(func(_, _ *podgroup_info.PodGroupInfo) bool { return true })

	queue := getOrderedVictimsQueueOnNode(ssn, preemptor, "node-0")()
	require.Same(t, onNode, queue.PopNextJob())
	require.True(t, queue.IsEmpty(), "equal priority job is not a victim")
}

func TestIsEligibleForDeferredResizeUsesNonPreemptibleQuota(t *testing.T) {
	ssn, preemptor, _, _ := newPreemptVictimQueueTestSession()
	tasks := []*pod_info.PodInfo{}
	for _, task := range preemptor.GetAllPodsMap() {
		tasks = append(tasks, task)
	}

	var gotTasks []*pod_info.PodInfo
	overQuota := true
	ssn.AddIsNonPreemptibleJobOverQueueQuotaFns(func(_ *podgroup_info.PodGroupInfo, t []*pod_info.PodInfo) *api.SchedulableResult {
		gotTasks = t
		return &api.SchedulableResult{IsSchedulable: !overQuota}
	})

	require.False(t, isEligibleForDeferredResize(ssn)(preemptor, tasks))
	require.Equal(t, tasks, gotTasks)
	overQuota = false
	require.True(t, isEligibleForDeferredResize(ssn)(preemptor, tasks))
}
