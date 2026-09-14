// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpu_sharing

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

const GpuSharingConfigCRDName = "gpusharingconfigs.gpu-sharing.kai.scheduler"

var GpuSharingConfigGVK = schema.GroupVersionKind{
	Group:   "gpu-sharing.kai.scheduler",
	Version: "v1alpha1",
	Kind:    "GpuSharingConfig",
}

type GpuSharing struct{}

func (g *GpuSharing) DesiredState(context.Context, client.Reader, *kaiv1.Config) ([]client.Object, error) {
	return nil, nil
}

func (g *GpuSharing) IsDeployed(context.Context, client.Reader) (bool, error) {
	return true, nil
}

func (g *GpuSharing) IsAvailable(context.Context, client.Reader) (bool, error) {
	return true, nil
}

func (g *GpuSharing) Monitor(context.Context, client.Reader, *kaiv1.Config) error {
	return nil
}

func (g *GpuSharing) HasMissingDependencies(
	ctx context.Context, readerClient client.Reader, kaiConfig *kaiv1.Config,
) (string, error) {
	if !isNvFractionsConfigured(kaiConfig) {
		return "", nil
	}

	gpuSharingConfig := &unstructured.Unstructured{}
	gpuSharingConfig.SetGroupVersionKind(GpuSharingConfigGVK)
	if err := readerClient.Get(ctx, types.NamespacedName{Name: "default"}, gpuSharingConfig); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return "Gpu Sharing is not ready. GpuSharingConfig not found", nil
		}
		return "", err
	}

	readyConditions, found, err := unstructured.NestedSlice(gpuSharingConfig.Object, "status", "conditions")
	if err != nil {
		return "", err
	}
	for _, condition := range readyConditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}
		if conditionMap["type"] != "Ready" {
			continue
		}
		if conditionMap["status"] == "True" {
			return "", nil
		}
		return fmt.Sprintf(
			"Gpu Sharing is not ready. Ready=%s, reason=%s, message=%s",
			conditionFieldString(conditionMap, "status"),
			conditionFieldString(conditionMap, "reason"),
			conditionFieldString(conditionMap, "message"),
		), nil
	}

	if !found {
		return "Gpu Sharing is not ready. GpuSharingConfig conditions not found", nil
	}
	return "Gpu Sharing is not ready. Ready condition not found", nil
}

func (g *GpuSharing) Name() string {
	return "GPU-sharing"
}

func isNvFractionsConfigured(kaiConfig *kaiv1.Config) bool {
	return kaiConfig != nil && kaiConfig.Spec.Global != nil && kaiConfig.Spec.Global.GpuSharingMode != nil &&
		*kaiConfig.Spec.Global.GpuSharingMode == kaiv1common.GpuSharingModeNvFractions
}

func conditionFieldString(condition map[string]interface{}, field string) string {
	value, ok := condition[field].(string)
	if !ok {
		return ""
	}
	return value
}
