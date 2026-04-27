// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package workload

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
)

const testNamespace = "team-a"

func baseMetadata() *podgroup.Metadata {
	return &podgroup.Metadata{
		Namespace:         testNamespace,
		Name:              "pg-owner-uid",
		MinAvailable:      1,
		Queue:             "base-queue",
		PriorityClassName: "train",
		Preemptibility:    v2alpha2.Preemptible,
		Labels:            map[string]string{"owner-label": "a"},
		Annotations:       map[string]string{"owner-annotation": "1"},
		SubGroups: []*podgroup.SubGroupMetadata{
			{Name: "sg-from-top-owner", MinAvailable: 2},
		},
	}
}

func newPod(name string, ref *corev1.WorkloadReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: name},
		Spec:       corev1.PodSpec{WorkloadRef: ref},
	}
}

// buildReaderWith builds a controller-runtime fake client.Reader seeded
// with the given Workloads — the same shape ApplyOverride consumes in
// production (the manager's cached client).
func buildReaderWith(t *testing.T, workloads ...*schedulingv1alpha1.Workload) (client.Reader, func()) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv1alpha1.AddToScheme(scheme))
	objs := make([]client.Object, 0, len(workloads))
	for _, w := range workloads {
		objs = append(objs, w)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return c, func() {}
}

func TestApplyOverride_NoWorkloadRef_NoChange(t *testing.T) {
	base := baseMetadata()
	pod := newPod("p1", nil)
	reader, stop := buildReaderWith(t)
	defer stop()

	got, err := ApplyOverride(context.Background(), base, pod, nil, reader)
	require.NoError(t, err)
	assert.Same(t, base, got, "no workloadRef -> base metadata returned unchanged")
}

func TestApplyOverride_Ignored_OnPod(t *testing.T) {
	base := baseMetadata()
	pod := newPod("p1", &corev1.WorkloadReference{Name: "w", PodGroup: "g"})
	pod.Annotations = map[string]string{commonconstants.WorkloadIgnoreAnnotationKey: "true"}
	reader, stop := buildReaderWith(t)
	defer stop()

	got, err := ApplyOverride(context.Background(), base, pod, nil, reader)
	require.NoError(t, err)
	assert.Same(t, base, got)
}

func TestApplyOverride_Ignored_OnTopOwner(t *testing.T) {
	base := baseMetadata()
	pod := newPod("p1", &corev1.WorkloadReference{Name: "w", PodGroup: "g"})
	top := &unstructured.Unstructured{}
	top.SetAnnotations(map[string]string{commonconstants.WorkloadIgnoreAnnotationKey: "true"})
	reader, stop := buildReaderWith(t)
	defer stop()

	got, err := ApplyOverride(context.Background(), base, pod, top, reader)
	require.NoError(t, err)
	assert.Same(t, base, got)
}

func TestApplyOverride_Gang(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "my-training"},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name: "workers",
				Policy: schedulingv1alpha1.PodGroupPolicy{
					Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 4},
				},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	pod := newPod("worker-0", &corev1.WorkloadReference{
		Name: "my-training", PodGroup: "workers", PodGroupReplicaKey: "0",
	})

	got, err := ApplyOverride(context.Background(), baseMetadata(), pod, nil, lister)
	require.NoError(t, err)
	assert.Equal(t, "my-training-workers-0", got.Name)
	assert.Equal(t, int32(4), got.MinAvailable)
	assert.Nil(t, got.SubGroups, "top-owner SubGroups must be dropped")
}

func TestApplyOverride_Gang_NoReplicaKey(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "w"},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "g",
				Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	got, err := ApplyOverride(context.Background(), baseMetadata(), newPod("p", &corev1.WorkloadReference{Name: "w", PodGroup: "g"}), nil, lister)
	require.NoError(t, err)
	assert.Equal(t, "w-g", got.Name)
	assert.Equal(t, int32(2), got.MinAvailable)
}

func TestApplyOverride_Basic_CollapsesReplicas(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "w"},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "g",
				Policy: schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	// Even with a replicaKey, Basic collapses into a single PodGroup.
	got, err := ApplyOverride(context.Background(), baseMetadata(), newPod("p", &corev1.WorkloadReference{
		Name: "w", PodGroup: "g", PodGroupReplicaKey: "ignored",
	}), nil, lister)
	require.NoError(t, err)
	assert.Equal(t, "w-g", got.Name)
	assert.Equal(t, int32(1), got.MinAvailable)
}

func TestApplyOverride_OverridesScheduling(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace, Name: "w",
			Labels: map[string]string{
				commonconstants.DefaultQueueLabel: "ml-training",
				"priorityClassName":               "build",
				"kai.scheduler/preemptibility":    "non-preemptible",
				"wl-label":                        "wl",
			},
			Annotations: map[string]string{
				"kai.scheduler/topology":                     "gpu-topology",
				"kai.scheduler/topology-required-placement":  "rack",
				"kai.scheduler/topology-preferred-placement": "zone",
				"wl-annotation":                              "wa",
			},
		},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "g",
				Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 3}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	got, err := ApplyOverride(context.Background(), baseMetadata(), newPod("p", &corev1.WorkloadReference{Name: "w", PodGroup: "g"}), nil, lister)
	require.NoError(t, err)

	assert.Equal(t, "ml-training", got.Queue)
	assert.Equal(t, "build", got.PriorityClassName)
	assert.Equal(t, v2alpha2.NonPreemptible, got.Preemptibility)
	assert.Equal(t, "gpu-topology", got.Topology)
	assert.Equal(t, "rack", got.RequiredTopologyLevel)
	assert.Equal(t, "zone", got.PreferredTopologyLevel)
	// Labels/annotations merged, Workload wins on collision (none here),
	// both sets present.
	assert.Equal(t, "a", got.Labels["owner-label"])
	assert.Equal(t, "wl", got.Labels["wl-label"])
	assert.Equal(t, "1", got.Annotations["owner-annotation"])
	assert.Equal(t, "wa", got.Annotations["wl-annotation"])
}

func TestApplyOverride_LabelCollision_WorkloadWins(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace, Name: "w",
			Labels: map[string]string{"shared": "from-workload"},
		},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "g",
				Policy: schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	base := baseMetadata()
	base.Labels["shared"] = "from-base"

	got, err := ApplyOverride(context.Background(), base, newPod("p", &corev1.WorkloadReference{Name: "w", PodGroup: "g"}), nil, lister)
	require.NoError(t, err)
	assert.Equal(t, "from-workload", got.Labels["shared"])
}

func TestApplyOverride_WorkloadMissing(t *testing.T) {
	lister, stop := buildReaderWith(t /* nothing */)
	defer stop()
	_, err := ApplyOverride(context.Background(), baseMetadata(), newPod("p", &corev1.WorkloadReference{Name: "ghost", PodGroup: "g"}), nil, lister)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkloadNotFound), "got %v", err)
}

func TestApplyOverride_PodGroupMissing(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "w"},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "other",
				Policy: schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	_, err := ApplyOverride(context.Background(), baseMetadata(), newPod("p", &corev1.WorkloadReference{Name: "w", PodGroup: "missing"}), nil, lister)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkloadPodGroupNotFound), "got %v", err)
}

// Workload > Top Owner > Pod fallback: when the Workload carries none of the
// KAI scheduling labels/annotations, base values produced by the top-owner
// plugin must survive untouched. Only Name, MinAvailable, and SubGroups are
// always-overridden by the Workload (they're structural to the grouping
// decision); everything else falls through.
func TestApplyOverride_FieldFallback_NoWorkloadLabels(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "w"},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "g",
				Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	base := baseMetadata()
	got, err := ApplyOverride(context.Background(), base, newPod("p", &corev1.WorkloadReference{Name: "w", PodGroup: "g"}), nil, lister)
	require.NoError(t, err)

	// Always-overridden fields.
	assert.Equal(t, "w-g", got.Name)
	assert.Equal(t, int32(2), got.MinAvailable)
	assert.Nil(t, got.SubGroups)

	// Fallback fields: base values from top-owner survive.
	assert.Equal(t, base.Queue, got.Queue)
	assert.Equal(t, base.PriorityClassName, got.PriorityClassName)
	assert.Equal(t, base.Preemptibility, got.Preemptibility)
	assert.Equal(t, base.Topology, got.Topology)
	assert.Equal(t, base.RequiredTopologyLevel, got.RequiredTopologyLevel)
	assert.Equal(t, base.PreferredTopologyLevel, got.PreferredTopologyLevel)
	assert.Equal(t, base.Owner, got.Owner)
}

// An empty-string label/annotation on the Workload must NOT override the base
// value — the design's "Workload-wins" rule kicks in only on a meaningful
// declaration. The current implementation guards each field with `v != ""`.
func TestApplyOverride_FieldFallback_EmptyWorkloadLabel(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace, Name: "w",
			Labels: map[string]string{
				commonconstants.DefaultQueueLabel: "",
				"priorityClassName":               "",
				"kai.scheduler/preemptibility":    "",
			},
			Annotations: map[string]string{
				"kai.scheduler/topology":                     "",
				"kai.scheduler/topology-required-placement":  "",
				"kai.scheduler/topology-preferred-placement": "",
			},
		},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "g",
				Policy: schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	base := baseMetadata()
	got, err := ApplyOverride(context.Background(), base, newPod("p", &corev1.WorkloadReference{Name: "w", PodGroup: "g"}), nil, lister)
	require.NoError(t, err)

	assert.Equal(t, base.Queue, got.Queue, "empty queue label must not blank out base")
	assert.Equal(t, base.PriorityClassName, got.PriorityClassName)
	assert.Equal(t, base.Preemptibility, got.Preemptibility)
	assert.Equal(t, base.Topology, got.Topology)
	assert.Equal(t, base.RequiredTopologyLevel, got.RequiredTopologyLevel)
	assert.Equal(t, base.PreferredTopologyLevel, got.PreferredTopologyLevel)
}

// Unknown preemptibility values fall back to base — the workload plugin
// doesn't validate (the KAI admission webhook does), but it shouldn't blank
// the field either.
func TestApplyOverride_UnknownPreemptibility_FallsBack(t *testing.T) {
	wl := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace, Name: "w",
			Labels: map[string]string{"kai.scheduler/preemptibility": "garbage"},
		},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{{
				Name:   "g",
				Policy: schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}},
			}},
		},
	}
	lister, stop := buildReaderWith(t, wl)
	defer stop()

	base := baseMetadata()
	got, err := ApplyOverride(context.Background(), base, newPod("p", &corev1.WorkloadReference{Name: "w", PodGroup: "g"}), nil, lister)
	require.NoError(t, err)
	assert.Equal(t, base.Preemptibility, got.Preemptibility)
}

// Workload.metadata.name is a DNS subdomain (253), workloadRef.podGroup and
// podGroupReplicaKey are DNS labels (63 each). Worst-case naive concatenation
// produces 253+1+63+1+63 = 381 chars, which would be rejected by the apiserver
// when used as the synthesized KAI PodGroup CR's metadata.name. The synthesizer
// must keep its output a valid DNS-1123 subdomain.

func TestBuildPodGroupName_ShortInputs_NoTruncation(t *testing.T) {
	gang := schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}}
	basic := schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}}

	assert.Equal(t, "wl-pg", generatePodGroupName("wl", "pg", "", basic))
	assert.Equal(t, "wl-pg", generatePodGroupName("wl", "pg", "", gang))
	assert.Equal(t, "wl-pg-r0", generatePodGroupName("wl", "pg", "r0", gang))
	assert.Equal(t, "wl-pg", generatePodGroupName("wl", "pg", "r0", basic))
}

func TestBuildPodGroupName_OverflowingInputs_TruncatedAndDNSValid(t *testing.T) {
	longWorkload := strings.Repeat("a", 253)
	pg := strings.Repeat("b", 63)
	rk := strings.Repeat("c", 63)
	gang := schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}}

	got := generatePodGroupName(longWorkload, pg, rk, gang)

	assert.LessOrEqual(t, len(got), validation.DNS1123SubdomainMaxLength,
		"synthesized name must fit a DNS subdomain (253)")
	assert.Empty(t, validation.IsDNS1123Subdomain(got),
		"synthesized name %q must be a valid DNS-1123 subdomain", got)
}

func TestBuildPodGroupName_DistinctOverflowingInputs_DistinctOutputs(t *testing.T) {
	prefix := strings.Repeat("a", 253)
	gang := schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}}

	a := generatePodGroupName(prefix, "alpha", "r0", gang)
	b := generatePodGroupName(prefix, "alpha", "r1", gang)
	c := generatePodGroupName(prefix, "beta", "r0", gang)

	assert.NotEqual(t, a, b, "different replicaKeys must produce different names")
	assert.NotEqual(t, a, c, "different podGroups must produce different names")
	assert.NotEqual(t, b, c)
}

func TestBuildPodGroupName_DeterministicAcrossCalls(t *testing.T) {
	longWorkload := strings.Repeat("a", 253)
	gang := schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}}

	first := generatePodGroupName(longWorkload, "pg", "r0", gang)
	for range 5 {
		assert.Equal(t, first, generatePodGroupName(longWorkload, "pg", "r0", gang))
	}
}
