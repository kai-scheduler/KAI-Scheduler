# Resource sizing

KAI Scheduler resource usage depends on the number of Kubernetes objects and
their update rate, not only on the number of nodes. Use the recommendations in
this document as starting points, measure the target workload, and adjust them
before production rollout.

## Sizing inputs

Collect the peak and steady-state values for the following inputs. Size every
scheduler shard independently. Node evaluation follows the shard's node pool,
but each scheduler process currently caches cluster-wide non-terminal Pods.

| Service | Primary sizing inputs | Why they matter |
| --- | --- | --- |
| Scheduler | Nodes per shard, cluster-wide non-terminal Pods, attempted pending Pods, unschedulable Pods, PodGroups, queues, BindRequests, storage and DRA objects | The scheduler caches cluster objects, builds a session snapshot, and evaluates attempted Pods against candidate nodes. Fit-error and scoring work adds action-specific pressure shaped by both attempted Pods and candidate nodes. |
| Binder | Total Pod objects and their size, outstanding BindRequests, binding rate, volumes, storage and DRA objects, GPU-sharing groups | The binder maintains controller-runtime and client-go caches. Binding work and workqueue backlog can grow independently of the node count. |
| Pod grouper | KAI-scheduled Pods and their size, workload owners, owner-chain depth, Pod and workload update rate | Each KAI Pod is associated with a top-level workload and PodGroup. Owner discovery can require API reads. |
| PodGroup controller | KAI-scheduled Pods and their size, PodGroups, maximum gang size, Pod status update rate | A PodGroup status reconciliation lists and scans its member Pods. Large gangs and simultaneous Pod updates create bursts. |
| Queue controller | Queues, queue-tree depth, PodGroups per queue, status update rate | Queue status updates list child queues and PodGroups. Distribution across queues matters as much as the total count. |
| Admission | Admission request rate, object size and rule complexity, replica count | Admission is request-driven and does not keep a cluster-sized scheduling cache. |
| Node scale adjuster | Total Pod objects, KAI Pod event rate, unschedulable fractional-GPU Pods, scaling Pods | The controller caches Pods and serializes adjustments. Each adjustment lists the Pod cache to find unschedulable work. |
| NUMA placement exporter | Selected nodes, Pods and allocated devices per node, poll intervals | One DaemonSet Pod polls the local kubelet allocation API on each selected node and periodically checks that node's Pods through the API server. |
| Resource-reservation Pods | Active node and GPU-group combinations | The binder creates a reservation Pod for each active node/GPU-group combination. Their configured resources multiply by concurrent reservation Pods. |
| Operator | Scheduler shards, KAI custom resources, operand changes | The operator is mainly reconciliation-driven. Its usage normally grows more slowly than the data-plane services. |

Consequently, a node-only formula is unsafe. Two clusters with the same number
of nodes can need different resources when one retains more terminal Pods,
creates larger gangs, or has a higher object update rate.

## Scale-test measurements

The following measurements came from synthetic scale runs in August 2026. The
500-node run used KAI commit `bf64586d23b9f19fdfa105e2706ad00906b7ce73`;
the 1000-node run used commit `8673bb3`. They are different test runs and are
not a controlled node-count-only comparison.

Prometheus samples were evaluated in five-minute steps over each run. CPU is
cores consumed and memory is container working set. Admission CPU is the sum
across four replicas; its memory is the largest replica.

| Peak object count | 500-node profile | 1000-node profile |
| --- | ---: | ---: |
| Nodes | 520 | 1,008 |
| Pods | 46,076 | 90,458 |
| Pending Pods | 37,674 | 77,420 |
| Running Pods | 4,086 | 8,086 |
| Jobs | 8,006 | 16,006 |
| PodGroups | 8,005 | 16,005 |
| Queues | 8 | 8 |
| BindRequests | 14,248 | 13,694 |

The measurements were taken with the following installed resources. Values are
`request / limit`; each side contains CPU and memory. The product defaults are
not a large-cluster sizing profile.

| Service | 500-node run | 1000-node run |
| --- | --- | --- |
| Scheduler | `3, 7Gi / 5, 7Gi` | `3, 7Gi / 5, 7Gi` |
| Binder | `100m, 2500Mi / 400m, 2500Mi` | `1, 8Gi / 2, 8Gi` |
| Pod grouper | `50m, 2000Mi / 200m, 2000Mi` | `1, 2Gi / 2, 2Gi` |
| PodGroup controller | `50m, 8Gi / 200m, 8Gi` | `50m, 8Gi / 200m, 8Gi` |
| Queue controller | `50m, 256Mi / 100m, 512Mi` | `50m, 256Mi / 100m, 512Mi` |
| Admission, per replica | `50m, 256Mi / 100m, 512Mi` | `50m, 256Mi / 100m, 512Mi` |
| Operator | unset | unset |

| Service | 500 CPU p95 / max | 500 memory p95 / max | 1000 CPU p95 / max | 1000 memory p95 / max |
| --- | ---: | ---: | ---: | ---: |
| Scheduler | 1.61 / 1.93 | 2.68 / 5.49 GiB | 2.23 / 2.38 | 6.00 / 6.08 GiB |
| Binder | 0.087 / 0.119 | 1.61 / 1.88 GiB | 0.093 / 0.131 | 3.10 / 3.42 GiB |
| Pod grouper | 0.113 / 0.148 | 0.96 / 1.08 GiB | 0.176 / 0.282 | 1.68 / 1.77 GiB |
| PodGroup controller | 0.200 / 0.200[^cpu-capped] | 1.12 / 1.29 GiB | 0.200 / 0.200[^cpu-capped] | 1.96 / 2.49 GiB |
| Queue controller | 0.098 / 0.100[^cpu-capped] | 84 / 137 MiB | 0.100 / 0.100[^cpu-capped] | 172 / 288 MiB |
| Admission | 0.042 / 0.124 | 19 / 21 MiB | 0.061 / 0.163 | 20 / 22 MiB |
| Operator | 0.008 / 0.012 | 89 / 91 MiB | 0.006 / 0.011 | 101 / 109 MiB |

[^cpu-capped]: The container was CPU-throttled in nearly all measured periods,
    so these values represent the configured limit rather than CPU demand. The
    500-node Pod grouper was also throttled in about 26% of periods.

The largest scheduler, binder, Pod grouper, and PodGroup controller working
sets occurred during the NCCL phase, when the tests retained the most Pods.
The mass-reclaim phases retained the most PodGroups. Queue-controller memory
also peaked during those reclaim phases.
Binder workqueue depth reached 4,006 in the 500-node run and 9,005 in the
1000-node run. These correlations are why Pods, gangs, BindRequests, update
rate, and workload shape must be captured alongside node count.

### Empirical memory signals

The following univariate relationships were stable enough to help construct an
initial memory request. They are ordinary correlations within each run, not
causal models; object dimensions often change together.

| Service and dimension | 500-node correlation / slope | 1000-node correlation / slope |
| --- | ---: | ---: |
| Binder vs total Pod objects | 0.959 / 38.6 MiB per 1,000 | 0.857 / 39.5 MiB per 1,000 |
| Pod grouper vs total Pod objects | 0.958 / 24.8 MiB per 1,000 | 0.898 / 17.9 MiB per 1,000 |
| PodGroup controller vs total Pod objects | 0.983 / 30.1 MiB per 1,000 | 0.967 / 29.8 MiB per 1,000 |
| Queue controller vs PodGroups | 0.969 / 13.5 MiB per 1,000 | 0.981 / 15.2 MiB per 1,000 |
| Scheduler vs total Pod objects | 0.341 / 26.6 MiB per 1,000 | 0.788 / 69.9 MiB per 1,000 |

The scheduler relationship changed too much between runs to use as a sizing
coefficient. Its memory must be measured with representative scheduling
actions, Pod shapes, gang sizes, and pending-task pressure.

### Scheduler memory model

Scheduler memory has an additive object footprint and an action-specific
transient footprint:

```text
session footprint ~= base
                  + pod coefficient * cluster-wide non-terminal Pods
                  + PodGroup coefficient * shard PodGroups
                  + node coefficient * selected nodes
                  + BindRequest and other cached-object terms

transient pressure = f(attempted tasks, candidate nodes per task,
                       action and plugin path, victim/scenario state, GC state)
```

The scheduler Pod informer excludes terminal Pods but otherwise watches all
namespaces. Each scheduling session constructs `PodInfo`, node, PodGroup, and
BindRequest indexes. Scoring and predicates then inspect candidate nodes for
each attempted task. The upper-bound product `attempted tasks * candidate
nodes` is therefore a useful pressure signal, but it is not a byte formula:
feasibility filters, gang shape, plugins, and the selected action change the
work performed.

The two scale runs cannot identify independent coefficients for Pods,
PodGroups, and nodes because those dimensions increased together. A phase-level
fit using non-terminal Pods alone produced a descriptive `0.729 GiB + 0.809
GiB per 10,000 Pods`, but its leave-one-phase-out error was 1.27 GiB. Adding
PodGroups, nodes, BindRequests, or a pending-by-node interaction improved the
in-sample fit while producing unstable or negative coefficients and did not
improve held-out accuracy enough for sizing. Do not use those fitted
coefficients as a production formula.

Explicit products performed worse. `Pods * nodes` had 2.25 GiB held-out error;
`Pods * PodGroups` had 1.65 GiB; and `Pods * nodes * PodGroups` had 1.58 GiB.
Adding product terms to the additive model overfit the observed phases and
produced unstable coefficient signs. The source hot path is better represented
as `sum(candidate nodes for each attempted task)`: PodGroups partition the
attempted tasks and should not normally be multiplied by that total again.

Use high-water counts over a complete scheduling session. In the 500-node NCCL
phase, non-terminal Pods peaked at 38,177 when scheduler RSS was 1.97 GiB, but
RSS peaked at 5.54 GiB 70 minutes later, after the visible non-terminal count
had fallen to 5,885. The corresponding 1000-node lag was 31 minutes. A session
snapshot remains reachable until its actions finish, and Go can retain heap
pages after object counts fall, so an instantaneous object-count formula would
underestimate the limit.

For a new workload shape, model the scheduler limit as:

```text
limit >= headroom * max over a complete scheduling session(
  additive session footprint + action-specific transient pressure
)
```

Calibrating the coefficients requires controlled runs that vary nodes, Pods,
and Pods per PodGroup independently, followed by runs that vary attempted
unschedulable tasks and candidate nodes while retained counts stay fixed.
Capture heap-live/in-use, allocation rate, GC target, session/action duration,
and a heap profile in addition to container working set.

For the other services, the following conservative formulae can provide a
pre-test memory request. Round the result up. The coefficients include some
headroom above the observed slopes:

```text
Binder:             256Mi + max(50Mi * total Pods / 1000,
                                100Mi * outstanding BindRequests / 1000)
Pod grouper:        256Mi + 25Mi * retained KAI Pods / 1000
PodGroup controller:256Mi + 35Mi * retained KAI Pods / 1000
Queue controller:   64Mi + 20Mi * PodGroups / 1000
```

Do not add univariate allowances for dimensions that represent the same object
population; that double-counts correlated growth. Validate the result against
the peak of a complete workload cycle. Terminal KAI Pods remain relevant to
the Pod grouper and PodGroup-controller caches, so use retained objects rather
than only running Pods. Almost all Pods at the measured object peaks were KAI
scale-test Pods. Clusters with many non-KAI Pods must distinguish total and
KAI-scheduled populations as shown in the sizing-input table. The synthetic
Pods were also relatively small. Pods with many containers, environment
references, volumes, annotations, or large status fields can require more cache
memory per object, especially in services that retain full Pod objects.

## Starting resources

The following values are provisional starting envelopes for workloads no
larger than the profiles above. They include headroom over the observed memory
peaks. They have not been validated as minimums or as capacity guarantees.

`request / limit` is shown in each cell. `omit` means that no CPU limit should
be configured until an uncapped load test establishes actual demand; a CPU
request is still required for placement and capacity planning.

| Service | 500 CPU | 500 memory | 1000 CPU | 1000 memory |
| --- | ---: | ---: | ---: | ---: |
| Scheduler | `2 / 4` | `4Gi / 7Gi` | `3 / 5` | `7Gi / 8Gi` |
| Binder | `250m / 1` | `3Gi / 4Gi` | `1 / 2` | `5Gi / 6Gi` |
| Pod grouper | `250m / 1` | `1500Mi / 2Gi` | `500m / 2` | `3Gi / 4Gi` |
| PodGroup controller | `500m / omit` | `2Gi / 3Gi` | `1 / omit` | `3500Mi / 4Gi` |
| Queue controller | `250m / omit` | `256Mi / 512Mi` | `500m / omit` | `400Mi / 512Mi` |
| Admission, per replica | `50m / 250m` | `64Mi / 128Mi` | `50m / 250m` | `64Mi / 128Mi` |
| Operator | `25m / 100m` | `128Mi / 256Mi` | `25m / 100m` | `128Mi / 256Mi` |

Memory recommendations have stronger evidence than CPU recommendations. The
PodGroup and queue controllers never exposed their uncapped CPU demand in
these runs. Their CPU requests are conservative placement starting points, not
measured saturation points, and must not be converted into limits without an
uncapped test.

### Configure static resources

The Helm chart accepts a `resources` block under each service. For example,
the 1000-node starting profile can be expressed as:

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

Apply the values with the normal Helm install or upgrade workflow. When
managing operand resources through the Config custom resource directly, set
the equivalent `spec.<service>.service.resources` fields. The operator itself
is configured through the chart Deployment rather than its Config CR. Avoid
having Helm and another controller continuously overwrite the same Config
fields.

These profiles assume one default scheduler shard and four admission replicas.
For multiple shards, size each scheduler independently using its selected nodes
and attempted workload, while still accounting for the cluster-wide
non-terminal Pod cache in every scheduler process. Shared services must be
sized for their cluster-wide object population.

### Optional operands and dynamic Pods

The node scale adjuster and NUMA placement exporter were not enabled in these
runs. The following guidance is source-derived and has lower confidence than
the measured core-service profiles.

The node scale adjuster keeps a controller-runtime Pod cache. A relevant Pod
event triggers a serialized adjustment that lists scaling Pods and then all
Pods to find unschedulable fractional-GPU work. Its default `50m`, `256Mi`
request and `100m`, `512Mi` limit have not been validated at these object
counts. A conservative pre-test memory request is:

```text
Node scale adjuster: 256Mi + 25Mi * total Pods / 1000
```

This produces approximately 1.5 GiB for the 500-node profile and 2.5 GiB for
the 1000-node profile after rounding. Start CPU requests at 500m and 1 CPU,
respectively, omit the CPU limit, and test the peak Pod-update burst. These CPU
values are source-derived test inputs, not observed recommendations.

The NUMA placement exporter defaults to `50m`, `64Mi` request and `200m`,
`128Mi` limit **per selected node**. Start with those per-Pod values and test a
node with the maximum expected local Pod and allocated-device density. If all
nodes are selected, the aggregate budget is:

| Nodes | Approximate aggregate request | Approximate aggregate limit |
| ---: | ---: | ---: |
| 500 | 25 CPUs, 32 GiB | 100 CPUs, 64 GiB |
| 1,000 | 50 CPUs, 64 GiB | 200 CPUs, 128 GiB |

Resource-reservation Pods are created dynamically by the binder. CPU and
memory have no explicit defaults. Configure and test their per-Pod resources,
then reserve `per-Pod resources * maximum simultaneous node/GPU-group pairs`
in cluster capacity planning.

For a different workload envelope:

1. Start memory requests at least 15% above the observed p95 working set.
2. Start memory limits at least 25% above the observed peak, rounded up to an
   operationally convenient value.
3. Start CPU requests at least 25% above uncapped p95 CPU usage. Increase them
   further when workqueue depth or processing latency grows during a steady
   load.
4. Prefer omitting CPU limits for latency-sensitive controllers. If policy
   requires limits, run the representative workload without throttling first
   and leave enough burst headroom.
5. Repeat the full workload lifecycle. Initial fill, mass deletion or reclaim,
   large gangs, and retained terminal objects can peak at different times.

Repeat this process after KAI or Kubernetes upgrades and after enabling new
scheduler plugins, storage integrations, DRA, GPU sharing, or additional
shards. Cache contents and allocation behavior can change between versions.

Do not extrapolate memory linearly from node count unless Pods, PodGroups,
BindRequests, queues, storage objects, and update rates scale by the same ratio.
The two measured Binder profiles demonstrate this: the 1000-node cluster had
approximately twice as many Pods but slightly fewer peak BindRequests.

## Vertical Pod Autoscaler

KAI does not install the Kubernetes Vertical Pod Autoscaler (VPA). When its CRD
and controllers are installed, KAI can create a VPA for the scheduler, binder,
Pod grouper, PodGroup controller, queue controller, admission, and node scale
adjuster operands.

VPA is disabled by default. The Helm chart exposes one `global.vpa` policy that
is inherited by the supported operands. The default policy uses
`InPlaceOrRecreate`, permits a minimum of `50m` CPU and `500Mi` memory, and caps
request recommendations at 2 CPUs and 5 GiB. Those maximums are below the observed
1000-node scheduler CPU p95 and memory peak, while the 500-MiB minimum is far
above the observed admission and queue-controller working sets. One unchanged
global policy therefore both caps the largest service too low and raises the
smallest services unnecessarily.

The policy also omits `controlledValues`, whose VPA default is
`RequestsAndLimits`. In that mode VPA adjusts limits proportionally to requests;
`maxAllowed` caps the recommendation but is not a hard ceiling on the resulting
container limit. Use `controlledValues: RequestsOnly` when limits must remain
the statically validated safety envelope.

The upstream [VPA mode documentation](https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler/docs/quickstart.md)
states that `Off` continues calculating recommendations without applying them.
`InPlaceOrRecreate` attempts an in-place update and falls back to eviction and
recreation when it cannot resize the Pod in place.

Use VPA safely as follows:

1. Enable it with `updateMode: Off` first and observe recommendations through a
   complete workload cycle. Compare `target`, `lowerBound`, `upperBound`, and
   `uncappedTarget` rather than copying one instantaneous value.
2. Set service-specific minimum and maximum bounds that include the measured
   burst envelope. Service-specific VPA policies are available on the Config
   custom resource even though the Helm values currently expose only the
   global policy.
3. Choose `RequestsOnly` or `RequestsAndLimits` explicitly. Prefer
   `RequestsOnly` when applying the static limits from this guide.
4. Check recommendations against workqueue latency and CPU throttling. VPA
   samples consumption, which can understate demand for a CPU-limited service.
5. Change to `InPlaceOrRecreate` only after validating the policy and the
   cluster's support for in-place resource resizing.
6. Avoid sharp memory-limit reductions. Scheduler shards derive `GOMEMLIMIT`
   from their cgroup memory limit using `goMemLimitRatio` (default `0.9`) and
   refresh it every 15 seconds, so a process can be OOM-killed before a lower
   limit is observed.

Only scheduler shards currently derive `GOMEMLIMIT` automatically. In the
measured clusters, Binder and queue-controller Go runtimes reported an
effectively unlimited memory limit. Downscale other Go operands more
conservatively and monitor their working set and restart reason.

Memory can also remain resident after objects are deleted. After the 500-node
run was cleaned to 81 infrastructure Pods, the scheduler still used 1.66 GiB
working set, including 1.03 GiB of allocated Go heap. Do not use a short idle
window as evidence that a lower limit is safe.

Example observation-only policy for the 500-node scheduler profile:

```yaml
apiVersion: kai.scheduler/v1
kind: Config
metadata:
  name: kai-config
spec:
  scheduler:
    vpa:
      enabled: true
      updatePolicy:
        updateMode: Off
      resourcePolicy:
        containerPolicies:
          - containerName: scheduler
            controlledValues: RequestsOnly
            minAllowed:
              cpu: "2"
              memory: 4Gi
            maxAllowed:
              cpu: "6"
              memory: 10Gi
```

See [Scheduling shards](./scheduling-shards.md#go-memory-limit) for scheduler
Go-memory behavior.

## Measure the target cluster

At minimum, retain service working set, CPU usage, CPU throttling, restart
count, workqueue depth and API request rate. Correlate them with object counts
and workload phases. Example PromQL expressions are:

```promql
max_over_time(container_memory_working_set_bytes{namespace="kai-scheduler",container!="POD"}[24h])

quantile_over_time(0.95, container_memory_working_set_bytes{namespace="kai-scheduler",container!="POD"}[24h])

sum by (container) (rate(container_cpu_usage_seconds_total{namespace="kai-scheduler",container!="POD"}[5m]))

sum by (container) (rate(container_cpu_cfs_throttled_periods_total{namespace="kai-scheduler",container!="POD"}[5m]))
/
sum by (container) (rate(container_cpu_cfs_periods_total{namespace="kai-scheduler",container!="POD"}[5m]))

max by (pod) (go_memstats_heap_alloc_bytes{namespace="kai-scheduler"})

max by (pod) (process_resident_memory_bytes{namespace="kai-scheduler"})

max by (name, pod) (workqueue_depth{namespace="kai-scheduler"})

max by (resource) (apiserver_storage_objects{resource=~"pods|jobs.batch|podgroups.scheduling.run.ai|bindrequests.scheduling.run.ai|queues.scheduling.run.ai|nodes"})
```

Also record `apiserver_storage_objects`, `workqueue_depth`, Pod phases,
PodGroups, queues, BindRequests, Jobs, ResourceClaims, and the timestamps of
major workload operations. The [scale-test guide](../developer/scale-tests.md)
describes the repository's scale-test results and dashboard. The setup used by
those tests is in [`hack/setup-scale-test-env.sh`](../../hack/setup-scale-test-env.sh).
