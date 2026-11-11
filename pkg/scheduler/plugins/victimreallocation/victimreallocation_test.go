// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package victimreallocation

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

func TestVictimReallocationPlugin(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	newPodInfo := func(name string, uid string, nodeName string, phase v1.PodPhase) *pod_info.PodInfo {
		return pod_info.NewTaskInfo(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				UID:       types.UID(uid),
			},
			Spec: v1.PodSpec{
				NodeName: nodeName,
			},
			Status: v1.PodStatus{
				Phase: phase,
			},
		}, vectorMap)
	}

	t.Run("scores victim original node highest only after preemptor is placed", func(t *testing.T) {
		ssn := &framework.Session{}
		plugin := New(nil)
		plugin.OnSessionOpen(ssn)
		defer plugin.OnSessionClose(ssn)

		preemptorTask := newPodInfo("preemptor-0", "preemptor-uid", "", v1.PodPending)
		preemptorJob := podgroup_info.NewPodGroupInfo("preemptor-job", preemptorTask)

		victimTask := newPodInfo("victim-0", "victim-uid", "node-a", v1.PodRunning)
		victimTask.Status = pod_status.Releasing
		victimTask.IsVirtualStatus = true
		victimJob := podgroup_info.NewPodGroupInfo("victim-job", victimTask)

		nodeA := &node_info.NodeInfo{Name: "node-a"}
		nodeB := &node_info.NodeInfo{Name: "node-b"}

		// Victim allocated before preemptor is placed (e.g. consolidation): score must be 0.
		ssn.PreJobAllocation(victimJob)
		score, err := ssn.NodeOrderFn(victimTask, nodeA)
		require.NoError(t, err)
		require.Zero(t, score)

		// Preemptor allocation starts: score for preemptor task must be 0.
		ssn.PreJobAllocation(preemptorJob)
		score, err = ssn.NodeOrderFn(preemptorTask, nodeA)
		require.NoError(t, err)
		require.Zero(t, score)

		// Simulate preemptor placement in statement.
		preemptorTask.Status = pod_status.Pipelined
		preemptorTask.IsVirtualStatus = true
		preemptorJob.AddTaskInfo(preemptorTask)

		// Victim re-placed after preemptor is placed: original node scores highest, other nodes score 0.
		ssn.PreJobAllocation(victimJob)
		score, err = ssn.NodeOrderFn(victimTask, nodeA)
		require.NoError(t, err)
		require.Equal(t, float64(scores.VictimReallocation), score)

		score, err = ssn.NodeOrderFn(victimTask, nodeB)
		require.NoError(t, err)
		require.Zero(t, score)

		// After simulation rollback/commit clears virtual status on preemptor, a subsequent pre-preemptor victim scores 0.
		preemptorTask.Status = pod_status.Pending
		preemptorTask.IsVirtualStatus = false
		preemptorJob.AddTaskInfo(preemptorTask)

		ssn.PreJobAllocation(victimJob)
		score, err = ssn.NodeOrderFn(victimTask, nodeA)
		require.NoError(t, err)
		require.Zero(t, score)
	})
}
