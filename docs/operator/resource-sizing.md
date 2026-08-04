# Resource sizing

KAI Scheduler resource usage depends on Kubernetes object counts and scheduling
pressure, not only on the number of nodes. Use this guide to select a starting
profile, configure resources, and tune them with measurements from the target
cluster.

## Quick start

1. Record the expected workload envelope: nodes, Pods, Pending Pods,
   PodGroups, BindRequests, and queues.
2. Select the smallest profile below that covers every expected maximum.
3. Apply the profile and run a representative workload lifecycle, including
   cluster fill, large gangs, reclaim or preemption, and cleanup.
4. Tune requests from sustained usage and limits from complete-cycle peaks.

### Reference workload envelopes

The profiles in this guide were exercised against these approximate maximums:

| Sizing input | 500-node profile | 1000-node profile |
| --- | ---: | ---: |
| Nodes per scheduler shard | 520 | 1,008 |
| Total Pod objects | 46,000 | 90,000 |
| Pending Pods | 38,000 | 77,000 |
| Pending Pods x nodes[^pressure] | 20 million | 78 million |
| PodGroups | 8,000 | 16,000 |
| BindRequests | 14,000 | 14,000 |
| Queues | 8 | 8 |

[^pressure]: This is a conservative scheduler-pressure proxy. When available,
    use attempted Pods multiplied by their effective candidate-node count.

Use the next profile or run a dedicated scale test if any expected maximum is
larger. A cluster with fewer nodes can still require the larger profile when it
has more Pods, larger submission bursts, or more PodGroups.

## Starting profiles

Values are `request / limit`. `omit` means that no CPU limit should be set
until an uncapped load test establishes actual demand.

| Service | 500 CPU | 500 memory | 1000 CPU | 1000 memory |
| --- | ---: | ---: | ---: | ---: |
| Scheduler | `2 / 4` | `4Gi / 7Gi` | `3 / 5` | `7Gi / 8Gi` |
| Binder | `250m / 1` | `3Gi / 4Gi` | `1 / 2` | `5Gi / 6Gi` |
| Pod grouper | `250m / 1` | `1500Mi / 2Gi` | `500m / 2` | `3Gi / 4Gi` |
| PodGroup controller | `500m / omit` | `2Gi / 3Gi` | `1 / omit` | `3500Mi / 4Gi` |
| Queue controller | `250m / omit` | `256Mi / 512Mi` | `500m / omit` | `400Mi / 512Mi` |
| Admission, per replica | `50m / 250m` | `64Mi / 128Mi` | `50m / 250m` | `64Mi / 128Mi` |
| Operator | `25m / 100m` | `128Mi / 256Mi` | `25m / 100m` | `128Mi / 256Mi` |

These are starting envelopes, not minimum requirements or capacity guarantees.
Pod shape, scheduler plugins, storage integrations, DRA, GPU sharing, queue
distribution, and event rate can change resource usage.

The profiles assume one default scheduler shard. Size each additional shard
from its selected nodes and eligible workload. Admission values are per replica;
size shared services for cluster-wide object counts.

### Helm configuration

The following example applies the 1000-node profile:

```yaml
scheduler:
  resources:
    requests: {cpu: "3", memory: 7Gi}
    limits: {cpu: "5", memory: 8Gi}
binder:
  resources:
    requests: {cpu: "1", memory: 5Gi}
    limits: {cpu: "2", memory: 6Gi}
podgrouper:
  resources:
    requests: {cpu: 500m, memory: 3Gi}
    limits: {cpu: "2", memory: 4Gi}
podgroupcontroller:
  resources:
    requests: {cpu: "1", memory: 3500Mi}
    limits: {memory: 4Gi}
queuecontroller:
  resources:
    requests: {cpu: 500m, memory: 400Mi}
    limits: {memory: 512Mi}
admission:
  resources:
    requests: {cpu: 50m, memory: 64Mi}
    limits: {cpu: 250m, memory: 128Mi}
operator:
  resources:
    requests: {cpu: 25m, memory: 128Mi}
    limits: {cpu: 100m, memory: 256Mi}
```

Operand resources can also be managed through the equivalent
`spec.<service>.service.resources` fields on the Config custom resource. The
operator itself is configured through the Helm chart. Use one owner for each
field so Helm and another controller do not continuously overwrite each other.

## Adapt a profile

### Scheduler

Scheduler memory has two parts:

```text
object footprint = base
                 + non-terminal Pod memory
                 + shard PodGroup memory
                 + selected-node memory
                 + BindRequest and other cached-object memory

action pressure = C * attempted Pods * effective candidate nodes
```

`C` is workload-specific. It changes with the scheduling action, enabled
plugins, Pod shape, gang structure, reclaim candidates, and Go garbage
collection. PodGroups organize attempted Pods and should not be multiplied by
the task-node product again.

For capacity planning, use this conservative form:

```text
scheduler limit >= headroom * maximum over a complete scheduling session(
    object footprint + action pressure
)
```

When attempted-Pod or candidate-node metrics are unavailable, use
`Pending Pods x nodes per shard` as an upper-bound pressure proxy. Use the
maximum over the workload lifecycle rather than the value visible when memory
peaks; a long scheduling session can retain an earlier object snapshot.

If scheduler pressure exceeds the selected profile:

- increase the memory limit for higher burst peaks and raise the request when
  sustained usage also increases;
- split selected nodes across scheduler shards to reduce candidate nodes per
  task;
- stagger large submissions or reduce the unschedulable backlog;
- retest after enabling plugins or changing reclaim and preemption behavior.

Every scheduler process caches cluster-wide non-terminal Pods. Sharding reduces
node-evaluation work but does not divide that Pod-cache footprint.

### Binder and controllers

The following formulas provide conservative initial **memory requests**. Round
up and keep the profile's limit until a complete-cycle test supports changing
it.

```text
Binder:
  256Mi + max(50Mi * total Pods / 1000,
              100Mi * outstanding BindRequests / 1000)

Pod grouper:
  256Mi + 25Mi * retained KAI Pods / 1000

PodGroup controller:
  256Mi + 35Mi * retained KAI Pods / 1000

Queue controller:
  64Mi + 20Mi * PodGroups / 1000
```

Do not add allowances for multiple dimensions that describe the same object
population. That double-counts correlated growth. Use retained objects rather
than only Running Pods for controllers that retain terminal Pods.

### Requests and limits

For a workload not covered by a reference profile:

1. Set memory requests at least 15% above the sustained p95 working set.
2. Set memory limits at least 25% above the complete-cycle peak and round up.
3. Set CPU requests at least 25% above uncapped p95 usage.
4. Prefer no CPU limit for latency-sensitive controllers. If policy requires
   one, measure demand without throttling first.
5. Increase requests when workqueue depth or processing latency grows under a
   steady load, even if average CPU or memory looks low.

Repeat validation after KAI or Kubernetes upgrades and after changing shards,
plugins, storage, DRA, GPU sharing, or workload submission patterns.

## Validate and troubleshoot

Monitor a complete workload lifecycle. At minimum, retain:

- container working set, RSS, and Go heap;
- CPU usage and throttling;
- restarts and termination reasons;
- scheduler latency and Binder/controller workqueue depth;
- Nodes, Pod phases, PodGroups, BindRequests, Jobs, queues, and storage/DRA
  objects.

Useful PromQL expressions include:

```promql
max_over_time(container_memory_working_set_bytes{namespace="kai-scheduler",container!="POD"}[24h])

quantile_over_time(0.95, container_memory_working_set_bytes{namespace="kai-scheduler",container!="POD"}[24h])

sum by (container) (rate(container_cpu_usage_seconds_total{namespace="kai-scheduler",container!="POD"}[5m]))

sum by (container) (rate(container_cpu_cfs_throttled_periods_total{namespace="kai-scheduler",container!="POD"}[5m]))
/
sum by (container) (rate(container_cpu_cfs_periods_total{namespace="kai-scheduler",container!="POD"}[5m]))

max by (pod) (process_resident_memory_bytes{namespace="kai-scheduler"})

max by (name, pod) (workqueue_depth{namespace="kai-scheduler"})

max by (resource) (apiserver_storage_objects{resource=~"pods|jobs.batch|podgroups.scheduling.run.ai|bindrequests.scheduling.run.ai|queues.scheduling.run.ai|nodes"})
```

| Symptom | Recommended action |
| --- | --- |
| Scheduler memory rises with a Pending-Pod burst | Increase the scheduler envelope, reduce nodes per shard, or stagger submissions. |
| Scheduler remains large after cleanup | Wait for active scheduling sessions and GC; do not downsize from a short idle sample. |
| Binder memory and BindRequest depth rise together | Increase Binder memory/CPU and investigate binding throughput, API throttling, volumes, and DRA. |
| Controller workqueue grows while CPU is throttled | Raise the CPU request and remove or increase the CPU limit. |
| Pod grouper or PodGroup controller grows with retained Pods | Clean up completed workloads where appropriate and apply the object-count formula. |
| VPA recommends less than the validated limit | Use `RequestsOnly` and keep the static safety limit. |

The [scale-test guide](../developer/scale-tests.md) describes how to exercise a
representative workload. The scale environment configuration is in
[`hack/setup-scale-test-env.sh`](../../hack/setup-scale-test-env.sh).

## Vertical Pod Autoscaler

KAI does not install Vertical Pod Autoscaler (VPA). When VPA is installed, KAI
can create policies for its operands.

Use VPA safely:

1. Start with `updateMode: Off` and observe `target`, `lowerBound`,
   `upperBound`, and `uncappedTarget` through a complete workload cycle.
2. Set service-specific minimum and maximum bounds. The default global maximum
   of 2 CPUs and 5 GiB is too low for the 1000-node scheduler profile, while its
   500-MiB minimum is unnecessarily high for smaller services.
3. Set `controlledValues` explicitly. Prefer `RequestsOnly` when retaining the
   static limits from this guide.
4. Enable `InPlaceOrRecreate` only after validating the policy and the
   cluster's in-place resize support.
5. Avoid sharp memory-limit reductions.

Scheduler shards derive `GOMEMLIMIT` from their cgroup memory limit using
`goMemLimitRatio` (default `0.9`) and refresh it every 15 seconds. A reduced
container limit can take effect before the process releases enough memory.
Other KAI operands do not currently set `GOMEMLIMIT` automatically and should
be downscaled conservatively.

The upstream [VPA mode documentation](https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler/docs/quickstart.md)
describes `Off` and `InPlaceOrRecreate` behavior. See
[Scheduling shards](./scheduling-shards.md#go-memory-limit) for scheduler
Go-memory configuration.

## Optional operands

Optional operands were not included in the reference profiles and must be
validated separately:

- The node scale adjuster caches Pods and scans for unschedulable fractional-GPU
  work. Size it from total Pods and peak Pod-update rate.
- The NUMA placement exporter runs once per selected node. Multiply its
  per-Pod resources by the selected node count for cluster capacity planning.
- Resource-reservation Pods are created for active node/GPU-group combinations.
  Reserve their per-Pod resources multiplied by the maximum simultaneous
  reservation-Pod count.
