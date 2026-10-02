// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestOrderedVictimsQueueCachesUnlimitedCandidateSnapshot(t *testing.T) {
	ssn, preemptor, originalVictim, queueID := newConsolidationVictimQueueTestSession()
	ssn.OverrideMaxNumberConsolidationPreemptees(noConsolidationPreempteesRestrcition)

	generateVictimsQueue := getOrderedVictimsQueue(ssn, preemptor)
	firstQueue := generateVictimsQueue()
	require.Same(t, originalVictim, firstQueue.PopNextJob())

	newVictim, _ := newConsolidationVictimQueueTestJob("new-victim", queueID, v1.PodRunning)
	ssn.ClusterInfo.PodGroupInfos[newVictim.UID] = newVictim
	secondQueue := generateVictimsQueue()

	require.NotSame(t, firstQueue, secondQueue)
	require.Equal(t, 1, secondQueue.Len())
	require.Same(t, originalVictim, secondQueue.PopNextJob())
	require.True(t, secondQueue.IsEmpty())
}

func TestOrderedVictimsQueueRechecksActiveState(t *testing.T) {
	ssn, preemptor, originalVictim, _ := newConsolidationVictimQueueTestSession()
	ssn.OverrideMaxNumberConsolidationPreemptees(noConsolidationPreempteesRestrcition)

	generateVictimsQueue := getOrderedVictimsQueue(ssn, preemptor)
	firstQueue := generateVictimsQueue()
	require.Same(t, originalVictim, firstQueue.PopNextJob())

	var victimTask *pod_info.PodInfo
	for _, task := range originalVictim.GetAllPodsMap() {
		victimTask = task
		break
	}
	require.NotNil(t, victimTask)
	require.NoError(t, originalVictim.UpdateTaskStatus(victimTask, pod_status.Succeeded))

	secondQueue := generateVictimsQueue()
	require.True(t, secondQueue.IsEmpty())
}

func TestOrderedVictimsQueueRescansCandidatesWithFiniteLimit(t *testing.T) {
	ssn, preemptor, _, queueID := newConsolidationVictimQueueTestSession()
	ssn.OverrideMaxNumberConsolidationPreemptees(10)

	generateVictimsQueue := getOrderedVictimsQueue(ssn, preemptor)
	firstQueue := generateVictimsQueue()
	require.Equal(t, 1, firstQueue.Len())

	newVictim, _ := newConsolidationVictimQueueTestJob("new-victim", queueID, v1.PodRunning)
	ssn.ClusterInfo.PodGroupInfos[newVictim.UID] = newVictim
	secondQueue := generateVictimsQueue()

	require.Equal(t, 2, secondQueue.Len())
}

func newConsolidationVictimQueueTestSession() (
	*framework.Session,
	*podgroup_info.PodGroupInfo,
	*podgroup_info.PodGroupInfo,
	common_info.QueueID,
) {
	queueID := common_info.QueueID("consolidation-queue")
	preemptor, _ := newConsolidationVictimQueueTestJob("preemptor", queueID, v1.PodPending)
	originalVictim, _ := newConsolidationVictimQueueTestJob("original-victim", queueID, v1.PodRunning)

	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				preemptor.UID:      preemptor,
				originalVictim.UID: originalVictim,
			},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				queueID: {UID: queueID, Name: string(queueID)},
			},
			Nodes: map[string]*node_info.NodeInfo{"node-0": nil},
		},
	}
	return ssn, preemptor, originalVictim, queueID
}

func newConsolidationVictimQueueTestJob(
	name string,
	queueID common_info.QueueID,
	phase v1.PodPhase,
) (*podgroup_info.PodGroupInfo, *pod_info.PodInfo) {
	pod := common_info.BuildPod(
		"consolidation-victim-queue-test",
		name+"-pod",
		"node-0",
		phase,
		common_info.BuildResourceList("1", "1Gi"),
		nil,
		nil,
		nil,
	)
	task := pod_info.NewTaskInfo(pod, resource_info.NewResourceVectorMap())
	job := podgroup_info.NewPodGroupInfo(common_info.PodGroupID(name), task)
	job.Name = name
	job.Queue = queueID
	job.Preemptibility = schedulingv2alpha2.Preemptible
	return job, task
}
