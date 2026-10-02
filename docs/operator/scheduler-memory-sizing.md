# Scheduler memory sizing

This page explains the formula behind the
[resource sizing calculator](https://kai-scheduler.github.io/KAI-Scheduler/resource-sizing/).
Use the calculator for an initial recommendation and the
[resource sizing guide](./resource-sizing.md) for tested profiles,
configuration, and validation guidance.

The calculation separates memory into two parts:

- `cache`: cluster objects the scheduler keeps in memory;
- `scheduling reserve`: temporary memory used while evaluating the largest job,
  handling a backlog, and simulating reclaim or constrained placement.

## Infer workload Pods and capacity

```text
workloadPods = min(totalPods, round up(workloads * averageWorkloadSize))

workerCapacity = floor(totalGPUs / GPUsPerWorkerPod)
backlogRatio = workloadPods / workerCapacity
```

The scheduler creates or reads several internal objects for a workload. Users
do not need to count them. The calculation assumes approximately one PodGroup
per workload and at most one active BindRequest per workload Pod. BindRequests
are per Pod, not per workload.

## Calculate cached-object memory

All values in this formula are GiB:

```text
cache = 1
      + 0.16 * totalPods / 10,000
      + 0.01 * workloads / 1,000
      + 0.02 * eligibleNodes / 1,000
      + 0.04 * workloadPods / 10,000
```

The 1-GiB base covers the scheduler process, informers, queues, plugins, and
smaller Kubernetes objects. Every scheduler shard watches cluster-wide Pods,
so sharding does not divide the `totalPods` term.

## Calculate scheduling reserve

When scheduling a job, KAI evaluates its Pods against eligible nodes. A larger
job or a larger node pool creates more temporary scoring, predicate, and
simulation work.

```text
largestSearch = largestWorkloadPods * eligibleNodes
```

Use `largestSearch` to select a reserve:

| Largest Pod-node search | Search reserve |
| ---: | ---: |
| Up to 4,000 | 0.5 GiB |
| Up to 16,000 | 1 GiB |
| Up to 64,000 | 1.5 GiB |
| Up to 256,000 | 2 GiB |
| Up to 1,024,000 | 2.5 GiB |
| Each further 4x increase | Add 0.5 GiB |

This is a pressure tier, not a retained `Pods x nodes` matrix. KAI processes
tasks sequentially and can stop a job attempt after a failure. The tier leaves
room for temporary allocations and garbage-collection delay without assuming
that every task-node result remains in memory.

Add a backlog reserve:

| Backlog ratio | Backlog reserve |
| ---: | ---: |
| `<= 1` | 0 GiB |
| `> 1` and `<= 4` | 0.5 GiB |
| `> 4` | 1 GiB |

Add a workload-complexity reserve:

| Workload behavior | Complexity reserve |
| --- | ---: |
| Standard placement | 0 GiB |
| Topology-heavy or frequently uses reclaim/preemption | 0.5 GiB |
| Both, or uses advanced placement integrations extensively | 1 GiB |

Topology-heavy includes strong affinity, anti-affinity, or topology
constraints. Advanced placement includes large DRA, NUMA, or storage-aware
workloads. Reclaim and preemption require KAI to simulate potential victims
before changing the cluster.

```text
schedulingReserve = searchReserve
                  + backlogReserve
                  + complexityReserve
```

## Calculate request and limit

```text
memoryLimitGiB = round up(1.25 * (cache + schedulingReserve))
memoryRequestGiB = round up(0.75 * memoryLimitGiB)
```

The request is a conservative starting value when no measurements exist.
Replace it with at least 15% above sustained p95 memory after observing a
representative lifecycle. Keep the calculated limit until complete-cycle peaks
support reducing it.

## Example: 1000-node burst

```text
nodes and eligible nodes = 1,008
total Pods = 90,193
workloads = 900
average workload = 102.3 Pods
largest workload = 512 Pods
total GPUs = 8,000
GPUs per worker Pod = 8

workloadPods = 90,193
workerCapacity = 1,000
backlogRatio = 90.2
largestSearch = 516,096

cache = 2.83 GiB
search reserve = 2.5 GiB
backlog reserve = 1 GiB
complexity reserve = 0 GiB

memory limit = round up(1.25 * (2.83 + 3.5)) = 8 GiB
memory request = round up(0.75 * 8) = 6 GiB
```

This is an initial calculation. The tested 1000-node profile uses a 7-GiB
request because sustained use in that environment was higher.
