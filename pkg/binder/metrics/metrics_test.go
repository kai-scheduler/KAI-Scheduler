// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestBinderMetricsAreServedOnTheControllerRuntimeRegistry(t *testing.T) {
	RecordBindRequest(ResultFailed, ReasonBindFailed)
	IncBindRetries()
	ObserveBindRequestToBound(time.Second)
	ObserveReservationPodAllocation(ReservationTimeout, time.Minute)

	families, err := ctrlmetrics.Registry.Gather()
	require.NoError(t, err)
	names := map[string]bool{}
	for _, family := range families {
		names[family.GetName()] = true
	}

	for _, name := range []string{
		"kai_binder_bind_requests_total",
		"kai_binder_bind_retries_total",
		"kai_binder_bind_request_to_bound_duration_seconds",
		"kai_binder_reservation_pod_allocation_duration_seconds",
	} {
		assert.True(t, names[name], "metric %s is not registered", name)
	}
}

func TestBinderMetricsRecordValues(t *testing.T) {
	requestsBefore := BindRequestsCount(ResultSucceeded, ReasonAlreadyBound)
	retriesBefore := BindRetriesCount()
	toBoundBefore := BindRequestToBoundSampleCount()
	allocatedBefore := ReservationPodAllocationSampleCount(ReservationAllocated)
	errorBefore := ReservationPodAllocationSampleCount(ReservationError)

	RecordBindRequest(ResultSucceeded, ReasonAlreadyBound)
	RecordBindRequest(ResultSucceeded, ReasonAlreadyBound)
	IncBindRetries()
	ObserveBindRequestToBound(30 * time.Millisecond)
	ObserveReservationPodAllocation(ReservationAllocated, 2*time.Second)

	assert.Equal(t, requestsBefore+2, BindRequestsCount(ResultSucceeded, ReasonAlreadyBound))
	assert.Equal(t, retriesBefore+1, BindRetriesCount())
	assert.Equal(t, toBoundBefore+1, BindRequestToBoundSampleCount())
	assert.Equal(t, allocatedBefore+1, ReservationPodAllocationSampleCount(ReservationAllocated))
	assert.Equal(t, errorBefore, ReservationPodAllocationSampleCount(ReservationError))
}
