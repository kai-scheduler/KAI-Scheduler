// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	receivedResourceTypeAnnotationName = "received-resource-type"
	receivedTypeFraction               = "Fraction"
	receivedTypeRegular                = "Regular"
	receivedTypeNone                   = ""
)

func ExtractGPUSharingReceivedResources(ctx context.Context, pod *v1.Pod, kubeClient client.Client) (
	v1.ResourceList, error) {
	resources := v1.ResourceList{}

	receivedResourceType := pod.Annotations[receivedResourceTypeAnnotationName]
	if receivedResourceType != receivedTypeFraction {
		return resources, nil
	}

	fractionResource, err := calculateAllocatedFraction(ctx, pod, kubeClient)
	resources[constants.NvidiaGpuResource] = fractionResource
	return resources, err
}
