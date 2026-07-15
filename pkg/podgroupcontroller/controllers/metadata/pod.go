// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
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
	if isAllocatedPod(pod) {
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

// podSteadyStateResources sums what a pod holds while it runs: regular containers, native sidecars
// (init containers with restartPolicy Always) and Pod overhead, the way the scheduler counts a running pod.
// The peak of a non-restartable init container is not folded in yet; #1880 tracks closing that gap.
// A native sidecar's GPU is counted, since the scheduler counts it, unless the pod's GPU is rebuilt from an
// annotation (see gpuIsRebuiltFromAnnotation), where the annotation decides and the container request is
// dropped. Pod overhead never adds a GPU, since the scheduler adds only its base resources.
func podSteadyStateResources(pod *v1.Pod) v1.ResourceList {
	total := v1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		total = resources.SumResources(total, container.Resources.Requests)
	}
	dropSidecarGpu := gpuIsRebuiltFromAnnotation(pod)
	for _, initContainer := range pod.Spec.InitContainers {
		if isNativeSidecar(initContainer) {
			requests := initContainer.Resources.Requests
			if dropSidecarGpu {
				requests = withoutGpuResources(requests)
			}
			total = resources.SumResources(total, requests)
		}
	}
	return resources.SumResources(total, withoutGpuResources(pod.Spec.Overhead))
}

// gpuIsRebuiltFromAnnotation reports whether the scheduler derives the pod's GPU from an annotation
// (a GPU fraction, gpu-memory, or a legacy MIG annotation) instead of from the container requests. For
// those pods the scheduler drops the container GPU request and the annotation decides, so a sidecar's GPU
// must not reach the status either. Admission rejects a whole nvidia.com/gpu next to a fraction, but it
// does not inspect MIG annotations, so the MIG case is handled here.
func gpuIsRebuiltFromAnnotation(pod *v1.Pod) bool {
	if commonresources.RequestsGPUFraction(pod) {
		return true
	}
	for name := range pod.Annotations {
		if commonresources.IsMigResource(name) {
			return true
		}
	}
	return false
}

// isNativeSidecar reports whether an init container keeps running alongside the regular containers.
func isNativeSidecar(initContainer v1.Container) bool {
	return initContainer.RestartPolicy != nil && *initContainer.RestartPolicy == v1.ContainerRestartPolicyAlways
}

// withoutGpuResources drops every name the queue accounting treats as a GPU, the same rule getAllocatedGpus
// applies to Queue.status.allocated. It is applied to Pod overhead, which never contributes a GPU, and to a
// native sidecar on a pod whose GPU is rebuilt from an annotation.
func withoutGpuResources(list v1.ResourceList) v1.ResourceList {
	filtered := v1.ResourceList{}
	for name, quantity := range list {
		if strings.HasSuffix(string(name), constants.GpuResourceSuffix) || commonresources.IsMigResource(string(name)) {
			continue
		}
		filtered[name] = quantity.DeepCopy()
	}
	return filtered
}

func calculatedAllocatedResources(
	ctx context.Context, pod *v1.Pod, kubeClient client.Client, draClaims []*resourceapi.ResourceClaim,
) (v1.ResourceList, error) {
	allocatedResources := podSteadyStateResources(pod)

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
	requestedResources := podSteadyStateResources(pod)

	gpuSharingRequestedResources, err := resources.ExtractGPUSharingRequestedResources(pod)
	if err != nil {
		return nil, err
	}
	requestedResources = resources.SumResources(requestedResources, gpuSharingRequestedResources)

	draGPURequested := commonresources.DRAGPUResourceListFromClaims(draClaims)
	requestedResources = resources.SumResources(requestedResources, draGPURequested)

	return requestedResources, nil
}
