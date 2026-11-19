// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/kai-scheduler/api/constants"
)

var (
	podGroupsCreatedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "podgrouper_podgroups_created_total",
			Help:      "Total number of PodGroups created by the pod-grouper, by grouper plugin and top owner kind",
		}, []string{"plugin", "owner_gvk"})

	errorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "podgrouper_errors_total",
			Help:      "Total number of errors returned by pod-grouper plugins while computing PodGroup metadata",
		}, []string{"plugin"})
)

func init() {
	metrics.Registry.MustRegister(podGroupsCreatedTotal, errorsTotal)
}

// IncPodGroupsCreated records a PodGroup created for a workload handled by the given grouper plugin.
// ownerGVK identifies the top owner of the workload, formatted as "<apiVersion>/<kind>".
func IncPodGroupsCreated(plugin, ownerGVK string) {
	podGroupsCreatedTotal.WithLabelValues(plugin, ownerGVK).Inc()
}

// IncErrors records an error returned by the given grouper plugin.
func IncErrors(plugin string) {
	errorsTotal.WithLabelValues(plugin).Inc()
}
