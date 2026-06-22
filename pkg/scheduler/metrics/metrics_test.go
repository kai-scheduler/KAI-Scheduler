// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestQueueLabels exercises the three queue identification labels emitted on
// scheduler queue metrics. The interesting case is a queue whose Spec.DisplayName
// differs from metadata.name: queue_name carries the legacy display-name-fallback
// value, while queue_metadata_name and queue_display_name disambiguate it.
func TestQueueLabels(t *testing.T) {
	cases := []struct {
		name              string
		queueName         string // value passed as queue_name (display-name fallback)
		queueMetadataName string // value passed as queue_metadata_name
		queueDisplayName  string // value passed as queue_display_name
	}{
		{
			name:              "displayName set and different from metadata.name",
			queueName:         "Research Team A",
			queueMetadataName: "research-team-a",
			queueDisplayName:  "Research Team A",
		},
		{
			name:              "displayName unset falls back to metadata.name",
			queueName:         "research-team-a",
			queueMetadataName: "research-team-a",
			queueDisplayName:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			UpdateQueueFairShare(tc.queueName, tc.queueMetadataName, tc.queueDisplayName, 1.5, 2.5, 3)
			UpdateQueueUsage(tc.queueName, tc.queueMetadataName, tc.queueDisplayName, 0.5, 1.0, 2)

			labels := prometheus.Labels{
				"queue_name":          tc.queueName,
				"queue_metadata_name": tc.queueMetadataName,
				"queue_display_name":  tc.queueDisplayName,
			}

			assertGauge(t, queueFairShareCPU, labels, 1.5)
			assertGauge(t, queueFairShareMemory, labels, 2.5)
			assertGauge(t, queueFairShareGPU, labels, 3)
			assertGauge(t, queueCPUUsage, labels, 0.5)
			assertGauge(t, queueMemoryUsage, labels, 1.0)
			assertGauge(t, queueGPUUsage, labels, 2)

			ResetQueueFairShare()
			ResetQueueUsage()
		})
	}
}

func assertGauge(t *testing.T, gauge *prometheus.GaugeVec, labels prometheus.Labels, expected float64) {
	t.Helper()
	g, err := gauge.GetMetricWith(labels)
	if err != nil {
		t.Fatalf("GetMetricWith(%v) failed: %v", labels, err)
	}
	if got := testutil.ToFloat64(g); got != expected {
		t.Errorf("metric value for labels %v: got %v, want %v", labels, got, expected)
	}
}
