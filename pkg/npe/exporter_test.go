// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package npe

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
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
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(namespace + "/" + name)},
		Spec:       corev1.PodSpec{NodeName: node},
	}
	if annotation != "" {
		pod.Annotations = map[string]string{consts.NumaPlacementAnnotation: annotation}
	}
	return pod
}

func newTestExporter(t *testing.T, node string, resources resourceLister, cs *fake.Clientset) *Exporter {
	t.Helper()
	exporter := New(node, time.Minute, time.Minute, resources, nil, cs)
	syncTestPods(t, exporter, cs)
	return exporter
}

func syncTestPods(t *testing.T, exporter *Exporter, cs *fake.Clientset) {
	t.Helper()
	pods, err := cs.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for index := range pods.Items {
		pod := &pods.Items[index]
		if pod.Spec.NodeName != exporter.nodeName {
			continue
		}
		if err := indexer.Add(pod); err != nil {
			t.Fatal(err)
		}
	}
	exporter.pods = corelisters.NewPodLister(indexer)
}

func annotationOf(t *testing.T, cs *fake.Clientset, namespace, name string) string {
	t.Helper()
	pod, err := cs.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod %s/%s: %v", namespace, name, err)
	}
	return pod.Annotations[consts.NumaPlacementAnnotation]
}

func TestReconcileRepairsAnnotations(t *testing.T) {
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

	a := newTestExporter(t, node, lister, cs)
	if err := a.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, name := range []string{"missing", "stale", "correct"} {
		if got := annotationOf(t, cs, "ns", name); got != gpuPlacement {
			t.Errorf("pod %q annotation = %q, want %q", name, got, gpuPlacement)
		}
	}

	if got := annotationOf(t, cs, "ns", "unaligned"); got != "" {
		t.Errorf("unaligned pod annotation = %q, want empty", got)
	}
}

func TestReconcilePropagatesListError(t *testing.T) {
	lister := &stubLister{err: context.DeadlineExceeded}
	a := New("node-1", time.Minute, time.Minute, lister, cputopology.CPUToNUMA{}, fake.NewSimpleClientset())
	if err := a.reconcile(context.Background()); err == nil {
		t.Fatal("expected error from podresources list")
	}
}

func TestReconcileRepairsInformerAnnotationDrift(t *testing.T) {
	pod := apiPod("ns", "gpu", "node-1", gpuPlacement)
	pod.UID = "pod-uid"
	pod.ResourceVersion = "7"
	pod.Annotations[constants.NumaMemoryGroupsObserved] = "[]"
	cs := fake.NewSimpleClientset(pod)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if err := indexer.Add(pod); err != nil {
		t.Fatal(err)
	}
	exporter := newTestExporter(t, "node-1", &stubLister{pods: []*podresourcesv1.PodResources{gpuPod("ns", "gpu")}}, cs)
	exporter.pods = corelisters.NewPodLister(indexer)
	patches := 0
	cs.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		t.Fatal("informer-backed reconciliation should not list pods from the API server")
		return true, nil, nil
	})
	cs.PrependReactor("patch", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patches++
		var patched corev1.Pod
		if err := json.Unmarshal(action.(clienttesting.PatchAction).GetPatch(), &patched); err != nil {
			t.Fatal(err)
		}
		if patched.UID != pod.UID || patched.ResourceVersion != pod.ResourceVersion {
			t.Fatalf("patch lost UID/resourceVersion preconditions: %v", patched.ObjectMeta)
		}
		return false, nil, nil
	})
	reconcile := func() {
		t.Helper()
		if err := exporter.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	if patches != 0 {
		t.Fatalf("unchanged observations generated %d patches", patches)
	}

	drifted := pod.DeepCopy()
	drifted.Annotations[consts.NumaPlacementAnnotation] = "stale"
	drifted.Annotations[constants.NumaMemoryGroupsObserved] = "null"
	if _, err := cs.CoreV1().Pods("ns").Update(context.Background(), drifted, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := indexer.Update(drifted); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if patches != 1 {
		t.Fatalf("drift repair generated %d patches, want 1", patches)
	}
	repaired, err := cs.CoreV1().Pods("ns").Get(context.Background(), "gpu", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Annotations[consts.NumaPlacementAnnotation] != gpuPlacement || repaired.Annotations[constants.NumaMemoryGroupsObserved] != "[]" {
		t.Fatalf("drifted annotations were not repaired: %v", repaired.Annotations)
	}
	if err := indexer.Update(repaired); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if patches != 1 {
		t.Fatalf("unchanged observations generated an additional patch: %d", patches)
	}
}

func TestReconcileRetriesFailedPatch(t *testing.T) {
	cs := fake.NewSimpleClientset(apiPod("ns", "gpu", "node-1", ""))
	patches := 0
	cs.PrependReactor("patch", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		patches++
		if patches == 1 {
			return true, nil, errors.New("temporary patch failure")
		}
		return false, nil, nil
	})
	exporter := newTestExporter(t, "node-1", &stubLister{pods: []*podresourcesv1.PodResources{gpuPod("ns", "gpu")}}, cs)
	for range 2 {
		if err := exporter.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if patches != 2 {
		t.Fatalf("patch attempts = %d, want 2", patches)
	}
	if got := annotationOf(t, cs, "ns", "gpu"); got != gpuPlacement {
		t.Fatalf("annotation = %q, want %q", got, gpuPlacement)
	}
}

func TestReconcileRetriesConflict(t *testing.T) {
	pod := apiPod("ns", "gpu", "node-1", "")
	cs := fake.NewSimpleClientset(pod)
	exporter := newTestExporter(t, "node-1", &stubLister{pods: []*podresourcesv1.PodResources{gpuPod("ns", "gpu")}}, cs)
	patches := 0
	cs.PrependReactor("patch", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		patches++
		if patches == 1 {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, pod.Name, errors.New("stale resource version"))
		}
		return false, nil, nil
	})
	if err := exporter.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(exporter.writtenGroups) != 0 {
		t.Fatal("conflicting patches must not establish observation history")
	}
	if err := exporter.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := annotationOf(t, cs, "ns", "gpu"); got != gpuPlacement {
		t.Fatalf("annotation = %q, want %q", got, gpuPlacement)
	}
}

func TestReconcileKeepsGroupHistoryUntilPodUIDChanges(t *testing.T) {
	pod := apiPod("ns", "memory", "node-1", "")
	pod.UID = "old"
	pod.Status.QOSClass = corev1.PodQOSGuaranteed
	pod.Spec.Containers = []corev1.Container{{Name: "main", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}}}}
	cs := fake.NewSimpleClientset(pod)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if err := indexer.Add(pod); err != nil {
		t.Fatal(err)
	}
	lister := &stubLister{pods: []*podresourcesv1.PodResources{{Namespace: "ns", Name: "memory", Containers: []*podresourcesv1.ContainerResources{{Name: "main", Memory: []*podresourcesv1.ContainerMemory{{MemoryType: "memory", Size: 10, Topology: &podresourcesv1.TopologyInfo{Nodes: []*podresourcesv1.NUMANode{{ID: 0}}}}}}}}}}
	exporter := New("node-1", time.Minute, time.Minute, lister, nil, cs)
	exporter.pods = corelisters.NewPodLister(indexer)
	check := func(want string) {
		t.Helper()
		if err := exporter.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		actual, err := cs.CoreV1().Pods("ns").Get(context.Background(), "memory", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := actual.Annotations[constants.NumaMemoryGroupsObserved]; got != want {
			t.Fatalf("memory groups = %q, want %q", got, want)
		}
	}
	check("null")
	lister.pods = nil
	check("null")
	if err := cs.CoreV1().Pods("ns").Delete(context.Background(), "memory", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	recreated := pod.DeepCopy()
	recreated.UID = "new"
	if _, err := cs.CoreV1().Pods("ns").Create(context.Background(), recreated, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := indexer.Update(recreated); err != nil {
		t.Fatal(err)
	}
	check("")
	if _, exists := exporter.writtenGroups[types.UID("old")]; exists {
		t.Fatal("recreated pods must remove the old UID's observation history")
	}
}

func TestReconcileMemoryGroupsRegressionAndRecovery(t *testing.T) {
	pod := apiPod("ns", "memory", "node-1", "")
	pod.UID = "pod-uid"
	pod.Status.QOSClass = corev1.PodQOSGuaranteed
	pod.Spec.Containers = []corev1.Container{{Name: "main", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}}}}
	cs := fake.NewSimpleClientset(pod)
	lister := &stubLister{}
	exporter := newTestExporter(t, "node-1", lister, cs)
	check := func(want string) {
		t.Helper()
		syncTestPods(t, exporter, cs)
		if err := exporter.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		actual, err := cs.CoreV1().Pods("ns").Get(context.Background(), "memory", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := actual.Annotations[constants.NumaMemoryGroupsObserved]; got != want {
			t.Fatalf("groups = %q, want %q", got, want)
		}
	}
	check("")
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "main", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
	if _, err := cs.CoreV1().Pods("ns").UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	lister.pods = []*podresourcesv1.PodResources{{Namespace: "ns", Name: "memory", Containers: []*podresourcesv1.ContainerResources{{Name: "main", Memory: []*podresourcesv1.ContainerMemory{{MemoryType: "memory", Size: 10, Topology: &podresourcesv1.TopologyInfo{Nodes: []*podresourcesv1.NUMANode{{ID: 1}, {ID: 0}}}}}}}}}
	complete := `[{"memoryNodes":["node-0","node-1"],"amount":{"memory":"10"}}]`
	check(complete)
	lister.pods = nil
	check("null")
	lister.pods = []*podresourcesv1.PodResources{{Namespace: "ns", Name: "memory", Containers: []*podresourcesv1.ContainerResources{{Name: "main", Memory: []*podresourcesv1.ContainerMemory{{MemoryType: "memory", Size: 10, Topology: &podresourcesv1.TopologyInfo{Nodes: []*podresourcesv1.NUMANode{{ID: 0}, {ID: 1}}}}}}}}}
	check(complete)
}

func TestReconcilePublishesPlacementForRecreatedPod(t *testing.T) {
	pod := apiPod("ns", "gpu", "node-1", "")
	pod.UID = types.UID("old")
	cs := fake.NewSimpleClientset(pod)
	exporter := newTestExporter(t, "node-1", &stubLister{pods: []*podresourcesv1.PodResources{gpuPod("ns", "gpu")}}, cs)
	if err := exporter.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := cs.CoreV1().Pods("ns").Delete(context.Background(), "gpu", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	pod.UID = types.UID("new")
	if _, err := cs.CoreV1().Pods("ns").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	syncTestPods(t, exporter, cs)
	if err := exporter.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := annotationOf(t, cs, "ns", "gpu"); got != gpuPlacement {
		t.Fatalf("annotation = %q", got)
	}
}
