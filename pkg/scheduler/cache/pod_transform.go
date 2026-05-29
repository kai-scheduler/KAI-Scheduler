// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

func setSchedulerPodTransform(informer cache.SharedIndexInformer) error {
	return informer.SetTransform(compactSchedulerPod)
}

func compactSchedulerPod(obj any) (any, error) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return obj, nil
	}

	compact := pod.DeepCopy()
	compact.ManagedFields = nil
	compact.Spec.Containers = compactContainers(compact.Spec.Containers)
	compact.Spec.InitContainers = compactInitContainers(compact.Spec.InitContainers)
	compact.Spec.EphemeralContainers = compactEphemeralContainers(compact.Spec.EphemeralContainers)
	return compact, nil
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
