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

	metrics.Registry.MustRegister(queueInfo, queueDeservedGPUs, queueQuotaCPU, queueQuotaMemory,
		queueAllocatedGpus, queueAllocatedCpus, queueAllocatedMemory)
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
	allocatedGpus := getAllocatedGpus(queue.Status)
	allocatedCpus := getAllocatedCpuCores(queue.Status)
	allocatedMemory := getAllocatedMemoryBytes(queue.Status)

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
	queueAllocatedGpus.With(queueLabels).Set(allocatedGpus)
	queueAllocatedCpus.With(queueLabels).Set(allocatedCpus)
	queueAllocatedMemory.With(queueLabels).Set(allocatedMemory)
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
	cpuQuota := queueSpecResources.CPU.Quota
	if cpuQuota == unlimitedQuota {
		return unlimitedQuota
	}
	return queueSpecResources.CPU.Quota / milliCpuToCpuDivider
}

func getMemoryQuotaBytes(queueSpecResources *v2.QueueResources) float64 {
	if queueSpecResources == nil {
		return float64(0)
	}
	memoryQuota := queueSpecResources.Memory.Quota
	if memoryQuota == unlimitedQuota {
		return unlimitedQuota
	}
	return memoryQuota * megabytesToBytesMultiplier
}

func getAllocatedGpus(queueStatus v2.QueueStatus) float64 {
	for resourceName, quantity := range queueStatus.Allocated {
		if strings.HasSuffix(string(resourceName), gpuResourceNameSuffix) {
			return roundResourceQuantity(quantity)
		}
	}
	return 0
}

func getAllocatedCpuCores(queueStatus v2.QueueStatus) float64 {
	allocated, ok := queueStatus.Allocated[v1.ResourceCPU]
	if !ok {
		return 0
	}
	return roundResourceQuantity(allocated)
}

func getAllocatedMemoryBytes(queueStatus v2.QueueStatus) float64 {
	allocated, ok := queueStatus.Allocated[v1.ResourceMemory]
	if !ok {
		return 0
	}
	return roundResourceQuantity(allocated)
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
