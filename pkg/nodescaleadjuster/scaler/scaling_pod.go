// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scaler

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/consts"
)

func scalingPodName(unschedulablePodNamespace string, unschedulablePodName string) string {
	return fmt.Sprintf("%s-%s", unschedulablePodNamespace, unschedulablePodName)
}

func createScalingPodSpec(
	scalingPodAppLabel string, scalingPodServiceAccount string,
	unschedulablePod *corev1.Pod, image, namespace string, numDevices int64,
) *corev1.Pod {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			constants.NvidiaGpuResource: *resource.NewQuantity(numDevices, resource.DecimalSI),
		},
		Limits: corev1.ResourceList{
			constants.NvidiaGpuResource: *resource.NewQuantity(numDevices, resource.DecimalSI),
		},
	}
	for _, container := range unschedulablePod.Spec.Containers {
		if container.Resources.Requests != nil {
			addCpuResourceRequest(&resources, container.Resources.Requests.Cpu())
			addMemoryResourceRequest(&resources, container.Resources.Requests.Memory())
		}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalingPodName(unschedulablePod.Namespace, unschedulablePod.Name),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName:       scalingPodAppLabel,
				consts.SharedGpuPodName:      unschedulablePod.Name,
				consts.SharedGpuPodNamespace: unschedulablePod.Namespace,
			},
		},
		Spec: corev1.PodSpec{
			Affinity:      unschedulablePod.Spec.Affinity.DeepCopy(),
			Tolerations:   unschedulablePod.Spec.Tolerations,
			NodeSelector:  unschedulablePod.Spec.NodeSelector,
			RestartPolicy: corev1.RestartPolicyOnFailure,
			Containers: []corev1.Container{
				{
					Name:            consts.ScalingPodContainerName,
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Resources:       resources,
				},
			},
			ServiceAccountName: scalingPodServiceAccount,
		},
	}
}

func addCpuResourceRequest(resources *corev1.ResourceRequirements, cpu *resource.Quantity) {
	requestedCpu, found := resources.Requests[corev1.ResourceCPU]
	if found {
		requestedCpu.Add(*cpu)
	} else {
		requestedCpu = cpu.DeepCopy()
	}
	resources.Requests[corev1.ResourceCPU] = requestedCpu
}

func addMemoryResourceRequest(resources *corev1.ResourceRequirements, memory *resource.Quantity) {
	requestedMemory, found := resources.Requests[corev1.ResourceMemory]
	if found {
		requestedMemory.Add(*memory)
	} else {
		requestedMemory = memory.DeepCopy()
	}
	resources.Requests[corev1.ResourceMemory] = requestedMemory
}
