# Time-Based Fairshare Examples

Time-based fairshare is a feature in KAI Scheduler that uses historical resource usage by queues for making allocation and reclaim decisions.

## Key Features

1. **Historical Usage Consideration**: All else being equal, queues with higher past usage will get to run jobs after queues with lower usage.
2. **Usage-Based Reclaim**: Queues that are starved over time will reclaim resources from queues that used a lot of resources.
   > Note: This does not affect in-quota allocation—deserved quota still takes precedence over time-based fairshare.

## How It Works

Resource usage data is collected and persisted in Prometheus. The scheduler uses this data to make resource fairness calculations: the more resources consumed by a queue, the less over-quota resources it will receive compared to other queues.

### Time Decay (Optional)

If configured, the scheduler applies an [exponential time decay](https://en.wikipedia.org/wiki/Exponential_decay) formula controlled by a half-life period. For example, with a half-life of one hour, a GPU-second consumed an hour ago will be considered half as significant as a GPU-second consumed just now.

## Examples in This Directory

| File | Description |
|------|-------------|
| [scheduling-shard-minimal.yaml](scheduling-shard-minimal.yaml) | Minimal configuration to enable time-based fairshare |
| [scheduling-shard-managed-prometheus.yaml](scheduling-shard-managed-prometheus.yaml) | Full configuration using KAI-managed Prometheus |
| [scheduling-shard-external-prometheus.yaml](scheduling-shard-external-prometheus.yaml) | Configuration for using an external Prometheus instance |
| [two-queue-oscillation/](two-queue-oscillation/) | Complete example demonstrating fair resource oscillation between two queues |

## Quick Setup

### Step 0: Install Prometheus (Optional)

> **Note**: If you already have Prometheus and kube-state-metrics installed, skip to Step 1.

If you don't already have Prometheus installed in your cluster, you can install it using the [kube-prometheus-stack](https://artifacthub.io/packages/helm/prometheus-community/kube-prometheus-stack) Helm chart. This chart includes the Prometheus Operator and kube-state-metrics.

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace 
```

Wait for the pods to be ready:

```bash
kubectl wait --for=condition=Ready pods --all -n monitoring --timeout=300s
```

### Step 1: Enable Prometheus

First, enable Prometheus via the KAI operator:

```bash
kubectl patch config kai-config --type merge -p '{"spec":{"prometheus":{"enabled":true}}}'
```

Wait for the Prometheus pod to be ready:

```bash
watch kubectl get pod -n kai-scheduler prometheus-prometheus-0
```

### Step 2: Configure the Scheduler

Apply the minimal scheduling shard configuration:

```bash
kubectl apply -f scheduling-shard-minimal.yaml
```

Or patch the existing shard:

```bash
kubectl patch schedulingshard default --type merge -p '{"spec":{"usageDBConfig":{"clientType":"prometheus"}}}'
```

The scheduler will restart and connect to Prometheus.

## Configuration Options

### Usage Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `windowSize` | `1w` (1 week) | Time period considered for fairness calculations |
| `windowType` | `sliding` | Window type: `sliding`, `tumbling`, or `cron` |
| `halfLifePeriod` | disabled | Half-life for exponential decay (e.g., `10m`, `1h`) |
| `fetchInterval` | `1m` | How often to fetch usage data from Prometheus |
| `stalenessPeriod` | `5m` | Maximum age of usage data before considered stale |

### kValue

The `kValue` parameter controls the impact of historical usage on fairness calculations:
- Higher values = more aggressive correction based on historical usage
- Lower values = more weight on over-quota weights, less on history
- Default: `1.0`

### Window Types

- **Sliding**: Considers usage from the last `windowSize` duration (rolling window)
- **Tumbling**: Non-overlapping fixed windows that reset at `tumblingWindowStartTime`
- **Cron**: Windows defined by a cron expression

## Using External Prometheus

If you have an existing Prometheus instance, configure it in the KAI config:

```bash
kubectl patch config kai-config --type merge -p '{
  "spec": {
    "prometheus": {
      "enabled": true,
      "externalPrometheusUrl": "http://prometheus.monitoring.svc.cluster.local:9090"
    }
  }
}'
```

See [scheduling-shard-external-prometheus.yaml](scheduling-shard-external-prometheus.yaml) for a complete example.

## Troubleshooting

### Prerequisites

Ensure the [Prometheus Operator](https://prometheus-operator.dev/docs/getting-started/installation/) is installed:

```bash
kubectl get crd prometheuses.monitoring.coreos.com
```

For cluster capacity metrics, [kube-state-metrics](https://artifacthub.io/packages/helm/prometheus-community/kube-state-metrics/) must also be installed.

### Check Scheduler Logs

If the scheduler cannot fetch usage metrics:

```bash
kubectl logs -n kai-scheduler deployment/kai-scheduler-default | grep -i usage
```

### Verify Prometheus Connection

Check if the scheduler can reach Prometheus:

```bash
kubectl exec -n kai-scheduler deployment/kai-scheduler-default -- wget -q -O- http://prometheus-operated.kai-scheduler.svc.cluster.local:9090/api/v1/status/config
```

## Further Reading

- [Time-Based Fairshare Documentation](../../docs/time-based-fairshare/README.md)
- [Fairness Concepts](../../docs/fairness/README.md)
- [Time-Based Fairshare Design Document](../../docs/developer/designs/time-based-fairshare/time-based-fairshare.md)
- [Time-Based Fairshare Simulator](../../cmd/time-based-fairshare-simulator/README.md)

