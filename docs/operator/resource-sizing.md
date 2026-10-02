# Resource sizing

KAI Scheduler resource usage depends on cluster size and workload shape. A
cluster with fewer nodes can require more scheduler memory when it has more
Pods, larger jobs, or a larger submission backlog.

This guide provides tested starting profiles and sizing guidance for KAI
Scheduler services.

## Quick start

1. Estimate the values below for each important workload scenario.
2. Use a tested profile when the complete scenario fits its envelope.
3. Otherwise, calculate the scheduler memory limit and use the larger result.
4. Run a representative workload lifecycle before production rollout.

Do not combine unrelated maxima. For example, calculate a many-small-jobs
scenario separately from a single-large-job scenario, then use the largest
recommendation.

### Information to collect

| Input | Meaning |
| --- | --- |
| Nodes | Nodes usable by this scheduler shard |
| Total Pods | Peak cluster-wide Pod objects, including non-KAI Pods |
| Workloads | Peak simultaneously active KAI workloads |
| Average workload size | Average schedulable Pods per workload |
| Largest workload | Schedulable Pods in the largest workload |
| Total GPUs | GPUs available to these workloads |
| GPUs per worker Pod | Average GPU request per GPU worker Pod; use `1` when unsure |
| Eligible nodes | Nodes where these workloads can run; use all nodes when unsure |

For CPU-only workloads, replace GPU capacity with the estimated maximum number
of worker Pods that can run concurrently.

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

The profiles were exercised across these workload shapes. The values are
separate lifecycle maxima and did not all occur simultaneously.

| Workload property | 500 profile | 1000 profile |
| --- | ---: | ---: |
| Nodes | 520 | 1,008 |
| Total Pods | 46,000 | 90,000 |
| Active workloads | 8,000 | 16,000 |
| Largest workload | 500 Pods | 1,000 Pods |
| Average workload in the largest burst | 57 Pods | 102 Pods |
| GPUs | 4,000 | 8,000 |

Use the next profile or the calculator when an expected scenario is larger.
These values are starting points, not capacity guarantees. Pod shape, storage,
DRA, topology constraints, GPU sharing, and enabled plugins can change resource
usage.

## Calculate scheduler memory

Use the [resource sizing calculator](https://kai-scheduler.github.io/KAI-Scheduler/resource-sizing/)
to get initial resources for the Scheduler, Binder, and controllers. It also
generates a command that patches Config-managed services. The calculator runs
entirely in the browser and does not send cluster information anywhere.

For the formula, assumptions, pressure tiers, and a worked example, see the
[scheduler memory sizing deep dive](./scheduler-memory-sizing.md).

## Size Binder and controllers

Use these conservative initial memory requests. Round up and keep the selected
profile's limit until a complete-cycle test supports changing it.

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

The calculator rounds formula results up to 128 MiB, never below the selected
profile request. When a formula raises a request, it preserves the profile's
request-to-limit headroom.

Use the deep dive's [`workloadPods`](./scheduler-memory-sizing.md#infer-workload-pods-and-capacity)
as an initial BindRequest upper bound when no measurement is available. Do not
add allowances for multiple dimensions that describe the same object
population.

## Configure resources

The following example applies the tested 1000-node profile:

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

Resources can also be managed through `spec.<service>.service.resources` on the
Config custom resource. The calculator generates this patch. The operator
itself is configured through Helm at `operator.resources`. If Helm or GitOps
owns the Config, update that source of truth instead of applying a direct patch.

## Validate in your environment

The tested profiles and calculator results are starting points, not guarantees.
Cluster administrators must validate them against a representative workload
lifecycle before relying on them in production. Every deployment has different
workload shapes, plugins, placement constraints, and submission patterns.

This is deployment validation performed by the platform operator; it does not
require running KAI's development scale-test suite.

Run cluster fill, the largest jobs, a submission burst, reclaim or preemption,
and cleanup. Monitor:

- container working set, RSS, and Go heap;
- CPU usage and throttling;
- restarts and termination reasons;
- scheduling latency and controller workqueue depth;
- Pods, Jobs, PodGroups, BindRequests, queues, and storage/DRA objects.

Useful PromQL:

```promql
max_over_time(container_memory_working_set_bytes{namespace="kai-scheduler",container!="POD"}[24h])

quantile_over_time(0.95, container_memory_working_set_bytes{namespace="kai-scheduler",container!="POD"}[24h])

sum by (container) (rate(container_cpu_usage_seconds_total{namespace="kai-scheduler",container!="POD"}[5m]))

max by (name, pod) (workqueue_depth{namespace="kai-scheduler"})
```

| Symptom | Action |
| --- | --- |
| Memory rises during a large job | Increase scheduler limit or reduce eligible nodes with scheduler sharding. |
| Memory rises during submission bursts | Increase scheduler memory or reduce simultaneous submissions. |
| Memory remains high after cleanup | Wait for active scheduling and garbage collection; use complete-cycle peaks. |
| Binder memory and BindRequest depth rise together | Increase Binder resources and investigate binding throughput. |
| Workqueue grows while CPU is throttled | Raise CPU request and remove or increase CPU limit. |

Prefer no CPU limit for latency-sensitive controllers. If policy requires one,
measure demand without throttling first. Repeat validation after changing KAI,
Kubernetes, shards, plugins, storage, DRA, GPU sharing, or workload shape.

The [scale-test guide](../developer/scale-tests.md) provides examples for
building representative workload tests.

## Vertical Pod Autoscaler

KAI can create VPA policies when VPA is installed. Start with `updateMode: Off`
and observe recommendations through a complete workload cycle.

The default VPA maximum of 2 CPUs and 5 GiB is too low for the 1000-node
scheduler profile. Raise the service-specific bounds before enabling VPA for
that profile.

For the Scheduler, prefer `controlledValues: RequestsOnly` when keeping a
validated static limit. If VPA lowers a scheduler Pod's memory limit in place,
the cgroup limit can fall before the process has released memory. KAI updates
the Go memory target after observing the cgroup change, but the Pod can be
OOM-killed before garbage collection releases enough memory. Reduce the
scheduler limit only after complete-cycle measurements show that it is safe.

See the upstream [VPA mode documentation](https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler/docs/quickstart.md)
and [Scheduling shards](./scheduling-shards.md#go-memory-limit).

## Optional operands

Optional operands were not included in the profiles:

- Size node scale adjuster from total Pods and peak Pod-update rate.
- NUMA placement exporter runs once per selected node; multiply its resources
  by selected node count.
- Reserve capacity for the maximum simultaneous resource-reservation Pods.
