// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingconfigmap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const timeout = 5 * time.Second

type upsertConfigMapTest struct {
	name string

	existingPod          *v1.Pod
	existingConfigMap    *v1.ConfigMap
	desiredConfigMapName string
	desiredConfigMapData map[string]string
	expectedError        bool
	expectedPod          *v1.Pod
	expectedConfigMap    *v1.ConfigMap
}

func TestUpsertConfigMap(t *testing.T) {
	tests := []upsertConfigMapTest{
		{
			name: "pod with no config map",
			existingPod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					UID:       "test-pod-uid",
				},
			},
			existingConfigMap:    nil,
			desiredConfigMapName: "config-map-name",
			desiredConfigMapData: map[string]string{
				"bla": "blu",
			},
			expectedError: false,
			expectedPod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					UID:       "test-pod-uid",
				},
			},
			expectedConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-map-name",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "test-pod",
							UID:        "test-pod-uid",
						},
					},
				},
				Data: map[string]string{
					"bla": "blu",
				},
			},
		},
		{
			name: "pod with config map, wrong owner",
			existingPod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					UID:       "test-pod-uid",
				},
			},
			existingConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-map-name",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v100",
							Kind:       "ReplicaSet",
							Name:       "inference",
							UID:        "bla-uid",
						},
					},
				},
				Data: map[string]string{
					"bla": "blu",
				},
			},
			desiredConfigMapName: "config-map-name",
			desiredConfigMapData: map[string]string{
				"bla": "blu",
			},
			expectedError: false,
			expectedPod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					UID:       "test-pod-uid",
				},
			},
			expectedConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-map-name",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "test-pod",
							UID:        "test-pod-uid",
						},
					},
				},
				Data: map[string]string{
					"bla": "blu",
				},
			},
		},
	}

	for _, test := range tests {
		kubeClient := initTest(test)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := UpsertJobConfigMap(ctx, kubeClient, test.existingPod, test.desiredConfigMapName, test.desiredConfigMapData)

		if test.expectedError {
			assert.Error(t, err)
		} else {
			assert.Nil(t, err)
		}

		resultPod := &v1.Pod{}
		err = kubeClient.Get(ctx, client.ObjectKeyFromObject(test.existingPod), resultPod)
		assert.Nil(t, err)
		assertPodsEqual(t, test.expectedPod, resultPod)

		resultConfigMap := &v1.ConfigMap{}
		err = kubeClient.Get(ctx,
			types.NamespacedName{Namespace: test.existingPod.Namespace, Name: test.desiredConfigMapName},
			resultConfigMap,
		)
		assert.Nil(t, err)
		test.expectedConfigMap.Labels = map[string]string{GPUSharingConfigMapLabel: "true"}
		assertConfigMapsEqual(t, test.expectedConfigMap, resultConfigMap)
	}
}

func initTest(test upsertConfigMapTest) client.Client {
	testScheme := runtime.NewScheme()
	v1.AddToScheme(testScheme)

	kubeClientBuilder := fake.NewClientBuilder().WithScheme(testScheme)

	if test.existingPod != nil {
		kubeClientBuilder.WithObjects(test.existingPod)
	}

	if test.existingConfigMap != nil {
		kubeClientBuilder.WithObjects(test.existingConfigMap)
	}

	return kubeClientBuilder.Build()
}

func assertPodsEqual(t *testing.T, p1, p2 *v1.Pod) {
	assert.Equal(t, p1.Annotations, p2.Annotations)
	assert.Equal(t, p1.Labels, p2.Labels)
	assert.Equal(t, p1.Spec, p2.Spec)
}

func assertConfigMapsEqual(t *testing.T, p1, p2 *v1.ConfigMap) {
	assert.Equal(t, p1.Annotations, p2.Annotations)
	assert.Equal(t, p1.Labels, p2.Labels)
	assert.Equal(t, p1.Data, p2.Data)
	assert.Equal(t, p1.OwnerReferences, p2.OwnerReferences)
}
