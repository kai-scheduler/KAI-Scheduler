// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"reflect"

	enginev2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
)

func podGroupsEqual(oldPodGroup, newPodGroup *enginev2alpha2.PodGroup) bool {
	return reflect.DeepEqual(oldPodGroup.Spec, newPodGroup.Spec) &&
		reflect.DeepEqual(oldPodGroup.OwnerReferences, newPodGroup.OwnerReferences) &&
		mapsEqualBySourceKeys(newPodGroup.Labels, oldPodGroup.Labels) &&
		mapsEqualBySourceKeys(newPodGroup.Annotations, oldPodGroup.Annotations)
}

func mapsEqualBySourceKeys(source, target map[string]string) bool {
	if source != nil && target == nil {
		return false
	}

	for key, sourceValue := range source {
		if targetValue, exists := target[key]; !exists || targetValue != sourceValue {
			return false
		}
	}
	return true
}

func updatePodGroup(oldPodGroup, newPodGroup *enginev2alpha2.PodGroup) {
	oldPodGroup.Annotations = copyStringMap(newPodGroup.Annotations, oldPodGroup.Annotations)
	oldPodGroup.Labels = copyStringMap(newPodGroup.Labels, oldPodGroup.Labels)
	oldPodGroup.Spec = newPodGroup.Spec
	oldPodGroup.OwnerReferences = newPodGroup.OwnerReferences
}

func copyStringMap(source map[string]string, target map[string]string) map[string]string {
	if source != nil && target == nil {
		target = map[string]string{}
	}
	for k, v := range source {
		target[k] = v
	}
	return target
}
