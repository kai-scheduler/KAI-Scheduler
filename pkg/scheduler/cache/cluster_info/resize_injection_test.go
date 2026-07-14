// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// A running pod with a cpu-growing deferred resize (spec 2, status 1) must yield exactly one
// synthetic reservation PodGroupInfo, in the resizing pod's queue/priority, whose task is flagged
// IsResizeReservation. A non-resizing job must not.
func TestInjectResizeReservations(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()

	resizingPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:         "resizer-uid",
			Name:        "resizer",
			Namespace:   "ns",
			Annotations: map[string]string{commonconstants.PodGroupAnnotationForPod: "resizer-job"},
		},
		Spec: v1.PodSpec{
			NodeName: "node0",
			Containers: []v1.Container{{
				Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")}},
			}},
		},
		Status: v1.PodStatus{
			Phase:      v1.PodRunning,
			Conditions: []v1.PodCondition{{Type: v1.PodResizePending, Reason: v1.PodReasonDeferred, Status: v1.ConditionTrue}},
			ContainerStatuses: []v1.ContainerStatus{{
				Resources: &v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}},
			}},
		},
	}
	resizingJob := podgroup_info.NewPodGroupInfoWithVectorMap("resizer-job", vm, pod_info.NewTaskInfo(resizingPod, vm))
	resizingJob.Queue = "queue0"
	resizingJob.Priority = 100

	snapshot := api.NewClusterInfo()
	snapshot.ResourceVectorMap = vm
	snapshot.PodGroupInfos["resizer-job"] = resizingJob

	InjectResizeReservations(snapshot)

	if len(snapshot.PodGroupInfos) != 2 {
		t.Fatalf("PodGroupInfos count = %d, want 2 (resizer + its reservation)", len(snapshot.PodGroupInfos))
	}
	reservation, found := snapshot.PodGroupInfos["resizer-job-resize-reservation"]
	if !found {
		t.Fatal("no resize reservation was injected")
	}
	if reservation.Queue != "queue0" || reservation.Priority != 100 {
		t.Errorf("reservation queue/priority = %q/%d, want queue0/100", reservation.Queue, reservation.Priority)
	}
	tasks := reservation.GetAllPodsMap()
	if len(tasks) != 1 {
		t.Fatalf("reservation task count = %d, want 1", len(tasks))
	}
	for _, task := range tasks {
		if !task.IsResizeReservation {
			t.Error("reservation task IsResizeReservation = false, want true")
		}
	}
}

// A job with no deferred resize must not spawn any reservation.
func TestInjectResizeReservationsNoOpForPlainJob(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	plainPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: "plain-uid", Name: "plain", Namespace: "ns",
			Annotations: map[string]string{commonconstants.PodGroupAnnotationForPod: "plain-job"},
		},
		Spec: v1.PodSpec{
			NodeName:   "node0",
			Containers: []v1.Container{{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}}},
		},
		Status: v1.PodStatus{Phase: v1.PodRunning},
	}
	job := podgroup_info.NewPodGroupInfoWithVectorMap("plain-job", vm, pod_info.NewTaskInfo(plainPod, vm))

	snapshot := api.NewClusterInfo()
	snapshot.ResourceVectorMap = vm
	snapshot.PodGroupInfos["plain-job"] = job

	InjectResizeReservations(snapshot)

	if len(snapshot.PodGroupInfos) != 1 {
		t.Fatalf("PodGroupInfos count = %d, want 1 (no reservation for a non-resizing job)", len(snapshot.PodGroupInfos))
	}
}
