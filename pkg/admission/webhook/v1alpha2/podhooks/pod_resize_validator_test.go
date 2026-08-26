// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podhooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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

func podWithRequests(namespace, name, pgName, schedulerName, cpu, memory string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: pgName,
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
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
	v := NewPodResizeValidator(fake.NewClientBuilder().WithScheme(scheme).Build(), testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", "other-scheduler", "1", "1Gi")
	newPod := podWithRequests("ns", "p", "pg", "other-scheduler", "2", "2Gi")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err)
}

func TestPodResizeValidator_AllowedWhenNoPodGroup(t *testing.T) {
	scheme := buildScheme()
	v := NewPodResizeValidator(fake.NewClientBuilder().WithScheme(scheme).Build(), testSchedulerName, true, false)

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"},
		Spec:       corev1.PodSpec{SchedulerName: testSchedulerName},
	}
	newPod := oldPod.DeepCopy()
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err)
}

func TestPodResizeValidator_AllowedOnDownsize(t *testing.T) {
	scheme := buildScheme()
	queue := newQueue("q", 4000, 4096, 0, "2", "2Gi")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "2", "2Gi")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "1Gi") // downsize
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "downsize should always be allowed")
}

func TestPodResizeValidator_DeniedWhenCPULimitExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue has 4000m limit, 2000m already allocated.
	queue := newQueue("q", 4000, -1, 0, "2", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	// Upsize by 3 CPU → total would be 2+3 = 5, over limit of 4
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "4", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "CPU upsize over limit should be denied")
}

func TestPodResizeValidator_AllowedWhenCPULimitNotExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue has 4000m limit, 2000m already allocated.
	queue := newQueue("q", 4000, -1, 0, "2", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	// Upsize by 1 CPU → total 2+1 = 3, under limit of 4
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "2", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err)
}

func TestPodResizeValidator_DeniedWhenMemoryLimitExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue has 4096 MB limit, 2048 MB allocated.
	queue := newQueue("q", -1, 4096, 0, "0", "2Gi")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "0", "1Gi")
	// Upsize by 3Gi → total 2Gi + 3Gi = 5Gi > 4096MB
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "0", "4Gi")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "memory upsize over limit should be denied")
}

func TestPodResizeValidator_AllowedWhenLimitIsUnlimited(t *testing.T) {
	scheme := buildScheme()
	// Limit = -1 means unlimited.
	queue := newQueue("q", -1, -1, 0, "100", "100Gi")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "1Gi")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "50", "50Gi")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "unlimited queue should never be denied")
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
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	// Upsize by 2 → non-preemptible total = 3+2 = 5 > quota 4
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "3", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "non-preemptible upsize over quota should be denied")
}

// TestPodResizeValidator_InfeasibleOldSpec verifies that a pod with an infeasible resize
// target (old spec=4, enacted=1) uses enacted as the delta baseline, not the spec.
// Queue allocated=1, limit=4. New target=5 → delta should be 5-1=4, over limit → denied.
func TestPodResizeValidator_InfeasibleOldSpec_DeltaUsesEnacted(t *testing.T) {
	scheme := buildScheme()
	queue := newQueue("q", 4000, -1, 0, "1", "0") // limit=4000m, allocated=1000m
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	// Old pod: spec=4 CPU (infeasible), enacted=1 CPU.
	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "4", "0")
	oldPod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: corev1.PodReasonInfeasible},
	}
	oldPod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "main",
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			},
			AllocatedResources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		},
	}
	// New target = 5 CPU. Correct delta = 5-1=4; queue 1+4=5 > limit 4 → denied.
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "5", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "delta should use enacted baseline, not infeasible spec")
}

// TestPodResizeValidator_UnchangedResourceNotCounted verifies that a resource whose
// pod-level spec is unchanged by this resize does not contribute to delta, even when an
// earlier infeasible attempt left a stale enacted value far below the old spec.
func TestPodResizeValidator_UnchangedResourceNotCounted(t *testing.T) {
	scheme := buildScheme()
	// Limit: 4000m CPU, 4 GB memory. Allocated: 1000m CPU, 1 GB memory.
	queue := newQueue("q", 4000, 4096, 0, "1", "1Gi")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	// Old pod: CPU spec=4 (infeasible), memory spec=8Gi (infeasible). Enacted=1/1Gi.
	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "4", "8Gi")
	oldPod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: corev1.PodReasonInfeasible},
	}
	oldPod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "main",
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
			AllocatedResources: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}
	// This resize only changes CPU (4→5); memory spec stays at 8Gi (same as old spec).
	// delta_cpu = 5-1=4 (infeasible baseline); delta_mem = 0 (pod-level spec unchanged).
	// CPU: 1(alloc)+4(delta)=5 > 4000m limit → denied.
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "5", "8Gi")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "CPU upsize over limit should be denied")
}

// TestPodResizeValidator_ZeroQuota_NonPreemptibleDenied verifies that a queue with
// quota=0 blocks non-preemptible upsizes (0 is a finite boundary, not unlimited).
func TestPodResizeValidator_ZeroQuota_NonPreemptibleDenied(t *testing.T) {
	scheme := buildScheme()
	// Quota=0 means no non-preemptible capacity.
	queue := newQueue("q", -1, -1, 0, "0", "0")
	pg := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns"},
		Spec: schedulingv2alpha2.PodGroupSpec{
			Queue:          "q",
			Preemptibility: v2alpha2.NonPreemptible,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "2", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "quota=0 should block non-preemptible upsize")
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
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "5", "0") // over quota, ok for preemptible
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "preemptible upsize should not be blocked by quota alone")
}

// TestPodResizeValidator_ValidateQuotaFalse_AlwaysAllowed verifies that when
// validateQuota=false the webhook admits all resizes without checking limits.
func TestPodResizeValidator_ValidateQuotaFalse_AlwaysAllowed(t *testing.T) {
	scheme := buildScheme()
	// Queue limit = 4 CPU, allocated = 3 CPU. Upsize by 2 would exceed it.
	queue := newQueue("q", 4000, -1, 0, "3", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, false /* validateQuota */, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "3", "0") // would exceed limit
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "validateQuota=false should admit even limit-exceeding resizes")
}

// TestPodResizeValidator_BlockUpsizeOnBounded_DeniedWhenLimitSet verifies that when
// blockUpsizeOnBoundedQueues=true any upsize on a queue with a finite limit is denied.
func TestPodResizeValidator_BlockUpsizeOnBounded_DeniedWhenLimitSet(t *testing.T) {
	scheme := buildScheme()
	// Queue has a finite CPU limit but plenty of headroom.
	queue := newQueue("q", 100000, -1, 0, "1", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, true /* blockUpsizeOnBoundedQueues */)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "2", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "blockUpsizeOnBoundedQueues should deny any CPU upsize on bounded queue")
}

// TestPodResizeValidator_BlockUpsizeOnBounded_AllowedWhenUnlimited verifies that when
// blockUpsizeOnBoundedQueues=true but the queue is fully unlimited, upsizes are allowed.
func TestPodResizeValidator_BlockUpsizeOnBounded_AllowedWhenUnlimited(t *testing.T) {
	scheme := buildScheme()
	queue := newQueue("q", -1, -1, -1, "1", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, true /* blockUpsizeOnBoundedQueues */)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "50", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "unlimited queue should never be blocked by blockUpsizeOnBoundedQueues")
}

// TestPodResizeValidator_BlockUpsizeOnBounded_NonPreemptibleQuota verifies that when
// blockUpsizeOnBoundedQueues=true, a non-preemptible upsize on a queue with finite
// quota is denied even when the limit is unlimited.
func TestPodResizeValidator_BlockUpsizeOnBounded_NonPreemptibleQuota(t *testing.T) {
	scheme := buildScheme()
	// Limit is unlimited, but quota is finite → non-preemptible upsize should be blocked.
	queue := newQueue("q", -1, -1, 4000, "0", "0")
	pg := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns"},
		Spec: schedulingv2alpha2.PodGroupSpec{
			Queue:          "q",
			Preemptibility: v2alpha2.NonPreemptible,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, true /* blockUpsizeOnBoundedQueues */)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "2", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "non-preemptible upsize on bounded quota should be denied")
}

// TestPodResizeValidator_SidecarUpsize_DeniedWhenLimitExceeded verifies that a
// restartable init container (sidecar) upsize is included in the delta and can be denied.
func TestPodResizeValidator_SidecarUpsize_DeniedWhenLimitExceeded(t *testing.T) {
	scheme := buildScheme()
	// Queue: 3000m CPU limit, 2000m already allocated (from the sidecar).
	queue := newQueue("q", 3000, -1, 0, "2", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	restartAlways := corev1.ContainerRestartPolicyAlways
	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p",
			Namespace: "ns",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: "pg",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
		Spec: corev1.PodSpec{
			SchedulerName: testSchedulerName,
			InitContainers: []corev1.Container{
				{
					Name:          "sidecar",
					RestartPolicy: &restartAlways,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
					},
				},
			},
		},
	}
	newPod := oldPod.DeepCopy()
	newPod.Spec.InitContainers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("4")
	// Sidecar upsize 2→4: delta=2; queue 2+2=4 > limit 3 → denied.
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "sidecar upsize over limit should be denied")
}

// TestPodResizeValidator_NonInfeasible_SpecIncludedInBaseline verifies that for a pod
// in a normal / InProgress state the old spec is included in the effective-old baseline,
// so that only the true net increase beyond what the queue already reflects is charged.
func TestPodResizeValidator_NonInfeasible_SpecIncludedInBaseline(t *testing.T) {
	scheme := buildScheme()
	// Queue limit=6000m, allocated=4000m (reflects old spec of 4 CPU, not enacted).
	queue := newQueue("q", 6000, -1, 0, "4", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	// Old pod: spec=4 CPU, enacted=1 CPU — state is InProgress, NOT infeasible.
	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "4", "0")
	oldPod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "main",
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			},
			AllocatedResources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		},
	}
	// New spec=5 CPU. Effective-old = max(spec=4, enacted=1, alloc=1) = 4.
	// delta=1; queue 4+1=5 < limit 6 → allowed.
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "5", "0")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "non-infeasible pod: effective-old should include old spec, reducing the delta")
}

// TestPodResizeValidator_Redistribution_AllowedAtLimit verifies that moving CPU from
// one container to another (same total) produces a zero delta and is admitted even
// when the queue is at its limit.
func TestPodResizeValidator_Redistribution_AllowedAtLimit(t *testing.T) {
	scheme := buildScheme()
	// Queue at exactly its limit (4000m allocated = 4000m limit).
	queue := newQueue("q", 4000, -1, 0, "4", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	makePod := func(cpuA, cpuB string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "p",
				Namespace: "ns",
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod: "pg",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				SchedulerName: testSchedulerName,
				Containers: []corev1.Container{
					{Name: "a", Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpuA)},
					}},
					{Name: "b", Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpuB)},
					}},
				},
			},
		}
	}
	// Move 1 CPU from container A to container B; pod total stays at 4 CPU.
	oldPod := makePod("2", "2")
	newPod := makePod("1", "3")
	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "CPU redistribution with unchanged pod total should be allowed even at limit")
}

// TestPodResizeValidator_InitPeakDominates_NoDelta covers a pod whose queue charge is
// dominated by the init-phase peak rather than the steady-state sum. Resizing a regular
// container below that peak does not change what the queue charges, so the delta must be
// zero even though the container itself grew.
func TestPodResizeValidator_InitPeakDominates_NoDelta(t *testing.T) {
	scheme := buildScheme()
	// Queue is exactly at its limit: 10 CPU limit, 10 CPU allocated (the init peak).
	queue := newQueue("q", 10000, -1, 0, "10", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	makePod := func(mainCPU string) *corev1.Pod {
		p := podWithRequests("ns", "p", "pg", testSchedulerName, mainCPU, "0")
		p.Spec.InitContainers = []corev1.Container{
			{
				Name: "heavy-init",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10")},
				},
			},
		}
		return p
	}
	// Pod charge = max(steady, initPeak) = max(1,10) = 10 before, max(2,10) = 10 after.
	oldPod := makePod("1")
	newPod := makePod("2")

	assert.Empty(t, podResizeDelta(oldPod, newPod), "init-peak-dominated resize should produce no delta")

	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "queue charge is unchanged, so the resize must be admitted at the limit")
}

func TestPodResizeValidator_PendingUnscheduled_NotValidated(t *testing.T) {
	scheme := buildScheme()
	// Queue already at its limit — a checked upsize would be denied.
	queue := newQueue("q", 2000, -1, 0, "2", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	oldPod.Status.Phase = corev1.PodPending
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "5", "0")

	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.NoError(t, err, "unscheduled pending pod resize must not be quota-checked")
}

func TestPodResizeValidator_PendingScheduled_StillValidated(t *testing.T) {
	scheme := buildScheme()
	queue := newQueue("q", 2000, -1, 0, "2", "0")
	pg := newPodGroup("pg", "ns", "q")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(queue, pg).Build()
	v := NewPodResizeValidator(c, testSchedulerName, true, false)

	oldPod := podWithRequests("ns", "p", "pg", testSchedulerName, "1", "0")
	oldPod.Status.Phase = corev1.PodPending
	oldPod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
	}
	newPod := podWithRequests("ns", "p", "pg", testSchedulerName, "5", "0")

	_, err := v.ValidateUpdate(context.Background(), oldPod, newPod)
	assert.Error(t, err, "scheduled pending pod holds capacity and must be checked")
}
