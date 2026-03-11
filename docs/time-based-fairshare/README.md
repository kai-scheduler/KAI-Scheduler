# Time Based Fairshare

Time based fairshare is a feature in KAI-Scheduler which makes use of historical resource usage by queues for making allocation and reclaim decisions. Key features are:

1. All else being equal, queues with higher past usage will get to run jobs after queues with lower usage
2. Reclaim based on usage: queues which are starved over time will reclaim resources from queues which used a lot of resources.
    1. Note: this does not effect in-quota allocation: deserved quota still takes precedence over time-based fairshare


> **Prerequisites**: Familiarity with [fairness](../fairness/README.md)

## How it works

In high level: resource usage data in the cluster is collected and persisted in prometheus. It is then used by the scheduler to make resource fairness calculations: the more resources consumed by a queue, the less over-quota resources it will get compared to other queues. This will eventually result in the queues' over-quota resources being reclaimed by more starved queues, thus achieving a more fair allocation of resources over time.

### Resource usage data

Queue historical resource usage data is collected in a prometheus instance in the cluster *(external prometheus instances can be used - see [External prometheus](#external-prometheus))*. The scheduler configuration determines the time period that will be considered, as well as allows configuration for time-decay, which, if configured, gives more weight to recent usage than past usage.

The metrics are collected continuously: the pod-group-controller publishes resource usage for individual pod-groups on their status, which are then aggregated by the queue-controller and published as a metric, which gets collected and persisted by prometheus.

If configured, the scheduler applies an [exponential time decay](https://en.wikipedia.org/wiki/Exponential_decay) formula which is configured by a half-life period. This can be more intuitively understood with an example: for a half life of one hour, a usage (for example, 1 gpu-second) that occurred an hour ago will be considered half as significant as a gpu-second that was consumed just now.

Mathematically, the following formula is applied to historical usage:

$$U = 0.5^{\frac{\Delta{t}}{t_{1/2}}}*A$$

Where:

- $U$ is the usage
- $t_{1/2}$ is the half life constant set by the user
- $\Delta{t}$ is the time elapsed since that usage
- $A$ is the allocated resource

#### Normalization to cluster capacity

The aggregated usage for each queue is then normalized to the **cluster capacity** at the relevant time period: the scheduler looks at the available resources in the cluster for that time period, and normalizes all resource usage to it. For example, in a cluster with 10 GPUs, and considering a time period of 10 hours, a queue which consumed 24 GPU hours (wether it's 8 GPUs for 3 hours, or 12 GPUs for 2 hours), will get a normalized usage score of 0.24 (used 24 GPU hours out of a potential 100). This normalization ensures that a small amount of resource usage relative to the cluster size will not result in a heavy penalty.

### Effect on fair share

Usually, over quota resources are assigned to each queue proportionally to it's Over Quota Weight. With time-based fairshare, queues with historical usage will get relatively less resources in over-quota. The significance of the resource usage in this calculation can be controlled with a parameter called "kValue": the bigger it is, the more impact (or weight) the historical usage has on the calculated fairshare, i.e. it will decrease the fairshare of that queue.

Check out the [time based fairshare simulator](../../cmd/time-based-fairshare-simulator/README.md) to understand scheduling behavior over time better.

### Example

The following plot demonstrates the GPU allocation over time in a 16 GPU cluster, with two queues, each having 0 deserved quota and 1 Over Quota weight for GPUs, each trying to run 16-GPU, single-pod Jobs.

![Time-based fairshare GPU allocation over time](./results.png)

*Time units are intentionally omitted*

## Setup and Configurations

### Quick setup

Enable prometheus in KAI operator:

```sh
kubectl patch config kai-config --type merge -p '{"spec":{"prometheus":{"enabled":true}}}'
```

It's recommended to wait for the prometheus pod to be available. Look for it in `kai-scheduler` namespace:

```sh
watch kubectl get pod -n kai-scheduler prometheus-prometheus-0
```

And configure the scheduler to connect to it by patching the scheduling shard:

```sh
kubectl patch schedulingshard default --type merge -p '{"spec":{"usageDBConfig":{"clientType":"prometheus"}}}'
```

The scheduler should now restart and attempt to connect to prometheus.

### Scheduler configurations

You can further configure the scheduler by editing the scheduling shard:

```sh
kubectl edit schedulingshard default
```
*Replace `default` with the shard name if relevant*

Add the following section under `spec`:
```yaml
  usageDBConfig:
    clientType: prometheus
    connectionString: http://prometheus-operated.kai-scheduler.svc.cluster.local:9090 # Optional: if not configured, the kai config will populate it 
    usageParams:
      windowSize: 1w # The time period considered for fairness calculations. One week is the default
      windowType: sliding # Change to the desired value (sliding/tumbling). Sliding is the default
      halfLifePeriod: 10m # Leave empty to not use time decay. Off by default
```

#### kValue

KValue is a parameter used by the proportion plugin to determine the impact of historical usage in fairness calculations - higher values mean more aggressive effects on fairness. To set it, add it to the scheduling shard spec:
```sh
kubectl edit schedulingshard default
```

```yaml
spec:
  ... # Other configurations
  kValue: 0.5
  usageDBConfig:
    ... # Other configurations
```

#### Advanced: overriding metrics

> *This configuration should not be changed under normal conditions*

In some cases, the admin might want to configure the scheduler to query different metrics for usage and capacity of certain resources. This can be done with the following config:

```sh
kubectl edit schedulingshard default
```

```yaml
  usageDBConfig:
    extraParams:
      gpuAllocationMetric: kai_queue_allocated_gpus
      cpuAllocationMetric: kai_queue_allocated_cpu_cores
      memoryAllocationMetric: kai_queue_allocated_memory_bytes
      gpuCapacityMetric: sum(kube_node_status_capacity{resource=\"nvidia_com_gpu\"})
      cpuCapacityMetric: sum(kube_node_status_capacity{resource=\"cpu\"})
      memoryCapacityMetric: sum(kube_node_status_capacity{resource=\"memory\"})
```

###  Prometheus configurations

> Using a kai-operated prometheus assumes that the [prometheus operator](https://prometheus-operator.dev/docs/getting-started/installation/) is installed in the cluster

To enable prometheus via kai-operator, apply the following patch:
```sh
kubectl patch config kai-config --type merge -p '{"spec":{"prometheus":{"enabled":true}}}'
```

You can also customize the following configurations:

```
  externalPrometheusHealthProbe	# defines the configuration for external Prometheus connectivity validation, with defaults.
  externalPrometheusUrl	# defines the URL of an external Prometheus instance to use. When set, KAI will not deploy its own Prometheus but will configure ServiceMonitors for the external instance and validate connectivity
  retentionPeriod # defines how long to retain data (e.g., "2w", "1d", "30d")
  sampleInterval # defines the interval of sampling (e.g., "1m", "30s", "5m")
  serviceMonitor # defines ServiceMonitor configuration for KAI services
  storageClassName # defines the name of the storageClass that will be used to store the TSDB data. defaults to "standard".
  storageSize # defines the size of the storage (e.g., "20Gi", "30Gi")
```

## Troubleshooting

### Dependencies

Before enabling prometheus in kai config, make sure that the prometheus is installed. If it's not, you will see the following condition in the kai config:

``` sh 
kubectl describe config kai-config
```
```
Status:
  Conditions:
    ...
    Last Transition Time:  2025-11-10T11:25:48Z
    Message:               KAI-prometheus: no matches for kind "Prometheus" in version "monitoring.coreos.com/v1"
KAI-prometheus: not available
    Observed Generation:   2
    Reason:                available
    Status:                False
    Type:                  Available
```

Simply follow the [prometheus installation instructions](https://prometheus-operator.dev/docs/getting-started/installation/).

In order to collect cluster capacity metrics, [kube-state-metrics](https://artifacthub.io/packages/helm/prometheus-community/kube-state-metrics/) needs to be installed as well. By default, the kai operator creates a ServiceMonitor for it, assuming it's installed in `monitoring` or `default` namespace.

### Missing metrics

If the scheduler is unable to collect the usage metrics from prometheus, you will see a message in the logs, similar to this:

```
2025-11-10T12:33:07.318Z	ERROR	usagedb/usagedb.go:142	failed to fetch usage data: error querying nvidia.com/gpu and capacity: error querying cluster capacity metric ((sum(kube_node_status_capacity{resource="nvidia_com_gpu"})) * (0.5^((1762777987 - time()) / 600.000000))): bad_data: invalid parameter "query": 1:124: parse error: unexpected character in duration expression: '&'
```

Prometheus connectivity
Metrics availability
