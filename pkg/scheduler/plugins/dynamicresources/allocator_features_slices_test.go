// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

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
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
)

type singleDeviceClassLister struct {
	class *resourceapi.DeviceClass
}

func (l *singleDeviceClassLister) List() ([]*resourceapi.DeviceClass, error) {
	return []*resourceapi.DeviceClass{l.class}, nil
}

func (l *singleDeviceClassLister) Get(string) (*resourceapi.DeviceClass, error) {
	return l.class, nil
}

// counterBackedGPUSlices mirrors what the NVIDIA DRA driver publishes with
// DynamicMIG enabled: the full GPU consumes counters from a counter set that
// lives in its own ResourceSlice of the same pool.
func counterBackedGPUSlices(nodeName string) []*resourceapi.ResourceSlice {
	memory := map[string]resourceapi.Counter{"memory": {Value: resource.MustParse("48Gi")}}
	pool := resourceapi.ResourcePool{Name: nodeName, ResourceSliceCount: 2}
	return []*resourceapi.ResourceSlice{
		{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName + "-counters"},
			Spec: resourceapi.ResourceSliceSpec{
				Driver:         "gpu.nvidia.com",
				NodeName:       ptr.To(nodeName),
				Pool:           pool,
				SharedCounters: []resourceapi.CounterSet{{Name: "gpu-0-counter-set", Counters: memory}},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName + "-devices"},
			Spec: resourceapi.ResourceSliceSpec{
				Driver:   "gpu.nvidia.com",
				NodeName: ptr.To(nodeName),
				Pool:     pool,
				Devices: []resourceapi.Device{{
					Name:             "gpu-0",
					ConsumesCounters: []resourceapi.DeviceCounterConsumption{{CounterSet: "gpu-0-counter-set", Counters: memory}},
				}},
			},
		},
	}
}

func allocateCounterBackedGPU(t *testing.T, features structured.Features) []resourceapi.AllocationResult {
	t.Helper()

	const className = "gpu.nvidia.com"
	allocator, err := structured.NewAllocator(
		context.Background(), features,
		structured.AllocatedState{
			AllocatedDevices:         sets.New[structured.DeviceID](),
			AllocatedSharedDeviceIDs: sets.New[structured.SharedDeviceID](),
			AggregatedCapacity:       structured.NewConsumedCapacityCollection(),
		},
		&singleDeviceClassLister{class: &resourceapi.DeviceClass{ObjectMeta: metav1.ObjectMeta{Name: className}}},
		counterBackedGPUSlices("node0"),
		cel.NewCache(10, cel.Features{}),
	)
	require.NoError(t, err)

	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-0", Namespace: "default"},
		Spec: resourceapi.ResourceClaimSpec{Devices: resourceapi.DeviceClaim{
			Requests: []resourceapi.DeviceRequest{{
				Name: "gpu",
				Exactly: &resourceapi.ExactDeviceRequest{
					DeviceClassName: className,
					AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
					Count:           1,
				},
			}},
		}},
	}
	results, err := allocator.Allocate(context.Background(),
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node0"}}, []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	return results
}

// TestAllocatorFeatures_FromSlices covers
// https://github.com/kai-scheduler/KAI-Scheduler/issues/2631: a cluster that
// publishes counter-backed devices has DRAPartitionableDevices on in the API
// server, so the scheduler must allocate them without --feature-gates.
func TestAllocatorFeatures_FromSlices(t *testing.T) {
	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() { featuregates.SetDynamicResourcesEnabledForTest(false) })

	gateFeatures := allocatorFeatures(k8s_utils.GetK8sFeatures())
	require.False(t, gateFeatures.PartitionableDevices, "vendored default is off")
	assert.Empty(t, allocateCounterBackedGPU(t, gateFeatures))

	enableAll := sliceFeatureEnable{partitionableDevices: true, consumableCapacity: true}
	features := sliceAllocatorFeatures(gateFeatures, enableAll, counterBackedGPUSlices("node0"))
	assert.True(t, features.PartitionableDevices)
	assert.False(t, features.ConsumableCapacity, "no slice uses consumable capacity")
	require.Len(t, allocateCounterBackedGPU(t, features), 1)

	disabled := sliceFeatureEnable{partitionableDevices: false, consumableCapacity: true}
	features = sliceAllocatorFeatures(gateFeatures, disabled, counterBackedGPUSlices("node0"))
	assert.False(t, features.PartitionableDevices, "an explicit gate wins over the slices")
	assert.Empty(t, allocateCounterBackedGPU(t, features))
}

func sliceWithDevices(devices ...resourceapi.Device) *resourceapi.ResourceSlice {
	return &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "slice"},
		Spec: resourceapi.ResourceSliceSpec{
			Driver:   "gpu.nvidia.com",
			NodeName: ptr.To("node0"),
			Pool:     resourceapi.ResourcePool{Name: "node0", ResourceSliceCount: 1},
			Devices:  devices,
		},
	}
}

func TestSliceAllocatorFeatures(t *testing.T) {
	plainCapacity := map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
		"memory": {Value: resource.MustParse("48Gi")},
	}
	policyCapacity := map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
		"memory": {
			Value:         resource.MustParse("48Gi"),
			RequestPolicy: &resourceapi.CapacityRequestPolicy{Default: ptr.To(resource.MustParse("1Gi"))},
		},
	}
	perDeviceNode := sliceWithDevices(resourceapi.Device{Name: "gpu-0", NodeName: ptr.To("node0")})
	perDeviceNode.Spec.NodeName = nil
	perDeviceNode.Spec.PerDeviceNodeSelection = ptr.To(true)

	enableAll := sliceFeatureEnable{partitionableDevices: true, consumableCapacity: true}
	tests := []struct {
		name              string
		gates             structured.Features
		enable            sliceFeatureEnable
		slices            []*resourceapi.ResourceSlice
		wantPartitionable bool
		wantConsumable    bool
	}{
		{name: "no slices", enable: enableAll},
		{
			name:   "plain device",
			enable: enableAll,
			slices: []*resourceapi.ResourceSlice{sliceWithDevices(resourceapi.Device{Name: "gpu-0", Capacity: plainCapacity})},
		},
		{
			name:              "shared counters and consuming device",
			enable:            enableAll,
			slices:            counterBackedGPUSlices("node0"),
			wantPartitionable: true,
		},
		{
			name:              "device consumes counters",
			enable:            enableAll,
			slices:            counterBackedGPUSlices("node0")[1:],
			wantPartitionable: true,
		},
		{
			name:              "per-device node selection",
			enable:            enableAll,
			slices:            []*resourceapi.ResourceSlice{perDeviceNode},
			wantPartitionable: true,
		},
		{
			name:              "device on all nodes",
			enable:            enableAll,
			slices:            []*resourceapi.ResourceSlice{sliceWithDevices(resourceapi.Device{Name: "gpu-0", AllNodes: ptr.To(true)})},
			wantPartitionable: true,
		},
		{
			name:           "device allows multiple allocations",
			enable:         enableAll,
			slices:         []*resourceapi.ResourceSlice{sliceWithDevices(resourceapi.Device{Name: "gpu-0", AllowMultipleAllocations: ptr.To(true)})},
			wantConsumable: true,
		},
		{
			name:           "capacity with request policy",
			enable:         enableAll,
			slices:         []*resourceapi.ResourceSlice{sliceWithDevices(resourceapi.Device{Name: "gpu-0", Capacity: policyCapacity})},
			wantConsumable: true,
		},
		{
			name:   "explicit gates keep both off",
			enable: sliceFeatureEnable{},
			slices: []*resourceapi.ResourceSlice{
				counterBackedGPUSlices("node0")[0],
				sliceWithDevices(resourceapi.Device{Name: "gpu-1", AllowMultipleAllocations: ptr.To(true)}),
			},
		},
		{
			name:              "gates already on stay on without slices",
			gates:             structured.Features{PartitionableDevices: true, ConsumableCapacity: true},
			enable:            sliceFeatureEnable{},
			wantPartitionable: true,
			wantConsumable:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sliceAllocatorFeatures(tt.gates, tt.enable, tt.slices)
			assert.Equal(t, tt.wantPartitionable, got.PartitionableDevices, "PartitionableDevices")
			assert.Equal(t, tt.wantConsumable, got.ConsumableCapacity, "ConsumableCapacity")
		})
	}
}

func TestSliceFeatureEnableFromGates(t *testing.T) {
	gates := feature.DefaultMutableFeatureGate.DeepCopy()
	assert.Equal(t, sliceFeatureEnable{partitionableDevices: true, consumableCapacity: true},
		sliceFeatureEnableFromGates(gates))

	require.NoError(t, gates.Set("DRAPartitionableDevices=false"))
	assert.Equal(t, sliceFeatureEnable{partitionableDevices: false, consumableCapacity: true},
		sliceFeatureEnableFromGates(gates))
}

func TestSetAllocatorFeatures_CelCachePerConsumableCapacity(t *testing.T) {
	drap := &draPlugin{celCaches: map[bool]*cel.Cache{}, maxCelCacheEntries: 10}

	drap.setAllocatorFeatures(structured.Features{})
	off := drap.celCache
	require.NotNil(t, off)

	drap.setAllocatorFeatures(structured.Features{ConsumableCapacity: true})
	on := drap.celCache
	assert.NotSame(t, off, on)
	assert.True(t, drap.allocatorFeatures.ConsumableCapacity)

	drap.setAllocatorFeatures(structured.Features{})
	assert.Same(t, off, drap.celCache, "the cache is reused when the setting comes back")
}
