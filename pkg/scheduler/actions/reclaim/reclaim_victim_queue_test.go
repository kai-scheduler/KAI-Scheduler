// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestOrderedVictimsQueueCachesFilteredCandidatesPerReclaimer(t *testing.T) {
	ssn, reclaimer, acceptedVictim, _ := newVictimQueueTestSession()
	filterCalls := 0
	ssn.AddReclaimVictimFilterFn(func(_ *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
		filterCalls++
		return victim == acceptedVictim
	})

	generateVictimsQueue := getOrderedVictimsQueue(ssn, reclaimer)
	firstQueue := generateVictimsQueue()
	secondQueue := generateVictimsQueue()

	require.NotSame(t, firstQueue, secondQueue)
	require.Same(t, acceptedVictim, firstQueue.PopNextJob())
	require.Same(t, acceptedVictim, secondQueue.PopNextJob())
	require.True(t, firstQueue.IsEmpty())
	require.True(t, secondQueue.IsEmpty())
	require.Equal(t, 2, filterCalls)
}

func TestOrderedVictimsQueueRechecksDynamicJobState(t *testing.T) {
	ssn, reclaimer, acceptedVictim, _ := newVictimQueueTestSession()
	filterCalls := 0
	ssn.AddReclaimVictimFilterFn(func(_ *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
		filterCalls++
		return victim == acceptedVictim
	})

	generateVictimsQueue := getOrderedVictimsQueue(ssn, reclaimer)
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
	const (
		victimCount = 1000
		queueCount  = 10
	)

	reclaimerQueue := common_info.QueueID("reclaimer-queue")
	reclaimer, _ := newVictimQueueTestJob("reclaimer", reclaimerQueue, v1.PodPending)
	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				reclaimer.UID: reclaimer,
			},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				reclaimerQueue: {UID: reclaimerQueue, Name: string(reclaimerQueue)},
			},
		},
	}
	for i := 0; i < victimCount; i++ {
		queueID := common_info.QueueID(fmt.Sprintf("victim-queue-%d", i%queueCount))
		if _, found := ssn.ClusterInfo.Queues[queueID]; !found {
			ssn.ClusterInfo.Queues[queueID] = &queue_info.QueueInfo{UID: queueID, Name: string(queueID)}
		}
		job, _ := newVictimQueueTestJob(fmt.Sprintf("victim-%d", i), queueID, v1.PodRunning)
		ssn.ClusterInfo.PodGroupInfos[job.UID] = job
	}
	ssn.AddReclaimVictimFilterFn(func(_, _ *podgroup_info.PodGroupInfo) bool { return true })

	generateVictimsQueue := getOrderedVictimsQueue(ssn, reclaimer)
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

func newVictimQueueTestSession() (
	*framework.Session,
	*podgroup_info.PodGroupInfo,
	*podgroup_info.PodGroupInfo,
	*podgroup_info.PodGroupInfo,
) {
	reclaimerQueue := common_info.QueueID("reclaimer-queue")
	acceptedQueue := common_info.QueueID("accepted-queue")
	filteredQueue := common_info.QueueID("filtered-queue")

	reclaimer, _ := newVictimQueueTestJob("reclaimer", reclaimerQueue, v1.PodPending)
	acceptedVictim, _ := newVictimQueueTestJob("accepted-victim", acceptedQueue, v1.PodRunning)
	filteredVictim, _ := newVictimQueueTestJob("filtered-victim", filteredQueue, v1.PodRunning)
	sameQueueVictim, _ := newVictimQueueTestJob("same-queue-victim", reclaimerQueue, v1.PodRunning)

	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				reclaimer.UID:       reclaimer,
				acceptedVictim.UID:  acceptedVictim,
				filteredVictim.UID:  filteredVictim,
				sameQueueVictim.UID: sameQueueVictim,
			},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				reclaimerQueue: {UID: reclaimerQueue, Name: string(reclaimerQueue)},
				acceptedQueue:  {UID: acceptedQueue, Name: string(acceptedQueue)},
				filteredQueue:  {UID: filteredQueue, Name: string(filteredQueue)},
			},
		},
	}
	return ssn, reclaimer, acceptedVictim, filteredVictim
}

func newVictimQueueTestJob(
	name string,
	queueID common_info.QueueID,
	phase v1.PodPhase,
) (*podgroup_info.PodGroupInfo, *pod_info.PodInfo) {
	pod := common_info.BuildPod(
		"victim-queue-test",
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
