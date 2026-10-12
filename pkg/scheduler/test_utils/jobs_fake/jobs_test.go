// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobs_fake

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestBuildJobsAndTasksMapsAddsPersistentVolumeClaimVolumes(t *testing.T) {
	jobs, _, _ := BuildJobsAndTasksMaps(
		[]*TestJobBasic{{
			Name:                "job-with-pvc",
			Namespace:           "test-namespace",
			QueueName:           "test-queue",
			RequiredCPUsPerTask: 0.5,
			Tasks: []*tasks_fake.TestTaskBasic{
				{
					State:                      pod_status.Pending,
					PersistentVolumeClaimNames: []string{"missing-pvc"},
				},
			},
		}},
		resource_info.NewResourceVectorMap(),
	)

	job := jobs[common_info.NewPodGroupID("test-namespace", "job-with-pvc")]
	task := job.GetAllPodsMap()[common_info.PodID("job-with-pvc-0")]

	require.Len(t, task.Pod.Spec.Volumes, 1)
	require.Equal(t, "missing-pvc", task.Pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
}

func TestBuildJobsAndTasksMapsKeepsSameNamedPodGroupsInDifferentNamespaces(t *testing.T) {
	jobs, _, _ := BuildJobsAndTasksMaps([]*TestJobBasic{
		{Name: "training", Namespace: "team-a", QueueName: "queue", Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}}},
		{Name: "training", Namespace: "team-b", QueueName: "queue", Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}}},
	}, resource_info.NewResourceVectorMap())

	require.Len(t, jobs, 2)
	for _, namespace := range []string{"team-a", "team-b"} {
		id := common_info.NewPodGroupID(namespace, "training")
		job := jobs[id]
		require.NotNil(t, job)
		require.Equal(t, id, job.UID)
		require.Equal(t, namespace, job.PodGroup.Namespace)
		require.Equal(t, "training", job.PodGroup.Name)
		require.Len(t, job.GetAllPodsMap(), 1)
		for _, task := range job.GetAllPodsMap() {
			require.Equal(t, id, task.Job)
			require.Same(t, job, jobs[task.Job])
		}
	}
}
