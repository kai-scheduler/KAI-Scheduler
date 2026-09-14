// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpu_sharing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

func TestHasMissingDependencies(t *testing.T) {
	nvFractionsConfig := func() *kaiv1.Config {
		return &kaiv1.Config{
			Spec: kaiv1.ConfigSpec{
				Global: &kaiv1.GlobalConfig{
					GpuSharingMode: ptr.To(kaiv1common.GpuSharingModeNvFractions),
				},
			},
		}
	}

	tests := []struct {
		name             string
		kaiConfig        *kaiv1.Config
		gpuSharingConfig *unstructured.Unstructured
		expectedMissing  string
	}{
		{
			name: "returns empty when NvFractions is not configured",
			kaiConfig: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Global: &kaiv1.GlobalConfig{
						GpuSharingMode: ptr.To(kaiv1common.GpuSharingModeNonMemoryEnforced),
					},
				},
			},
			gpuSharingConfig: gpuSharingConfigWithReadyCondition("False", "RolloutInProgress", "0 of 2 pods ready"),
		},
		{
			name:             "returns empty when GpuSharingConfig is ready",
			kaiConfig:        nvFractionsConfig(),
			gpuSharingConfig: gpuSharingConfigWithReadyCondition("True", "AllComponentsReady", "all components are healthy"),
		},
		{
			name:             "details false ready condition",
			kaiConfig:        nvFractionsConfig(),
			gpuSharingConfig: gpuSharingConfigWithReadyCondition("False", "GPUOperatorNotReady", "ClusterPolicy is not ready"),
			expectedMissing:  "Gpu Sharing is not ready. Ready=False, reason=GPUOperatorNotReady, message=ClusterPolicy is not ready",
		},
		{
			name:             "details unknown ready condition",
			kaiConfig:        nvFractionsConfig(),
			gpuSharingConfig: gpuSharingConfigWithReadyCondition("Unknown", "ComponentUnknown", "unknown: MpsdReady"),
			expectedMissing:  "Gpu Sharing is not ready. Ready=Unknown, reason=ComponentUnknown, message=unknown: MpsdReady",
		},
		{
			name:            "reports missing GpuSharingConfig",
			kaiConfig:       nvFractionsConfig(),
			expectedMissing: "Gpu Sharing is not ready. GpuSharingConfig not found",
		},
		{
			name:             "reports missing status conditions",
			kaiConfig:        nvFractionsConfig(),
			gpuSharingConfig: gpuSharingConfigWithoutStatus(),
			expectedMissing:  "Gpu Sharing is not ready. GpuSharingConfig conditions not found",
		},
		{
			name:             "reports missing Ready condition",
			kaiConfig:        nvFractionsConfig(),
			gpuSharingConfig: gpuSharingConfigWithConditions([]interface{}{}),
			expectedMissing:  "Gpu Sharing is not ready. Ready condition not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder()
			if tt.gpuSharingConfig != nil {
				builder.WithObjects(tt.gpuSharingConfig)
			}

			missing, err := (&GpuSharing{}).HasMissingDependencies(
				context.Background(), builder.Build(), tt.kaiConfig,
			)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedMissing, missing)
		})
	}
}

func gpuSharingConfigWithReadyCondition(status, reason, message string) *unstructured.Unstructured {
	return gpuSharingConfigWithConditions([]interface{}{
		map[string]interface{}{
			"type":    "Ready",
			"status":  status,
			"reason":  reason,
			"message": message,
		},
	})
}

func gpuSharingConfigWithConditions(conditions []interface{}) *unstructured.Unstructured {
	obj := gpuSharingConfigWithoutStatus()
	obj.Object["status"] = map[string]interface{}{
		"conditions": conditions,
	}
	return obj
}

func gpuSharingConfigWithoutStatus() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gpu-sharing.kai.scheduler/v1alpha1",
			"kind":       "GpuSharingConfig",
			"metadata": map[string]interface{}{
				"name": "default",
			},
		},
	}
	obj.SetGroupVersionKind(GpuSharingConfigGVK)
	return obj
}
