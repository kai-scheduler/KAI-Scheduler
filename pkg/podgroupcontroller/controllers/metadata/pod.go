// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	resourcehelpers "k8s.io/component-helpers/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	commonpod "github.com/kai-scheduler/KAI-scheduler/pkg/common/pod"
	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/resources"
)

type PodMetadata struct {
	RequestedResources v1.ResourceList
	AllocatedResources v1.ResourceList
}

func GetPodMetadata(
	ctx context.Context, pod *v1.Pod, kubeClient client.Client, draAPIVersion string,
) (*PodMetadata, error) {
	var err error

	if isTerminalPod(pod) {
		// DRA ResourceClaims of terminal pods are deleted by the DRA driver, and
		// the pod no longer requests or holds any resources, so skip the lookup.
		return &PodMetadata{
			RequestedResources: v1.ResourceList{},
			AllocatedResources: v1.ResourceList{},
		}, nil
	}

	draClaims, err := commonresources.FetchPodResourceClaims(ctx, pod, kubeClient, draAPIVersion)
	if err != nil {
		return nil, err
	}

	requestedResources := v1.ResourceList{}
	if isActivePod(pod) {
		requestedResources, err = calculateRequestedResources(ctx, pod, kubeClient, draClaims)
		if err != nil {
			return nil, err
		}
	}

	allocatedResources := v1.ResourceList{}
	if commonpod.IsAllocated(pod) {
		allocatedResources, err = calculatedAllocatedResources(ctx, pod, kubeClient, draClaims)
		if err != nil {
			return nil, err
		}
	}

	return &PodMetadata{
		RequestedResources: requestedResources,
		AllocatedResources: allocatedResources,
	}, nil
}

func isActivePod(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodPending || pod.Status.Phase == v1.PodRunning
}

func isTerminalPod(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}

func calculatedAllocatedResources(
	ctx context.Context, pod *v1.Pod, kubeClient client.Client, draClaims []*resourceapi.ResourceClaim,
) (v1.ResourceList, error) {
	// Same aggregation the scheduler charges internally (KEP-753 sidecar formula,
	// KEP-1287 effective requests), keeping queue status consistent with the
	// resize webhook's delta baseline.
	allocatedResources := resourcehelpers.AggregateContainerRequests(pod, resourcehelpers.PodResourcesOptions{
		UseStatusResources: true,
	})

	gpuSharingReceivedResources, err := resources.ExtractGPUSharingReceivedResources(ctx, pod, kubeClient)
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Error(err, fmt.Sprintf("failed to calculate GPU sharing received resources for pod %s/%s",
			pod.Namespace, pod.Name))
		return nil, err
	}
	allocatedResources = resources.SumResources(allocatedResources, gpuSharingReceivedResources)

	draGPUAllocated := commonresources.DRAGPUResourceListFromClaims(draClaims)
	allocatedResources = resources.SumResources(allocatedResources, draGPUAllocated)

	return allocatedResources, nil
}

func calculateRequestedResources(
	ctx context.Context, pod *v1.Pod, kubeClient client.Client, draClaims []*resourceapi.ResourceClaim,
) (v1.ResourceList, error) {
	requestedResources := resourcehelpers.AggregateContainerRequests(pod, resourcehelpers.PodResourcesOptions{})
	gpuSharingRequestedResources, err := resources.ExtractGPUSharingRequestedResources(pod)
	if err != nil {
		return nil, err
	}
	requestedResources = resources.SumResources(requestedResources, gpuSharingRequestedResources)

	draGPURequested := commonresources.DRAGPUResourceListFromClaims(draClaims)
	requestedResources = resources.SumResources(requestedResources, draGPURequested)

	return requestedResources, nil
}
