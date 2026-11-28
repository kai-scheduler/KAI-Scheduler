// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1 "github.com/kai-scheduler/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"
)

// clusterPolicyCRDName is the GPU Operator CRD whose schema default for
// spec.cdi.enabled applies when a ClusterPolicy leaves it unset.
const clusterPolicyCRDName = "clusterpolicies.nvidia.com"

// IsGPUOperatorCDIEnabled returns whether the GPU Operator ClusterPolicy enables CDI.
// An unset spec.cdi.enabled takes the CRD default (true from GPU Operator 25.10).
// When that default is false, spec.cdi.default must also be true.
func IsGPUOperatorCDIEnabled(ctx context.Context, readerClient client.Reader) (bool, error) {
	nvidiaClusterPolicy, found, err := gpuOperatorClusterPolicy(ctx, readerClient)
	if err != nil || !found {
		return false, err
	}

	cdiOnByDefault := clusterPolicyCDIEnabledByDefault(ctx, readerClient)
	if !ptr.Deref(nvidiaClusterPolicy.Spec.CDI.Enabled, cdiOnByDefault) {
		return false, nil
	}
	return cdiOnByDefault || ptr.Deref(nvidiaClusterPolicy.Spec.CDI.Default, false), nil
}

func clusterPolicyCDIEnabledByDefault(ctx context.Context, readerClient client.Reader) bool {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := readerClient.Get(ctx, types.NamespacedName{Name: clusterPolicyCRDName}, crd); err != nil {
		if !apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("cannot read the ClusterPolicy CRD, assuming CDI is off by default", "error", err)
		}
		return false
	}
	for _, version := range crd.Spec.Versions {
		if !version.Storage || version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			continue
		}
		enabled := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["cdi"].Properties["enabled"]
		var value bool
		if enabled.Default == nil || json.Unmarshal(enabled.Default.Raw, &value) != nil {
			return false
		}
		return value
	}
	return false
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
