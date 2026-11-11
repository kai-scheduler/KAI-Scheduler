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
// From GPU Operator 25.10 CDI is on unless spec.cdi.enabled is false, and
// spec.cdi.default no longer matters; before that, CDI is used only when both
// spec.cdi.enabled and spec.cdi.default are true.
func IsGPUOperatorCDIEnabled(ctx context.Context, readerClient client.Reader) (bool, error) {
	nvidiaClusterPolicy, found, err := gpuOperatorClusterPolicy(ctx, readerClient)
	if err != nil || !found {
		return false, err
	}

	cdiOnByDefault := gpuOperatorDefaultsToCDI(nvidiaClusterPolicy)
	enabled := cdiOnByDefault
	if nvidiaClusterPolicy.Spec.CDI.Enabled != nil {
		enabled = *nvidiaClusterPolicy.Spec.CDI.Enabled
	}
	if !enabled {
		return false, nil
	}
	if cdiOnByDefault {
		return true, nil
	}
	return nvidiaClusterPolicy.Spec.CDI.Default != nil && *nvidiaClusterPolicy.Spec.CDI.Default, nil
}

// gpuOperatorDefaultsToCDI reports whether the ClusterPolicy's GPU Operator
// version label is 25.10 or newer; an absent label is treated as older.
func gpuOperatorDefaultsToCDI(nvidiaClusterPolicy *nvidiav1.ClusterPolicy) bool {
	gpuOperatorVersion, found := nvidiaClusterPolicy.Labels[gpuOperatorVersionLabelName]
	return found && semver.Compare(gpuOperatorVersion, gpuOperatorVersionDefaultCDIDeprecated) >= 0
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
