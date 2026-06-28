// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
)

func TestReducedBudgetFailedReclaimRecordsScenarioSearchUnresolved(t *testing.T) {
	defer gock.Off()

	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := buildUnschedulableDistributedReclaimBenchmarkTopology(
		defaultUnschedulableDistributedReclaimBenchmarkParams(10),
	)
	ssn := test_utils.BuildSession(topology, controller)
	ssn.Config.ScenarioSearchBudgets = &kaiv1.ScenarioSearchBudgets{
		MaxActionSearchDuration: map[string]metav1.Duration{
			commonconstants.ActionReclaim: {Duration: 250 * time.Millisecond},
		},
		MaxJobSearchDuration: &metav1.Duration{Duration: time.Second},
		MinJobSearchDuration: &metav1.Duration{Duration: 500 * time.Millisecond},
	}

	reclaim.New().Execute(ssn)

	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(unschedulableDistributedJobName)]
	require.NotNil(t, job)
	require.Empty(t, job.JobFitErrors)
	require.NotNil(t, job.ScenarioSearchUnresolved)
	require.Equal(t, podgroup_info.ScenarioSearchResultGeneratorsExhausted, job.ScenarioSearchUnresolved.Reason)
	require.True(t, job.ScenarioSearchUnresolved.ReducedBudget)
}
