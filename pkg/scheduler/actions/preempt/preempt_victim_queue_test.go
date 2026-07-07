// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt

import (
	"fmt"
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

func TestOrderedVictimsQueueCachesCandidatesPerPreemptor(t *testing.T) {
	ssn, preemptor, acceptedVictim, _ := newPreemptVictimQueueTestSession()
	filterCalls := 0
	ssn.AddPreemptVictimFilterFn(func(_ *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
		filterCalls++
		return victim == acceptedVictim
	})

	generateVictimsQueue := getOrderedVictimsQueue(ssn, preemptor)
	firstQueue := generateVictimsQueue()
	secondQueue := generateVictimsQueue()

	require.NotSame(t, firstQueue, secondQueue)
	require.Same(t, acceptedVictim, firstQueue.PopNextJob())
	require.Same(t, acceptedVictim, secondQueue.PopNextJob())
	require.True(t, firstQueue.IsEmpty())
	require.True(t, secondQueue.IsEmpty())
	require.Equal(t, 2, filterCalls)
}

func TestOrderedVictimsQueueRechecksActiveState(t *testing.T) {
	ssn, preemptor, acceptedVictim, _ := newPreemptVictimQueueTestSession()
	filterCalls := 0
	ssn.AddPreemptVictimFilterFn(func(_ *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
		filterCalls++
		return victim == acceptedVictim
	})

	generateVictimsQueue := getOrderedVictimsQueue(ssn, preemptor)
	firstQueue := generateVictimsQueue()
	require.Same(t, acceptedVictim, firstQueue.PopNextJob())

	var acceptedTask *pod_info.PodInfo
	for _, task := range acceptedVictim.GetAllPodsMap() {
		acceptedTask = task
		break
	}
	require.NotNil(t, acceptedTask)
	require.NoError(t, acceptedVictim.UpdateTaskStatus(acceptedTask, pod_status.Succeeded))

	secondQueue := generateVictimsQueue()
	require.True(t, secondQueue.IsEmpty())
	require.Equal(t, 2, filterCalls)
}

func BenchmarkOrderedVictimsQueueConstruction(b *testing.B) {
	const victimCount = 1000

	queueID := common_info.QueueID("preempt-queue")
	preemptor, _ := newPreemptVictimQueueTestJob("preemptor", queueID, v1.PodPending, 100)
	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				preemptor.UID: preemptor,
			},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				queueID: {UID: queueID, Name: string(queueID)},
			},
			Nodes: map[string]*node_info.NodeInfo{"node-0": nil},
		},
	}
	for i := 0; i < victimCount; i++ {
		job, _ := newPreemptVictimQueueTestJob(fmt.Sprintf("victim-%d", i), queueID, v1.PodRunning, 50)
		ssn.ClusterInfo.PodGroupInfos[job.UID] = job
	}
	ssn.AddPreemptVictimFilterFn(func(_, _ *podgroup_info.PodGroupInfo) bool { return true })

	generateVictimsQueue := getOrderedVictimsQueue(ssn, preemptor)
	warmQueue := generateVictimsQueue()
	for !warmQueue.IsEmpty() {
		warmQueue.PopNextJob()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		queue := generateVictimsQueue()
		for !queue.IsEmpty() {
			queue.PopNextJob()
		}
	}
}

func newPreemptVictimQueueTestSession() (
	*framework.Session,
	*podgroup_info.PodGroupInfo,
	*podgroup_info.PodGroupInfo,
	*podgroup_info.PodGroupInfo,
) {
	queueID := common_info.QueueID("preempt-queue")
	preemptor, _ := newPreemptVictimQueueTestJob("preemptor", queueID, v1.PodPending, 100)
	acceptedVictim, _ := newPreemptVictimQueueTestJob("accepted-victim", queueID, v1.PodRunning, 50)
	filteredVictim, _ := newPreemptVictimQueueTestJob("filtered-victim", queueID, v1.PodRunning, 50)

	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				preemptor.UID:      preemptor,
				acceptedVictim.UID: acceptedVictim,
				filteredVictim.UID: filteredVictim,
			},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				queueID: {UID: queueID, Name: string(queueID)},
			},
			Nodes: map[string]*node_info.NodeInfo{"node-0": nil},
		},
	}
	return ssn, preemptor, acceptedVictim, filteredVictim
}

func newPreemptVictimQueueTestJob(
	name string,
	queueID common_info.QueueID,
	phase v1.PodPhase,
	priority int32,
) (*podgroup_info.PodGroupInfo, *pod_info.PodInfo) {
	pod := common_info.BuildPod(
		"preempt-victim-queue-test",
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
	job.Priority = priority
	job.Preemptibility = schedulingv2alpha2.Preemptible
	return job, task
}
