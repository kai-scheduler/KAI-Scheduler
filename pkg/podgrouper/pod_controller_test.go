// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
)

type testLogSink struct {
	buffer *bytes.Buffer
}

func (s *testLogSink) Init(info logr.RuntimeInfo) {}
func (s *testLogSink) Enabled(level int) bool     { return true }
func (s *testLogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	s.buffer.WriteString(fmt.Sprintf("[%d] %s", level, msg))
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			s.buffer.WriteString(fmt.Sprintf(" %v=%v", keysAndValues[i], keysAndValues[i+1]))
		}
	}
	s.buffer.WriteString("\n")
}
func (s *testLogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	s.buffer.WriteString(fmt.Sprintf("ERROR %s: %v", msg, err))
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			s.buffer.WriteString(fmt.Sprintf(" %v=%v", keysAndValues[i], keysAndValues[i+1]))
		}
	}
	s.buffer.WriteString("\n")
}
func (s *testLogSink) WithValues(keysAndValues ...interface{}) logr.LogSink { return s }
func (s *testLogSink) WithName(name string) logr.LogSink                    { return s }

const nodePoolKey = "kai.scheduler/node-pool"

func TestAddNodePoolLabel(t *testing.T) {
	metadata := podgroup.Metadata{
		Annotations:       nil,
		Labels:            nil,
		PriorityClassName: "",
		Queue:             "",
		Namespace:         "",
		Name:              "",
		MinAvailable:      0,
		Owner:             metav1.OwnerReference{},
	}

	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				nodePoolKey: "my-node-pool",
			},
		},
		Spec:   v1.PodSpec{},
		Status: v1.PodStatus{},
	}

	addNodePoolLabel(&metadata, &pod, nodePoolKey)
	assert.Equal(t, "my-node-pool", metadata.Labels[nodePoolKey])

	metadata.Labels = nil
	pod.Labels = nil

	addNodePoolLabel(&metadata, &pod, nodePoolKey)
	assert.Equal(t, "", metadata.Labels[nodePoolKey])

	metadata.Labels = map[string]string{
		nodePoolKey: "non-default-pool",
	}

	addNodePoolLabel(&metadata, &pod, nodePoolKey)
	assert.Equal(t, "non-default-pool", metadata.Labels[nodePoolKey])
}

func TestIsOrphanPodWithPodGroup(t *testing.T) {
	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				constants.PodGroupAnnotationForPod: "blabla",
			},
		},
		Spec:   v1.PodSpec{},
		Status: v1.PodStatus{},
	}

	assert.True(t, isOrphanPodWithPodGroup(&pod))

	pod.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "gorilla",
			Name:       "harambe",
			UID:        "1",
		},
	}
	assert.False(t, isOrphanPodWithPodGroup(&pod))
}

func TestEventOnFailure(t *testing.T) {
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "my-namespace",
		},
		Spec: v1.PodSpec{
			SchedulerName: "kai-scheduler",
		},
		Status: v1.PodStatus{},
	}
	fakeClient := fake.NewClientBuilder().WithObjects(&pod).Build()
	fakeEventRecorder := record.NewFakeRecorder(10)

	podReconciler := PodReconciler{
		Client:          fakeClient,
		Scheme:          scheme.Scheme,
		podGrouper:      &fakePodGrouper{},
		PodGroupHandler: nil,
		configs: Configs{
			SchedulerName: "kai-scheduler",
		},
		eventRecorder: fakeEventRecorder,
	}

	podReconciler.Reconcile(context.TODO(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		},
	})

	if len(fakeEventRecorder.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(fakeEventRecorder.Events))
	}

}

type fakePodGrouper struct{}

func (*fakePodGrouper) GetPGMetadata(ctx context.Context, pod *v1.Pod, topOwner *unstructured.Unstructured, allOwners []*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	return nil, nil
}

func (*fakePodGrouper) GetPodOwners(ctx context.Context, pod *v1.Pod) (*unstructured.Unstructured, []*metav1.PartialObjectMetadata, error) {
	return nil, nil, fmt.Errorf("failed")
}

func TestEventFilterFn(t *testing.T) {
	// Setup test cases
	tests := []struct {
		name            string
		podNamespace    string
		namespaceLabels map[string]string
		selector        map[string]string
		expectedResult  bool
		expectError     bool
	}{
		{
			name:            "empty selector should allow all pods",
			podNamespace:    "test-ns",
			namespaceLabels: map[string]string{"environment": "test"},
			selector:        nil,
			expectedResult:  true,
			expectError:     false,
		},
		{
			name:            "matching namespace labels",
			podNamespace:    "test-ns",
			namespaceLabels: map[string]string{"environment": "test", "team": "ai"},
			selector:        map[string]string{"environment": "test", "team": "ai"},
			expectedResult:  true,
			expectError:     false,
		},
		{
			name:            "non-matching namespace labels",
			podNamespace:    "test-ns",
			namespaceLabels: map[string]string{"environment": "test"},
			selector:        map[string]string{"environment": "production"},
			expectedResult:  false,
			expectError:     false,
		},
		{
			name:            "partial match should fail",
			podNamespace:    "test-ns",
			namespaceLabels: map[string]string{"environment": "test", "team": "ai"},
			selector:        map[string]string{"environment": "test", "team": "ml"},
			expectedResult:  false,
			expectError:     false,
		},
		{
			name:            "missing namespace should return false",
			podNamespace:    "non-existent-ns",
			namespaceLabels: map[string]string{"environment": "test"},
			selector:        map[string]string{"environment": "test"},
			expectedResult:  false,
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			if !tt.expectError {
				objs = append(objs,
					&v1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name:   tt.podNamespace,
							Labels: tt.namespaceLabels,
						},
					})
			}

			fakeClient := fake.NewClientBuilder().WithObjects(objs...).Build()
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: tt.podNamespace,
				},
			}

			filter := eventFilterFn(fakeClient, Configs{
				NamespaceLabelSelector: tt.selector,
			})
			result := filter(pod)
			assert.Equal(t, tt.expectedResult, result, "eventFilterFn() = %v, want %v", result, tt.expectedResult)
		})
	}
}

func TestLabelsMatch(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		selector map[string]string
		expected bool
	}{
		{
			name:     "empty labels and empty selector",
			labels:   map[string]string{},
			selector: map[string]string{},
			expected: true,
		},
		{
			name:     "non-empty labels and empty selector",
			labels:   map[string]string{"key1": "value1", "key2": "value2"},
			selector: map[string]string{},
			expected: true,
		},
		{
			name:     "matching single label",
			labels:   map[string]string{"key1": "value1"},
			selector: map[string]string{"key1": "value1"},
			expected: true,
		},
		{
			name:     "matching multiple labels",
			labels:   map[string]string{"key1": "value1", "key2": "value2", "key3": "value3"},
			selector: map[string]string{"key1": "value1", "key2": "value2"},
			expected: true,
		},
		{
			name:     "missing label key",
			labels:   map[string]string{"key1": "value1"},
			selector: map[string]string{"key2": "value2"},
			expected: false,
		},
		{
			name:     "label value mismatch",
			labels:   map[string]string{"key1": "value1"},
			selector: map[string]string{"key1": "different-value"},
			expected: false,
		},
		{
			name:     "nil labels",
			labels:   nil,
			selector: map[string]string{"key1": "value1"},
			expected: false,
		},
		{
			name:     "nil selector",
			labels:   map[string]string{"key1": "value1"},
			selector: nil,
			expected: true, // matches the behavior of the function where nil selector is treated as empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := labelsMatch(tt.labels, tt.selector)
			assert.Equal(t, tt.expected, result, "labelsMatch() = %v, want %v", result, tt.expected)
		})
	}
}

func TestReconcilePodNotFound(t *testing.T) {
	testNamespace := "test-namespace"
	testPodName := "test-pod"

	var logBuffer bytes.Buffer
	ctx := log.IntoContext(context.TODO(), logr.New(&testLogSink{buffer: &logBuffer}))
	fakeClient := fake.NewClientBuilder().Build()

	reconciler := PodReconciler{
		Client:          fakeClient,
		Scheme:          scheme.Scheme,
		podGrouper:      &fakePodGrouper{},
		PodGroupHandler: nil,
		configs: Configs{
			SchedulerName: "kai-scheduler",
		},
		eventRecorder: record.NewFakeRecorder(10),
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: testNamespace,
			Name:      testPodName,
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	logOutput := logBuffer.String()
	expectedPattern := fmt.Sprintf("Pod %s/%s not found", testNamespace, testPodName)

	assert.Contains(t, logOutput, expectedPattern,
		"Log should contain correct namespace/name from req, got: %s", logOutput)
	assert.NotContains(t, logOutput, "Pod / not found",
		"Log should not contain empty namespace/name pattern")
}

func TestAssignPodToGroupAndSubGroup(t *testing.T) {
	tests := []struct {
		name             string
		pod              *v1.Pod
		metadata         *podgroup.Metadata
		expectedPodGroup string
		expectedSubGroup string
		expectPatch      bool
	}{
		{
			name: "assigns podgroup annotation and subgroup label",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "test-ns",
					Annotations: map[string]string{},
					Labels:      map[string]string{},
				},
			},
			metadata: &podgroup.Metadata{
				Name:      "test-pg",
				Namespace: "test-ns",
				SubGroups: []*podgroup.SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{"test-pod"},
					},
				},
			},
			expectedPodGroup: "test-pg",
			expectedSubGroup: "subgroup-1",
			expectPatch:      true,
		},
		{
			name: "assigns only podgroup when pod not in subgroup",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "test-ns",
					Annotations: map[string]string{},
					Labels:      map[string]string{},
				},
			},
			metadata: &podgroup.Metadata{
				Name:      "test-pg",
				Namespace: "test-ns",
				SubGroups: []*podgroup.SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{"other-pod"},
					},
				},
			},
			expectedPodGroup: "test-pg",
			expectedSubGroup: "",
			expectPatch:      true,
		},
		{
			name: "no patch when already correct",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					Annotations: map[string]string{
						constants.PodGroupAnnotationForPod: "test-pg",
					},
					Labels: map[string]string{
						constants.SubGroupLabelKey: "subgroup-1",
					},
				},
			},
			metadata: &podgroup.Metadata{
				Name:      "test-pg",
				Namespace: "test-ns",
				SubGroups: []*podgroup.SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{"test-pod"},
					},
				},
			},
			expectedPodGroup: "test-pg",
			expectedSubGroup: "subgroup-1",
			expectPatch:      false,
		},
		{
			name: "handles nil annotations and labels",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			metadata: &podgroup.Metadata{
				Name:      "test-pg",
				Namespace: "test-ns",
				SubGroups: []*podgroup.SubGroupMetadata{},
			},
			expectedPodGroup: "test-pg",
			expectedSubGroup: "",
			expectPatch:      true,
		},
		{
			name: "updates when podgroup differs",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					Annotations: map[string]string{
						constants.PodGroupAnnotationForPod: "old-pg",
					},
					Labels: map[string]string{},
				},
			},
			metadata: &podgroup.Metadata{
				Name:      "new-pg",
				Namespace: "test-ns",
				SubGroups: []*podgroup.SubGroupMetadata{},
			},
			expectedPodGroup: "new-pg",
			expectedSubGroup: "",
			expectPatch:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuffer bytes.Buffer
			ctx := log.IntoContext(context.TODO(), logr.New(&testLogSink{buffer: &logBuffer}))
			fakeClient := fake.NewClientBuilder().WithObjects(tt.pod).Build()

			reconciler := PodReconciler{
				Client: fakeClient,
			}

			err := reconciler.assignPodToGroupAndSubGroup(ctx, tt.pod, tt.metadata)
			assert.NoError(t, err)

			updatedPod := &v1.Pod{}
			err = fakeClient.Get(ctx, types.NamespacedName{Namespace: tt.pod.Namespace, Name: tt.pod.Name}, updatedPod)
			assert.NoError(t, err)

			assert.Equal(t, tt.expectedPodGroup, updatedPod.Annotations[constants.PodGroupAnnotationForPod])
			if tt.expectedSubGroup != "" {
				assert.Equal(t, tt.expectedSubGroup, updatedPod.Labels[constants.SubGroupLabelKey])
			}
		})
	}
}
