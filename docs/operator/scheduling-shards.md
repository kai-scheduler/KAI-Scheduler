# Scheduling Shards

Scheduling shards partition a cluster into logical groups, each with a dedicated scheduler instance that operates on a subset of nodes, queues, and pod groups.

## How It Works

Each shard creates a separate KAI scheduler deployment that filters resources by the `partitionLabelValue`. The scheduler only considers resources with matching labels:

- **Nodes**: `<nodePoolLabelKey>=<partitionLabelValue>`
- **Queues**: Same label structure
- **Pod Groups**: Same label structure

The `nodePoolLabelKey` is configurable in the global configuration (default: `kai.scheduler/node-pool`). For example, with the default configuration, nodes are labeled as `kai.scheduler/node-pool=gpu-nodes`.

Shards operate independently with their own configuration for placement strategies, queue depths, and runtime requirements.

## Creating Scheduling Shards

### Basic Shard Default Configuration

This shard will define the partition of all nodes that are not labeled by the partition label:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: default
```

### Advanced Shard Configuration

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: gpu-shard
spec:
  partitionLabelValue: gpu-nodes
  
  # Custom scheduler arguments
  args:
    leader-elect: "true"
    log-level: "3"
  
  # Placement strategy
  placementStrategy:
    gpu: binpack
    cpu: binpack
  
  # Queue depth configuration
  queueDepthPerAction:
    preempt: 15
    reclaim: 8
    allocate: 25
  
  # Minimum runtime requirements
  minRuntime:
    preemptMinRuntime: "10m"
    reclaimMinRuntime: "5m"
```

For customizing which plugins and actions run in a shard (disabling, reordering, overriding arguments), see [Scheduler Config Customization](./scheduler-config-customization.md).

## Node Preparation

### Labeling Nodes

To use the scheduling shards, label your nodes appropriately:

```bash
# Label GPU nodes
kubectl label nodes node-1 kai.scheduler/node-pool=gpu-nodes
kubectl label nodes node-2 kai.scheduler/node-pool=gpu-nodes

# Label CPU nodes
kubectl label nodes node-3 kai.scheduler/node-pool=cpu-nodes
kubectl label nodes node-4 kai.scheduler/node-pool=cpu-nodes

# Label high-memory nodes
kubectl label nodes node-5 kai.scheduler/node-pool=high-memory-nodes
```

## Queue Configuration

### Shard-Specific Queues

Create queues that target specific shards:

```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: gpu-queue-parent
  labels:
    kai.scheduler/node-pool: gpu-nodes  # Targets GPU shard
spec:
  priority: 100
  resources:
    gpu:
      quota: -1
---
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: gpu-queue
  labels:
    kai.scheduler/node-pool: gpu-nodes  # Targets GPU shard
spec:
  parentQueue: gpu-queue-parent
  priority: 100
  resources:
    gpu:
      quota: 10
```

```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: cpu-queue
  labels:
    kai.scheduler/node-pool: cpu-nodes  # Targets CPU shard
spec:
  priority: 50
  parentQueue: cpu-queue-parent
  resourceQuota:
    cpu:
       quota: 200
    memory:
        quota: -1
```

## Job Submission

### Direct Shard Targeting

Jobs should be directly submitted to a specific shard:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  namespace: test
  labels:
    kai.scheduler/queue: foo-queue-test
    kai.scheduler/node-pool: foo
spec:
  schedulerName: kai-scheduler
  containers:
    - name: main
      image: ubuntu
      command: ["bash", "-c"]
      args: ["nvidia-smi; trap 'exit 0' TERM; sleep infinity & wait"]
      resources:
        limits:
          nvidia.com/gpu: "1"
```

The created pod group will have the same labels as the top owner of the pod, which will then include the node-pool label

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  annotations:
    kai.scheduler/top-owner-metadata: |
      name: gpu-pod
      uid:
      group: ""
      version: v1
      kind: Pod
  labels:
    kai.scheduler/queue: foo-queue-test
  name: pg-gpu-pod-d81e6f2c-8da7-4e61-8758-d8a2c38d2bfb
  namespace: test
  ownerReferences:
  - apiVersion: v1
    kind: Pod
    name: gpu-pod
    uid:
  uid:
spec:
  minMember: 1
  priorityClassName: train
  queue: foo-queue-test
```

The PodGroup's label can later be updated manually to direct the job to a different shard.

## Monitoring and Observability

### Shard Status

Monitor shard status and health:

```bash
# Check shard status
kubectl get schedulingshard
kubectl describe schedulingshard gpu-shard

# Check shard deployments
kubectl get deployments -n kai-system -l kai.scheduler/shard=gpu-shard
```

### Shard Logs

View logs for specific shards:

```bash
# View shard scheduler logs
kubectl logs -n kai-system deployment/kai-scheduler-gpu-shard
```
