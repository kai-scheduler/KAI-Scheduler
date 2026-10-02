// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"

	"golang.org/x/mod/semver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1 "github.com/kai-scheduler/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"
)

const (
	gpuOperatorVersionDefaultCDIDeprecated = "v25.10.0"
	gpuOperatorVersionLabelName            = "app.kubernetes.io/version"
)

// IsGPUOperatorCDIEnabled returns whether the GPU Operator ClusterPolicy enables CDI.
func IsGPUOperatorCDIEnabled(ctx context.Context, readerClient client.Reader) (bool, error) {
	nvidiaClusterPolicy, found, err := gpuOperatorClusterPolicy(ctx, readerClient)
	if err != nil || !found {
		return false, err
	}

	if nvidiaClusterPolicy.Spec.CDI.Enabled != nil && *nvidiaClusterPolicy.Spec.CDI.Enabled {
		gpuOperatorVersion, found := nvidiaClusterPolicy.Labels[gpuOperatorVersionLabelName]
		if found && semver.Compare(gpuOperatorVersion, gpuOperatorVersionDefaultCDIDeprecated) >= 0 {
			return true, nil
		}
		if nvidiaClusterPolicy.Spec.CDI.Default != nil && *nvidiaClusterPolicy.Spec.CDI.Default {
			return true, nil
		}
	}

	return false, nil
}

// IsGPUOperatorNRIPluginEnabled returns whether the GPU Operator ClusterPolicy enables the CDI NRI plugin.
func IsGPUOperatorNRIPluginEnabled(ctx context.Context, readerClient client.Reader) (bool, error) {
	nvidiaClusterPolicy, found, err := gpuOperatorClusterPolicy(ctx, readerClient)
	if err != nil || !found {
		return false, err
	}

	return nvidiaClusterPolicy.Spec.CDI.NRIPluginEnabled != nil && *nvidiaClusterPolicy.Spec.CDI.NRIPluginEnabled, nil
}

func gpuOperatorClusterPolicy(
	ctx context.Context, readerClient client.Reader,
) (*nvidiav1.ClusterPolicy, bool, error) {
	nvidiaClusterPolicies := &nvidiav1.ClusterPolicyList{}
	err := readerClient.List(ctx, nvidiaClusterPolicies)
	if err != nil {
		if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		logger := log.FromContext(ctx)
		logger.Error(err, "cannot list nvidia cluster policy")
		return nil, false, err
	}

	if len(nvidiaClusterPolicies.Items) == 0 {
		return nil, false, nil
	}
	if len(nvidiaClusterPolicies.Items) > 1 {
		logger := log.FromContext(ctx)
		logger.Info(fmt.Sprintf("Cluster has %d clusterpolicies.nvidia.com/v1 objects."+
			" First one is queried for the GPU Operator configuration", len(nvidiaClusterPolicies.Items)))
	}

	return &nvidiaClusterPolicies.Items[0], true, nil
}
