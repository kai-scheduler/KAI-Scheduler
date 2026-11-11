// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package metrics holds the Prometheus metrics of the binder. They are registered on the
// controller-runtime registry, so they are served on the binder's metrics endpoint.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/kai-scheduler/api/constants"
)

const (
	ResultSucceeded = "succeeded"
	ResultFailed    = "failed"

	// Reasons for binder_bind_requests_total. Keep this set small and fixed: the reason is a label.
	ReasonBound            = "bound"
	ReasonAlreadyBound     = "already_bound"
	ReasonPodLookupFailed  = "pod_lookup_failed"
	ReasonNodeLookupFailed = "node_lookup_failed"
	ReasonBindFailed       = "bind_failed"
	ReasonInvalidCrd       = "invalid_crd"
	ReasonPanic            = "panic"

	// Outcomes for binder_reservation_pod_allocation_duration_seconds.
	ReservationAllocated = "allocated"
	ReservationTimeout   = "timeout"
	ReservationError     = "error"
)

var (
	bindRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "binder_bind_requests_total",
			Help:      "Total number of BindRequest reconcile attempts, by result and reason",
		}, []string{"result", "reason"})

	bindRetriesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "binder_bind_retries_total",
			Help:      "Total number of failed BindRequest attempts that were requeued for another attempt",
		})

	bindRequestToBoundSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "binder_bind_request_to_bound_duration_seconds",
			Help:      "Time from BindRequest creation until its pod is bound",
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 16), // 5ms to ~164s
		})

	reservationPodAllocationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "binder_reservation_pod_allocation_duration_seconds",
			Help: "Time the binder waits for a GPU-sharing reservation pod to report its GPU index, " +
				"by outcome (allocated, timeout, error)",
			Buckets: prometheus.ExponentialBuckets(0.05, 2, 14), // 50ms to ~410s
		}, []string{"result"})
)

func init() {
	metrics.Registry.MustRegister(
		bindRequestsTotal,
		bindRetriesTotal,
		bindRequestToBoundSeconds,
		reservationPodAllocationSeconds,
	)
}

// RecordBindRequest records the outcome of one BindRequest reconcile attempt.
func RecordBindRequest(result, reason string) {
	bindRequestsTotal.WithLabelValues(result, reason).Inc()
}

// IncBindRetries records a failed BindRequest attempt that was requeued for another attempt.
func IncBindRetries() {
	bindRetriesTotal.Inc()
}

// ObserveBindRequestToBound records the time from BindRequest creation until its pod was bound.
func ObserveBindRequestToBound(duration time.Duration) {
	bindRequestToBoundSeconds.Observe(duration.Seconds())
}

// ObserveReservationPodAllocation records how long a GPU reservation pod took to report its GPU
// index, or how long the binder waited before it gave up.
func ObserveReservationPodAllocation(result string, duration time.Duration) {
	reservationPodAllocationSeconds.WithLabelValues(result).Observe(duration.Seconds())
}
