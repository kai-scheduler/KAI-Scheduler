// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/consts"
)

const (
	SchedulerName            = "kai-scheduler"
	ScalingPodNamespace      = "kai-scale-adjust"
	ScalingPodAppLabel       = "scaling-pod"
	ScalingPodServiceAccount = "scaling-pod"
)

func NewFakeClient(interceptorFuncs *interceptor.Funcs, objects ...client.Object) client.Client {
	builder := fake.NewClientBuilder()
	if len(objects) > 0 {
		builder.WithObjects(objects...)
	}
	if interceptorFuncs != nil {
		builder.WithInterceptorFuncs(*interceptorFuncs)
	}
	return builder.Build()
}

func CreateUnschedulableFractionPod(name, namespace string, fractionValue string, numDevices int) *corev1.Pod {
	annotations := map[string]string{
		constants.GpuFraction: fractionValue,
	}
	if numDevices > 1 {
		annotations[constants.GpuFractionsNumDevices] = strconv.Itoa(numDevices)
	}
	return CreateUnschedulablePod(name, namespace, annotations)
}

func CreateUnschedulablePodWithGpuMemory(name, namespace string, gpuMemory string, numDevices int) *corev1.Pod {
	annotations := map[string]string{
		constants.GpuMemory: gpuMemory,
	}
	if numDevices > 1 {
		annotations[constants.GpuFractionsNumDevices] = strconv.Itoa(numDevices)
	}
	return CreateUnschedulablePod(name, namespace, annotations)
}

func CreateUnschedulablePod(name, namespace string, annotations map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			SchedulerName: SchedulerName,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionFalse,
					Reason: corev1.PodReasonUnschedulable,
				},
			},
		},
	}
	return pod
}

func CreatePendingFractionPod(name, namespace string, fractionValue string, numDevices int) *corev1.Pod {
	annotations := map[string]string{
		constants.GpuFraction: fractionValue,
	}
	if numDevices > 1 {
		annotations[constants.GpuFractionsNumDevices] = strconv.Itoa(numDevices)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			SchedulerName: SchedulerName,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

func CreateScalingPod(unschedulablePodNamespace, unschedulablePodName string, numDevices int) *corev1.Pod {
	name := fmt.Sprintf("%s-%s", unschedulablePodNamespace, unschedulablePodName)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ScalingPodNamespace,
			Labels: map[string]string{
				consts.SharedGpuPodName:      unschedulablePodName,
				consts.SharedGpuPodNamespace: unschedulablePodNamespace,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							constants.NvidiaGpuResource: *resource.NewQuantity(int64(numDevices), resource.DecimalSI),
						},
						Limits: corev1.ResourceList{
							constants.NvidiaGpuResource: *resource.NewQuantity(int64(numDevices), resource.DecimalSI),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}
