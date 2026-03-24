// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package knative

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	knative "knative.dev/serving/pkg/apis/serving/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestGetPodGroupMetadata(t *testing.T) {
	service := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Service",
			"apiVersion": "serving.knative.dev/v1",
			"metadata": map[string]interface{}{
				"name":      "test_service",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							queueLabelKey: "test_queue",
						},
					},
					"spec": map[string]interface{}{
						"schedulerName": "kai-scheduler",
						"containers": []map[string]interface{}{{
							"name": "container",
						}},
					},
				},
			},
		},
	}

	rev := &knative.Revision{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Revision",
			APIVersion: "serving.knative.dev/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "revision",
			Namespace: "test_namespace",
			UID:       "2",
		},
		Spec:   knative.RevisionSpec{},
		Status: knative.RevisionStatus{},
	}

	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "test_namespace",
			Labels: map[string]string{
				knativeRevisionLabel: "revision",
				queueLabelKey:        "test_queue",
			},
			UID: "3",
		},
		Spec:   v1.PodSpec{},
		Status: v1.PodStatus{},
	}

	scheme := runtime.NewScheme()
	err := knative.AddToScheme(scheme)
	assert.Nil(t, err)
	err = v1.AddToScheme(scheme)
	assert.Nil(t, err)
	err = schedulingv1.AddToScheme(scheme)
	assert.Nil(t, err)

	inferencePriorityClass := priorityClassObj(constants.InferencePriorityClass, 1000)

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(rev, inferencePriorityClass).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	grouper := NewKnativeGrouper(client, defaultGrouper, true)

	metadata, err := grouper.GetPodGroupMetadata(service, pod)
	assert.Nil(t, err)
	assert.Equal(t, "pg-revision-2", metadata.Name)
	assert.Equal(t, constants.InferencePriorityClass, metadata.PriorityClassName)
	assert.Equal(t, "test_queue", metadata.Queue)
}

func TestGetPodGroupMetadata_MinScale(t *testing.T) {
	service := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Service",
			"apiVersion": "serving.knative.dev/v1",
			"metadata": map[string]interface{}{
				"name":      "test_service",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							queueLabelKey: "test_queue",
						},
					},
					"spec": map[string]interface{}{
						"schedulerName": "kai-scheduler",
						"containers": []map[string]interface{}{{
							"name": "container",
						}},
					},
				},
			},
		},
	}

	rev := &knative.Revision{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Revision",
			APIVersion: "serving.knative.dev/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "revision",
			Namespace: "test_namespace",
			UID:       "2",
			Annotations: map[string]string{
				"autoscaling.knative.dev/min-scale": "3",
			},
		},
		Spec:   knative.RevisionSpec{},
		Status: knative.RevisionStatus{},
	}

	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "test_namespace",
			Labels: map[string]string{
				knativeRevisionLabel: "revision",
				queueLabelKey:        "test_queue",
			},
			UID: "3",
		},
		Spec:   v1.PodSpec{},
		Status: v1.PodStatus{},
	}

	scheme := runtime.NewScheme()
	err := knative.AddToScheme(scheme)
	assert.Nil(t, err)
	err = v1.AddToScheme(scheme)
	assert.Nil(t, err)
	err = schedulingv1.AddToScheme(scheme)
	assert.Nil(t, err)

	inferencePriorityClass := priorityClassObj(constants.InferencePriorityClass, 1000)

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(rev, inferencePriorityClass).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	grouper := NewKnativeGrouper(client, defaultGrouper, true)

	metadata, err := grouper.GetPodGroupMetadata(service, pod)
	assert.Nil(t, err)
	assert.Equal(t, "pg-revision-2", metadata.Name)
	assert.Equal(t, constants.InferencePriorityClass, metadata.PriorityClassName)
	assert.Equal(t, "test_queue", metadata.Queue)
	assert.Equal(t, "3", strconv.Itoa(int(metadata.MinAvailable)))
}

func TestGetPodGroupMetadataBackwardsCompatibility(t *testing.T) {
	const (
		serviceName = "test_service"
		namespace   = "test_namespace"
	)
	type testData struct {
		name                  string
		gangSchedule          *bool // defaults to true
		service               *knative.Service
		revisions             []*knative.Revision
		pods                  []*v1.Pod
		podGroups             []*v2alpha2.PodGroup
		priorityClass         *schedulingv1.PriorityClass
		expectedError         bool
		expectedPodGroupName  string
		expectedPriorityClass *string // defaults to inference
		expectedMinAvailable  int32
	}

	tests := []testData{
		{
			name: "one pod service - default gang schedule",
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
					},
				},
			},
			expectedError:        false,
			expectedPodGroupName: "pg-revision-revUID",
			expectedMinAvailable: 1,
		},
		{
			name:         "one pod service - disabled gang schedule",
			gangSchedule: ptr.To(false),
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						UID: "podUID",
					},
				},
			},
			expectedError:        false,
			expectedPodGroupName: "pg-pod-podUID",
			expectedMinAvailable: 1,
		},
		{
			name:         "legacy service - gang schedule enabled",
			gangSchedule: ptr.To(true),
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						UID: "podUID",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-1",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-old-pod-oldPodUID-1",
						},
						UID: "oldPodUID-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-2",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-old-pod-oldPodUID-2",
						},
						UID: "oldPodUID-2",
					},
				},
			},
			expectedError:        false,
			expectedPodGroupName: "pg-pod-podUID",
			expectedMinAvailable: 1,
		},
		{
			name:         "gang service - gang schedule enabled",
			gangSchedule: ptr.To(true),
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						UID: "podUID",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-1",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-revision-revUID",
						},
						UID: "oldPodUID-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-2",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-revision-revUID",
						},
						UID: "oldPodUID-2",
					},
				},
			},
			expectedError:        false,
			expectedPodGroupName: "pg-revision-revUID",
			expectedMinAvailable: 1,
		},
		{
			name:         "gang service - gang schedule enabled - minavailable defined",
			gangSchedule: ptr.To(true),
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
						Annotations: map[string]string{
							"autoscaling.knative.dev/min-scale": "3",
						},
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						UID: "podUID",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-1",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-revision-revUID",
						},
						UID: "oldPodUID-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-2",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-revision-revUID",
						},
						UID: "oldPodUID-2",
					},
				},
			},
			expectedError:        false,
			expectedPodGroupName: "pg-revision-revUID",
			expectedMinAvailable: 3,
		},
		{
			name:         "one existing pod, no podgroup - no error expected",
			gangSchedule: ptr.To(true),
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
						Annotations: map[string]string{
							"autoscaling.knative.dev/min-scale": "3",
						},
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						UID: "podUID",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-1",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-revision-revUID",
						},
						UID: "oldPodUID-1",
					},
				},
			},
			expectedPodGroupName: "pg-revision-revUID",
			expectedMinAvailable: 3,
			expectedError:        false,
		},
		{
			name:         "one existing pod, existing non-gang podgroup",
			gangSchedule: ptr.To(true),
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
						Annotations: map[string]string{
							"autoscaling.knative.dev/min-scale": "3",
						},
					},
				},
			},
			pods: []*v1.Pod{

				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						UID: "podUID",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-pod-1",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg-old-pod-1-oldPodUID-1",
						},
						UID: "oldPodUID-1",
					},
				},
			},
			podGroups: []*v2alpha2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg-old-pod-1-oldPodUID-1",
						Namespace: namespace,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "v1",
								Kind:       "Pod",
								Name:       "old-pod-1",
								UID:        "oldPodUID-1",
							},
						},
					},
				},
			},
			expectedMinAvailable: 1,
			expectedPodGroupName: "pg-pod-podUID",
		},
		{
			name: "priority class name set in pod",
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel:       "revision",
							constants.PriorityLabelKey: "very-high",
						},
					},
				},
			},
			priorityClass:         priorityClassObj("very-high", 125),
			expectedError:         false,
			expectedPodGroupName:  "pg-revision-revUID",
			expectedMinAvailable:  1,
			expectedPriorityClass: ptr.To("very-high"),
		},
		{
			name: "priority class name set in top owner",
			service: &knative.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: namespace,
				},
			},
			revisions: []*knative.Revision{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "revision",
						Namespace: namespace,
						UID:       "revUID",
						Labels: map[string]string{
							constants.PriorityLabelKey: "high",
						},
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod",
						Namespace: namespace,
						Labels: map[string]string{
							knativeRevisionLabel: "revision",
						},
					},
				},
			},
			priorityClass:         priorityClassObj("high", 100),
			expectedError:         false,
			expectedPodGroupName:  "pg-revision-revUID",
			expectedMinAvailable:  1,
			expectedPriorityClass: ptr.To("high"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			assert.Nil(t, knative.AddToScheme(scheme))
			assert.Nil(t, v1.AddToScheme(scheme))
			assert.Nil(t, v2alpha2.AddToScheme(scheme))
			assert.Nil(t, schedulingv1.AddToScheme(scheme))

			var runtimeObjects []runtime.Object
			runtimeObjects = append(runtimeObjects, test.service)
			for _, revision := range test.revisions {
				runtimeObjects = append(runtimeObjects, revision)
			}
			for _, pod := range test.pods {
				runtimeObjects = append(runtimeObjects, pod)
			}
			for _, podGroup := range test.podGroups {
				runtimeObjects = append(runtimeObjects, podGroup)
			}
			inferencePriorityClass := priorityClassObj(constants.InferencePriorityClass, 1000)
			runtimeObjects = append(runtimeObjects, inferencePriorityClass)
			if test.priorityClass != nil {
				runtimeObjects = append(runtimeObjects, test.priorityClass)
			}

			client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(runtimeObjects...).Build()

			gangSchedule := true
			if test.gangSchedule != nil {
				gangSchedule = *test.gangSchedule
			}
			defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
			grouper := NewKnativeGrouper(client, defaultGrouper, gangSchedule)

			unstructuredService, err := runtime.DefaultUnstructuredConverter.ToUnstructured(test.service)
			assert.Nil(t, err, "failed to convert knative service to unstructured")

			metadata, err := grouper.GetPodGroupMetadata(
				&unstructured.Unstructured{Object: unstructuredService},
				test.pods[0],
			)

			if test.expectedError {
				assert.NotNil(t, err, "expected error")
			} else {
				assert.Nil(t, err, "unexpected error")
				assert.Equal(t, test.expectedPodGroupName, metadata.Name, "unexpected pod group name")
				expectedPriorityClass := constants.InferencePriorityClass
				if test.expectedPriorityClass != nil {
					expectedPriorityClass = *test.expectedPriorityClass
				}
				assert.Equal(t, expectedPriorityClass, metadata.PriorityClassName, "unexpected priority class")
				assert.Equal(t, test.expectedMinAvailable, metadata.MinAvailable, "unexpected min available")
			}
		})
	}
}

func priorityClassObj(name string, value int32) *schedulingv1.PriorityClass {
	return &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Value: value,
	}
}
