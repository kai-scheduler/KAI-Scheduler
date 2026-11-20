// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// The readers below return the current value of a binder metric. They are meant for tests that
// check how much a metric changed.

// BindRequestsCount returns the value of binder_bind_requests_total for the given labels.
func BindRequestsCount(result, reason string) float64 {
	return counterValue(bindRequestsTotal.WithLabelValues(result, reason))
}

// BindRetriesCount returns the value of binder_bind_retries_total.
func BindRetriesCount() float64 {
	return counterValue(bindRetriesTotal)
}

// BindRequestToBoundSampleCount returns the number of observations of
// binder_bind_request_to_bound_duration_seconds.
func BindRequestToBoundSampleCount() uint64 {
	return histogramSampleCount(bindRequestToBoundSeconds)
}

// ReservationPodAllocationSampleCount returns the number of observations of
// binder_reservation_pod_allocation_duration_seconds for the given result.
func ReservationPodAllocationSampleCount(result string) uint64 {
	return histogramSampleCount(reservationPodAllocationSeconds.WithLabelValues(result))
}

func counterValue(counter prometheus.Counter) float64 {
	m := &dto.Metric{}
	if err := counter.Write(m); err != nil {
		return 0
	}
	return m.GetCounter().GetValue()
}

func histogramSampleCount(observer prometheus.Observer) uint64 {
	histogram, ok := observer.(prometheus.Histogram)
	if !ok {
		return 0
	}
	m := &dto.Metric{}
	if err := histogram.Write(m); err != nil {
		return 0
	}
	return m.GetHistogram().GetSampleCount()
}
