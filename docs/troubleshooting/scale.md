# General scale recommendations

## Scheduler:
1. **Lower log verbosity** — The scheduler binary uses the `--v` flag (klog-style verbosity). The default is **3** (`cmd/scheduler/app/options/options.go`). **This is not set on the `Config` CR.** It is set per **SchedulingShard** under `spec.args`, which the operator merges into the scheduler Deployment as CLI flags (`pkg/operator/operands/scheduler/resources_for_shard.go`).

   For each shard (for example `default`), run `kubectl edit schedulingshard <shard-name>` and set a lower integer to reduce log volume under load:

   ```yaml
   spec:
     args:
       v: "2"   # or "1"; default when omitted is 3
   ```

   Only keys that match registered scheduler flags are applied; see `SchedulingShardSpec.Args` in `pkg/apis/kai/v1/schedulingshard_types.go`.

2. **Limit or disable consolidation work** — Consolidation is by far the heaviest action computationally. Its configured per **SchedulingShard** (`pkg/apis/kai/v1/schedulingshard_types.go`), not on the cluster `Config` (`pkg/apis/kai/v1/config_types.go`). Edit `kubectl edit schedulingshard <shard-name>`.

   **Cap how much consolidation victims are consider per cycle** — Use `spec.args` to lower `--max-consolidation-preemptees` (flag name `max-consolidation-preemptees`; default **16** in `cmd/scheduler/app/options/options.go`):

   ```yaml
   spec:
     args:
       max-consolidation-preemptees: "8"
   ```

   **Cap jobs per queue for the consolidation action** — `spec.queueDepthPerAction` limits how many jobs the scheduler considers per queue for each action name. Use the key **`consolidation`** (see `SchedulingShardSpec.QueueDepthPerAction` and `framework.ActionType`):

   ```yaml
   spec:
     queueDepthPerAction:
       consolidation: 10
   ```

   **Disable the consolidation action** — Under `spec.actions`, set `consolidation.enabled` to `false` (see `ActionConfig` and default action priorities in `SchedulingShardSpec`). This removes the action from the scheduling cycle for that shard.

   ```yaml
   spec:
     actions:
       consolidation:
         enabled: false
   ```

   By default, consolidation is also **off** when either `spec.placementStrategy.gpu` or `spec.placementStrategy.cpu` is `spread` (see `setDefaultActions` in the same file). Using spread for placement is another way to avoid consolidation without an explicit `actions` override.

3. Use similar shape jobs: signature mechanism
4. **Noisy neighbors: cap per-action, per-queue work** — A single queue (or a few queues) with very large backlogs can dominate a scheduling cycle: each **action** (`allocate`, `consolidation`, `reclaim`, `preempt`, `stalegangeviction`; see `framework.ActionType` in `pkg/scheduler/framework/interface.go`) walks candidate jobs in **queue order**, and without a cap the scheduler may spend most of a cycle on one neighbor’s jobs.

   **`spec.queueDepthPerAction`** on **SchedulingShard** (`SchedulingShardSpec` in `pkg/apis/kai/v1/schedulingshard_types.go`) maps each action name to the **maximum number of jobs to consider per leaf queue** for that action. It is written into the shard ConfigMap as `queueDepthPerAction` in `pkg/scheduler/conf/scheduler_conf.go` and applied via `Session.GetJobsDepth` (`pkg/scheduler/framework/session.go`). **If an action is omitted, there is no limit** (effectively unbounded depth for that action).

   Tune per shard when one tenant/queue starves others or CPU time per cycle spikes:

   ```yaml
   spec:
     queueDepthPerAction:
       allocate: 50
       preempt: 20
       reclaim: 20
       consolidation: 10
       stalegangeviction: 30
   ```

5. **Scheduler pod resources (manual or VPA)** — Static requests/limits live on **`spec.scheduler.service.resources`** in the cluster **`Config`** CR (`pkg/apis/kai/v1/scheduler/scheduler.go`, referenced from `pkg/apis/kai/v1/config_types.go`). Defaults are set in code when fields are empty.

   To **let recommendations drive CPU/memory** for each **scheduler Deployment** (one per `SchedulingShard`), use the **Vertical Pod Autoscaler** fields on the same CR. The operator creates a `VerticalPodAutoscaler` targeting the shard scheduler Deployment when VPA is enabled (`pkg/operator/operands/common/vpa.go` `BuildVPAFromObjects`).

   - **`spec.scheduler.vpa`** — VPA for the scheduler only.  
   - **`spec.global.vpa`** — Default used when `spec.scheduler.vpa` is omitted (`Scheduler` inherits `globalVPA` in `SetDefaultsWhereNeeded`).

   Shared shape (`pkg/apis/kai/v1/common/vpa.go`): set **`enabled: true`**, and optionally **`updatePolicy`** (how updates are applied; defaults include `InPlaceOrRecreate`) and **`resourcePolicy`** (min/max per container for recommendations).

   Example (scheduler-specific):

   ```yaml
   spec:
     scheduler:
       vpa:
         enabled: true
   ```


## Binder:

The **binder** carries out placements after the scheduler commits a decision: it reconciles **`BindRequest`** objects and performs the actual pod bind to the chosen node (including resource-reservation and binder-plugin work). That pipeline is separate from the scheduler’s scheduling cycle; under heavy churn or in large clusters, binding throughput, client QPS, or reconcile concurrency can become the bottleneck. Tune it via **`spec.binder`** on the `Config` CR below.

1. **Resources (binder Deployment)** — `spec.binder.service.resources`: CPU/memory requests and limits for the binder pods. Defaults are modest (e.g. 50m CPU / 200Mi memory requests); raise if the API server throttles less often but the process needs more headroom under load. Optionally use `spec.binder.vpa` (and `spec.global.vpa`) for Vertical Pod Autoscaler instead of hand-tuning.

2. **Rate limiting (Kubernetes client)** — `spec.binder.service.k8sClientConfig.qps` and `spec.binder.service.k8sClientConfig.burst`. Defaults are QPS `20` and burst `100` (`pkg/apis/kai/v1/common/client_config.go`). Increase gradually if binder logs show client-side throttling or slow reconciliation while the API server has capacity; keep burst ≥ QPS.

3. **Concurrency** — `spec.binder.maxConcurrentReconciles`: caps concurrent reconciles for **both** pods and `BindRequest` controllers. Raise when bind latency dominates and CPU/memory allow; lower if the apiserver or etcd is overloaded.

## PodGrouper:

The **pod-grouper** reconciles **Pods** that use the KAI **scheduler name**: it resolves workload owners (Job, workflow CRs, and so on), builds **PodGroup** metadata, and ensures the corresponding **PodGroup** objects exist and match the workload. The scheduler’s gang-aware logic operates on **PodGroups**, not raw pod counts—if PodGroup creation or updates lag behind pod churn, work queues up as `Pending` even when cluster capacity exists.

That path is independent of the scheduler loop; at high scale it can bottleneck on **reconcile throughput**, **Kubernetes client QPS/burst**, or **memory/CPU** on the controller. Tune it under **`spec.podGrouper`** on the `Config` CR (`pkg/apis/kai/v1/pod_grouper/pod_grouper.go`).

1. **Resources (pod-grouper Deployment)** — `spec.podGrouper.service.resources`: CPU/memory for the controller. Defaults match the binder-style small footprint; scale up if the controller is CPU-bound. `spec.podGrouper.vpa` / `spec.global.vpa` can automate sizing.

2. **Rate limiting (Kubernetes client)** — `spec.podGrouper.k8sClientConfig.qps` and `spec.podGrouper.k8sClientConfig.burst` (same semantics and defaults as in `common.K8sClientConfig`). The operand passes these as `--qps` / `--burst`. **Note:** pod-grouper uses the **top-level** `k8sClientConfig` on `PodGrouper`, not `spec.podGrouper.service.k8sClientConfig`.

3. **Concurrency** — `spec.podGrouper.maxConcurrentReconciles`: number of concurrent reconcile workers. Increase when many namespaces/workloads need PodGroup creation under load; decrease if the API server shows sustained LIST/WATCH pressure.

4. **Horizontal scale** — `spec.podGrouper.replicas` (defaults from `spec.global.replicaCount`). With more than one replica, `--leader-elect` is added automatically.

## Advanced troubleshooting:
1. **Find the slowest scheduling action** — Compare per-action time inside each scheduling cycle using the gauge **`action_scheduling_latency_milliseconds`** (Prometheus name is typically **`kai_action_scheduling_latency_milliseconds`** when the scheduler uses the default metrics namespace; the label **`action`** names the action). Recorded in `pkg/scheduler/metrics/metrics.go`. Scraping and ServiceMonitors are described in [`docs/metrics/README.md`](../metrics/README.md).
2. **Analyze a snapshot from a slow scheduler** — The snapshot **plugin** exposes `GET /get-snapshot` on the scheduler pod (ZIP containing JSON cluster/scheduler state). **Capture** it with a port-forward to the scheduler Deployment for your shard, then **analyze** the file offline with the **snapshot** CLI (`cmd/snapshot-tool`). Full steps, endpoint details, and `snapshot-tool` flags are documented in [`docs/plugins/snapshot.md`](../plugins/snapshot.md).

   Minimal flow (namespace and Deployment name depend on your install and shard; see examples under **Capturing a Snapshot** in the doc linked above):

   ```bash
   kubectl port-forward -n <namespace> deployment/<scheduler-deployment> 8081 &
   curl -sS "http://127.0.0.1:8081/get-snapshot" -o snapshot.zip
   snapshot-tool --filename snapshot.zip 
   ```

   For deeper help interpreting output, use your favorite clanker or contact the KAI Scheduler team.