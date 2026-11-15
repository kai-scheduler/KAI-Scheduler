// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scaler

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourcehelpers "k8s.io/component-helpers/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/fips"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/consts"
	"github.com/kai-scheduler/api/constants"
)

func scalingPodName(unschedulablePodNamespace string, unschedulablePodName string) string {
	return fmt.Sprintf("%s-%s", unschedulablePodNamespace, unschedulablePodName)
}

func createScalingPodSpec(
	scalingPodAppLabel string, scalingPodServiceAccount string,
	unschedulablePod *corev1.Pod, image, namespace string, numDevices int64, fipsOnly bool,
) *corev1.Pod {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			constants.NvidiaGpuResource: *resource.NewQuantity(numDevices, resource.DecimalSI),
		},
		Limits: corev1.ResourceList{
			constants.NvidiaGpuResource: *resource.NewQuantity(numDevices, resource.DecimalSI),
		},
	}
	// Mirror what kube-scheduler charges for the pod: init and sidecar containers (KEP-753),
	// pod-level resources (KEP-2837) and pod overhead.
	podRequests := resourcehelpers.PodRequests(unschedulablePod, resourcehelpers.PodResourcesOptions{})
	for _, resourceName := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		if quantity, found := podRequests[resourceName]; found {
			resources.Requests[resourceName] = quantity
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
					Env:             fips.OnlyEnv(fipsOnly),
				},
			},
			ServiceAccountName: scalingPodServiceAccount,
		},
	}
}
