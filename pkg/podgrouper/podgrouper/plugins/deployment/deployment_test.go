// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deployment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"

	deploymentName = "test_deployment"
	namespace      = "test_namespace"
	appLabelKey    = "app"
	appLabelValue  = "test"
)

func testDeployment(annotations map[string]string) *unstructured.Unstructured {
	unstructuredAnnotations := map[string]interface{}{
		"test_annotation": "test_value",
	}
	for key, value := range annotations {
		unstructuredAnnotations[key] = value
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Deployment",
			"apiVersion": "apps/v1",
			"metadata": map[string]interface{}{
				"name":      deploymentName,
				"namespace": namespace,
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": unstructuredAnnotations,
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						appLabelKey: appLabelValue,
					},
				},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							queueLabelKey: "test_queue",
							appLabelKey:   appLabelValue,
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
}

func testPod(name, uid, podGroupName string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				queueLabelKey: "test_queue",
				appLabelKey:   appLabelValue,
			},
			UID: apitypes.UID(uid),
		},
	}
	if podGroupName != "" {
		pod.Annotations = map[string]string{commonconstants.PodGroupAnnotationForPod: podGroupName}
	}
	return pod
}

func testPodGroup(name, ownerKind string) *v2alpha2.PodGroup {
	return &v2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{{Kind: ownerKind, Name: "owner"}},
		},
	}
}

func newTestClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = schedulingv1.AddToScheme(scheme)
	_ = v2alpha2.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestGetPodGroupMetadataPerPod(t *testing.T) {
	deployment := testDeployment(nil)
	pod1 := testPod("pod-1", "3", "")
	pod2 := testPod("pod-2", "4", "")

	grouper := NewDeploymentGrouper(newTestClient(),
		defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient()), false)

	metadata, err := grouper.GetPodGroupMetadata(deployment, pod1)
	assert.Nil(t, err)
	assert.Equal(t, "pg-pod-1-3", metadata.Name)
	assert.Equal(t, constants.InferencePriorityClass, metadata.PriorityClassName)
	assert.Equal(t, "test_queue", metadata.Queue)

	metadata2, err := grouper.GetPodGroupMetadata(deployment, pod2)
	assert.Nil(t, err)
	// assert that the second pod got a different podgroup
	assert.Equal(t, "pg-pod-2-4", metadata2.Name)
	assert.Equal(t, constants.InferencePriorityClass, metadata2.PriorityClassName)
	assert.Equal(t, "test_queue", metadata2.Queue)
}

func TestGetPodGroupMetadataGangSchedule(t *testing.T) {
	type testData struct {
		name                 string
		gangSchedule         bool
		annotations          map[string]string
		existingPods         []*v1.Pod
		existingPodGroups    []*v2alpha2.PodGroup
		expectedError        bool
		expectedPodGroupName string
		expectedOwnerName    string
		expectedMinAvailable int32
	}

	tests := []testData{
		{
			name:                 "gang schedule disabled",
			gangSchedule:         false,
			expectedPodGroupName: "pg-pod-1-3",
			expectedOwnerName:    "pod-1",
			expectedMinAvailable: 1,
		},
		{
			name:                 "gang schedule enabled, no existing pod groups",
			gangSchedule:         true,
			expectedPodGroupName: "pg-test_deployment-1",
			expectedOwnerName:    deploymentName,
			expectedMinAvailable: 1,
		},
		{
			name:         "gang schedule enabled, legacy pod groups per pod",
			gangSchedule: true,
			existingPods: []*v1.Pod{
				testPod("old-pod-1", "10", "pg-old-pod-1-10"),
				testPod("old-pod-2", "11", "pg-old-pod-2-11"),
			},
			existingPodGroups: []*v2alpha2.PodGroup{
				testPodGroup("pg-old-pod-1-10", "Pod"),
				testPodGroup("pg-old-pod-2-11", "Pod"),
			},
			expectedPodGroupName: "pg-pod-1-3",
			expectedOwnerName:    "pod-1",
			expectedMinAvailable: 1,
		},
		{
			name:         "gang schedule enabled, single legacy pod group owned by a pod",
			gangSchedule: true,
			existingPods: []*v1.Pod{
				testPod("old-pod-1", "10", "pg-old-pod-1-10"),
			},
			existingPodGroups: []*v2alpha2.PodGroup{
				testPodGroup("pg-old-pod-1-10", "Pod"),
			},
			expectedPodGroupName: "pg-pod-1-3",
			expectedOwnerName:    "pod-1",
			expectedMinAvailable: 1,
		},
		{
			name:         "gang schedule enabled, existing deployment pod group",
			gangSchedule: true,
			existingPods: []*v1.Pod{
				testPod("old-pod-1", "10", "pg-test_deployment-1"),
				testPod("old-pod-2", "11", "pg-test_deployment-1"),
			},
			existingPodGroups: []*v2alpha2.PodGroup{
				testPodGroup("pg-test_deployment-1", "Deployment"),
			},
			expectedPodGroupName: "pg-test_deployment-1",
			expectedOwnerName:    deploymentName,
			expectedMinAvailable: 1,
		},
		{
			name:                 "min member override",
			gangSchedule:         true,
			annotations:          map[string]string{constants.MinMemberOverrideKey: "3"},
			expectedPodGroupName: "pg-test_deployment-1",
			expectedOwnerName:    deploymentName,
			expectedMinAvailable: 3,
		},
		{
			name:         "min member override ignored for legacy pod groups",
			gangSchedule: true,
			annotations:  map[string]string{constants.MinMemberOverrideKey: "3"},
			existingPods: []*v1.Pod{
				testPod("old-pod-1", "10", "pg-old-pod-1-10"),
				testPod("old-pod-2", "11", "pg-old-pod-2-11"),
			},
			existingPodGroups: []*v2alpha2.PodGroup{
				testPodGroup("pg-old-pod-1-10", "Pod"),
				testPodGroup("pg-old-pod-2-11", "Pod"),
			},
			expectedPodGroupName: "pg-pod-1-3",
			expectedOwnerName:    "pod-1",
			expectedMinAvailable: 1,
		},
		{
			name:          "unparsable min member override",
			gangSchedule:  true,
			annotations:   map[string]string{constants.MinMemberOverrideKey: "not-a-number"},
			expectedError: true,
		},
		{
			name:          "min member override below one",
			gangSchedule:  true,
			annotations:   map[string]string{constants.MinMemberOverrideKey: "0"},
			expectedError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := testPod("pod-1", "3", "")

			var objects []client.Object
			for _, existingPod := range test.existingPods {
				objects = append(objects, existingPod)
			}
			for _, existingPodGroup := range test.existingPodGroups {
				objects = append(objects, existingPodGroup)
			}
			kubeClient := newTestClient(objects...)

			grouper := NewDeploymentGrouper(kubeClient,
				defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, kubeClient), test.gangSchedule)

			metadata, err := grouper.GetPodGroupMetadata(testDeployment(test.annotations), pod)
			if test.expectedError {
				assert.NotNil(t, err)
				return
			}

			assert.Nil(t, err)
			assert.Equal(t, test.expectedPodGroupName, metadata.Name)
			assert.Equal(t, test.expectedOwnerName, metadata.Owner.Name)
			assert.Equal(t, test.expectedMinAvailable, metadata.MinAvailable)
			assert.Equal(t, constants.InferencePriorityClass, metadata.PriorityClassName)
			assert.Equal(t, "test_queue", metadata.Queue)
		})
	}
}
