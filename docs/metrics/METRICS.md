# KAI Scheduler - Prometheus Metrics Documentation

This document provides a reference for all Prometheus metrics exposed by the KAI Scheduler components.

---

## Queue Controller Metrics

Metrics related to Queue resource management and resource quota tracking.

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `queue_info` | Gauge | `queue_name`, `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Queue existence marker (always 1 when queue exists). Standard Kubernetes labels track the queue-controller pod exposing the metric. |
| `queue_deserved_gpus` | Gauge | `queue_name`, `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Deserved/allocated GPU quota for the queue (fair-share allocation). |
| `queue_quota_cpu_cores` | Gauge | `queue_name`, `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | CPU quota for the queue in cores. Value of -1 indicates unlimited quota. |
| `queue_quota_memory_bytes` | Gauge | `queue_name`, `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Memory quota for the queue in bytes. Value of -1 indicates unlimited quota. |
| `queue_allocated_gpus` | Gauge | `queue_name`, `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Currently allocated GPUs in the queue (actual resource consumption). |
| `queue_allocated_cpu_cores` | Gauge | `queue_name`, `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Currently allocated CPU in cores (actual resource consumption). |
| `queue_allocated_memory_bytes` | Gauge | `queue_name`, `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Currently allocated memory in bytes (actual resource consumption). |

### Label Definitions

- **`queue_name`**: Name of the Queue resource (e.g., `default-parent-queue`, `default-queue`)
- **`endpoint`**: Prometheus scrape endpoint path (e.g., `metrics`)
- **`instance`**: Pod IP:Port (e.g., `10.244.1.5.8080`)
- **`job`**: Scrape job name from Prometheus config (e.g., `queue-controller`)
- **`namespace`**: Kubernetes namespace (e.g., `kai-scheduler`)
- **`pod`**: Pod name (e.g., `queue-controller-b8c6ff5b4-ghzd`)
- **`service`**: Kubernetes Service name (e.g., `queue-controller`)

---

## Scheduler Metrics

Metrics related to the core scheduling algorithm performance, task lifecycle, and fairness tracking.

### Latency Metrics

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `e2e_scheduling_latency_milliseconds` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | End-to-end scheduling cycle (all actions) duration in milliseconds |
| `open_session_latency_milliseconds` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Session open latency in milliseconds, including all plugin initialization. Latest value only. |
| `close_session_latency_milliseconds` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Session close latency in milliseconds, including all plugin cleanup. Latest value only. |
| `plugin_scheduling_latency_milliseconds` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `plugin`, `OnSession` | Per-plugin scheduling latency in milliseconds for OnSessionOpen and OnSessionClose methods |
| `action_scheduling_latency_milliseconds` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `action` | Per-action scheduling latency in milliseconds. Identifies which actions dominate scheduling time. |
| `task_scheduling_latency_milliseconds` | Histogram | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Duration in milliseconds from pod creation until scheduler bind attempt. Affected by scheduler performance and cluster conditions (e.g., resource availability). Does not include binder service execution. Buckets: [5ms, 10ms, 20ms, ..., 2560ms] (exponential). |
| `task_bind_latency_milliseconds` | Histogram | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Duration in milliseconds for the binder service to execute pod binding, including bind request creation and actual binding. Buckets: [5ms, 10ms, 20ms, ..., 2560ms] (exponential). |
| `usage_query_latency_milliseconds` | Histogram | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Usage database query latency distribution in milliseconds (if configured). Buckets: [5ms, 10ms, 20ms, ..., 2560ms] (exponential). |

### Scheduling Action Metrics

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `podgroups_scheduled_by_action` | Counter | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `action` | Cumulative count of pod groups successfully scheduled by each action. |
| `podgroups_acted_on_by_action` | Counter | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `action` | Cumulative count of pod groups considered/attempted by each action (may fail or be filtered). |
| `scenarios_simulation_by_action` | Counter | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `action` | Cumulative count of simulation scenarios run by each action during scheduling decisions. |
| `scenarios_filtered_by_action` | Counter | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `action` | Cumulative count of simulation scenarios filtered/rejected by each action. |
| `total_preemption_attempts` | Counter | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service` | Cumulative total of preemption attempts across the entire cluster lifetime. |
| `pod_group_evicted_pods_total` | Counter | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `podgroup`, `uid`, `nodepool`, `action` | Cumulative count of pods evicted per pod group, tracked by nodepool and action. |

### Queue Fair-Share & Usage Metrics

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `queue_fair_share_cpu_cores` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `queue_name` | CPU fair-share allocation for the queue in cores. Updated per scheduling cycle. |
| `queue_fair_share_memory_gb` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `queue_name` | Memory fair-share allocation for the queue in GB. Updated per scheduling cycle. |
| `queue_fair_share_gpu` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `queue_name` | GPU fair-share allocation for the queue in device count. Updated per scheduling cycle. |
| `queue_cpu_usage` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `queue_name` | CPU usage of the queue. Units depend on configured UsageDB (typically cores or cost units). |
| `queue_memory_usage` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `queue_name` | Memory usage of the queue. Units depend on configured UsageDB (typically GB or cost units). |
| `queue_gpu_usage` | Gauge | `endpoint`, `instance`, `job`, `namespace`, `pod`, `service`, `queue_name` | GPU usage of the queue. Units depend on configured UsageDB (typically device count or cost units). |

---

## Common Label Definitions

All metrics include these standard Prometheus scrape labels:
- **`endpoint`**: Metrics endpoint path (typically `metrics`)
- **`instance`**: Pod IP and container port (e.g., `10.244.1.5:8080`)
- **`job`**: Scrape job name (e.g., `queue-controller`, `scheduler`)
- **`namespace`**: Kubernetes namespace hosting the component
- **`pod`**: Pod name running the component
- **`service`**: Kubernetes Service name for the component

Business/Resource Labels:
- **`queue_name`**: Queue resource identifier
- **`action`**: Scheduling action name
- **`plugin`**: Plugin name
- **`OnSession`**: Session lifecycle phase (`OnSessionOpen` or `OnSessionClose`)
- **`podgroup`**: PodGroup resource identifier
- **`nodepool`**: Node pool identifier for resource allocation
- **`uid`**: Unique identifier (pod group UID)

---
