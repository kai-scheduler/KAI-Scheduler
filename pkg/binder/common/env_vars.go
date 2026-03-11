// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func AddDirectEnvVarsConfigMapSource(container *v1.Container, directEnvVarsMapName string) {
	for _, env := range container.EnvFrom {
		if env.ConfigMapRef != nil && env.ConfigMapRef.Name == directEnvVarsMapName {
			return
		}
	}

	container.EnvFrom = append(container.EnvFrom, v1.EnvFromSource{
		ConfigMapRef: &v1.ConfigMapEnvSource{
			LocalObjectReference: v1.LocalObjectReference{
				Name: directEnvVarsMapName,
			},
			Optional: ptr.To(false),
		},
	})
}

func AddEnvVarToContainer(container *v1.Container, envVar v1.EnvVar) {
	var envVars []v1.EnvVar

	for _, existingEnv := range container.Env {
		if existingEnv.Name != envVar.Name {
			envVars = append(envVars, existingEnv)
		}
	}
	container.Env = append(envVars, envVar)
}
