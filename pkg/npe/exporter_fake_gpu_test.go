// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package npe

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/kai-scheduler/api/constants"
)

func TestConfigureNodeFakeGPU(t *testing.T) {
	for _, test := range []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{name: "fake", labels: map[string]string{"run.ai/fake.gpu": "true"}, want: true},
		{name: "false", labels: map[string]string{"run.ai/fake.gpu": "false"}},
		{name: "absent"},
		{name: "not exact", labels: map[string]string{"run.ai/fake.gpu": "True"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cs := fake.NewSimpleClientset(
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "local", Labels: test.labels}},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{"run.ai/fake.gpu": "true"}}},
			)
			exporter := newTestExporter(t, "local", &stubLister{}, cs)
			if err := exporter.configureNode(context.Background()); err != nil {
				t.Fatal(err)
			}
			if exporter.fakeGPUNode != test.want {
				t.Fatalf("fakeGPUNode = %t, want %t", exporter.fakeGPUNode, test.want)
			}
		})
	}
}

func TestConfigureNodePropagatesGetError(t *testing.T) {
	exporter := newTestExporter(t, "missing", &stubLister{}, fake.NewSimpleClientset())
	if err := exporter.configureNode(context.Background()); err == nil {
		t.Fatal("expected node lookup error")
	}
}

func TestReconcileFakeGPUMemoryGroups(t *testing.T) {
	const prediction = `[{"memoryNodes":["node-0"],"amount":{"memory":"1Gi"}}]`
	for _, test := range []struct {
		name       string
		prediction string
		observed   string
		burstable  bool
		want       string
	}{
		{name: "prediction", prediction: prediction, want: prediction},
		{name: "replaces null", prediction: prediction, observed: "null", want: prediction},
		{name: "replaces stale observation", prediction: prediction, observed: "[]", want: prediction},
		{name: "known empty", prediction: "[]", want: "[]"},
		{name: "missing prediction"},
		{name: "missing removes stale", observed: prediction},
		{name: "missing removes null", observed: "null"},
		{name: "malformed removes stale", prediction: "garbage", observed: prediction},
		{name: "null removes stale", prediction: "null", observed: prediction},
		{name: "wrong shape removes stale", prediction: "{}", observed: prediction},
		{name: "burstable ignores prediction", prediction: prediction, burstable: true, want: "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pod := fakeGPUMemoryPod()
			if test.prediction != "" {
				pod.Annotations[constants.NumaMemoryGroupsPredicted] = test.prediction
			}
			if test.observed != "" {
				pod.Annotations[constants.NumaMemoryGroupsObserved] = test.observed
			}
			if test.burstable {
				pod.Status.QOSClass = corev1.PodQOSBurstable
			}
			cs := fake.NewSimpleClientset(pod, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"run.ai/fake.gpu": "true"}}})
			reported := gpuPod("ns", "memory")
			reported.Containers[0].Memory = []*podresourcesv1.ContainerMemory{{MemoryType: "memory", Size: 1024}}
			exporter := newTestExporter(t, "node-1", &stubLister{pods: []*podresourcesv1.PodResources{reported}}, cs)
			if err := exporter.configureNode(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := exporter.reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			actual, err := cs.CoreV1().Pods("ns").Get(context.Background(), "memory", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			value, exists := actual.Annotations[constants.NumaMemoryGroupsObserved]
			if value != test.want || exists != (test.want != "") {
				t.Fatalf("observation = %q (present %t), want %q", value, exists, test.want)
			}
			if got := annotationOf(t, cs, "ns", "memory"); got != gpuPlacement {
				t.Fatalf("GPU placement = %q, want %q", got, gpuPlacement)
			}
		})
	}
}

func TestReconcileFakeGPUMemoryPredictionUpdates(t *testing.T) {
	pod := fakeGPUMemoryPod()
	cs := fake.NewSimpleClientset(pod)
	exporter := newTestExporter(t, "node-1", &stubLister{}, cs)
	exporter.fakeGPUNode = true
	for _, prediction := range []string{
		`[{"memoryNodes":["node-0"],"amount":{"memory":"1Gi"}}]`,
		`[{"memoryNodes":["node-1"],"amount":{"memory":"1Gi"}}]`,
		"",
	} {
		actual, err := cs.CoreV1().Pods("ns").Get(context.Background(), "memory", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		actual.Annotations[constants.NumaMemoryGroupsPredicted] = prediction
		if _, err := cs.CoreV1().Pods("ns").Update(context.Background(), actual, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		syncTestPods(t, exporter, cs)
		if err := exporter.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		actual, err = cs.CoreV1().Pods("ns").Get(context.Background(), "memory", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := actual.Annotations[constants.NumaMemoryGroupsObserved]; got != prediction {
			t.Fatalf("observation = %q, want updated prediction %q", got, prediction)
		}
	}
}

func TestReconcileRealNodeUsesMemoryObservation(t *testing.T) {
	pod := fakeGPUMemoryPod()
	pod.Annotations[constants.NumaMemoryGroupsPredicted] = `[{"memoryNodes":["node-1"],"amount":{"memory":"1Gi"}}]`
	cs := fake.NewSimpleClientset(pod, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	reported := &podresourcesv1.PodResources{Namespace: "ns", Name: "memory", Containers: []*podresourcesv1.ContainerResources{{
		Name: "app", Memory: []*podresourcesv1.ContainerMemory{{MemoryType: "memory", Size: 1073741824, Topology: &podresourcesv1.TopologyInfo{Nodes: []*podresourcesv1.NUMANode{{ID: 0}}}}},
	}}}
	exporter := newTestExporter(t, "node-1", &stubLister{pods: []*podresourcesv1.PodResources{reported}}, cs)
	if err := exporter.configureNode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := exporter.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	actual, err := cs.CoreV1().Pods("ns").Get(context.Background(), "memory", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"memoryNodes":["node-0"],"amount":{"memory":"1Gi"}}]`
	if got := actual.Annotations[constants.NumaMemoryGroupsObserved]; got != want {
		t.Fatalf("observation = %q, want actual placement %q", got, want)
	}
}

func fakeGPUMemoryPod() *corev1.Pod {
	pod := apiPod("ns", "memory", "node-1", "")
	pod.Annotations = map[string]string{}
	pod.Spec.Containers = []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}}}}
	pod.Status.QOSClass = corev1.PodQOSGuaranteed
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
	return pod
}
