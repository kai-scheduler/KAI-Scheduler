// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	kaiprometheus "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/prometheus"
)

type Prometheus struct {
	namespace        string
	lastDesiredState []client.Object
	client           client.Client
	monitoringCtx    context.Context
	monitoringCancel context.CancelFunc
}
type promethuesResourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error)

func (p *Prometheus) DesiredState(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	p.namespace = kaiConfig.Spec.Namespace
	p.client = runtimeClient.(client.Client)

	var objects []client.Object
	if !prometheusExplicitlyEnabled(kaiConfig) {
		// Handle graceful deprecation of existing Prometheus instance
		prometheus, err := deprecatePrometheusForKAIConfig(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}

		if len(prometheus) == 0 {
			return nil, nil
		}

		objects = append(objects, prometheus...)
	}

	for _, resourceFunc := range []promethuesResourceForKAIConfig{
		prometheusForKAIConfig,
		prometheusServiceAccountForKAIConfig,
		serviceMonitorsForKAIConfig,
		usagePrometheusServiceForKAIConfig,
	} {
		obj, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj...)
	}

	p.lastDesiredState = objects
	return objects, nil

}

func (b *Prometheus) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	if len(b.lastDesiredState) == 0 {
		return true, nil
	}
	return common.AllObjectsExists(ctx, readerClient, b.lastDesiredState)
}

func (b *Prometheus) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	if len(b.lastDesiredState) == 0 {
		return true, nil
	}

	var prometheus *monitoringv1.Prometheus = nil
	for _, obj := range b.lastDesiredState {
		var ok bool
		if prometheus, ok = obj.(*monitoringv1.Prometheus); ok {
			break
		}
	}
	if prometheus == nil {
		return true, nil
	}

	err := readerClient.Get(ctx, client.ObjectKeyFromObject(prometheus), prometheus)
	if err != nil {
		return false, err
	}

	if len(prometheus.Status.Conditions) > 0 {
		for _, condition := range prometheus.Status.Conditions {
			if condition.Type == monitoringv1.ConditionType("Available") {
				return condition.Status == monitoringv1.ConditionTrue, nil
			}
		}
	}
	return false, nil
}

func (b *Prometheus) Name() string {
	return "KAI-prometheus"
}

func (p *Prometheus) HasMissingDependencies(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) (string, error) {
	if !prometheusExplicitlyEnabled(kaiConfig) {
		return "", nil
	}
	if kaiConfig.Spec.Prometheus.ExternalPrometheusUrl != nil && *kaiConfig.Spec.Prometheus.ExternalPrometheusUrl != "" {
		return "", nil
	}

	hasPrometheusOperator, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeReader, "prometheus")
	if err != nil {
		return "", err
	}

	if !hasPrometheusOperator {
		return "prometheus operator", nil
	}

	return "", nil
}

func (p *Prometheus) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	hasExternalPrometheus := kaiConfig.Spec.Prometheus != nil &&
		kaiConfig.Spec.Prometheus.ExternalPrometheusUrl != nil &&
		*kaiConfig.Spec.Prometheus.ExternalPrometheusUrl != ""

	if !hasExternalPrometheus {
		if p.monitoringCancel != nil {
			p.monitoringCancel()
			p.monitoringCtx = nil
			p.monitoringCancel = nil
		}
		return nil
	}

	if p.monitoringCtx != nil && p.monitoringCtx.Err() == nil {
		return nil
	}

	p.monitoringCtx, p.monitoringCancel = context.WithCancel(ctx)
	statusUpdater := p.createStatusUpdaterFunction(kaiConfig)
	StartMonitoring(p.monitoringCtx, kaiConfig.Spec.Prometheus, statusUpdater)

	return nil
}

// createStatusUpdaterFunction creates a function that updates the KAI Config status
func (p *Prometheus) createStatusUpdaterFunction(kaiConfig *kaiv1.Config) func(ctx context.Context, condition metav1.Condition) error {
	return func(ctx context.Context, condition metav1.Condition) error {
		currentConfig := &kaiv1.Config{}
		if err := p.client.Get(ctx, client.ObjectKey{
			Name:      kaiConfig.Name,
			Namespace: kaiConfig.Namespace,
		}, currentConfig); err != nil {
			return fmt.Errorf("failed to get current config: %w", err)
		}

		condition.ObservedGeneration = currentConfig.Generation

		configToUpdate := currentConfig.DeepCopy()

		// Find and update the Prometheus connectivity condition
		found := false
		for index, existingCondition := range configToUpdate.Status.Conditions {
			if existingCondition.Type == condition.Type {
				if existingCondition.ObservedGeneration == condition.ObservedGeneration &&
					existingCondition.Status == condition.Status &&
					existingCondition.Message == condition.Message {
					return nil // No change needed
				}
				found = true
				configToUpdate.Status.Conditions[index] = condition
				break
			}
		}

		if !found {
			configToUpdate.Status.Conditions = append(configToUpdate.Status.Conditions, condition)
		}

		// Update the status using the Prometheus struct's client
		return p.client.Status().Patch(ctx, configToUpdate, client.MergeFrom(currentConfig))
	}
}

// StartMonitoring starts a background goroutine to monitor external Prometheus connectivity
// and update the status periodically. The goroutine will stop when ctx is cancelled or when
// ExternalPrometheusUrl is set to nil or empty string.
func StartMonitoring(ctx context.Context, prometheusConfig *kaiprometheus.Prometheus, statusUpdater func(ctx context.Context, condition metav1.Condition) error) {
	if prometheusConfig == nil || prometheusConfig.ExternalPrometheusUrl == nil || *prometheusConfig.ExternalPrometheusUrl == "" {
		return
	}

	go func() {
		ticker := time.NewTicker(time.Duration(*prometheusConfig.ExternalPrometheusHealthProbe.Interval) * time.Second) // Check every 30 seconds
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Check if external Prometheus URL is still configured
				if prometheusConfig.ExternalPrometheusUrl == nil || *prometheusConfig.ExternalPrometheusUrl == "" {
					return
				}

				// Test connectivity
				err := pingExternalPrometheus(ctx, *prometheusConfig.ExternalPrometheusUrl, *prometheusConfig.ExternalPrometheusHealthProbe.Timeout, *prometheusConfig.ExternalPrometheusHealthProbe.MaxRetries)

				var condition metav1.Condition
				if err != nil {
					condition = metav1.Condition{
						Type:               string(kaiv1.ConditionTypeAvailable),
						Status:             metav1.ConditionFalse,
						Reason:             "prometheus_connection_failed",
						Message:            fmt.Sprintf("Failed to ping external Prometheus: %v", err),
						LastTransitionTime: metav1.Now(),
					}
				} else {
					condition = metav1.Condition{
						Type:               string(kaiv1.ConditionTypeAvailable),
						Status:             metav1.ConditionTrue,
						Reason:             "prometheus_connected",
						Message:            "External Prometheus connectivity verified",
						LastTransitionTime: metav1.Now(),
					}
				}

				// Update status
				if updateErr := statusUpdater(ctx, condition); updateErr != nil {
					logger := log.FromContext(ctx)
					logger.Error(updateErr, "Failed to update Prometheus connectivity status")
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// pingExternalPrometheus validates connectivity to an external Prometheus instance
func pingExternalPrometheus(ctx context.Context, prometheusURL string, timeout int, maxRetries int) error {
	logger := log.FromContext(ctx)

	if !strings.Contains(prometheusURL, "://") {
		prometheusURL = "http://" + prometheusURL
	}

	_, err := url.Parse(prometheusURL)
	if err != nil {
		return fmt.Errorf("invalid Prometheus URL: %w, prometheusURL: %s", err, prometheusURL)
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	statusURL := prometheusURL + "/api/v1/status/config"

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w, statusURL: %s", err, statusURL)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to connect to external Prometheus: %w, statusURL: %s", err, statusURL)
			if attempt < maxRetries {
				backoff := time.Duration(attempt) * time.Second
				logger.Info("Retrying Prometheus connection", "attempt", attempt, "maxRetries", maxRetries, "nextRetryInSeconds", backoff.Seconds())
				time.Sleep(backoff)
			}
			continue
		}

		// Check if we got a successful response
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			lastErr = fmt.Errorf("external Prometheus returned status code %d, statusURL: %s", resp.StatusCode, statusURL)
			if attempt < maxRetries {
				backoff := time.Duration(attempt) * time.Second
				logger.Info("Retrying Prometheus connection", "attempt", attempt, "maxRetries", maxRetries, "statusCode", resp.StatusCode, "nextRetryInSeconds", backoff.Seconds())
				time.Sleep(backoff)
			}
			continue
		}

		resp.Body.Close()
		return nil
	}

	return fmt.Errorf("failed to connect to external Prometheus after %d attempts: %w", maxRetries, lastErr)
}

func prometheusExplicitlyEnabled(kaiConfig *kaiv1.Config) bool {
	return kaiConfig.Spec.Prometheus != nil &&
		kaiConfig.Spec.Prometheus.Enabled != nil &&
		*kaiConfig.Spec.Prometheus.Enabled
}
