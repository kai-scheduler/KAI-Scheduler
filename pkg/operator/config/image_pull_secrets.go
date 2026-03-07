// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	v1 "k8s.io/api/core/v1"
)

func GetGlobalImagePullSecrets(globalConfig *kaiv1.GlobalConfig) []v1.LocalObjectReference {
	additionalImagePullSecrets := globalConfig.ImagePullSecrets
	var secretDeploymentObjs []v1.LocalObjectReference
	for _, secret := range additionalImagePullSecrets {
		deploymentObj := v1.LocalObjectReference{Name: secret}
		secretDeploymentObjs = append(secretDeploymentObjs, deploymentObj)
	}
	return secretDeploymentObjs
}
