// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func prometheusIndexer(object client.Object) []string {
	prometheus := object.(*monitoringv1.Prometheus)
	owner := metav1.GetControllerOf(prometheus)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerPrometheus() {
	// Only register Prometheus collectable if CRD is available
	// We'll check this at runtime during manager initialization
	collectable := &Collectable{
		Collect: getCurrentPrometheusState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			// Try to register the indexer, but don't fail if the CRD is not available
			log.FromContext(ctx).Info("Attempting to register Prometheus resource management")
			err := mgr.GetFieldIndexer().IndexField(ctx, &monitoringv1.Prometheus{}, CollectableOwnerKey, prometheusIndexer)
			if err != nil {
				log.FromContext(ctx).Info("Prometheus CRD not available, skipping field indexer registration", "error", err)
				return nil // Don't fail the test if CRD is not available
			}
			log.FromContext(ctx).Info("Successfully registered Prometheus resource management")
			return nil
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&monitoringv1.Prometheus{}, CollectableOwnerKey, prometheusIndexer)
		},
	}
	SetupKAIConfigOwned(collectable)

	// Register ServiceMonitor collectable if CRD is available
	serviceMonitorCollectable := &Collectable{
		Collect: getCurrentServiceMonitorState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			// Try to register the indexer, but don't fail if the CRD is not available
			log.FromContext(ctx).Info("Attempting to register ServiceMonitor resource management")
			err := mgr.GetFieldIndexer().IndexField(ctx, &monitoringv1.ServiceMonitor{}, CollectableOwnerKey, serviceMonitorIndexer)
			if err != nil {
				log.FromContext(ctx).Info("ServiceMonitor CRD not available, skipping field indexer registration", "error", err)
				return nil // Don't fail the test if CRD is not available
			}
			log.FromContext(ctx).Info("Successfully registered ServiceMonitor resource management")
			return nil
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&monitoringv1.ServiceMonitor{}, CollectableOwnerKey, serviceMonitorIndexer)
		},
	}
	SetupKAIConfigOwned(serviceMonitorCollectable)
}

func getCurrentPrometheusState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}

	// Check if Prometheus CRD is available before trying to list resources
	hasPrometheusCRD, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeClient, "prometheus")
	if err != nil {
		return nil, err
	}
	if !hasPrometheusCRD {
		return result, nil
	}

	prometheusList := &monitoringv1.PrometheusList{}
	reconcilerKey := getReconcilerKey(reconciler)

	// Try to list with field selector first, but fall back to listing all if field indexer is not available
	err = runtimeClient.List(ctx, prometheusList, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		// If field indexer is not available, fall back to listing all Prometheus resources
		// and filter by owner reference manually
		log.FromContext(ctx).Info("Field indexer not available, falling back to manual filtering")
		err = runtimeClient.List(ctx, prometheusList)
		if err != nil {
			return nil, err
		}

		// Filter by owner reference manually
		for _, prometheus := range prometheusList.Items {
			owner := metav1.GetControllerOf(&prometheus)
			if owner != nil && checkOwnerType(owner) && getOwnerKey(owner) == reconcilerKey {
				result[GetKey(prometheus.GroupVersionKind(), prometheus.Namespace, prometheus.Name)] = &prometheus
			}
		}
		return result, nil
	}

	for _, prometheus := range prometheusList.Items {
		result[GetKey(prometheus.GroupVersionKind(), prometheus.Namespace, prometheus.Name)] = &prometheus
	}

	return result, nil
}

func serviceMonitorIndexer(object client.Object) []string {
	serviceMonitor := object.(*monitoringv1.ServiceMonitor)
	owner := metav1.GetControllerOf(serviceMonitor)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func getCurrentServiceMonitorState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}

	// Check if ServiceMonitor CRD is available before trying to list resources
	hasServiceMonitorCRD, err := common.CheckPrometheusCRDsAvailable(ctx, runtimeClient, "serviceMonitor")
	if err != nil {
		return nil, err
	}
	if !hasServiceMonitorCRD {
		return result, nil
	}

	serviceMonitorList := &monitoringv1.ServiceMonitorList{}
	reconcilerKey := getReconcilerKey(reconciler)

	// Try to list with field selector first, but fall back to listing all if field indexer is not available
	err = runtimeClient.List(ctx, serviceMonitorList, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		// If field indexer is not available, fall back to listing all ServiceMonitor resources
		// and filter by owner reference manually
		log.FromContext(ctx).Info("Failed to list ServiceMonitor", "error", err)
		err = runtimeClient.List(ctx, serviceMonitorList)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to manually list ServiceMonitor resource", "error", err)
			return nil, err
		}

		// Filter by owner reference manually
		for _, serviceMonitor := range serviceMonitorList.Items {
			owner := metav1.GetControllerOf(&serviceMonitor)
			if owner != nil && checkOwnerType(owner) && getOwnerKey(owner) == reconcilerKey {
				result[GetKey(serviceMonitor.GroupVersionKind(), serviceMonitor.Namespace, serviceMonitor.Name)] = &serviceMonitor
			}
		}
		return result, nil
	}

	for _, serviceMonitor := range serviceMonitorList.Items {
		result[GetKey(serviceMonitor.GroupVersionKind(), serviceMonitor.Namespace, serviceMonitor.Name)] = &serviceMonitor
	}

	return result, nil
}
