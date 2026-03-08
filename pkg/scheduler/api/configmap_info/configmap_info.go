// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package configmap_info

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

type ConfigMapInfo struct {
	UID       common_info.ConfigMapID
	Name      string
	Namespace string
}

func NewConfigMapInfo(configMap *v1.ConfigMap) *ConfigMapInfo {
	name := types.NamespacedName{
		Namespace: configMap.Namespace,
		Name:      configMap.Name,
	}
	return &ConfigMapInfo{
		UID:       common_info.ConfigMapID(name.String()),
		Namespace: configMap.Namespace,
		Name:      configMap.Name,
	}
}
