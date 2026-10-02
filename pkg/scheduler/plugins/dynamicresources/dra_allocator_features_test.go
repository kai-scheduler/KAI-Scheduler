// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"
)

// fakeDeviceClassLister is the minimal structured.DeviceClassLister needed to
// drive the real k8s.io/dynamic-resource-allocation allocator in tests,
// without pulling in kai-scheduler's full SharedDRAManager/session wiring.
type fakeDeviceClassLister struct {
	classes map[string]*resourceapi.DeviceClass
}

func (f *fakeDeviceClassLister) List() ([]*resourceapi.DeviceClass, error) {
	var out []*resourceapi.DeviceClass
	for _, c := range f.classes {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeDeviceClassLister) Get(className string) (*resourceapi.DeviceClass, error) {
	return f.classes[className], nil
}

// buildPerDeviceNodeSelectionSlice mirrors the ResourceSlice shape published by
// DRA drivers using a shared-counter/partitionable-device capacity model (e.g.
// nvidia-dra-driver-gpu with DynamicMIG): node selection is expressed per-device
// via Spec.PerDeviceNodeSelection + Device.NodeName, rather than at the slice level.
func buildPerDeviceNodeSelectionSlice(nodeName string) *resourceapi.ResourceSlice {
	return &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "node0-gpu"},
		Spec: resourceapi.ResourceSliceSpec{
			Driver: "gpu.nvidia.com",
			Pool: resourceapi.ResourcePool{
				Name:               nodeName,
				ResourceSliceCount: 1,
			},
			PerDeviceNodeSelection: ptr.To(true),
			Devices: []resourceapi.Device{
				{
					Name:     "gpu0",
					NodeName: ptr.To(nodeName),
					Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
						"gpu": {Value: resource.MustParse("1")},
					},
				},
			},
		},
	}
}

func buildGpuClaim(name, deviceClassName string) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{
					{
						Name: "request",
						Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: deviceClassName,
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           1,
						},
					},
				},
			},
		},
	}
}

// runAllocate replicates exactly how draPlugin.allocate() in dynamicresources.go
// drives the upstream allocator: same AllocatedState, DeviceClassLister, and
// Allocate() call shape, differing only in the Features passed to NewAllocator.
func runAllocate(t *testing.T, features structured.Features) []resourceapi.AllocationResult {
	t.Helper()

	const deviceClassName = "gpu.nvidia.com"
	classLister := &fakeDeviceClassLister{
		classes: map[string]*resourceapi.DeviceClass{
			deviceClassName: {ObjectMeta: metav1.ObjectMeta{Name: deviceClassName}},
		},
	}

	slices := []*resourceapi.ResourceSlice{buildPerDeviceNodeSelectionSlice("node0")}
	claims := []*resourceapi.ResourceClaim{buildGpuClaim("claim-0", deviceClassName)}

	allocatedState := structured.AllocatedState{
		AllocatedDevices:         sets.New[structured.DeviceID](),
		AllocatedSharedDeviceIDs: sets.New[structured.SharedDeviceID](),
		AggregatedCapacity:       structured.NewConsumedCapacityCollection(),
	}

	allocator, err := structured.NewAllocator(
		context.Background(), features,
		allocatedState,
		classLister,
		slices,
		cel.NewCache(10, cel.Features{}),
	)
	require.NoError(t, err)

	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node0"}}
	results, err := allocator.Allocate(context.Background(), node, claims)
	require.NoError(t, err)
	return results
}

// TestDRAAllocator_PerDeviceNodeSelection_RequiresPartitionableDevicesFeature reproduces
// https://github.com/kai-scheduler/KAI-Scheduler/issues/2100: kai-scheduler's
// dynamicresources plugin calls structured.NewAllocator with a hardcoded empty
// structured.Features{}, so ResourceSlices published with PerDeviceNodeSelection
// (the shape used by shared-counter/partitionable-device DRA drivers, e.g.
// nvidia-dra-driver-gpu with DynamicMIG) are silently dropped from pool gathering
// and allocation fails with no matching devices, even though a matching device exists.
func TestDRAAllocator_PerDeviceNodeSelection_RequiresPartitionableDevicesFeature(t *testing.T) {
	t.Run("pre-fix: empty Features{} silently drops the slice, allocation yields no results", func(t *testing.T) {
		results := runAllocate(t, structured.Features{})
		assert.Empty(t, results, "bug reproduced: allocator should return 0 results when PartitionableDevices is not enabled")
	})

	t.Run("fix: PartitionableDevices enabled, allocator finds and allocates the device", func(t *testing.T) {
		results := runAllocate(t, structured.Features{PartitionableDevices: true})
		require.Len(t, results, 1, "fix verified: allocator should allocate the device once PartitionableDevices is set")
	})
}
