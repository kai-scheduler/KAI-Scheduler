// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package timeaware

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiprometheus "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/prometheus"
	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/prometheus/common/model"
)

// timeAwareConfig holds configuration for time-aware fairness tests
type timeAwareConfig struct {
	PrometheusEnabled      bool
	ServiceMonitorInterval string
	ScrapeTimeout          string
	WindowSize             time.Duration
	HalfLifePeriod         time.Duration
	FetchInterval          time.Duration
	KValue                 float64
}

// defaultTimeAwareConfig returns a timeAwareConfig suitable for fast e2e testing
func defaultTimeAwareConfig() timeAwareConfig {
	return timeAwareConfig{
		PrometheusEnabled:      true,
		ServiceMonitorInterval: "5s",
		ScrapeTimeout:          "4s",
		WindowSize:             30 * time.Second,
		HalfLifePeriod:         15 * time.Second,
		FetchInterval:          5 * time.Second,
		KValue:                 10000.0, // Extreme fairness aggressiveness to ensure reclaim happens
	}
}

// configureTimeAwareFairness configures KAI for time-aware fairness testing.
func configureTimeAwareFairness(ctx context.Context, testCtx *testcontext.TestContext, shardName string, config timeAwareConfig) error {
	// Step 1: Enable prometheus in KAI config
	err := configurations.PatchKAIConfig(ctx, testCtx, func(kaiConfig *kaiv1.Config) {
		if kaiConfig.Spec.Prometheus == nil {
			kaiConfig.Spec.Prometheus = &kaiprometheus.Prometheus{}
		}
		kaiConfig.Spec.Prometheus.Enabled = ptr.To(config.PrometheusEnabled)
		if kaiConfig.Spec.Prometheus.ServiceMonitor == nil {
			kaiConfig.Spec.Prometheus.ServiceMonitor = &kaiprometheus.ServiceMonitor{}
		}
		kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(true)
		kaiConfig.Spec.Prometheus.ServiceMonitor.Interval = ptr.To(config.ServiceMonitorInterval)
		kaiConfig.Spec.Prometheus.ServiceMonitor.ScrapeTimeout = ptr.To(config.ScrapeTimeout)
	})
	if err != nil {
		return err
	}

	// Step 2: Configure shard with usageDBConfig
	return configurations.PatchSchedulingShard(ctx, testCtx, shardName, func(shard *kaiv1.SchedulingShard) {
		windowType := usagedbapi.SlidingWindow
		shard.Spec.UsageDBConfig = &usagedbapi.UsageDBConfig{
			ClientType: "prometheus",
			UsageParams: &usagedbapi.UsageParams{
				WindowSize:     monitoringv1.DurationPointer(model.Duration(config.WindowSize).String()),
				HalfLifePeriod: &metav1.Duration{Duration: config.HalfLifePeriod},
				FetchInterval:  &metav1.Duration{Duration: config.FetchInterval},
				WindowType:     &windowType,
			},
		}
		shard.Spec.KValue = ptr.To(config.KValue)
	})
}
