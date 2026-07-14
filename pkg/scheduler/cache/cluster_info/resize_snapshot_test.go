// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func onlyTask(job interface{ GetAllPodsMap() pod_info.PodsMap }) *pod_info.PodInfo {
	for _, task := range job.GetAllPodsMap() {
		return task
	}
	return nil
}

// TestSnapshotInjectsResizeReservationAndChargesActual exercises the full real Snapshot() path
// end to end: a running pod with a kubelet-deferred resize (spec cpu 2, status cpu 1) must be
// (a) charged at its actual size (1 core) via the real getPodResourceRequest path, and
// (b) accompanied by a synthetic node-pinned reservation for the 1-core delta - flagged
// IsResizeReservation and consuming no pod slot. This chains the injection wiring and the
// actual-size accounting through Snapshot(), which unit tests exercise only in isolation.
func TestSnapshotInjectsResizeReservationAndChargesActual(t *testing.T) {
	deferredPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "resizer",
			Namespace:   "ns",
			UID:         "resizer-uid",
			Annotations: map[string]string{commonconstants.PodGroupAnnotationForPod: "resizer-pg"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node0",
			Containers: []corev1.Container{{
				Name:      "main",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}},
			}},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodResizePending, Reason: corev1.PodReasonDeferred, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:      "main",
				Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}},
			}},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node0", Labels: map[string]string{corev1.LabelHostname: "node0"}},
		Status: corev1.NodeStatus{
			Capacity:    corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("16Gi")},
			Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("16Gi")},
		},
	}
	podGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "resizer-pg", Namespace: "ns", UID: "pg-uid"},
		Spec:       enginev2alpha2.PodGroupSpec{Queue: "queue0"},
	}
	queue := &enginev2.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "queue0"},
		Spec:       enginev2.QueueSpec{Resources: &enginev2.QueueResources{}},
	}

	clusterInfo := newClusterInfoTests(t, clusterInfoTestParams{
		kubeObjects:         []runtime.Object{node, deferredPod},
		kaiSchedulerObjects: []runtime.Object{queue, podGroup},
	})
	snapshot, err := clusterInfo.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	// (a) the resizing pod is charged at its actual size (1 core), not desired (2).
	resizer := snapshot.PodGroupInfos["resizer-pg"]
	if resizer == nil {
		t.Fatal("resizing pod's PodGroupInfo not found in snapshot")
	}
	resizerTask := onlyTask(resizer)
	if resizerTask == nil {
		t.Fatal("resizing task not found")
	}
	if got := resizerTask.ResReqVector.Get(resource_info.CPUIndex); got != 1000 {
		t.Errorf("resizing pod charged cpu = %vm, want 1000m (actual, not the 2000m desired)", got)
	}

	// (b) a node-pinned reservation for the delta was injected via the real Snapshot path.
	reservation := snapshot.PodGroupInfos["resizer-pg-resize-reservation"]
	if reservation == nil {
		t.Fatal("resize reservation was not injected by Snapshot()")
	}
	resTask := onlyTask(reservation)
	if resTask == nil {
		t.Fatal("reservation task not found")
	}
	if !resTask.IsResizeReservation {
		t.Error("reservation task IsResizeReservation = false, want true")
	}
	if got := resTask.ResReqVector.Get(resource_info.CPUIndex); got != 1000 {
		t.Errorf("reservation cpu = %vm, want 1000m (the delta)", got)
	}
	if got := resTask.ResReqVector.Get(resource_info.PodsIndex); got != 0 {
		t.Errorf("reservation pods = %v, want 0 (a reservation must not consume a pod slot)", got)
	}
}
