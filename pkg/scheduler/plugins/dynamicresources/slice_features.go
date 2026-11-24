// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/component-base/featuregate"
	"k8s.io/dynamic-resource-allocation/structured"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/utils/ptr"
)

// sliceFeatureEnable says which allocator features may be turned on because a
// ResourceSlice uses them. A feature set with --feature-gates is left as the
// gate says.
type sliceFeatureEnable struct {
	partitionableDevices bool
	consumableCapacity   bool
}

// explicitGates reports whether a feature gate was set with --feature-gates.
type explicitGates interface {
	ExplicitlySet(featuregate.Feature) bool
}

func sliceFeatureEnableFromGates(gates explicitGates) sliceFeatureEnable {
	return sliceFeatureEnable{
		partitionableDevices: !gates.ExplicitlySet(kubefeatures.DRAPartitionableDevices),
		consumableCapacity:   !gates.ExplicitlySet(kubefeatures.DRAConsumableCapacity),
	}
}

// sliceAllocatorFeatures turns on PartitionableDevices and ConsumableCapacity
// when a ResourceSlice uses fields of those features. The API server drops
// these fields when its gate is off, so a slice that carries them means the
// cluster has the feature on, whatever the Kubernetes version. Features that
// no slice uses keep their gate value, so clusters without such slices keep
// the allocator they had.
func sliceAllocatorFeatures(features structured.Features, enable sliceFeatureEnable, slices []*resourceapi.ResourceSlice) structured.Features {
	for _, slice := range slices {
		if enable.partitionableDevices && !features.PartitionableDevices && usesPartitionableDevices(slice) {
			features.PartitionableDevices = true
		}
		if enable.consumableCapacity && !features.ConsumableCapacity && usesConsumableCapacity(slice) {
			features.ConsumableCapacity = true
		}
	}
	return features
}

// usesPartitionableDevices reports whether a slice has shared counters,
// devices that consume counters, or per-device node selection.
func usesPartitionableDevices(slice *resourceapi.ResourceSlice) bool {
	if len(slice.Spec.SharedCounters) > 0 || ptr.Deref(slice.Spec.PerDeviceNodeSelection, false) {
		return true
	}
	for i := range slice.Spec.Devices {
		device := &slice.Spec.Devices[i]
		if len(device.ConsumesCounters) > 0 || device.NodeName != nil || device.NodeSelector != nil || device.AllNodes != nil {
			return true
		}
	}
	return false
}

// usesConsumableCapacity reports whether a slice has a device that allows
// multiple allocations or a capacity with a request policy.
func usesConsumableCapacity(slice *resourceapi.ResourceSlice) bool {
	for i := range slice.Spec.Devices {
		device := &slice.Spec.Devices[i]
		if ptr.Deref(device.AllowMultipleAllocations, false) {
			return true
		}
		for _, capacity := range device.Capacity {
			if capacity.RequestPolicy != nil {
				return true
			}
		}
	}
	return false
}
