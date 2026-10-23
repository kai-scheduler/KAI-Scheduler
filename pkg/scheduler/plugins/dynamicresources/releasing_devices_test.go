// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
)

func TestReleasingDevicesApply(t *testing.T) {
	releasingDevice := structured.MakeDeviceID("driver", "node0", "0")
	otherDevice := structured.MakeDeviceID("driver", "node0", "1")
	allocation := &resourceapi.AllocationResult{
		Devices: resourceapi.DeviceAllocationResult{
			Results: []resourceapi.DeviceRequestAllocationResult{{Driver: "driver", Pool: "node0", Device: "0"}},
		},
	}
	stateOf := func(r *releasingDevices, mode api.PredicateMode, allocated ...structured.DeviceID) sets.Set[structured.DeviceID] {
		state := &structured.AllocatedState{AllocatedDevices: sets.New(allocated...)}
		r.apply(state, mode)
		return state.AllocatedDevices
	}
	newReleasing := func() *releasingDevices {
		return &releasingDevices{
			devices: sets.New(releasingDevice),
			holders: map[structured.DeviceID]int{},
		}
	}

	t.Run("strict keeps releasing devices allocated", func(t *testing.T) {
		assert.Equal(t, sets.New(releasingDevice, otherDevice),
			stateOf(newReleasing(), api.StrictPredicate, releasingDevice, otherDevice))
	})

	t.Run("pipeline frees releasing devices", func(t *testing.T) {
		assert.Equal(t, sets.New(otherDevice),
			stateOf(newReleasing(), api.PipelinePredicate, releasingDevice, otherDevice))
	})

	t.Run("pipeline keeps a releasing device held by a claim of this session", func(t *testing.T) {
		r := newReleasing()
		r.hold(allocation)
		assert.Equal(t, sets.New(releasingDevice), stateOf(r, api.PipelinePredicate, releasingDevice))

		r.release(allocation)
		assert.Empty(t, stateOf(r, api.PipelinePredicate, releasingDevice))
	})

	t.Run("admin access allocations do not change holders", func(t *testing.T) {
		adminAllocation := &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{
				Results: []resourceapi.DeviceRequestAllocationResult{
					{Driver: "driver", Pool: "node0", Device: "0", AdminAccess: ptr.To(true)},
				},
			},
		}
		r := newReleasing()
		r.hold(allocation)
		r.release(adminAllocation)
		assert.Equal(t, sets.New(releasingDevice), stateOf(r, api.PipelinePredicate, releasingDevice))

		r.release(allocation)
		r.hold(adminAllocation)
		assert.Empty(t, stateOf(r, api.PipelinePredicate, releasingDevice))
	})

	t.Run("strict restores a releasing device dropped by the tracker on unpipeline", func(t *testing.T) {
		assert.Equal(t, sets.New(releasingDevice), stateOf(newReleasing(), api.StrictPredicate))
	})

	t.Run("nil is a no-op", func(t *testing.T) {
		var r *releasingDevices
		r.hold(allocation)
		r.release(allocation)
		assert.Equal(t, sets.New(releasingDevice), stateOf(r, api.PipelinePredicate, releasingDevice))
	})
}
