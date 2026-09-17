// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

func nvFractionsRequestKey(container string) string {
	return resources.CalcGpuFractionAnnotationForContainer(container)
}

func nvFractionsLimitKey(container string) string {
	return resources.CalcGpuFractionLimitAnnotationForContainer(container)
}

func TestMutateConvertsLegacyGpuMemoryWithoutConfigmap(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{constants.GpuMemory: "2000"}},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}

	err := New("").Mutate(pod)
	assert.NoError(t, err)

	assert.NotEmpty(t, pod.Annotations[nvFractionsRequestKey("container-0")])
	// Decoupled from configmap: no volumes or configmap-backed env vars are added.
	assert.Empty(t, pod.Spec.Volumes)
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			assert.Nil(t, env.ValueFrom)
		}
	}
}

func TestMutateConvertsLegacyGpuMemoryForNamedContainer(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			constants.GpuMemory:                "2000",
			constants.GpuFractionContainerName: "container-1",
		}},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}, {Name: "container-1"}}},
	}

	err := New("").Mutate(pod)
	assert.NoError(t, err)

	assert.NotEmpty(t, pod.Annotations[nvFractionsRequestKey("container-1")])
	assert.Empty(t, pod.Annotations[nvFractionsRequestKey("container-0")])
}

func TestMutateNoOpWithoutFractionRequest(t *testing.T) {
	pod := &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}}}
	err := New("").Mutate(pod)
	assert.NoError(t, err)
	assert.Empty(t, pod.Annotations)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name            string
		annotations     map[string]string
		wantErrContains string
	}{
		{
			name:        "valid nvfractions request",
			annotations: map[string]string{nvFractionsRequestKey("container-0"): "1Gi"},
		},
		{
			name:        "valid legacy gpu-fraction (portion) request",
			annotations: map[string]string{constants.GpuFraction: "0.5"},
		},
		{
			name:        "valid legacy gpu-memory request",
			annotations: map[string]string{constants.GpuMemory: "2000"},
		},
		{
			name: "valid legacy gpu-memory with matching container-name",
			annotations: map[string]string{
				constants.GpuMemory:                "2000",
				constants.GpuFractionContainerName: "container-0",
			},
		},
		{
			name: "rejects both gpu-fraction and gpu-memory",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				constants.GpuMemory:   "2000",
			},
			wantErrContains: "cannot request both gpu-fraction and GPU memory request",
		},
		{
			name: "rejects container-name mismatch with nvfractions annotation",
			annotations: map[string]string{
				nvFractionsRequestKey("container-0"): "1Gi",
				constants.GpuFractionContainerName:   "other",
			},
			wantErrContains: "does not match container name",
		},
		{
			name:            "references missing container",
			annotations:     map[string]string{nvFractionsRequestKey("missing"): "1Gi"},
			wantErrContains: "not found in pod spec",
		},
		{
			name:        "no fraction request",
			annotations: map[string]string{},
		},
		{
			name:            "rejects non-quantity nvfractions request value",
			annotations:     map[string]string{nvFractionsRequestKey("container-0"): "not-a-quantity"},
			wantErrContains: "must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name:            "rejects non-quantity nvfractions request Nan",
			annotations:     map[string]string{nvFractionsRequestKey("container-0"): "NaN"},
			wantErrContains: "must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name:            "rejects empty nvfractions request value",
			annotations:     map[string]string{nvFractionsRequestKey("container-0"): ""},
			wantErrContains: "must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name:            "rejects zero nvfractions request value",
			annotations:     map[string]string{nvFractionsRequestKey("container-0"): "0"},
			wantErrContains: "must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name:            "rejects negative nvfractions request value",
			annotations:     map[string]string{nvFractionsRequestKey("container-0"): "-1Gi"},
			wantErrContains: "must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name:            "rejects non-quantity nvfractions limit value",
			annotations:     map[string]string{nvFractionsLimitKey("container-0"): "not-a-quantity"},
			wantErrContains: "must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name:            "rejects non-quantity nvfractions request Nan",
			annotations:     map[string]string{nvFractionsLimitKey("container-0"): "NaN"},
			wantErrContains: "must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name: "rejects nvfractions request greater than limit",
			annotations: map[string]string{
				nvFractionsRequestKey("container-0"): "2Gi",
				nvFractionsLimitKey("container-0"):   "1Gi",
			},
			wantErrContains: "request is greater than limit",
		},
		{
			name:            "rejects unknown nvfractions annotation suffix",
			annotations:     map[string]string{constants.NvFractionsAnnotationPrefix + "container-0.gpu-memory": "1Gi"},
			wantErrContains: "invalid NvFractions annotation key",
		},
		{
			name:            "rejects nvfractions annotation with empty container name",
			annotations:     map[string]string{nvFractionsRequestKey(""): "1Gi"},
			wantErrContains: "invalid NvFractions annotation key",
		},
		{
			name: "rejects nvfractions annotations on multiple containers",
			annotations: map[string]string{
				nvFractionsRequestKey("container-0"): "1Gi",
				nvFractionsRequestKey("container-1"): "1Gi",
			},
			wantErrContains: "doesn't support multiple containers",
		},
		{
			name: "rejects nvfractions request mismatching gpu-memory annotation",
			annotations: map[string]string{
				nvFractionsRequestKey("container-0"): "1Gi",
				constants.GpuMemory:                  "2000",
			},
			wantErrContains: "does not match",
		},
		{
			name: "rejects nvfractions limit combined with gpu-fraction",
			annotations: map[string]string{
				nvFractionsLimitKey("container-0"): "1Gi",
				constants.GpuFraction:              "0.5",
			},
			wantErrContains: "cannot combine",
		},
		{
			name:            "rejects non-numeric gpu-fraction value",
			annotations:     map[string]string{constants.GpuFraction: "half"},
			wantErrContains: "gpu-fraction",
		},
		{
			name:            "rejects non-numeric gpu-memory value",
			annotations:     map[string]string{constants.GpuMemory: "2Gi"},
			wantErrContains: "gpu-memory",
		},
		{
			name:            "rejects device count without fraction details",
			annotations:     map[string]string{constants.GpuFractionsNumDevices: "2"},
			wantErrContains: "cannot request multiple fractional devices",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
				Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}, {Name: "container-1"}}},
			}

			err := New("").Validate(context.Background(), nil, pod)
			if tt.wantErrContains != "" {
				assert.ErrorContains(t, err, tt.wantErrContains)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateDeviceAnnotationAuthorization(t *testing.T) {
	const binderUsername = "system:serviceaccount:kai-scheduler:binder"
	deviceAnnotationKey := resources.CalcGpuVisibleDevicesAnnotationForContainer("container-0")
	podWithDeviceAnnotation := func(value string) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				nvFractionsRequestKey("container-0"): "1Gi",
				deviceAnnotationKey:                  value,
			}},
			Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
		}
	}

	t.Run("rejects create by non-binder", func(t *testing.T) {
		err := New(binderUsername).Validate(contextWithUser("alice"), nil, podWithDeviceAnnotation("GPU-0"))
		assert.ErrorContains(t, err, ".gpus.devices annotations may only be modified")
	})

	t.Run("allows create by binder", func(t *testing.T) {
		err := New(binderUsername).Validate(contextWithUser(binderUsername), nil, podWithDeviceAnnotation("GPU-0"))
		assert.NoError(t, err)
	})

	t.Run("allows init container annotation by binder", func(t *testing.T) {
		initContainerName := "init-container"
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				nvFractionsRequestKey(initContainerName):                                 "1Gi",
				resources.CalcGpuVisibleDevicesAnnotationForContainer(initContainerName): "GPU-0",
			}},
			Spec: v1.PodSpec{
				InitContainers: []v1.Container{{Name: initContainerName}},
				Containers:     []v1.Container{{Name: "container-0"}},
			},
		}

		err := New(binderUsername).Validate(contextWithUser(binderUsername), nil, pod)
		assert.NoError(t, err)
	})

	t.Run("allows unchanged device annotation by non-binder", func(t *testing.T) {
		oldPod := podWithDeviceAnnotation("GPU-0")
		newPod := podWithDeviceAnnotation("GPU-0")
		newPod.Labels = map[string]string{"foo": "bar"}

		err := New(binderUsername).Validate(contextWithUser("alice"), oldPod, newPod)
		assert.NoError(t, err)
	})

	t.Run("rejects changed device annotation by non-binder", func(t *testing.T) {
		oldPod := podWithDeviceAnnotation("GPU-0")
		newPod := podWithDeviceAnnotation("GPU-1")

		err := New(binderUsername).Validate(contextWithUser("alice"), oldPod, newPod)
		assert.ErrorContains(t, err, ".gpus.devices annotations may only be modified")
	})
}

func TestValidateDeviceAnnotationValues(t *testing.T) {
	const binderUsername = "binder"
	tests := []struct {
		name            string
		annotations     map[string]string
		wantErrContains string
	}{
		{
			name: "device annotation alongside a request on the same container",
			annotations: map[string]string{
				nvFractionsRequestKey("container-0"):                                 "1Gi",
				resources.CalcGpuVisibleDevicesAnnotationForContainer("container-0"): "GPU-0",
			},
		},
		{
			name: "rejects device annotation on a container other than the requesting one",
			annotations: map[string]string{
				nvFractionsRequestKey("container-0"):                                 "1Gi",
				resources.CalcGpuVisibleDevicesAnnotationForContainer("container-1"): "GPU-0",
			},
			wantErrContains: "doesn't support multiple containers",
		},
		{
			name:            "rejects device annotation referencing a missing container",
			annotations:     map[string]string{resources.CalcGpuVisibleDevicesAnnotationForContainer("missing"): "GPU-0"},
			wantErrContains: "not found in pod spec",
		},
		{
			name:            "rejects device annotation with empty container name",
			annotations:     map[string]string{resources.CalcGpuVisibleDevicesAnnotationForContainer(""): "GPU-0"},
			wantErrContains: "invalid NvFractions annotation key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
				Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}, {Name: "container-1"}}},
			}

			err := New(binderUsername).Validate(contextWithUser(binderUsername), nil, pod)
			if tt.wantErrContains != "" {
				assert.ErrorContains(t, err, tt.wantErrContains)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func contextWithUser(username string) context.Context {
	return admission.NewContextWithRequest(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UserInfo: authenticationv1.UserInfo{Username: username},
		},
	})
}
