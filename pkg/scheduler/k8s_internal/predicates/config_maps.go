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

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/configmap_info"
)

const (
	sharedGPUConfigMapAnnotation = "runai/shared-gpu-configmap"
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
	*ksf.PreFilterResult, *ksf.Status) {
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

type configMapRef struct {
	name     string
	optional bool
}

func getAllRequiredConfigMapNames(pod *v1.Pod) []string {
	configmaps := map[string]bool{}
	mountedVolumes := getMountedVolumes(pod)

	for _, volume := range pod.Spec.Volumes {
		if !mountedVolumes[volume.Name] {
			continue
		}
		for _, ref := range getConfigMapRefsFromVolume(volume) {
			if !ref.optional && !isSharedGPUConfigMap(pod, ref.name) {
				configmaps[ref.name] = true
			}
		}
	}

	for _, ref := range getConfigMapRefsFromContainers(pod) {
		if !ref.optional && !isSharedGPUConfigMap(pod, ref.name) {
			configmaps[ref.name] = true
		}
	}

	return maps.Keys(configmaps)
}

func getConfigMapRefsFromVolume(volume v1.Volume) []configMapRef {
	var refs []configMapRef
	addRef := func(name string, optional *bool) {
		refs = append(refs, configMapRef{
			name:     name,
			optional: optional != nil && *optional,
		})
	}
	if volume.ConfigMap != nil {
		addRef(volume.ConfigMap.LocalObjectReference.Name, volume.ConfigMap.Optional)
	}
	if volume.Projected != nil {
		for _, source := range volume.Projected.Sources {
			if source.ConfigMap != nil {
				addRef(source.ConfigMap.Name, source.ConfigMap.Optional)
			}
		}
	}
	return refs
}

func getConfigMapRefsFromContainers(pod *v1.Pod) []configMapRef {
	var refs []configMapRef
	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, container := range allContainers {
		refs = append(refs, getConfigMapRefsFromEnv(container.Env, container.EnvFrom)...)
	}
	for _, container := range pod.Spec.EphemeralContainers {
		refs = append(refs, getConfigMapRefsFromEnv(container.Env, container.EnvFrom)...)
	}
	return refs
}

func getConfigMapRefsFromEnv(envVars []v1.EnvVar, envFromSources []v1.EnvFromSource) []configMapRef {
	var refs []configMapRef
	for _, envVar := range envVars {
		if envVar.ValueFrom != nil && envVar.ValueFrom.ConfigMapKeyRef != nil {
			optional := envVar.ValueFrom.ConfigMapKeyRef.Optional != nil && *envVar.ValueFrom.ConfigMapKeyRef.Optional
			refs = append(refs, configMapRef{
				name:     envVar.ValueFrom.ConfigMapKeyRef.Name,
				optional: optional,
			})
		}
	}
	for _, envFrom := range envFromSources {
		if envFrom.ConfigMapRef != nil {
			optional := envFrom.ConfigMapRef.Optional != nil && *envFrom.ConfigMapRef.Optional
			refs = append(refs, configMapRef{
				name:     envFrom.ConfigMapRef.Name,
				optional: optional,
			})
		}
	}
	return refs
}

func getMountedVolumes(pod *v1.Pod) map[string]bool {
	mounted := make(map[string]bool)
	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, container := range allContainers {
		for _, volumeMount := range container.VolumeMounts {
			mounted[volumeMount.Name] = true
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		for _, volumeMount := range container.VolumeMounts {
			mounted[volumeMount.Name] = true
		}
	}
	return mounted
}

func isSharedGPUConfigMap(pod *v1.Pod, configMapName string) bool {
	sharedGPUConfigMapPrefix, found := pod.Annotations[sharedGPUConfigMapAnnotation]
	if !found {
		return false
	}
	return strings.HasPrefix(configMapName, sharedGPUConfigMapPrefix)
}
