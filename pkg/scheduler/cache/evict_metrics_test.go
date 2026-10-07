// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

type fakeEvictor struct {
	err error
}

func (f *fakeEvictor) Evict(*v1.Pod, string) error {
	return f.err
}

func TestEvictCountsOnlySuccessfulEvictions(t *testing.T) {
	cases := []struct {
		name     string
		evictErr error
		expected float64
	}{
		{name: "successful eviction is counted", evictErr: nil, expected: 1},
		{name: "failed eviction is not counted", evictErr: errors.New("eviction rejected"), expected: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := &SchedulerCache{Evictor: &fakeEvictor{err: tc.evictErr}}
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: "ns"}}
			podGroup := &enginev2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns"}}

			before := preemptionEvictionsValue(t)
			sc.evict(pod, podGroup, eviction_info.EvictionMetadata{Action: "preempt"}, "")
			sc.WaitForWorkers(make(chan struct{}))

			require.Equal(t, tc.expected, preemptionEvictionsValue(t)-before)
		})
	}
}

func preemptionEvictionsValue(t *testing.T) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == "total_preemption_evictions" {
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}
	t.Fatal("metric family total_preemption_evictions not found")
	return 0
}
