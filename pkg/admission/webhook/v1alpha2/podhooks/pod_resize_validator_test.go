// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podhooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	v2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

const testSchedulerName = "kai-scheduler"

func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v2.AddToScheme(s)
	_ = v2alpha2.AddToScheme(s)
	return s
}

func newQueue(name string, cpuLimit, memLimitMB, cpuQuota float64, cpuAlloc, memAlloc string) *v2.Queue {
	q := &v2.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v2.QueueSpec{
			Resources: &v2.QueueResources{
				CPU:    v2.QueueResource{Limit: cpuLimit, Quota: cpuQuota},
				Memory: v2.QueueResource{Limit: memLimitMB},
				GPU:    v2.QueueResource{Limit: -1},
			},
		},
	}
	if cpuAlloc != "" {
		q.Status.Allocated = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpuAlloc),
			corev1.ResourceMemory: resource.MustParse(memAlloc),
		}
	}
	return q
}

func newPodGroup(name, namespace, queue string) *schedulingv2alpha2.PodGroup {
	return &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: schedulingv2alpha2.PodGroupSpec{
			Queue:          queue,
			Preemptibility: v2alpha2.Preemptible,
		},
	}
}

func encodePod(t *testing.T, pod *corev1.Pod) runtime.RawExtension {
	t.Helper()
	raw, err := json.Marshal(pod)
	require.NoError(t, err)
	return runtime.RawExtension{Raw: raw}
}

func makeRequest(t *testing.T, oldPod, newPod *corev1.Pod) admission.Request {
	t.Helper()
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation:   admissionv1.Update,
			Resource:    metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			SubResource: "resize",
			Object:      encodePod(t, newPod),
			OldObject:   encodePod(t, oldPod),
		},
	}
}

func podWithRequests(namespace, name, pgName, schedulerName, cpu, memory string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: pgName,
			},
		},
		Spec: corev1.PodSpec{
			SchedulerName: schedulerName,
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpu),
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
		},
	}
	return pod
}

func TestPodResizeValidator_AllowedWhenNotKAIPod(t *testing.T) {
	scheme := buildScheme()
	v := NewPodResizeValidator(fake.NewClientBuilder().WithScheme(scheme).Build(), scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", "other-scheduler", "1", "1Gi")
	newPod := podWithRequests("ns", "p", "pg", "other-scheduler", "2", "2Gi")
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.True(t, resp.Allowed)
}

func TestPodResizeValidator_AllowedWhenNoPodGroup(t *testing.T) {
	scheme := buildScheme()
	v := NewPodResizeValidator(fake.NewClientBuilder().WithScheme(scheme).Build(), scheme, testSchedulerName)

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"},
		Spec:       corev1.PodSpec{SchedulerName: testSchedulerName},
	}
	newPod := oldPod.DeepCopy()
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.True(t, resp.Allowed)
}

func TestPodResizeValidator_AllowedOnDownsize(t *testing.T) {
	scheme := buildScheme()
	queue := newQueue("q", 4000, 4096, 0, "2", "2Gi")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "2", "2Gi")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "1Gi") // downsize
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.True(t, resp.Allowed, "downsize should always be allowed")
}

func TestPodResizeValidator_DeniedWhenCPULimitExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue has 4000m limit, 2000m already allocated.
	queue := newQueue("q", 4000, -1, 0, "2", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	// Upsize by 3 CPU → total would be 2+3 = 5, over limit of 4
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "4", "0")
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.False(t, resp.Allowed, "CPU upsize over limit should be denied")
}

func TestPodResizeValidator_AllowedWhenCPULimitNotExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue has 4000m limit, 2000m already allocated.
	queue := newQueue("q", 4000, -1, 0, "2", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	// Upsize by 1 CPU → total 2+1 = 3, under limit of 4
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "2", "0")
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.True(t, resp.Allowed)
}

func TestPodResizeValidator_DeniedWhenMemoryLimitExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue has 4096 MB limit, 2048 MB allocated.
	queue := newQueue("q", -1, 4096, 0, "0", "2Gi")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "0", "1Gi")
	// Upsize by 3Gi → total 2Gi + 3Gi = 5Gi > 4096MB
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "0", "4Gi")
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.False(t, resp.Allowed, "memory upsize over limit should be denied")
}

func TestPodResizeValidator_AllowedWhenLimitIsUnlimited(t *testing.T) {
	scheme := buildScheme()
	// Limit = -1 means unlimited.
	queue := newQueue("q", -1, -1, 0, "100", "100Gi")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "1Gi")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "50", "50Gi")
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.True(t, resp.Allowed, "unlimited queue should never be denied")
}

func TestPodResizeValidator_NonPreemptible_DeniedWhenQuotaExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue: unlimited limit, 4000m quota. 3000m non-preemptible already used.
	queue := newQueue("q", -1, -1, 4000, "0", "0")
	queue.Status.AllocatedNonPreemptible = corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("3"),
	}
	pg := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns"},
		Spec: schedulingv2alpha2.PodGroupSpec{
			Queue:          "q",
			Preemptibility: v2alpha2.NonPreemptible,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	// Upsize by 2 → non-preemptible total = 3+2 = 5 > quota 4
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "3", "0")
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.False(t, resp.Allowed, "non-preemptible upsize over quota should be denied")
}

func TestPodResizeValidator_Preemptible_AllowedWhenOnlyQuotaExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue: unlimited limit, small quota. Preemptible pods are not quota-checked.
	queue := newQueue("q", -1, -1, 1000, "0", "0") // quota = 1 CPU
	pg := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns"},
		Spec: schedulingv2alpha2.PodGroupSpec{
			Queue:          "q",
			Preemptibility: v2alpha2.Preemptible,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, scheme, testSchedulerName)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "5", "0") // over quota, ok for preemptible
	resp := v.Handle(context.Background(), makeRequest(t, oldPod, newPod))
	assert.True(t, resp.Allowed, "preemptible upsize should not be blocked by quota alone")
}
