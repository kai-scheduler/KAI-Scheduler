// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestGetFractionContainerRef(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		wantIndex   int
		wantType    gpusharingconfigmap.ContainerType
		wantName    string
		wantErr     bool
		errContains string
	}{
		{
			name: "no annotations - returns default container",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "container-0"},
						{Name: "container-1"},
					},
				},
			},
			wantIndex: 0,
			wantType:  gpusharingconfigmap.RegularContainer,
			wantName:  "container-0",
			wantErr:   false,
		},
		{
			name: "annotation points to first regular container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionContainerName: "container-0",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "container-0"},
						{Name: "container-1"},
					},
				},
			},
			wantIndex: 0,
			wantType:  gpusharingconfigmap.RegularContainer,
			wantName:  "container-0",
			wantErr:   false,
		},
		{
			name: "annotation points to second regular container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionContainerName: "container-1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "container-0"},
						{Name: "container-1"},
					},
				},
			},
			wantIndex: 1,
			wantType:  gpusharingconfigmap.RegularContainer,
			wantName:  "container-1",
			wantErr:   false,
		},
		{
			name: "annotation points to init container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionContainerName: "init-container",
					},
				},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{Name: "init-container"},
					},
					Containers: []v1.Container{
						{Name: "container-0"},
					},
				},
			},
			wantIndex: 0,
			wantType:  gpusharingconfigmap.InitContainer,
			wantName:  "init-container",
			wantErr:   false,
		},
		{
			name: "annotation points to second init container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionContainerName: "init-container-1",
					},
				},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{Name: "init-container-0"},
						{Name: "init-container-1"},
						{Name: "init-container-2"},
					},
					Containers: []v1.Container{
						{Name: "container-0"},
					},
				},
			},
			wantIndex: 1,
			wantType:  gpusharingconfigmap.InitContainer,
			wantName:  "init-container-1",
			wantErr:   false,
		},
		{
			name: "container not found in regular containers",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionContainerName: "non-existent",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "container-0"},
						{Name: "container-1"},
					},
				},
			},
			wantErr:     true,
			errContains: "container with name non-existent not found for fraction request",
		},
		{
			name: "annotation without type defaults to regular containers",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionContainerName: "container-1",
					},
				},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{Name: "init-container-0"},
					},
					Containers: []v1.Container{
						{Name: "container-0"},
						{Name: "container-1"},
					},
				},
			},
			wantIndex: 1,
			wantType:  gpusharingconfigmap.RegularContainer,
			wantName:  "container-1",
			wantErr:   false,
		},
		{
			name: "single container pod with no annotations",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "single-container"},
					},
				},
			},
			wantIndex: 0,
			wantType:  gpusharingconfigmap.RegularContainer,
			wantName:  "single-container",
			wantErr:   false,
		},
		{
			name: "multiple containers with annotation to last one",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionContainerName: "container-4",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "container-0"},
						{Name: "container-1"},
						{Name: "container-2"},
						{Name: "container-3"},
						{Name: "container-4"},
					},
				},
			},
			wantIndex: 4,
			wantType:  gpusharingconfigmap.RegularContainer,
			wantName:  "container-4",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := common.GetFractionContainerRef(tt.pod)

			if tt.wantErr {
				if err == nil {
					t.Errorf("getFractionContainerRef() expected error but got none")
					return
				}
				if tt.errContains != "" && err.Error() != tt.errContains {
					t.Errorf("getFractionContainerRef() error = %v, want error containing %v", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("getFractionContainerRef() unexpected error = %v", err)
				return
			}

			if got == nil {
				t.Errorf("getFractionContainerRef() returned nil")
				return
			}

			if got.Index != tt.wantIndex {
				t.Errorf("getFractionContainerRef() Index = %v, want %v", got.Index, tt.wantIndex)
			}

			if got.Type != tt.wantType {
				t.Errorf("getFractionContainerRef() Type = %v, want %v", got.Type, tt.wantType)
			}

			if got.Container == nil {
				t.Errorf("getFractionContainerRef() Container is nil")
				return
			}

			if got.Container.Name != tt.wantName {
				t.Errorf("getFractionContainerRef() Container.Name = %v, want %v", got.Container.Name, tt.wantName)
			}
		})
	}
}

func TestGPUSharingRollback(t *testing.T) {
	tests := []struct {
		name                     string
		pod                      *v1.Pod
		bindRequest              *v1alpha2.BindRequest
		existingConfigMaps       []*v1.ConfigMap
		expectError              bool
		expectConfigMapsDeleted  bool
		expectedRemainingCMCount int
	}{
		{
			name: "rollback skipped for non-shared GPU allocation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "container-0"}},
				},
			},
			bindRequest: &v1alpha2.BindRequest{
				Spec: v1alpha2.BindRequestSpec{
					ReceivedResourceType: "regular", // Not a fraction
				},
			},
			existingConfigMaps:       nil,
			expectError:              false,
			expectConfigMapsDeleted:  false,
			expectedRemainingCMCount: 0,
		},
		{
			name: "rollback skipped when configmap annotation not set",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					// No runai/shared-gpu-configmap annotation
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "container-0"}},
				},
			},
			bindRequest: &v1alpha2.BindRequest{
				Spec: v1alpha2.BindRequestSpec{
					ReceivedResourceType: common.ReceivedTypeFraction,
				},
			},
			existingConfigMaps:       nil,
			expectError:              false,
			expectConfigMapsDeleted:  false,
			expectedRemainingCMCount: 0,
		},
		{
			name: "rollback successfully deletes both configmaps",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					UID:       "test-pod-uid",
					Annotations: map[string]string{
						"runai/shared-gpu-configmap": "test-pod-abc1234-shared-gpu",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "container-0"}},
				},
			},
			bindRequest: &v1alpha2.BindRequest{
				Spec: v1alpha2.BindRequestSpec{
					ReceivedResourceType: common.ReceivedTypeFraction,
				},
			},
			existingConfigMaps: []*v1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-abc1234-shared-gpu-0",
						Namespace: "test-ns",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-abc1234-shared-gpu-0-evar",
						Namespace: "test-ns",
					},
				},
			},
			expectError:              false,
			expectConfigMapsDeleted:  true,
			expectedRemainingCMCount: 0,
		},
		{
			name: "rollback succeeds when configmaps already deleted (idempotent)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					UID:       "test-pod-uid",
					Annotations: map[string]string{
						"runai/shared-gpu-configmap": "test-pod-abc1234-shared-gpu",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "container-0"}},
				},
			},
			bindRequest: &v1alpha2.BindRequest{
				Spec: v1alpha2.BindRequestSpec{
					ReceivedResourceType: common.ReceivedTypeFraction,
				},
			},
			existingConfigMaps:       nil, // ConfigMaps don't exist
			expectError:              false,
			expectConfigMapsDeleted:  true,
			expectedRemainingCMCount: 0,
		},
		{
			name: "rollback for init container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					UID:       "test-pod-uid",
					Annotations: map[string]string{
						"runai/shared-gpu-configmap":       "test-pod-abc1234-shared-gpu",
						constants.GpuFractionContainerName: "init-container",
					},
				},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{{Name: "init-container"}},
					Containers:     []v1.Container{{Name: "container-0"}},
				},
			},
			bindRequest: &v1alpha2.BindRequest{
				Spec: v1alpha2.BindRequestSpec{
					ReceivedResourceType: common.ReceivedTypeFraction,
				},
			},
			existingConfigMaps: []*v1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-abc1234-shared-gpu-i0",
						Namespace: "test-ns",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-abc1234-shared-gpu-i0-evar",
						Namespace: "test-ns",
					},
				},
			},
			expectError:              false,
			expectConfigMapsDeleted:  true,
			expectedRemainingCMCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)

			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.pod != nil {
				clientBuilder.WithObjects(tt.pod)
			}
			for _, cm := range tt.existingConfigMaps {
				clientBuilder.WithObjects(cm)
			}
			kubeClient := clientBuilder.Build()

			// Create GPUSharing plugin
			plugin := New(kubeClient, false)

			// Execute rollback
			err := plugin.Rollback(context.Background(), tt.pod, nil, tt.bindRequest, nil)

			// Verify error expectation
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify configmaps were deleted
			if tt.expectConfigMapsDeleted {
				cmList := &v1.ConfigMapList{}
				err := kubeClient.List(context.Background(), cmList, client.InNamespace(tt.pod.Namespace))
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedRemainingCMCount, len(cmList.Items),
					"Expected %d configmaps remaining, got %d", tt.expectedRemainingCMCount, len(cmList.Items))
			}
		})
	}
}

func TestGPUSharingRollbackDeleteConfigMap(t *testing.T) {
	tests := []struct {
		name        string
		namespace   string
		cmName      string
		existingCM  *v1.ConfigMap
		expectError bool
	}{
		{
			name:      "successfully deletes existing configmap",
			namespace: "test-ns",
			cmName:    "test-cm",
			existingCM: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
			},
			expectError: false,
		},
		{
			name:        "succeeds when configmap does not exist (IgnoreNotFound)",
			namespace:   "test-ns",
			cmName:      "non-existent-cm",
			existingCM:  nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)

			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.existingCM != nil {
				clientBuilder.WithObjects(tt.existingCM)
			}
			kubeClient := clientBuilder.Build()

			plugin := New(kubeClient, false)
			err := plugin.deleteConfigMap(context.Background(), tt.namespace, tt.cmName)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify configmap is gone
			cm := &v1.ConfigMap{}
			err = kubeClient.Get(context.Background(), types.NamespacedName{
				Namespace: tt.namespace,
				Name:      tt.cmName,
			}, cm)
			assert.True(t, client.IgnoreNotFound(err) == nil, "ConfigMap should not exist after deletion")
		})
	}
}
