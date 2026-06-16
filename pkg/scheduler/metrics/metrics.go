/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
)

const (
	// OnSessionOpen label
	OnSessionOpen = "OnSessionOpen"

	// OnSessionClose label
	OnSessionClose = "OnSessionClose"
)

var (
	currentAction                                  string
	e2eSchedulingLatency                           prometheus.Gauge
	openSessionLatency                             prometheus.Gauge
	closeSessionLatency                            prometheus.Gauge
	pluginSchedulingLatency                        *prometheus.GaugeVec
	actionSchedulingLatency                        *prometheus.GaugeVec
	taskSchedulingLatency                          prometheus.Histogram
	taskBindLatency                                prometheus.Histogram
	podgroupsScheduledByAction                     *prometheus.CounterVec
	podgroupsConsideredByAction                    *prometheus.CounterVec
	scenariosSimulatedByAction                     *prometheus.CounterVec
	scenariosFilteredByAction                      *prometheus.CounterVec
	preemptionAttempts                             prometheus.Counter
	queueFairShareCPU                              *prometheus.GaugeVec
	queueFairShareMemory                           *prometheus.GaugeVec
	queueFairShareGPU                              *prometheus.GaugeVec
	queueCPUUsage                                  *prometheus.GaugeVec
	queueMemoryUsage                               *prometheus.GaugeVec
	queueGPUUsage                                  *prometheus.GaugeVec
	usageQueryLatency                              *prometheus.HistogramVec
	podGroupEvictedPodsTotal                       *prometheus.CounterVec
	scenarioSearchJobsTotal                        *prometheus.CounterVec
	scenarioSearchActionBudgetConfiguredSeconds    *prometheus.GaugeVec
	scenarioSearchJobBudgetConfiguredSeconds       prometheus.Gauge
	scenarioSearchGeneratorBudgetConfiguredSeconds *prometheus.GaugeVec
	scenarioSearchActionBudgetExhaustedTotal       *prometheus.CounterVec
	scenarioSearchDurationSeconds                  *prometheus.HistogramVec
	scenarioSearchScenariosTotal                   *prometheus.CounterVec
)

func init() {
	InitMetrics("")
}

func InitMetrics(namespace string) {
	e2eSchedulingLatency = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "e2e_scheduling_latency_milliseconds",
			Help:      "E2e scheduling latency in milliseconds (scheduling algorithm + binding), as a gauge",
		},
	)

	openSessionLatency = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "open_session_latency_milliseconds",
			Help:      "Open session latency in milliseconds, including all plugins, as a gauge",
		},
	)

	closeSessionLatency = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "close_session_latency_milliseconds",
			Help:      "Close session latency in milliseconds, including all plugins, as a gauge",
		},
	)

	pluginSchedulingLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "plugin_scheduling_latency_milliseconds",
			Help:      "Plugin scheduling latency in milliseconds, as a gauge",
		}, []string{"plugin", "OnSession"})

	actionSchedulingLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "action_scheduling_latency_milliseconds",
			Help:      "Action scheduling latency in milliseconds, as a gauge",
		}, []string{"action"})

	taskSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "task_scheduling_latency_milliseconds",
			Help:      "Task scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 10),
		},
	)

	taskBindLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "task_bind_latency_milliseconds",
			Help:      "Task bind latency histogram in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 10),
		},
	)

	podgroupsScheduledByAction = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "podgroups_scheduled_by_action",
			Help:      "Count of podgroups scheduled per action",
		}, []string{"action"})

	podgroupsConsideredByAction = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "podgroups_acted_on_by_action",
			Help:      "Count of podgroups tried per action",
		}, []string{"action"})

	scenariosSimulatedByAction = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scenarios_simulation_by_action",
			Help:      "Count of scenarios simulated per action",
		}, []string{"action"})

	scenariosFilteredByAction = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scenarios_filtered_by_action",
			Help:      "Count of scenarios filtered per action",
		}, []string{"action"})

	preemptionAttempts = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "total_preemption_attempts",
			Help:      "Total preemption attempts in the cluster till now",
		},
	)

	queueFairShareCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_fair_share_cpu_cores",
			Help:      "CPU Fair share of queue, as a gauge. Value is in Cores",
		}, []string{"queue_name", "queue_metadata_name", "queue_display_name"})
	queueFairShareMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_fair_share_memory_gb",
			Help:      "Memory Fair share of queue, as a gauge. Value is in GB",
		}, []string{"queue_name", "queue_metadata_name", "queue_display_name"})
	queueFairShareGPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_fair_share_gpu",
			Help:      "GPU Fair share of queue, as a gauge. Values in GPU devices",
		}, []string{"queue_name", "queue_metadata_name", "queue_display_name"})

	queueCPUUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_cpu_usage",
			Help:      "CPU usage of queue, as a gauge. Units depend on UsageDB configuration",
		}, []string{"queue_name", "queue_metadata_name", "queue_display_name"})
	queueMemoryUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_memory_usage",
			Help:      "Memory usage of queue, as a gauge. Units depend on UsageDB configuration",
		}, []string{"queue_name", "queue_metadata_name", "queue_display_name"})
	queueGPUUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_gpu_usage",
			Help:      "GPU usage of queue, as a gauge. Units depend on UsageDB configuration",
		}, []string{"queue_name", "queue_metadata_name", "queue_display_name"})

	usageQueryLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "usage_query_latency_milliseconds",
			Help:      "Usage database query latency histogram in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 10),
		}, []string{})

	podGroupEvictedPodsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "pod_group_evicted_pods_total",
			Help:      "Total number of pods evicted per pod group",
		}, []string{"podgroup", "namespace", "uid", "nodepool", "action"})

	scenarioSearchJobsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scenario_search_jobs_total",
			Help:      "Count of jobs considered by bounded scenario search.",
		}, []string{"action", "result", "reduced_budget"})

	scenarioSearchActionBudgetConfiguredSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scenario_search_action_budget_configured_seconds",
			Help:      "Configured action scenario search budget in seconds.",
		}, []string{"action"})

	scenarioSearchJobBudgetConfiguredSeconds = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scenario_search_job_budget_configured_seconds",
			Help:      "Configured per-job scenario search budget in seconds.",
		})

	scenarioSearchGeneratorBudgetConfiguredSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scenario_search_generator_budget_configured_seconds",
			Help:      "Configured generator scenario search budget in seconds.",
		}, []string{"generator"})

	scenarioSearchActionBudgetExhaustedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scenario_search_action_budget_exhausted_total",
			Help:      "Count of action-level scenario search budget exhaustion events.",
		}, []string{"action"})

	scenarioSearchDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "scenario_search_duration_seconds",
			Help:      "Elapsed generator-search duration in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 16),
		}, []string{"action", "generator", "result"})

	scenarioSearchScenariosTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scenario_search_scenarios_total",
			Help:      "Count of bounded-search scenarios by state.",
		}, []string{"action", "generator", "state"})
}

// UpdateOpenSessionDuration updates latency for open session, including all plugins
func UpdateOpenSessionDuration(startTime time.Time) {
	duration := Duration(startTime).Milliseconds()
	openSessionLatency.Set(float64(duration))
}

// UpdateCloseSessionDuration updates latency for close session, including all plugins
func UpdateCloseSessionDuration(startTime time.Time) {
	duration := Duration(startTime).Milliseconds()
	closeSessionLatency.Set(float64(duration))
}

// UpdatePluginDuration updates latency for every plugin
func UpdatePluginDuration(pluginName, OnSessionStatus string, duration time.Duration) {
	pluginSchedulingLatency.WithLabelValues(pluginName, OnSessionStatus).Set(float64(duration.Milliseconds()))
}

// UpdateActionDuration updates latency for every action
func UpdateActionDuration(actionName string, duration time.Duration) {
	actionSchedulingLatency.WithLabelValues(actionName).Set(float64(duration.Milliseconds()))
}

// UpdateE2eDuration updates entire end to end scheduling latency
func UpdateE2eDuration(startTime time.Time) {
	duration := Duration(startTime).Milliseconds()
	e2eSchedulingLatency.Set(float64(duration))
}

// UpdateTaskScheduleDuration updates single task scheduling latency
func UpdateTaskScheduleDuration(duration time.Duration) {
	taskSchedulingLatency.Observe(float64(duration.Milliseconds()))
}

func SetCurrentAction(action string) {
	currentAction = action
}

func IncPodgroupScheduledByAction() {
	podgroupsScheduledByAction.WithLabelValues(currentAction).Inc()
}

func IncPodgroupsConsideredByAction() {
	podgroupsConsideredByAction.WithLabelValues(currentAction).Inc()
}

func IncScenarioSimulatedByAction() {
	scenariosSimulatedByAction.WithLabelValues(currentAction).Inc()
}

func IncScenarioFilteredByAction() {
	scenariosFilteredByAction.WithLabelValues(currentAction).Inc()
}

// UpdateTaskBindDuration updates single task bind latency, including bind request creation
func UpdateTaskBindDuration(startTime time.Time) {
	duration := Duration(startTime).Milliseconds()
	taskBindLatency.Observe(float64(duration))
}

// UpdateQueueFairShare updates fair share of queue for a resource.
// queueName preserves the legacy queue_name label value (DisplayName when set, otherwise metadata.name).
// queueMetadataName is always the Queue resource's metadata.name and is the recommended join key
// against queue-controller metrics. queueDisplayName is the Queue's spec.displayName (empty when unset).
func UpdateQueueFairShare(queueName, queueMetadataName, queueDisplayName string, cpu, memory, gpu float64) {
	queueFairShareCPU.WithLabelValues(queueName, queueMetadataName, queueDisplayName).Set(cpu)
	queueFairShareMemory.WithLabelValues(queueName, queueMetadataName, queueDisplayName).Set(memory)
	queueFairShareGPU.WithLabelValues(queueName, queueMetadataName, queueDisplayName).Set(gpu)
}

func ResetQueueFairShare() {
	queueFairShareCPU.Reset()
	queueFairShareMemory.Reset()
	queueFairShareGPU.Reset()
}

// UpdateQueueUsage updates usage of queue for a resource.
// See UpdateQueueFairShare for the meaning of each label.
func UpdateQueueUsage(queueName, queueMetadataName, queueDisplayName string, cpu, memory, gpu float64) {
	queueCPUUsage.WithLabelValues(queueName, queueMetadataName, queueDisplayName).Set(cpu)
	queueMemoryUsage.WithLabelValues(queueName, queueMetadataName, queueDisplayName).Set(memory)
	queueGPUUsage.WithLabelValues(queueName, queueMetadataName, queueDisplayName).Set(gpu)
}

func ResetQueueUsage() {
	queueCPUUsage.Reset()
	queueMemoryUsage.Reset()
	queueGPUUsage.Reset()
}

func UpdateUsageQueryLatency(latency time.Duration) {
	usageQueryLatency.WithLabelValues().Observe(float64(latency.Milliseconds()))
}

// RegisterPreemptionAttempts records number of attempts for preemption
func RegisterPreemptionAttempts() {
	preemptionAttempts.Inc()
}

// RecordPodGroupEvictedPods records the number of pods evicted for a pod group
func RecordPodGroupEvictedPods(name, namespace, uid, nodepool, action string, count int) {
	podGroupEvictedPodsTotal.WithLabelValues(name, namespace, uid, nodepool, action).Add(float64(count))
}

func IncScenarioSearchJobs[A ~string](action A, result string, reducedBudget bool) {
	scenarioSearchJobsTotal.WithLabelValues(string(action), result, strconv.FormatBool(reducedBudget)).Inc()
}

func SetScenarioSearchActionBudget[A ~string](action A, budget time.Duration) {
	scenarioSearchActionBudgetConfiguredSeconds.WithLabelValues(string(action)).Set(budget.Seconds())
}

func SetScenarioSearchJobBudget(budget time.Duration) {
	scenarioSearchJobBudgetConfiguredSeconds.Set(budget.Seconds())
}

func SetScenarioSearchGeneratorBudget(generator string, budget time.Duration) {
	scenarioSearchGeneratorBudgetConfiguredSeconds.WithLabelValues(generator).Set(budget.Seconds())
}

func IncScenarioSearchActionBudgetExhausted[A ~string](action A) {
	scenarioSearchActionBudgetExhaustedTotal.WithLabelValues(string(action)).Inc()
}

func ObserveScenarioSearchDuration[A ~string](action A, generator string, result string, duration time.Duration) {
	scenarioSearchDurationSeconds.WithLabelValues(string(action), generator, result).Observe(duration.Seconds())
}

func IncScenarioSearchScenario[A ~string](action A, generator string, state string) {
	scenarioSearchScenariosTotal.WithLabelValues(string(action), generator, state).Inc()
}

// Duration get the time since specified start
func Duration(start time.Time) time.Duration {
	return time.Since(start)
}
