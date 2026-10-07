// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

// The metrics package cannot import ActionType, so its action names are checked here.
func TestEvictionCountersMatchActionTypes(t *testing.T) {
	counters := map[ActionType]string{
		Preempt:       "total_preemption_evictions",
		Reclaim:       "total_reclaim_evictions",
		Consolidation: "total_consolidation_evictions",
	}

	for action, metricName := range counters {
		before := counterValue(t, metricName)
		metrics.IncEvictedPodsByAction(string(action))
		require.Equal(t, before+1, counterValue(t, metricName), action)
	}
}

func counterValue(t *testing.T, metricName string) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == metricName {
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}
	t.Fatalf("metric family %s not found", metricName)
	return 0
}
