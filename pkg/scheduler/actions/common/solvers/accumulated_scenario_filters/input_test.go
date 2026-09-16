// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestMonotonicScenarioInputPotentialVictimsSinceReturnsAppendedSuffix(t *testing.T) {
	first := inputTestTask("victim-1", "victim-job")
	second := inputTestTask("victim-2", "victim-job")
	third := inputTestTask("victim-3", "victim-job")
	testScenario := inputTestScenario(nil, []*pod_info.PodInfo{first, second}, nil)

	input := NewMonotonicScenarioInput(testScenario)
	delta := input.PotentialVictimsSince(VictimTaskCursor{})
	assertInputTestDelta(t, delta, true, 2, first, second)

	testScenario.AddPotentialVictimsTasks([]*pod_info.PodInfo{third})
	delta = input.PotentialVictimsSince(delta.Next)
	assertInputTestDelta(t, delta, true, 3, third)
}

func TestMonotonicScenarioInputFallsBackWhenCursorIsAhead(t *testing.T) {
	first := inputTestTask("victim-1", "victim-job")
	second := inputTestTask("victim-2", "victim-job")
	testScenario := inputTestScenario(nil, []*pod_info.PodInfo{first, second}, nil)

	input := NewMonotonicScenarioInput(testScenario)
	delta := input.PotentialVictimsSince(VictimTaskCursor{Len: 10})

	assertInputTestDelta(t, delta, false, 2, first, second)
}

func TestFullScanScenarioInputAlwaysReturnsFullVictimLists(t *testing.T) {
	recorded := inputTestTask("recorded-1", "recorded-job")
	potential := inputTestTask("victim-1", "victim-job")
	recordedJob := podgroup_info.NewPodGroupInfo("recorded-job", recorded)
	testScenario := inputTestScenario(nil, []*pod_info.PodInfo{potential}, []*podgroup_info.PodGroupInfo{recordedJob})

	input := NewFullScanScenarioInput(testScenario)

	potentialDelta := input.PotentialVictimsSince(VictimTaskCursor{Len: 1})
	assertInputTestDelta(t, potentialDelta, false, 1, potential)

	recordedDelta := input.RecordedVictimsSince(VictimTaskCursor{Len: 1})
	assertInputTestDelta(t, recordedDelta, false, 1, recorded)
}

func assertInputTestDelta(
	t *testing.T,
	delta VictimTaskDelta,
	wantMonotonic bool,
	wantNextLen int,
	wantTasks ...*pod_info.PodInfo,
) {
	t.Helper()
	if delta.Monotonic != wantMonotonic {
		t.Fatalf("Monotonic = %v, want %v", delta.Monotonic, wantMonotonic)
	}
	if delta.Next.Len != wantNextLen {
		t.Fatalf("Next.Len = %d, want %d", delta.Next.Len, wantNextLen)
	}
	if len(delta.Tasks) != len(wantTasks) {
		t.Fatalf("len(Tasks) = %d, want %d", len(delta.Tasks), len(wantTasks))
	}
	for i, wantTask := range wantTasks {
		if delta.Tasks[i] != wantTask {
			t.Fatalf("Tasks[%d] = %v, want %v", i, delta.Tasks[i].UID, wantTask.UID)
		}
	}
}

func inputTestScenario(
	pendingTasks []*pod_info.PodInfo,
	potentialVictims []*pod_info.PodInfo,
	recordedVictimJobs []*podgroup_info.PodGroupInfo,
) *scenario.ByNodeScenario {
	ssn := &framework.Session{ClusterInfo: api.NewClusterInfo()}
	pendingJob := podgroup_info.NewPodGroupInfo("pending-job", pendingTasks...)
	ssn.ClusterInfo.PodGroupInfos[pendingJob.UID] = pendingJob
	for _, task := range potentialVictims {
		if _, ok := ssn.ClusterInfo.PodGroupInfos[task.Job]; !ok {
			ssn.ClusterInfo.PodGroupInfos[task.Job] = podgroup_info.NewPodGroupInfo(task.Job)
		}
	}
	for _, victimJob := range recordedVictimJobs {
		ssn.ClusterInfo.PodGroupInfos[victimJob.UID] = victimJob
	}
	return scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, potentialVictims, recordedVictimJobs)
}

func inputTestTask(uid, jobID string) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      uid,
			Namespace: "test",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: jobID,
			},
		},
	}, resource_info.NewResourceVectorMap())
}
