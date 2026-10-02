// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"math"
	"sort"
	"strings"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	milliCpuToCpuDivider       = 1000
	megabytesToBytesMultiplier = 1000000
	unlimitedQuota             = float64(-1)

	queueNameLabel         = "queue_name"
	queueMetadataNameLabel = "queue_metadata_name"
	queueDisplayNameLabel  = "queue_display_name"

	gpuResourceNameSuffix = "/gpu"
)

var (
	initiated = false

	queueInfo            *prometheus.GaugeVec
	queueDeservedGPUs    *prometheus.GaugeVec
	queueQuotaCPU        *prometheus.GaugeVec
	queueQuotaMemory     *prometheus.GaugeVec
	queueAllocatedGpus   *prometheus.GaugeVec
	queueAllocatedCpus   *prometheus.GaugeVec
	queueAllocatedMemory *prometheus.GaugeVec

	queueAllocatedNonPreemptibleGpus   *prometheus.GaugeVec
	queueAllocatedNonPreemptibleCpus   *prometheus.GaugeVec
	queueAllocatedNonPreemptibleMemory *prometheus.GaugeVec
	queueRequestedGpus                 *prometheus.GaugeVec
	queueRequestedCpus                 *prometheus.GaugeVec
	queueRequestedMemory               *prometheus.GaugeVec
	queueLimitGpus                     *prometheus.GaugeVec
	queueLimitCpus                     *prometheus.GaugeVec
	queueLimitMemory                   *prometheus.GaugeVec

	additionalQueueLabelKeys       []string
	additionalMetricLabelKeys      []string
	queueLabelToDefaultMetricValue map[string]string
)

// InitMetrics initializes the metrics for the queue controller.
// params:
//
//	namespace: the Prometheus namespace for the metrics
//	queueLabelToMetricLabelMap: a map of queue label keys to metric label keys
//	queueLabelToDefaultMetricValueMap: a map of queue label keys to default metric values
//
// For example, if a queue has a label "priority" with value "high",
// and you want to use it as a metric label "queue_priority",
// with a default value of "normal" if the label is not present,
// you would pass:
// queueLabelToMetricLabelMap        = map[string]string{"priority": "queue_priority"}
// queueLabelToDefaultMetricValueMap = map[string]string{"priority": "normal"}
func InitMetrics(namespace string, queueLabelToMetricLabelMap, queueLabelToDefaultMetricValueMap map[string]string) {
	if initiated {
		return
	}
	initiated = true

	// Sort the keys to ensure consistent order
	sortedQueueLabelKeys := make([]string, 0, len(queueLabelToMetricLabelMap))
	for key := range queueLabelToMetricLabelMap {
		sortedQueueLabelKeys = append(sortedQueueLabelKeys, key)
	}
	sort.Strings(sortedQueueLabelKeys)

	additionalMetricLabelKeys = make([]string, 0, len(queueLabelToMetricLabelMap))
	for _, queueLabelKey := range sortedQueueLabelKeys {
		metricLabelKey := queueLabelToMetricLabelMap[queueLabelKey]
		additionalQueueLabelKeys = append(additionalQueueLabelKeys, queueLabelKey)
		additionalMetricLabelKeys = append(additionalMetricLabelKeys, metricLabelKey)
	}

	queueLabelToDefaultMetricValue = queueLabelToDefaultMetricValueMap

	queueMetricsLabels := append(
		[]string{queueNameLabel, queueMetadataNameLabel, queueDisplayNameLabel},
		additionalMetricLabelKeys...,
	)

	queueInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_info",
			Help:      "Queues info",
		}, queueMetricsLabels,
	)

	queueDeservedGPUs = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_deserved_gpus",
			Help:      "Queue deserved GPUs",
		}, queueMetricsLabels,
	)

	queueQuotaCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_quota_cpu_cores",
			Help:      "Queue quota CPU",
		}, queueMetricsLabels,
	)

	queueQuotaMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_quota_memory_bytes",
			Help:      "Queue quota memory",
		}, queueMetricsLabels,
	)

	queueAllocatedGpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_allocated_gpus",
			Help:      "Queue allocated GPUs",
		}, queueMetricsLabels,
	)

	queueAllocatedCpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_allocated_cpu_cores",
			Help:      "Queue allocated CPUs",
		}, queueMetricsLabels,
	)

	queueAllocatedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_allocated_memory_bytes",
			Help:      "Queue allocated memory",
		}, queueMetricsLabels,
	)

	queueAllocatedNonPreemptibleGpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_allocated_non_preemptible_gpus",
			Help:      "Queue allocated GPUs of non-preemptible workloads",
		}, queueMetricsLabels,
	)

	queueAllocatedNonPreemptibleCpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_allocated_non_preemptible_cpu_cores",
			Help:      "Queue allocated CPUs of non-preemptible workloads",
		}, queueMetricsLabels,
	)

	queueAllocatedNonPreemptibleMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_allocated_non_preemptible_memory_bytes",
			Help:      "Queue allocated memory of non-preemptible workloads",
		}, queueMetricsLabels,
	)

	queueRequestedGpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_requested_gpus",
			Help:      "Queue requested GPUs of running and pending workloads",
		}, queueMetricsLabels,
	)

	queueRequestedCpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_requested_cpu_cores",
			Help:      "Queue requested CPUs of running and pending workloads",
		}, queueMetricsLabels,
	)

	queueRequestedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_requested_memory_bytes",
			Help:      "Queue requested memory of running and pending workloads",
		}, queueMetricsLabels,
	)

	queueLimitGpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_limit_gpus",
			Help:      "Queue limit GPUs",
		}, queueMetricsLabels,
	)

	queueLimitCpus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_limit_cpu_cores",
			Help:      "Queue limit CPU",
		}, queueMetricsLabels,
	)

	queueLimitMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_limit_memory_bytes",
			Help:      "Queue limit memory",
		}, queueMetricsLabels,
	)

	metrics.Registry.MustRegister(queueInfo, queueDeservedGPUs, queueQuotaCPU, queueQuotaMemory,
		queueAllocatedGpus, queueAllocatedCpus, queueAllocatedMemory,
		queueAllocatedNonPreemptibleGpus, queueAllocatedNonPreemptibleCpus, queueAllocatedNonPreemptibleMemory,
		queueRequestedGpus, queueRequestedCpus, queueRequestedMemory,
		queueLimitGpus, queueLimitCpus, queueLimitMemory)
}

func SetQueueMetrics(queue *v2.Queue) {
	if queue == nil {
		return
	}

	ResetQueueMetrics(queue.Name)

	queueName := queue.Name
	queueDisplayName := queue.Spec.DisplayName
	gpuQuota := getGpuQuota(queue.Spec.Resources)
	cpuQuota := getCpuQuotaCores(queue.Spec.Resources)
	memoryQuota := getMemoryQuotaBytes(queue.Spec.Resources)
	gpuLimit := getGpuLimit(queue.Spec.Resources)
	cpuLimit := getCpuLimitCores(queue.Spec.Resources)
	memoryLimit := getMemoryLimitBytes(queue.Spec.Resources)

	queueLabels := prometheus.Labels{
		queueNameLabel:         queueName,
		queueMetadataNameLabel: queueName,
		queueDisplayNameLabel:  queueDisplayName,
	}
	for metricLabelKey, value := range getAdditionalMetricLabelValues(queue.Labels) {
		queueLabels[metricLabelKey] = value
	}

	queueInfo.With(queueLabels).Set(1)
	queueDeservedGPUs.With(queueLabels).Set(gpuQuota)
	queueQuotaCPU.With(queueLabels).Set(cpuQuota)
	queueQuotaMemory.With(queueLabels).Set(memoryQuota)
	queueAllocatedGpus.With(queueLabels).Set(getGpus(queue.Status.Allocated))
	queueAllocatedCpus.With(queueLabels).Set(getCpuCores(queue.Status.Allocated))
	queueAllocatedMemory.With(queueLabels).Set(getMemoryBytes(queue.Status.Allocated))
	queueAllocatedNonPreemptibleGpus.With(queueLabels).Set(getGpus(queue.Status.AllocatedNonPreemptible))
	queueAllocatedNonPreemptibleCpus.With(queueLabels).Set(getCpuCores(queue.Status.AllocatedNonPreemptible))
	queueAllocatedNonPreemptibleMemory.With(queueLabels).Set(getMemoryBytes(queue.Status.AllocatedNonPreemptible))
	queueRequestedGpus.With(queueLabels).Set(getGpus(queue.Status.Requested))
	queueRequestedCpus.With(queueLabels).Set(getCpuCores(queue.Status.Requested))
	queueRequestedMemory.With(queueLabels).Set(getMemoryBytes(queue.Status.Requested))
	queueLimitGpus.With(queueLabels).Set(gpuLimit)
	queueLimitCpus.With(queueLabels).Set(cpuLimit)
	queueLimitMemory.With(queueLabels).Set(memoryLimit)
}

func ResetQueueMetrics(queueName string) {
	queueLabelIdentifier := prometheus.Labels{queueNameLabel: queueName}
	queueInfo.DeletePartialMatch(queueLabelIdentifier)
	queueDeservedGPUs.DeletePartialMatch(queueLabelIdentifier)
	queueQuotaCPU.DeletePartialMatch(queueLabelIdentifier)
	queueQuotaMemory.DeletePartialMatch(queueLabelIdentifier)
	queueAllocatedGpus.DeletePartialMatch(queueLabelIdentifier)
	queueAllocatedCpus.DeletePartialMatch(queueLabelIdentifier)
	queueAllocatedMemory.DeletePartialMatch(queueLabelIdentifier)
	queueAllocatedNonPreemptibleGpus.DeletePartialMatch(queueLabelIdentifier)
	queueAllocatedNonPreemptibleCpus.DeletePartialMatch(queueLabelIdentifier)
	queueAllocatedNonPreemptibleMemory.DeletePartialMatch(queueLabelIdentifier)
	queueRequestedGpus.DeletePartialMatch(queueLabelIdentifier)
	queueRequestedCpus.DeletePartialMatch(queueLabelIdentifier)
	queueRequestedMemory.DeletePartialMatch(queueLabelIdentifier)
	queueLimitGpus.DeletePartialMatch(queueLabelIdentifier)
	queueLimitCpus.DeletePartialMatch(queueLabelIdentifier)
	queueLimitMemory.DeletePartialMatch(queueLabelIdentifier)
}

func getGpuQuota(queueSpecResources *v2.QueueResources) float64 {
	if queueSpecResources == nil {
		return float64(0)
	}
	return queueSpecResources.GPU.Quota
}

func getCpuQuotaCores(queueSpecResources *v2.QueueResources) float64 {
	if queueSpecResources == nil {
		return float64(0)
	}
	return milliCpuToCores(queueSpecResources.CPU.Quota)
}

func getMemoryQuotaBytes(queueSpecResources *v2.QueueResources) float64 {
	if queueSpecResources == nil {
		return float64(0)
	}
	return megabytesToBytes(queueSpecResources.Memory.Quota)
}

func getGpuLimit(queueSpecResources *v2.QueueResources) float64 {
	if queueSpecResources == nil {
		return float64(0)
	}
	return queueSpecResources.GPU.Limit
}

func getCpuLimitCores(queueSpecResources *v2.QueueResources) float64 {
	if queueSpecResources == nil {
		return float64(0)
	}
	return milliCpuToCores(queueSpecResources.CPU.Limit)
}

func getMemoryLimitBytes(queueSpecResources *v2.QueueResources) float64 {
	if queueSpecResources == nil {
		return float64(0)
	}
	return megabytesToBytes(queueSpecResources.Memory.Limit)
}

func milliCpuToCores(milliCpu float64) float64 {
	if milliCpu == unlimitedQuota {
		return unlimitedQuota
	}
	return milliCpu / milliCpuToCpuDivider
}

func megabytesToBytes(megabytes float64) float64 {
	if megabytes == unlimitedQuota {
		return unlimitedQuota
	}
	return megabytes * megabytesToBytesMultiplier
}

func getGpus(resources v1.ResourceList) float64 {
	for resourceName, quantity := range resources {
		if strings.HasSuffix(string(resourceName), gpuResourceNameSuffix) {
			return roundResourceQuantity(quantity)
		}
	}
	return 0
}

func getCpuCores(resources v1.ResourceList) float64 {
	quantity, ok := resources[v1.ResourceCPU]
	if !ok {
		return 0
	}
	return roundResourceQuantity(quantity)
}

func getMemoryBytes(resources v1.ResourceList) float64 {
	quantity, ok := resources[v1.ResourceMemory]
	if !ok {
		return 0
	}
	return roundResourceQuantity(quantity)
}

func roundResourceQuantity(quantity resource.Quantity) float64 {
	return math.Round(quantity.AsApproximateFloat64()*10000) / 10000
}

func getAdditionalMetricLabelValues(queueLabels map[string]string) prometheus.Labels {
	labelValues := make(prometheus.Labels, len(additionalQueueLabelKeys))

	for i, queueLabelKey := range additionalQueueLabelKeys {
		metricLabelKey := additionalMetricLabelKeys[i]
		if value, exists := queueLabels[queueLabelKey]; exists {
			labelValues[metricLabelKey] = value
		} else if defaultValue, defaultExists := queueLabelToDefaultMetricValue[queueLabelKey]; defaultExists {
			labelValues[metricLabelKey] = defaultValue
		} else {
			labelValues[metricLabelKey] = "" // Default to empty string if no value exists
		}
	}
	return labelValues
}

func GetQueueInfoMetric() *prometheus.GaugeVec {
	return queueInfo
}

func GetQueueDeservedGPUsMetric() *prometheus.GaugeVec {
	return queueDeservedGPUs
}

func GetQueueQuotaCPUMetric() *prometheus.GaugeVec {
	return queueQuotaCPU
}

func GetQueueQuotaMemoryMetric() *prometheus.GaugeVec {
	return queueQuotaMemory
}

func GetQueueAllocatedGPUsMetric() *prometheus.GaugeVec {
	return queueAllocatedGpus
}

func GetQueueAllocatedCPUMetric() *prometheus.GaugeVec {
	return queueAllocatedCpus
}

func GetQueueAllocatedMemoryMetric() *prometheus.GaugeVec {
	return queueAllocatedMemory
}

func GetQueueAllocatedNonPreemptibleGPUsMetric() *prometheus.GaugeVec {
	return queueAllocatedNonPreemptibleGpus
}

func GetQueueAllocatedNonPreemptibleCPUMetric() *prometheus.GaugeVec {
	return queueAllocatedNonPreemptibleCpus
}

func GetQueueAllocatedNonPreemptibleMemoryMetric() *prometheus.GaugeVec {
	return queueAllocatedNonPreemptibleMemory
}

func GetQueueRequestedGPUsMetric() *prometheus.GaugeVec {
	return queueRequestedGpus
}

func GetQueueRequestedCPUMetric() *prometheus.GaugeVec {
	return queueRequestedCpus
}

func GetQueueRequestedMemoryMetric() *prometheus.GaugeVec {
	return queueRequestedMemory
}
