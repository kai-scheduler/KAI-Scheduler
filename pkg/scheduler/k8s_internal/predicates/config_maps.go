// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/configmap_info"
)

const (
	sharedGPUConfigMapNamePrefix = "runai/shared-gpu-configmap"
)

type ConfigMapPredicate struct {
	// configMaps is a map from namespace to map of configmap names
	configMapsNames map[string]map[string]bool
}

func NewConfigMapPredicate(configmaps map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo) *ConfigMapPredicate {
	predicate := &ConfigMapPredicate{
		configMapsNames: make(map[string]map[string]bool),
	}
	for _, configMap := range configmaps {
		if predicate.configMapsNames[configMap.Namespace] == nil {
			predicate.configMapsNames[configMap.Namespace] = make(map[string]bool)
		}

		predicate.configMapsNames[configMap.Namespace][configMap.Name] = true
	}

	return predicate
}

func (_ *ConfigMapPredicate) isPreFilterRequired(pod *v1.Pod) bool {
	return len(getAllRequiredConfigMapNames(pod)) > 0
}

func (_ *ConfigMapPredicate) isFilterRequired(_ *v1.Pod) bool {
	return false
}

func (cmp *ConfigMapPredicate) PreFilter(ctx context.Context, _ ksf.CycleState, pod *v1.Pod, _ []ksf.NodeInfo) (
	*k8sframework.PreFilterResult, *ksf.Status) {
	requiredConfigMapNames := getAllRequiredConfigMapNames(pod)

	var missingConfigMaps []string
	for _, cm := range requiredConfigMapNames {
		if !cmp.configMapExists(pod.Namespace, cm) {
			missingConfigMaps = append(missingConfigMaps, cm)
		}
	}

	if len(missingConfigMaps) > 0 {
		return nil, ksf.NewStatus(ksf.Unschedulable, fmt.Sprintf("Missing required configmaps: %v", missingConfigMaps))
	}

	return nil, nil
}

func (cmp *ConfigMapPredicate) configMapExists(namespace, name string) bool {
	if cmp.configMapsNames[namespace] == nil {
		return false
	}

	return cmp.configMapsNames[namespace][name]
}

func getAllRequiredConfigMapNames(pod *v1.Pod) []string {
	configmaps := map[string]bool{}

	for _, volume := range pod.Spec.Volumes {
		if !isConfigMapVolume(volume, pod) {
			continue
		}
		configMapName := volume.ConfigMap.LocalObjectReference.Name
		if isSharedGPUConfigMap(pod, configMapName) {
			continue
		}
		configmaps[configMapName] = true
	}

	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, container := range allContainers {
		for _, envVar := range container.Env {
			if envVar.ValueFrom == nil || envVar.ValueFrom.ConfigMapKeyRef == nil {
				continue
			}
			configMapName := envVar.ValueFrom.ConfigMapKeyRef.Name
			if isSharedGPUConfigMap(pod, configMapName) {
				continue
			}
			configmaps[configMapName] = true
		}
		for _, envVar := range container.EnvFrom {
			if envVar.ConfigMapRef == nil {
				continue
			}
			configMapName := envVar.ConfigMapRef.Name
			if isSharedGPUConfigMap(pod, configMapName) {
				continue
			}
			configmaps[configMapName] = true
		}
	}

	return maps.Keys(configmaps)
}

func isSharedGPUConfigMap(pod *v1.Pod, configMapName string) bool {
	sharedGPUConfigMapPrefix, found := pod.Annotations[sharedGPUConfigMapNamePrefix]
	if !found {
		return false
	}
	return strings.HasPrefix(configMapName, sharedGPUConfigMapPrefix)
}

func isConfigMapVolume(volume v1.Volume, pod *v1.Pod) bool {
	if volume.ConfigMap == nil {
		return false
	}

	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, container := range allContainers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == volume.Name {
				return true
			}
		}
	}

	return false
}
