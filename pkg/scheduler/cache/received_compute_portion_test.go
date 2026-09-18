// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// The binder reads ComputePortion with strconv.ParseFloat and falls back to its
// own sizing when it is empty. Anything the scheduler writes has to survive that
// round trip, and a pod that never asked for compute has to leave it empty so an
// older binder - and the fallback - behave exactly as they did before.
func TestReceivedComputePortion(t *testing.T) {
	tests := []struct {
		name           string
		computePortion float64
		want           string
	}{
		{name: "not requested", computePortion: 0, want: ""},
		{name: "negative is treated as unrequested", computePortion: -0.5, want: ""},
		{name: "a quarter", computePortion: 0.25, want: "0.25"},
		{name: "an eighth", computePortion: 0.125, want: "0.125"},
		{name: "a whole device", computePortion: 1, want: "1"},
		// %.2f would round this to 0.33 and 0.01 respectively.
		{name: "a third", computePortion: 1.0 / 3.0, want: "0.3333333333333333"},
		{name: "a hundredth of a percent", computePortion: 0.0001, want: "0.0001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podInfo := &pod_info.PodInfo{
				AcceptedGpuRequirement: *resource_info.NewGpuResourceRequirementWithGpus(0.5, 0),
			}
			podInfo.AcceptedGpuRequirement.SetGpuComputePortion(tt.computePortion)

			got := receivedComputePortion(podInfo)
			assert.Equal(t, tt.want, got)
			if tt.want == "" {
				return
			}

			parsed, err := strconv.ParseFloat(got, 64)
			assert.NoError(t, err, "the binder parses this with strconv.ParseFloat")
			assert.Equal(t, tt.computePortion, parsed, "the granted share must survive serialization")
		})
	}
}

// ComputePortion has to round-trip through the CRD, and stay absent from the
// wire when unset so an old binder sees exactly the object it saw before.
func TestReceivedGPUComputePortionSerialization(t *testing.T) {
	requested, err := json.Marshal(&schedulingv1alpha2.ReceivedGPU{
		Count: 1, Portion: "0.50", ComputePortion: "0.125",
	})
	assert.NoError(t, err)
	assert.JSONEq(t, `{"count":1,"portion":"0.50","computePortion":"0.125"}`, string(requested))

	notRequested, err := json.Marshal(&schedulingv1alpha2.ReceivedGPU{Count: 1, Portion: "0.50"})
	assert.NoError(t, err)
	assert.JSONEq(t, `{"count":1,"portion":"0.50"}`, string(notRequested),
		"an unset compute portion must not appear on the wire")

	// A BindRequest written by an older scheduler has no computePortion at all.
	var fromOldScheduler schedulingv1alpha2.ReceivedGPU
	assert.NoError(t, json.Unmarshal([]byte(`{"count":1,"portion":"0.50"}`), &fromOldScheduler))
	assert.Equal(t, "", fromOldScheduler.ComputePortion)
	_, parseErr := strconv.ParseFloat(fromOldScheduler.ComputePortion, 64)
	assert.Error(t, parseErr, "the binder's fallback is keyed on this failing to parse")

	var fromNewScheduler schedulingv1alpha2.ReceivedGPU
	assert.NoError(t, json.Unmarshal(requested, &fromNewScheduler))
	assert.Equal(t, "0.125", fromNewScheduler.ComputePortion)

	// DeepCopy backs every cache read of the object.
	assert.Equal(t, &fromNewScheduler, fromNewScheduler.DeepCopy())
}
