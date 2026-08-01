// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// TestQueueLabels exercises the three queue identification labels emitted on
// scheduler queue metrics. The interesting case is a queue whose Spec.DisplayName
// differs from metadata.name: queue_name carries the legacy display-name-fallback
// value, while queue_metadata_name and queue_display_name disambiguate it.
func TestQueueLabels(t *testing.T) {
	cases := []struct {
		name              string
		queueName         string
		queueMetadataName string
		queueDisplayName  string
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

func TestScenarioSearchMetricWrappersUseExpectedLabels(t *testing.T) {
	jobsLabels := map[string]string{
		"action":         "test-action-jobs",
		"result":         "solved",
		"reduced_budget": "true",
	}
	actionExhaustedLabels := map[string]string{
		"action": "test-action-exhausted",
	}
	scenariosLabels := map[string]string{
		"action":    "test-action-scenarios",
		"generator": "test-generator-labels",
		"state":     "emitted",
	}
	jobsBefore := counterValueOrZero(t, "scenario_search_jobs_total", jobsLabels)
	actionExhaustedBefore := counterValueOrZero(
		t, "scenario_search_action_budget_exhausted_total", actionExhaustedLabels,
	)
	scenariosBefore := counterValueOrZero(t, "scenario_search_scenarios_total", scenariosLabels)

	IncScenarioSearchJobs("test-action-jobs", "solved", true)
	IncScenarioSearchActionBudgetExhausted("test-action-exhausted")
	IncScenarioSearchScenario("test-action-scenarios", "test-generator-labels", "emitted")

	require.Equal(t, jobsBefore+1, counterValue(t, "scenario_search_jobs_total", jobsLabels))
	require.Equal(t, actionExhaustedBefore+1, counterValue(
		t, "scenario_search_action_budget_exhausted_total", actionExhaustedLabels,
	))
	require.Equal(t, scenariosBefore+1, counterValue(t, "scenario_search_scenarios_total", scenariosLabels))
}

func TestScenarioSearchDurationMetricObservesSeconds(t *testing.T) {
	labels := map[string]string{
		"action":    "test-action-duration",
		"generator": "test-generator-duration",
		"result":    "generators_exhausted",
	}
	countBefore, sumBefore := histogramSnapshot(t, "scenario_search_duration_seconds", labels)

	ObserveScenarioSearchDuration(
		"test-action-duration", "test-generator-duration", "generators_exhausted", 2500*time.Millisecond,
	)

	countAfter, sumAfter := histogramSnapshot(t, "scenario_search_duration_seconds", labels)

	require.Equal(t, countBefore+1, countAfter)
	require.InEpsilon(t, sumBefore+2.5, sumAfter, 0.000001)
}

func TestScenarioSearchConfiguredBudgetMetricsAcceptUnlimitedZero(t *testing.T) {
	SetScenarioSearchActionBudget("test-action-zero-budget", 0)
	SetScenarioSearchJobBudget(0)
	SetScenarioSearchGeneratorBudget("test-generator-zero-budget", 0)

	require.Equal(t, 0.0, gaugeValue(t, "scenario_search_action_budget_configured_seconds", map[string]string{
		"action": "test-action-zero-budget",
	}))
	require.Equal(t, 0.0, gaugeValue(t, "scenario_search_job_budget_configured_seconds", nil))
	require.Equal(t, 0.0, gaugeValue(t, "scenario_search_generator_budget_configured_seconds", map[string]string{
		"generator": "test-generator-zero-budget",
	}))
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

func counterValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()

	metric := findMetric(t, metricName, labels)
	require.NotNil(t, metric.GetCounter())
	return metric.GetCounter().GetValue()
}

func counterValueOrZero(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()

	metric := findMetricOrNil(t, metricName, labels)
	if metric == nil || metric.GetCounter() == nil {
		return 0
	}
	return metric.GetCounter().GetValue()
}

func gaugeValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()

	metric := findMetric(t, metricName, labels)
	require.NotNil(t, metric.GetGauge())
	return metric.GetGauge().GetValue()
}

func histogramSnapshot(t *testing.T, metricName string, labels map[string]string) (uint64, float64) {
	t.Helper()

	metric := findMetricOrNil(t, metricName, labels)
	if metric == nil || metric.GetHistogram() == nil {
		return 0, 0
	}
	histogram := metric.GetHistogram()
	return histogram.GetSampleCount(), histogram.GetSampleSum()
}

func findMetric(t *testing.T, metricName string, labels map[string]string) *dto.Metric {
	t.Helper()

	family := findMetricFamily(t, metricName)
	for _, metric := range family.GetMetric() {
		if metricHasLabels(metric, labels) {
			return metric
		}
	}
	t.Fatalf("metric %q with labels %v not found", metricName, labels)
	return nil
}

func findMetricOrNil(t *testing.T, metricName string, labels map[string]string) *dto.Metric {
	t.Helper()

	family := findMetricFamilyOrNil(t, metricName)
	if family == nil {
		return nil
	}
	for _, metric := range family.GetMetric() {
		if metricHasLabels(metric, labels) {
			return metric
		}
	}
	return nil
}

func findMetricFamily(t *testing.T, metricName string) *dto.MetricFamily {
	t.Helper()

	if family := findMetricFamilyOrNil(t, metricName); family != nil {
		return family
	}
	t.Fatalf("metric family %q not found", metricName)
	return nil
}

func findMetricFamilyOrNil(t *testing.T, metricName string) *dto.MetricFamily {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == metricName {
			return family
		}
	}
	return nil
}

func metricHasLabels(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}
	for _, label := range metric.GetLabel() {
		expectedValue, found := labels[label.GetName()]
		if !found || expectedValue != label.GetValue() {
			return false
		}
	}
	return true
}
