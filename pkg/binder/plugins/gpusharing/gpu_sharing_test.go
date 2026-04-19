// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
