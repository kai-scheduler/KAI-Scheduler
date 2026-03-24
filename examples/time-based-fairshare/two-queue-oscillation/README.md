# Two-Queue Oscillation Example

This example demonstrates time-based fairshare with two competing queues. When both queues have pending jobs that require all cluster resources, the scheduler will oscillate resource allocation between them based on historical usage.

## Scenario

- **Cluster**: 16 GPUs (single node)
- **Queues**: Two queues (`team-a` and `team-b`) with equal weights and no guaranteed quota
- **Jobs**: Each queue submits multiple 16-GPU jobs (requiring the entire cluster)

Without time-based fairshare, one queue would continuously hold resources while the other starves. With time-based fairshare enabled, the scheduler tracks historical usage and reclaims resources from the queue that has consumed more, giving the starved queue a chance to run.

## Expected Behavior

```
Time →
team-a: ████████████████                ████████████████                ████████████████
team-b:                 ████████████████                ████████████████                
```

The allocations oscillate between queues as each accumulates historical usage and the other becomes "more deserving" of resources.

## Files

| File | Description |
|------|-------------|
| [queues.yaml](queues.yaml) | Queue hierarchy with parent and two child queues |
| [jobs.yaml](jobs.yaml) | Example jobs for each queue |
| [simulation-config.yaml](simulation-config.yaml) | Configuration for the time-based fairshare simulator |

## Setup

### Step 1: Enable Time-Based Fairshare

```bash
# Enable Prometheus
kubectl patch config kai-config --type merge -p '{"spec":{"prometheus":{"enabled":true}}}'

# Wait for Prometheus
kubectl wait --for=condition=ready pod -n kai-scheduler prometheus-prometheus-0 --timeout=120s

# Enable time-based fairshare with appropriate settings
kubectl apply -f ../scheduling-shard-managed-prometheus.yaml
```

### Step 2: Create Queues

```bash
kubectl apply -f queues.yaml
```

### Step 3: Submit Jobs

```bash
# Create a namespace for workloads
kubectl create namespace workloads

# Submit jobs for team-a
for i in {1..10}; do
  cat jobs.yaml | sed "s/training-job/training-job-$i/g" | kubectl apply -n workloads -f -
done
```

### Step 4: Observe Oscillation

Watch the allocations change over time:

```bash
# Watch queue allocations
watch 'kubectl get queues -o custom-columns=NAME:.metadata.name,ALLOCATED_GPU:.status.allocated.nvidia\\.com/gpu'

# Or check individual pods
kubectl get pods -n workloads -w

# Or see allocation metrics in prometheus
kubectl port-forward -nkai-scheduler svc/prometheus-operated 9090:9090 &
# Browse to localhost:9090
# Observe the kai_queue_allocated_gpus metric over time
```

## Simulation

You can simulate this scenario without a real cluster using the time-based fairshare simulator:

```bash
# Build the simulator (from repo root)
make time-based-fairshare-simulator

# Run the simulation
./bin/time-based-fairshare-simulator-amd64 -input examples/time-based-fairshare/two-queue-oscillation/simulation-config.yaml -output results.csv

# Analyze results (requires Python with pandas and matplotlib)
cd cmd/time-based-fairshare-simulator/examples
pip install -r requirements.txt
python plot_simple.py ../../../results.csv
```

## Configuration Tuning

### Faster Oscillation

To make queues switch more frequently:

```yaml
usageParams:
  windowSize: 1h        # Shorter window
  halfLifePeriod: 10m   # Faster decay
```

### Smoother Transitions

To reduce oscillation frequency:

```yaml
usageParams:
  windowSize: 168h      # Longer window (1 week)
  halfLifePeriod: 24h   # Slower decay
```

### More Aggressive Fairness

To make the scheduler correct imbalances more aggressively:

```yaml
spec:
  kValue: 2.0           # Higher k = more aggressive correction
```

## Notes

- The oscillation period depends on `windowSize`, `halfLifePeriod`, and job duration
- If min-runtime is configured, jobs will not be preempted until they exceed it
- Deserved quota (guaranteed resources) always takes precedence over time-based fairshare

