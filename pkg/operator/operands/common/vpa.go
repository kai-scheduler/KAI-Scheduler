// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaicommon "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

// BuildVPA creates a VerticalPodAutoscaler targeting the named resource of the given kind.
// Returns nil if VPA is not enabled.
func BuildVPA(vpaSpec *kaicommon.VPASpec, targetName, namespace, targetKind string) client.Object {
	if vpaSpec == nil || vpaSpec.Enabled == nil || !*vpaSpec.Enabled {
		return nil
	}

	return &vpav1.VerticalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "autoscaling.k8s.io/v1",
			Kind:       "VerticalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetName,
			Namespace: namespace,
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       targetKind,
				Name:       targetName,
			},
			UpdatePolicy:   vpaSpec.UpdatePolicy,
			ResourcePolicy: vpaSpec.ResourcePolicy,
		},
	}
}

// BuildVPAFromObjects finds the first Deployment or DaemonSet in objects and builds a VPA
// targeting it. Returns nil if VPA is not enabled or no workload is found.
func BuildVPAFromObjects(vpaSpec *kaicommon.VPASpec, objects []client.Object, namespace string) client.Object {
	if vpaSpec == nil || vpaSpec.Enabled == nil || !*vpaSpec.Enabled {
		return nil
	}
	for _, obj := range objects {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			return BuildVPA(vpaSpec, o.Name, namespace, "Deployment")
		case *appsv1.DaemonSet:
			return BuildVPA(vpaSpec, o.Name, namespace, "DaemonSet")
		}
	}
	return nil
}
