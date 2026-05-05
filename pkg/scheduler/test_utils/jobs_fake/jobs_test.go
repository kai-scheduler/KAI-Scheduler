// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobs_fake

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
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
	)

	job := jobs[common_info.PodGroupID("job-with-pvc")]
	task := job.PodInfos[common_info.PodID("job-with-pvc-0")]

	require.Len(t, task.Pod.Spec.Volumes, 1)
	require.Equal(t, "missing-pvc", task.Pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
}
