// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/resources"
)

type PodMetadata struct {
	RequestedResources v1.ResourceList
	AllocatedResources v1.ResourceList
}

func GetPodMetadata(ctx context.Context, pod *v1.Pod, kubeClient client.Client) (*PodMetadata, error) {
	var err error

	requestedResources := v1.ResourceList{}
	if isActivePod(pod) {
		requestedResources, err = calculateRequestedResources(pod)
		if err != nil {
			return nil, err
		}
	}

	allocatedResources := v1.ResourceList{}
	if isAllocatedPod(pod) {
		allocatedResources, err = calculatedAllocatedResources(ctx, pod, kubeClient)
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

func isAllocatedPod(pod *v1.Pod) bool {
	if pod.Status.Phase == v1.PodPending {
		return isPodScheduled(pod)
	}
	return pod.Status.Phase == v1.PodRunning
}

func isPodScheduled(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodScheduled {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

func calculatedAllocatedResources(ctx context.Context, pod *v1.Pod, kubeClient client.Client) (
	v1.ResourceList, error) {
	allocatedResources := v1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		allocatedResources = resources.SumResources(allocatedResources, container.Resources.Requests)
	}

	gpuSharingReceivedResources, err := resources.ExtractGPUSharingReceivedResources(ctx, pod, kubeClient)
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Error(err, fmt.Sprintf("failed to calculate GPU sharing received resources for pod %s/%s",
			pod.Namespace, pod.Name))
		return nil, err
	}
	allocatedResources = resources.SumResources(allocatedResources, gpuSharingReceivedResources)

	return allocatedResources, nil
}

func calculateRequestedResources(pod *v1.Pod) (v1.ResourceList, error) {
	requestedResources := v1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		requestedResources = resources.SumResources(requestedResources, container.Resources.Requests)
	}
	gpuSharingRequestedResources, err := resources.ExtractGPUSharingRequestedResources(pod)
	if err != nil {
		return nil, err
	}
	requestedResources = resources.SumResources(requestedResources, gpuSharingRequestedResources)
	return requestedResources, nil
}
