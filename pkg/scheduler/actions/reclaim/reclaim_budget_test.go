// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestAttemptToReclaimLetsSolverApplyMinJobBudgetAfterActionBudgetExpires(t *testing.T) {
	queueID := common_info.QueueID("reclaim-queue")
	jobID := common_info.PodGroupID("reclaim-job")
	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				queueID: {
					UID:  queueID,
					Name: string(queueID),
				},
			},
		},
		Config: &conf.SchedulerConfiguration{
			ScenarioSearchBudgets: &kaiv1.ScenarioSearchBudgets{
				MaxActionSearchDuration: map[string]metav1.Duration{
					constants.ActionReclaim: {Duration: time.Nanosecond},
				},
				MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1s"),
				MinJobSearchDuration: scenarioSearchDurationPtrForTest("50ms"),
			},
		},
	}
	onJobSolutionStartCalls := 0
	ssn.AddOnJobSolutionStartFn(func() {
		onJobSolutionStartCalls++
	})

	actionBudget, err := solvers.NewActionSearchBudget(ssn, framework.Reclaim)
	require.NoError(t, err)
	time.Sleep(time.Millisecond)

	pod := common_info.BuildPod(
		"runai-reclaim", "reclaim-job-0", "", v1.PodPending, common_info.BuildResourceList("1", "1Gi"),
		nil, nil, map[string]string{constants.PodGroupAnnotationForPod: string(jobID)},
	)
	task := pod_info.NewTaskInfo(pod, resource_info.NewResourceVectorMap())
	job := podgroup_info.NewPodGroupInfo(jobID, task)
	job.Name = "reclaim-job"
	job.Namespace = "runai-reclaim"
	job.Queue = queueID
	ssn.ClusterInfo.PodGroupInfos[job.UID] = job

	succeeded, statement, victims, result := New().attemptToReclaimForSpecificJob(ssn, job, actionBudget)

	require.False(t, succeeded)
	require.Nil(t, statement)
	require.Empty(t, victims)
	require.Equal(t, solvers.SearchResultNoGenerator, result.Reason())
	require.Equal(t, 1, onJobSolutionStartCalls)
}

func scenarioSearchDurationPtrForTest(value string) *metav1.Duration {
	duration, err := time.ParseDuration(value)
	if err != nil {
		panic(err)
	}
	return &metav1.Duration{Duration: duration}
}
