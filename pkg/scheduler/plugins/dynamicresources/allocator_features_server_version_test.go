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
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
)

// setServerVersion runs SetDRAFeatureGate against a fake API server of version
// 1.<minor> that serves resource.k8s.io/v1, so the server version takes the same
// parsing path as in production. The DRA availability flag is left as it was.
// An empty minor resets the version to unknown.
func setServerVersion(minor string) {
	draEnabled := featuregates.DynamicResourcesEnabled()
	defer featuregates.SetDynamicResourcesEnabledForTest(draEnabled)

	client := fake.NewClientset()
	client.Resources = []*metav1.APIResourceList{{GroupVersion: "resource.k8s.io/v1"}}
	discovery := client.Discovery().(*fakediscovery.FakeDiscovery)
	discovery.FakedServerVersion = &version.Info{Major: "1", Minor: minor}
	if err := featuregates.SetDRAFeatureGate(discovery); err != nil {
		panic(err)
	}
}

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

// TestAllocatorFeatures_FollowServerVersion covers
// https://github.com/kai-scheduler/KAI-Scheduler/issues/2631: on a 1.36+ API
// server DRAPartitionableDevices is on by default, so a counter-backed GPU must
// be allocatable without passing --feature-gates to the scheduler.
func TestAllocatorFeatures_FollowServerVersion(t *testing.T) {
	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
		setServerVersion("")
	})

	t.Run("1.35 server keeps partitionable devices off", func(t *testing.T) {
		setServerVersion("35")
		features := allocatorFeatures(k8s_utils.GetK8sFeatures())
		assert.False(t, features.PartitionableDevices)
		assert.Empty(t, allocateCounterBackedGPU(t, features))
	})

	t.Run("1.37 server allocates the counter-backed GPU", func(t *testing.T) {
		setServerVersion("37")
		features := allocatorFeatures(k8s_utils.GetK8sFeatures())
		assert.True(t, features.PartitionableDevices)
		assert.True(t, features.ConsumableCapacity)
		require.Len(t, allocateCounterBackedGPU(t, features), 1)
	})
}
