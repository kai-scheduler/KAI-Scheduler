// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package npe

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/consts"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/cputopology"
)

// gpuPlacement is the annotation value gpuPod resolves to.
const gpuPlacement = `[{"zone":"node-0","amount":{"nvidia.com/gpu":"1"}}]`

type stubLister struct {
	pods []*podresourcesv1.PodResources
	err  error
}

func (s *stubLister) List(context.Context) ([]*podresourcesv1.PodResources, error) {
	return s.pods, s.err
}

func gpuPod(namespace, name string) *podresourcesv1.PodResources {
	return &podresourcesv1.PodResources{
		Namespace: namespace,
		Name:      name,
		Containers: []*podresourcesv1.ContainerResources{{
			Devices: []*podresourcesv1.ContainerDevices{{
				ResourceName: "nvidia.com/gpu",
				DeviceIds:    []string{"GPU-0"},
				Topology:     &podresourcesv1.TopologyInfo{Nodes: []*podresourcesv1.NUMANode{{ID: 0}}},
			}},
		}},
	}
}

func apiPod(namespace, name, node, annotation string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       corev1.PodSpec{NodeName: node},
	}
	if annotation != "" {
		pod.Annotations = map[string]string{consts.NumaPlacementAnnotation: annotation}
	}
	return pod
}

func annotationOf(t *testing.T, cs *fake.Clientset, namespace, name string) string {
	t.Helper()
	pod, err := cs.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod %s/%s: %v", namespace, name, err)
	}
	return pod.Annotations[consts.NumaPlacementAnnotation]
}

func TestReconcileDriftRepairsAnnotations(t *testing.T) {
	const node = "node-1"

	cs := fake.NewSimpleClientset(
		apiPod("ns", "missing", node, ""),           // no annotation -> repaired
		apiPod("ns", "stale", node, "garbage"),      // modified out-of-band -> repaired
		apiPod("ns", "correct", node, gpuPlacement), // already correct -> left as is
		apiPod("ns", "unaligned", node, ""),         // holds no aligned resources -> untouched
	)

	lister := &stubLister{pods: []*podresourcesv1.PodResources{
		gpuPod("ns", "missing"),
		gpuPod("ns", "stale"),
		gpuPod("ns", "correct"),
	}}

	a := New(node, time.Minute, time.Minute, lister, cputopology.CPUToNUMA{}, cs)
	if err := a.reconcileDrift(context.Background()); err != nil {
		t.Fatalf("reconcileDrift: %v", err)
	}

	for _, name := range []string{"missing", "stale", "correct"} {
		if got := annotationOf(t, cs, "ns", name); got != gpuPlacement {
			t.Errorf("pod %q annotation = %q, want %q", name, got, gpuPlacement)
		}
		if got := a.written["ns/"+name]; got != gpuPlacement {
			t.Errorf("pod %q cache = %q, want %q", name, got, gpuPlacement)
		}
	}

	if got := annotationOf(t, cs, "ns", "unaligned"); got != "" {
		t.Errorf("unaligned pod annotation = %q, want empty", got)
	}
	if _, ok := a.written["ns/unaligned"]; ok {
		t.Errorf("unaligned pod should not be cached")
	}
}

func TestReconcileDriftPropagatesListError(t *testing.T) {
	lister := &stubLister{err: context.DeadlineExceeded}
	a := New("node-1", time.Minute, time.Minute, lister, cputopology.CPUToNUMA{}, fake.NewSimpleClientset())
	if err := a.reconcileDrift(context.Background()); err == nil {
		t.Fatal("expected error from podresources list")
	}
}

func TestDriftTickerDisabled(t *testing.T) {
	a := &Exporter{driftResyncInterval: 0}
	c, stop := a.driftTicker()
	defer stop()
	if c != nil {
		t.Fatal("expected nil tick channel when drift reconciliation is disabled")
	}
}

func TestDriftTickerEnabled(t *testing.T) {
	a := &Exporter{driftResyncInterval: time.Hour}
	c, stop := a.driftTicker()
	defer stop()
	if c == nil {
		t.Fatal("expected a tick channel when drift reconciliation is enabled")
	}
}
