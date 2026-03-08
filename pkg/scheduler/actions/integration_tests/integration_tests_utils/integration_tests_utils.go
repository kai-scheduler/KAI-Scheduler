// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests_utils

import (
	"fmt"
	"testing"
	"time"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/stalegangeviction"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
)

type TestTopologyMetadata struct {
	Name string
	test_utils.TestTopologyBasic
	RoundsUntilMatch   int
	RoundsAfterMatch   int           // Used in order to verify the test scenario remain (no allocation/delete loop)
	SchedulingDuration time.Duration // Used to specify delay between rounds, leave empty if not needed
}

const (
	defaultRoundsAfterMatch = 5
	defaultRoundsUntilMatch = 2
)

var schedulerActions []framework.Action

func RunTests(t *testing.T, testsMetadata []TestTopologyMetadata) {
	test_utils.InitTestingInfrastructure()
	SetSchedulerActions()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range testsMetadata {
		RunTest(t, testMetadata, testNumber, controller)
	}
}

func RunTest(t *testing.T, testMetadata TestTopologyMetadata, testNumber int, controller *Controller) {
	t.Logf("Running test number: %v, test name: %v", testNumber, testMetadata.TestTopologyBasic.Name)
	var ssn *framework.Session

	runRoundsUntilMatch(testMetadata, controller, &ssn)
	ssn = prepareSessionForMatch(ssn, testMetadata, controller)
	test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	runRoundsAfterAndMatch(t, testMetadata, controller, ssn, testNumber)
}

// prepare session for match by rebuilding the session while preserving the podgroup errors
func prepareSessionForMatch(ssn *framework.Session, testMetadata TestTopologyMetadata, controller *Controller) *framework.Session {
	jobFitErrors := make(map[string][]common_info.JobFitError)
	for jobId, job := range ssn.ClusterInfo.PodGroupInfos {
		jobFitErrors[string(jobId)] = job.JobFitErrors
	}
	ssn = test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
	for jobId, job := range ssn.ClusterInfo.PodGroupInfos {
		job.JobFitErrors = jobFitErrors[string(jobId)]
	}
	return ssn
}

func runRoundsAfterAndMatch(t *testing.T, testMetadata TestTopologyMetadata, controller *Controller, ssn *framework.Session, testNumber int) {
	roundsAfterMatch := defaultRoundsAfterMatch
	if testMetadata.RoundsAfterMatch != 0 {
		roundsAfterMatch = testMetadata.RoundsAfterMatch
	}
	for i := 0; i < roundsAfterMatch; i++ {
		runSchedulerOneRound(&testMetadata, controller, &ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func runRoundsUntilMatch(testMetadata TestTopologyMetadata, controller *Controller, ssn **framework.Session) {
	roundsUntilMatch := defaultRoundsUntilMatch
	if testMetadata.RoundsUntilMatch != 0 {
		roundsUntilMatch = testMetadata.RoundsUntilMatch
	}
	for i := 0; i < roundsUntilMatch; i++ {
		runSchedulerOneRound(&testMetadata, controller, ssn)
		time.Sleep(testMetadata.SchedulingDuration)
	}
}

func runSchedulerOneRound(testMetadata *TestTopologyMetadata, controller *Controller, ssn **framework.Session) {
	*ssn = test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
	for _, action := range schedulerActions {
		log.InfraLogger.SetAction(string(action.Name()))
		action.Execute(*ssn)
	}

	for _, jobMetadata := range testMetadata.Jobs {
		jobId := common_info.PodGroupID(jobMetadata.Name)
		job := (*ssn).ClusterInfo.PodGroupInfos[jobId]
		for taskId, taskMetadata := range jobMetadata.Tasks {
			task := job.GetAllPodsMap()[common_info.PodID(fmt.Sprintf("%s-%d", jobId, taskId))]
			switch task.Status {
			case pod_status.Releasing:
				if jobMetadata.DeleteJobInTest {
					taskMetadata.NodeName = task.NodeName
					taskMetadata.GPUGroups = task.GPUGroups
					taskMetadata.State = pod_status.Releasing
				} else {
					taskMetadata.NodeName = ""
					taskMetadata.State = pod_status.Pending
				}

			case pod_status.Pipelined:
				taskMetadata.NodeName = ""
				taskMetadata.State = pod_status.Pending

			case pod_status.Binding:
				taskMetadata.State = pod_status.Running
				taskMetadata.NodeName = task.NodeName
				taskMetadata.GPUGroups = task.GPUGroups

			default:
				taskMetadata.State = task.Status
				taskMetadata.NodeName = task.NodeName
				taskMetadata.GPUGroups = task.GPUGroups
			}

		}
	}
	if len(testMetadata.TestDRAObjects.ResourceClaims) > 0 {
		draManager := (*ssn).InternalK8sPlugins().FrameworkHandle.SharedDRAManager()
		for _, claim := range testMetadata.TestDRAObjects.ResourceClaims {
			clusterClaim, err := draManager.ResourceClaims().Get(claim.Namespace, claim.Name)
			if err != nil {
				log.InfraLogger.Errorf("Failed to get resource claim %s: %v", claim.Name, err)
				continue
			}
			clusterClaimStatus := clusterClaim.Status
			if clusterClaimStatus.Allocation != nil || clusterClaimStatus.ReservedFor != nil || clusterClaimStatus.Devices != nil {
				claim.ClaimStatus = clusterClaimStatus.DeepCopy()
			}
		}
	}

}

func SetSchedulerActions() {
	schedulerActions = []framework.Action{allocate.New(), consolidation.New(), reclaim.New(), preempt.New(), stalegangeviction.New()}
}
