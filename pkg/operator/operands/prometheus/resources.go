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

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/xhit/go-str2duration/v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	kaiprometheus "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/prometheus"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common"
	v1 "k8s.io/api/core/v1"
)

const (
	mainResourceName        = "prometheus"
	usagePrometheusService  = "usage-prometheus"
	defaultStorageSize      = "50Gi"
	deprecationTimestampKey = "kai/deprecation-timestamp"
	prometheusPort          = 9090
)

func prometheusForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	logger := log.FromContext(ctx)
	config := kaiConfig.Spec.Prometheus

	if !prometheusExplicitlyEnabled(kaiConfig) {
		return []client.Object{}, nil
	}

	if config.ExternalPrometheusUrl != nil && *config.ExternalPrometheusUrl != "" {
		logger.Info("External Prometheus URL provided, skipping Prometheus CR creation", "url", *config.ExternalPrometheusUrl)

		return createServiceMonitorsForExternalPrometheus(ctx, runtimeClient, kaiConfig)
	}

	// Check if Prometheus Operator is installed by looking for the Prometheus CRD
	// This is a simple check - in production you might want to check for the operator deployment
	hasPrometheusOperator, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeClient, "prometheus")
	if err != nil {
		logger.Error(err, "Failed to check for Prometheus Operator installation")
		return []client.Object{}, err
	}

	if !hasPrometheusOperator {
		logger.Info("Prometheus Operator not found - Prometheus CRD is not available")
		return []client.Object{}, nil
	}

	instanceName := getPrometheusInstanceName(config)
	prometheus, err := common.ObjectForKAIConfig(ctx, runtimeClient, &monitoringv1.Prometheus{}, instanceName, kaiConfig.Spec.Namespace)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Failed to check for existing Prometheus instance")
			return nil, err
		}
	}

	var ok bool
	prometheus, ok = prometheus.(*monitoringv1.Prometheus)
	if !ok {
		logger.Error(nil, "Failed to cast object to Prometheus type")
		return nil, fmt.Errorf("failed to cast object to Prometheus type")
	}

	prometheusSpec := monitoringv1.PrometheusSpec{
		CommonPrometheusFields: monitoringv1.CommonPrometheusFields{
			PodMetadata: &monitoringv1.EmbeddedObjectMetadata{
				Labels: map[string]string{
					"app": instanceName,
				},
			},
		},
	}

	prometheusSpec.Storage = getStorageSpecForPrometheus(config)

	if config.RetentionPeriod != nil {
		prometheusSpec.Retention = monitoringv1.Duration(*config.RetentionPeriod)
	}

	selectorLabelKey, selectorLabelValue := getAccountingSelectorLabels(config)

	if config.ServiceMonitor != nil && *config.ServiceMonitor.Enabled {
		prometheusSpec.ServiceMonitorSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				selectorLabelKey: selectorLabelValue,
			},
		}
		prometheusSpec.ServiceMonitorNamespaceSelector = &metav1.LabelSelector{}
	}

	prometheusSpec.RuleSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			selectorLabelKey: selectorLabelValue,
		},
	}
	prometheusSpec.RuleNamespaceSelector = &metav1.LabelSelector{}

	prometheusSpec.ServiceAccountName = instanceName

	// Remove deprecation timestamp annotation if it exists (prometheus is now enabled)
	annotations := prometheus.GetAnnotations()
	if annotations != nil {
		if _, exists := annotations[deprecationTimestampKey]; exists {
			delete(annotations, deprecationTimestampKey)
			prometheus.SetAnnotations(annotations)
			logger.Info("Removed deprecation timestamp annotation from Prometheus instance")
		}
	}

	prometheus.(*monitoringv1.Prometheus).Spec = prometheusSpec
	return []client.Object{prometheus}, nil
}

// deprecatePrometheusForKAIConfig handles graceful deletion of Prometheus instances
// when Prometheus is disabled. It adds a deprecation timestamp and only deletes
// after the retention period has passed.
func deprecatePrometheusForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	logger := log.FromContext(ctx)

	// Check if Prometheus CRD exists
	hasPrometheusOperator, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeClient, "prometheus")
	if err != nil {
		logger.Error(err, "Failed to check for Prometheus Operator installation")
		return []client.Object{}, err
	}

	if !hasPrometheusOperator {
		logger.V(1).Info("Prometheus CRD not available, nothing to deprecate")
		return []client.Object{}, nil
	}

	// Try to get existing Prometheus instance
	instanceName := getPrometheusInstanceName(kaiConfig.Spec.Prometheus)
	prometheusObj := &monitoringv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: kaiConfig.Spec.Namespace,
		},
	}

	err = runtimeClient.Get(ctx, client.ObjectKeyFromObject(prometheusObj), prometheusObj)
	if err != nil {
		if errors.IsNotFound(err) {
			return []client.Object{}, nil
		}
		logger.Error(err, "Failed to get Prometheus instance")
		return []client.Object{}, err
	}

	// Get current annotations
	annotations := prometheusObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Check if deprecation timestamp exists
	deprecationTimeStr, exists := annotations[deprecationTimestampKey]
	if !exists {
		// Add deprecation timestamp
		annotations[deprecationTimestampKey] = metav1.Now().Format(time.RFC3339)
		prometheusObj.SetAnnotations(annotations)
		logger.Info("Added deprecation timestamp to Prometheus instance", "timestamp", annotations[deprecationTimestampKey])
		return []client.Object{prometheusObj}, nil
	}

	// Parse the deprecation timestamp
	deprecationTime, err := time.Parse(time.RFC3339, deprecationTimeStr)
	if err != nil {
		logger.Error(err, "Failed to parse deprecation timestamp, re-setting it", "timestamp", deprecationTimeStr)
		annotations[deprecationTimestampKey] = metav1.Now().Format(time.RFC3339)
		prometheusObj.SetAnnotations(annotations)
		return []client.Object{prometheusObj}, nil
	}

	defaultRetentionPeriod := 30 * 24 * time.Hour
	// Use retention period from config, default to 30 days
	retentionPeriod, err := getRetentionPeriodForPrometheus(prometheusObj, defaultRetentionPeriod)
	if err != nil {
		return []client.Object{}, fmt.Errorf("failed to get retention period for Prometheus instance: %w", err)
	}

	// Check if retention period has passed
	deletionTime := deprecationTime.Add(retentionPeriod)
	if time.Now().After(deletionTime) {
		logger.Info("Retention period has passed, allowing Prometheus deletion",
			"deprecationTime", deprecationTime,
			"retentionPeriod", retentionPeriod,
			"deletionTime", deletionTime)
		return []client.Object{}, nil
	}

	// Retention period has not passed yet, keep the instance
	remainingTime := time.Until(deletionTime)
	logger.Info("Prometheus instance marked for deprecation, waiting for retention period",
		"deprecationTime", deprecationTime,
		"retentionPeriod", retentionPeriod,
		"remainingTime", remainingTime)
	return []client.Object{prometheusObj}, nil
}

func serviceMonitorsForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	logger := log.FromContext(ctx)
	config := kaiConfig.Spec.Prometheus

	// Check if ServiceMonitors are enabled
	if config.ServiceMonitor != nil && config.ServiceMonitor.Enabled != nil && !*config.ServiceMonitor.Enabled {
		logger.Info("ServiceMonitors are disabled, skipping ServiceMonitor creation")
		return []client.Object{}, nil
	}

	// Check if ServiceMonitor CRD is available
	hasServiceMonitorCRD, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeClient, "serviceMonitor")
	if err != nil {
		logger.Error(err, "Failed to check for ServiceMonitor CRD")
		return nil, err
	}

	if !hasServiceMonitorCRD {
		logger.Info("ServiceMonitor CRD not found - ServiceMonitor resources cannot be created")
		return []client.Object{}, nil
	}

	var serviceMonitors []client.Object

	// Create ServiceMonitor for each KAI service
	for _, kaiService := range common.KaiServicesForServiceMonitor {
		serviceMonitorObj, err := common.ObjectForKAIConfig(ctx, runtimeClient, &monitoringv1.ServiceMonitor{}, kaiService.Name, kaiConfig.Spec.Namespace)
		if err != nil {
			logger.Error(err, "Failed to check for existing ServiceMonitor instance", "service", kaiService.Name)
			return nil, err
		}

		labelKey, labelValue := getAccountingSelectorLabels(config)
		serviceMonitorObj.GetLabels()[labelKey] = labelValue

		namespaces := []string{kaiConfig.Spec.Namespace}
		if kaiService.Namespaces != nil {
			namespaces = kaiService.Namespaces
		}

		labelSelector := map[string]string{"app": kaiService.Name}
		if kaiService.LabelSelector != nil {
			labelSelector = kaiService.LabelSelector
		}

		// Set the ServiceMonitor spec from configuration
		serviceMonitorSpec := monitoringv1.ServiceMonitorSpec{
			JobLabel: kaiService.JobLabel,
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: namespaces,
			},
			Selector: metav1.LabelSelector{
				MatchLabels: labelSelector,
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port: kaiService.Port,
				},
			},
		}

		// Apply ServiceMonitor configuration if available
		if config.ServiceMonitor != nil {
			if config.ServiceMonitor.Interval != nil {
				serviceMonitorSpec.Endpoints[0].Interval = monitoringv1.Duration(*config.ServiceMonitor.Interval)
			}
			if config.ServiceMonitor.ScrapeTimeout != nil {
				serviceMonitorSpec.Endpoints[0].ScrapeTimeout = monitoringv1.Duration(*config.ServiceMonitor.ScrapeTimeout)
			}
			if config.ServiceMonitor.BearerTokenFile != nil {
				serviceMonitorSpec.Endpoints[0].BearerTokenFile = *config.ServiceMonitor.BearerTokenFile
			}
		}
		serviceMonitorObj.(*monitoringv1.ServiceMonitor).Spec = serviceMonitorSpec
		serviceMonitors = append(serviceMonitors, serviceMonitorObj)
	}

	return serviceMonitors, nil
}

// createPrometheusServiceAccount creates a ServiceAccount for Prometheus
func prometheusServiceAccountForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	serviceAccountName := getPrometheusInstanceName(kaiConfig.Spec.Prometheus)

	saObj, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.ServiceAccount{}, serviceAccountName, kaiConfig.Spec.Namespace)
	if err != nil {
		return []client.Object{}, err
	}

	serviceAccount := saObj.(*v1.ServiceAccount)
	serviceAccount.TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}
	serviceAccount.ObjectMeta = metav1.ObjectMeta{
		Name:      serviceAccountName,
		Namespace: kaiConfig.Spec.Namespace,
		Labels: map[string]string{
			"app": serviceAccountName,
		},
	}

	return []client.Object{serviceAccount}, nil
}

// validateExternalPrometheusConnection validates connectivity to an external Prometheus instance
func validateExternalPrometheusConnection(ctx context.Context, prometheusURL string) error {
	logger := log.FromContext(ctx)

	// Ensure the URL has a scheme
	if !strings.Contains(prometheusURL, "://") {
		prometheusURL = "http://" + prometheusURL
	}

	// Parse the URL to ensure it's valid
	_, err := url.Parse(prometheusURL)
	if err != nil {
		return fmt.Errorf("invalid Prometheus URL: %v", err)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Try to connect to the Prometheus /api/v1/status/config endpoint
	statusURL := prometheusURL + "/api/v1/status/config"
	logger.Info("Validating external Prometheus connection", "url", statusURL)

	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to external Prometheus: %v", err)
	}
	defer resp.Body.Close()

	// Check if we got a successful response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("external Prometheus returned status code %d", resp.StatusCode)
	}

	logger.Info("Successfully validated external Prometheus connection")
	return nil
}

// createServiceMonitorsForExternalPrometheus creates ServiceMonitors for external Prometheus
func createServiceMonitorsForExternalPrometheus(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	logger := log.FromContext(ctx)
	config := kaiConfig.Spec.Prometheus

	// Check if ServiceMonitor CRD is available
	hasServiceMonitorCRD, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeClient, "serviceMonitor")
	if err != nil {
		logger.Error(err, "Failed to check for ServiceMonitor CRD")
		return nil, err
	}

	if !hasServiceMonitorCRD {
		logger.Info("ServiceMonitor CRD not found - ServiceMonitor resources cannot be created")
		return []client.Object{}, nil
	}

	// Check if ServiceMonitors are enabled
	if config.ServiceMonitor == nil || config.ServiceMonitor.Enabled == nil || !*config.ServiceMonitor.Enabled {
		logger.Info("ServiceMonitors are disabled for external Prometheus")
		return []client.Object{}, nil
	}

	logger.Info("Creating ServiceMonitors for external Prometheus")

	// Create ServiceMonitors using the existing function
	serviceMonitors, err := serviceMonitorsForKAIConfig(ctx, runtimeClient, kaiConfig)
	if err != nil {
		logger.Error(err, "Failed to create ServiceMonitor instances for external Prometheus")
		return nil, err
	}

	logger.Info("Successfully created ServiceMonitors for external Prometheus", "count", len(serviceMonitors))
	return serviceMonitors, nil
}

func getStorageSpecForPrometheus(config *kaiprometheus.Prometheus) *monitoringv1.StorageSpec {
	// Only if explicitly disabled, return nil
	if config.EnablePersistentStorage != nil && !*config.EnablePersistentStorage {
		return nil
	}

	storageSize := defaultStorageSize
	if config.StorageSize != nil {
		storageSize = *config.StorageSize
	}
	storageSpec := &monitoringv1.StorageSpec{
		VolumeClaimTemplate: monitoringv1.EmbeddedPersistentVolumeClaim{
			Spec: v1.PersistentVolumeClaimSpec{
				StorageClassName: config.StorageClassName,
				AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: resource.MustParse(storageSize),
					},
				},
			},
		},
	}
	return storageSpec
}

func getRetentionPeriodForPrometheus(prom *monitoringv1.Prometheus, defaultRetentionPeriod time.Duration) (time.Duration, error) {
	if prom == nil {
		return defaultRetentionPeriod, nil
	}
	if string(prom.Spec.Retention) == "" {
		return defaultRetentionPeriod, nil
	}

	duration, err := str2duration.ParseDuration(string(prom.Spec.Retention))
	if err != nil {
		return defaultRetentionPeriod, err
	}

	return duration, nil
}

func getAccountingSelectorLabels(config *kaiprometheus.Prometheus) (string, string) {
	labelKey := constants.DefaultAccountingLabelKey
	labelValue := constants.DefaultAccountingLabelValue

	if config != nil {
		if config.AccountingLabelKey != nil {
			labelKey = *config.AccountingLabelKey
		}
		if config.AccountingLabelValue != nil {
			labelValue = *config.AccountingLabelValue
		}
	}

	return labelKey, labelValue
}

// getPrometheusInstanceName returns the Prometheus instance name from config or the default
func getPrometheusInstanceName(config *kaiprometheus.Prometheus) string {
	if config != nil && config.InstanceName != nil && *config.InstanceName != "" {
		return *config.InstanceName
	}
	return mainResourceName
}

func usagePrometheusServiceForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	config := kaiConfig.Spec.Prometheus

	if config == nil || config.Enabled == nil || !*config.Enabled {
		return []client.Object{}, nil
	}

	// Don't create service if using external Prometheus
	if config.ExternalPrometheusUrl != nil && *config.ExternalPrometheusUrl != "" {
		return []client.Object{}, nil
	}

	// Check if Prometheus Operator is installed
	hasPrometheusOperator, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeClient, "prometheus")
	if err != nil {
		return []client.Object{}, err
	}

	if !hasPrometheusOperator {
		return []client.Object{}, nil
	}

	serviceObj, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.Service{}, usagePrometheusService, kaiConfig.Spec.Namespace)
	if err != nil {
		return []client.Object{}, err
	}

	service := serviceObj.(*v1.Service)
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	instanceName := getPrometheusInstanceName(config)

	service.Spec.Selector = map[string]string{
		"app.kubernetes.io/instance": instanceName,
		"app.kubernetes.io/name":     "prometheus",
	}
	service.Spec.Ports = []v1.ServicePort{
		{
			Name:     "web",
			Port:     prometheusPort,
			Protocol: v1.ProtocolTCP,
		},
	}
	service.Spec.Type = v1.ServiceTypeClusterIP

	return []client.Object{service}, nil
}
