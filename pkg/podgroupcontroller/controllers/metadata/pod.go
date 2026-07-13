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

	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/resources"
)

// gpuResourceSuffix is how the queue accounting recognizes a GPU, matching getAllocatedGpus in the
// queue-controller metrics.
const gpuResourceSuffix = "/gpu"

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

// podSteadyStateResources sums what a pod holds concurrently while it runs: regular containers, native
// sidecars (init containers with restartPolicy Always) and Pod overhead. The peak of a plain init container is
// not included, and neither is a GPU asked for by a sidecar or by an overhead, see withoutGpuRequirements.
func podSteadyStateResources(pod *v1.Pod) v1.ResourceList {
	total := v1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		total = resources.SumResources(total, container.Resources.Requests)
	}
	for _, initContainer := range pod.Spec.InitContainers {
		if isNativeSidecar(initContainer) {
			total = resources.SumResources(total, withoutGpuResources(initContainer.Resources.Requests))
		}
	}
	return resources.SumResources(total, withoutGpuResources(pod.Spec.Overhead))
}

// isNativeSidecar reports whether an init container keeps running alongside the regular containers.
func isNativeSidecar(initContainer v1.Container) bool {
	return initContainer.RestartPolicy != nil && *initContainer.RestartPolicy == v1.ContainerRestartPolicyAlways
}

// withoutGpuResources drops every name the queue accounting counts as a GPU, which is the same rule
// getAllocatedGpus uses. Two reasons to leave them out of a sidecar or an overhead, and both matter. The
// scheduler rebuilds a pod's GPU requirement from a GPU-sharing or legacy MIG annotation and drops the
// container request, so counting a sidecar GPU would report one nothing reserved. And getAllocatedGpus returns
// the first "*/gpu" key it meets in map order, so putting a new one into the status would change, and could
// destabilize, a metric that feeds the fairshare usage database. Both wait on #1880.
func withoutGpuResources(list v1.ResourceList) v1.ResourceList {
	filtered := v1.ResourceList{}
	for name, quantity := range list {
		if strings.HasSuffix(string(name), gpuResourceSuffix) || commonresources.IsMigResource(string(name)) {
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
