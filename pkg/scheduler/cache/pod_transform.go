// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func setSchedulerPodTransform(informer cache.SharedIndexInformer) error {
	return informer.SetTransform(compactSchedulerPod)
}

func compactSchedulerPod(obj any) (any, error) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return obj, nil
	}

	if pod.Status.Phase == v1.PodSucceeded {
		return compactSucceededPod(pod), nil
	}

	compact := pod.DeepCopy()
	compact.ManagedFields = nil
	compact.Spec.Containers = compactContainers(compact.Spec.Containers)
	compact.Spec.InitContainers = compactInitContainers(compact.Spec.InitContainers)
	compact.Spec.EphemeralContainers = compactEphemeralContainers(compact.Spec.EphemeralContainers)
	compact.Status.ContainerStatuses = compactContainerStatuses(compact.Status.ContainerStatuses)
	compact.Status.InitContainerStatuses = compactContainerStatuses(compact.Status.InitContainerStatuses)
	compact.Status.Conditions = compactConditions(compact.Status.Conditions)
	return compact, nil
}

func compactSucceededPod(pod *v1.Pod) *v1.Pod {
	compact := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			UID:       pod.UID,
		},
		Status: v1.PodStatus{
			Phase: pod.Status.Phase,
		},
	}

	if podGroup, found := pod.Annotations[commonconstants.PodGroupAnnotationForPod]; found {
		compact.Annotations = map[string]string{commonconstants.PodGroupAnnotationForPod: podGroup}
	}
	if subGroup, found := pod.Labels[commonconstants.SubGroupLabelKey]; found {
		compact.Labels = map[string]string{commonconstants.SubGroupLabelKey: subGroup}
	}

	return compact
}

// compactContainerStatuses retains only the resize-relevant fields from each ContainerStatus.
// AllocatedResources and Resources are needed for effective-request accounting (KEP-1287).
func compactContainerStatuses(statuses []v1.ContainerStatus) []v1.ContainerStatus {
	compact := make([]v1.ContainerStatus, 0, len(statuses))
	for _, cs := range statuses {
		if cs.AllocatedResources == nil && cs.Resources == nil {
			continue
		}
		compact = append(compact, v1.ContainerStatus{
			Name:               cs.Name,
			AllocatedResources: cs.AllocatedResources.DeepCopy(),
			Resources:          cs.Resources.DeepCopy(),
		})
	}
	return compact
}

func compactConditions(conditions []v1.PodCondition) []v1.PodCondition {
	for _, c := range conditions {
		if c.Type == v1.PodResizePending {
			return []v1.PodCondition{c}
		}
	}
	return nil
}

func compactContainers(containers []v1.Container) []v1.Container {
	compact := make([]v1.Container, 0, len(containers))
	for _, container := range containers {
		compact = append(compact, v1.Container{
			Name:          container.Name,
			Ports:         container.Ports,
			EnvFrom:       compactEnvFromSources(container.EnvFrom),
			Env:           compactEnvVars(container.Env),
			Resources:     container.Resources,
			VolumeMounts:  container.VolumeMounts,
			RestartPolicy: container.RestartPolicy,
		})
	}
	return compact
}

func compactInitContainers(containers []v1.Container) []v1.Container {
	compact := compactContainers(containers)
	for i := range compact {
		compact[i].RestartPolicy = containers[i].RestartPolicy
	}
	return compact
}

func compactEphemeralContainers(containers []v1.EphemeralContainer) []v1.EphemeralContainer {
	compact := make([]v1.EphemeralContainer, 0, len(containers))
	for _, container := range containers {
		compact = append(compact, v1.EphemeralContainer{
			EphemeralContainerCommon: v1.EphemeralContainerCommon{
				Name:         container.Name,
				Ports:        container.Ports,
				EnvFrom:      compactEnvFromSources(container.EnvFrom),
				Env:          compactEnvVars(container.Env),
				Resources:    container.Resources,
				VolumeMounts: container.VolumeMounts,
			},
			TargetContainerName: container.TargetContainerName,
		})
	}
	return compact
}

func compactEnvVars(envVars []v1.EnvVar) []v1.EnvVar {
	compact := make([]v1.EnvVar, 0, len(envVars))
	for _, envVar := range envVars {
		if envVar.ValueFrom == nil || (envVar.ValueFrom.ConfigMapKeyRef == nil && envVar.ValueFrom.SecretKeyRef == nil) {
			continue
		}
		compact = append(compact, v1.EnvVar{
			Name: envVar.Name,
			ValueFrom: &v1.EnvVarSource{
				ConfigMapKeyRef: envVar.ValueFrom.ConfigMapKeyRef.DeepCopy(),
			},
		})
	}
	return compact
}

func compactEnvFromSources(envFrom []v1.EnvFromSource) []v1.EnvFromSource {
	compact := make([]v1.EnvFromSource, 0, len(envFrom))
	for _, source := range envFrom {
		if source.ConfigMapRef == nil && source.SecretRef == nil {
			continue
		}
		compact = append(compact, v1.EnvFromSource{
			ConfigMapRef: source.ConfigMapRef.DeepCopy(),
		})
	}
	return compact
}
