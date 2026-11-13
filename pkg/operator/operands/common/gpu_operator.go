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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1 "github.com/kai-scheduler/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"
)

const (
	// clusterPolicyCRDName is the GPU Operator CRD whose schema default for
	// spec.cdi.enabled says whether CDI is on when a ClusterPolicy leaves it out.
	clusterPolicyCRDName = "clusterpolicies.nvidia.com"
)

// IsGPUOperatorCDIEnabled returns whether the GPU Operator ClusterPolicy enables CDI.
// A ClusterPolicy that leaves spec.cdi.enabled unset takes the default its CRD
// declares, which is true from GPU Operator 25.10 and false before. Where that
// default is true, spec.cdi.default no longer matters; where it is false or
// unknown, CDI is used only when both spec.cdi.enabled and spec.cdi.default are
// true, as before.
func IsGPUOperatorCDIEnabled(ctx context.Context, readerClient client.Reader) (bool, error) {
	nvidiaClusterPolicy, found, err := gpuOperatorClusterPolicy(ctx, readerClient)
	if err != nil || !found {
		return false, err
	}

	cdiOnByDefault := clusterPolicyCDIEnabledByDefault(ctx, readerClient)
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

// clusterPolicyCDIEnabledByDefault reads the default of spec.cdi.enabled from the
// stored version of the ClusterPolicy CRD. An unreadable CRD or a schema with no
// default reads as false. The operator already watches CRDs for the queues CRD,
// so the read is served from that cache and starts no informer of its own.
func clusterPolicyCDIEnabledByDefault(ctx context.Context, readerClient client.Reader) bool {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := readerClient.Get(ctx, types.NamespacedName{Name: clusterPolicyCRDName}, crd); err != nil {
		if !apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("cannot read the ClusterPolicy CRD, assuming CDI is off unless the ClusterPolicy enables it", "error", err)
		}
		return false
	}
	for _, version := range crd.Spec.Versions {
		if !version.Storage || version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			continue
		}
		spec, ok := version.Schema.OpenAPIV3Schema.Properties["spec"]
		if !ok {
			return false
		}
		cdi, ok := spec.Properties["cdi"]
		if !ok {
			return false
		}
		enabled, ok := cdi.Properties["enabled"]
		if !ok || enabled.Default == nil {
			return false
		}
		var value bool
		if err := json.Unmarshal(enabled.Default.Raw, &value); err != nil {
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
