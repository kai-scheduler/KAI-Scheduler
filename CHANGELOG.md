# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v0.16.0] - 2026-06-24

### Added
- Added a bounded scenario generator portfolio for reclaim, preempt, and consolidation search, with `SchedulingShard.spec.scenarioSearchBudgets` time-budget configuration and production scenario-search metrics.
- Added `global.nodePoolLabelKey` Helm value to configure `spec.global.nodePoolLabelKey` in the Config CR for KAI sharding [#1774](https://github.com/kai-scheduler/KAI-Scheduler/issues/1774).
- Added an opt-in `deviceaccess` admission plugin (`--block-nvidia-visible-devices`, config field `admission.blockNvidiaVisibleDevices`, default disabled) that (1) rejects pods overriding the `NVIDIA_VISIBLE_DEVICES` environment variable with values other than `void`/`none` (or via a `valueFrom` reference), and (2) injects `NVIDIA_VISIBLE_DEVICES=void` into containers that do not request a GPU, blocking their access to GPUs on the node.
- Added support for configuring admission Pod Disruption Budget via Helm values (`admission.podDisruptionBudget`) [#1490](https://github.com/kai-scheduler/KAI-Scheduler/pull/1490) [dttung2905](https://github.com/dttung2905)
- Added an opt-in `hamicore` binder plugin (depends on `gpusharing`) to write the HAMI-core GPU memory limit (`CUDA_DEVICE_MEMORY_LIMIT`) for fractional GPU pods.
- Added `global.podSecurityContext`, `global.resourceReservation.namespaceLabels`, `nodescaleadjuster.labels`, `crdupgrader.resources`, `topologyMigration.resources`, and `postCleanup.resources` to the Helm. chart.
- Skill to capture and run snapshots
- Added `kaiConfigDeployer.enabled` Helm value (default `true`) to allow disabling the post-install/post-upgrade hook that applies the kai-config CR, for managing the CR outside of the chart.
- Added `defaultShard.enabled` Helm value (default `true`) to allow installing KAI without deploying the chart-managed default `SchedulingShard` CR.
- Added **NUMA-aware scheduling (v1)**. A new `numa` scheduler plugin predicts the kubelet Topology Manager's admission verdict for the `single-numa-node` and `restricted` policies from `NodeResourceTopology` (NRT) data, to prevent `TopologyAffinityError`s. The plugin is opt-in per shard (enable the `numa` plugin in a `SchedulingShard`). Shipped alongside it is an optional per-node **NUMA placement exporter** DaemonSet that exports the kubelet podresources API to each pod's observed NUMA placement. This is v1 — feasibility filtering and per-zone correctness; node scoring/optimization is future work. See the design for details: [NUMA-Aware Scheduling via NodeResourceTopology](docs/developer/designs/numa-topology/README.md).

### Changed
- Scoped admission `runtimeClassName` injection to GPU fraction pods only; whole-GPU pods are no longer mutated. `admission.gpuPodRuntimeClassName` is deprecated in favor of `admission.gpuFractionRuntimeClassName`. Reservation pod `runtimeClassName` now defaults to empty. [#1543](https://github.com/kai-scheduler/KAI-Scheduler/issues/1543) [davidLif](https://github.com/davidLif)
- Removed redundant `PodDisruptionBudgetImplemented` guard from operator PDB creation helper [#1613](https://github.com/kai-scheduler/KAI-Scheduler/pull/1613) [dttung2905](https://github.com/dttung2905)
- Updated Go toolchain and base build images to v1.26.3.
- **Breaking:** The podgroup produced for JobSet is now produces as a single PodGroup per JobSet with a two-level SubGroup hierarchy (one parent SubGroup per `replicatedJob`, one leaf SubGroup per replica) regardless of `startupPolicyOrder`. The `kai.scheduler/batch-min-member` annotation on the JobSet now overrides the root `minSubGroup`; the same annotation on `replicatedJobs[].template.metadata.annotations` overrides the leaf `minMember` (defaulting to `template.spec.parallelism`). [#1617](https://github.com/kai-scheduler/KAI-Scheduler/pull/1617) [davidLif](https://github.com/davidLif)

### Removed
- Removed the never-populated `status.phase`, `status.running`, `status.succeeded`, `status.failed`, and `status.pending` fields (and the `PodGroupPhase` type) from the `PodGroup` (`scheduling.run.ai/v2alpha2`) schema. No controller ever wrote them; use `status.resourcesStatus` and `status.schedulingConditions` to determine PodGroup liveness. [#1650](https://github.com/kai-scheduler/KAI-Scheduler/issues/1650) [david-gang](https://github.com/david-gang)

### Fixed
- Fixed Helm chart always creating the resource-reservation ServiceAccount and scaling pod namespace even when they already exist in the cluster, causing install/upgrade failures on GitOps or pre-provisioned clusters (now skips creation via `lookup`, matching the reservation namespace template) [#1732](https://github.com/kai-scheduler/KAI-Scheduler/issues/1732) [dttung2905](https://github.com/dttung2905)
- Reduced scheduler heap retention after scheduling cycles by clearing completed session snapshots and callback references, and by releasing the node scoring pool without waiting for finalizers.
- Fixed Helm chart prometheus RBAC always being installed when `prometheus.enabled` is false, and the `kai-prometheus` ClusterRoleBinding referencing the `prometheus` ServiceAccount in hardcoded `kai-scheduler` namespace instead of the Helm release namespace [#1684](https://github.com/kai-scheduler/KAI-Scheduler/pull/1684) [dttung2905](https://github.com/dttung2905)
- Fixed post-delete cleanup hook hardcoding `kai-scheduler` namespace instead of Helm release namespace on `helm uninstall` [#1619](https://github.com/kai-scheduler/KAI-Scheduler/pull/1619) [dttung2905](https://github.com/dttung2905)
- Fixed scheduler pod cache memory growth by transforming cached Pods to retain only scheduler-relevant container fields while stripping large literal env values and managed fields [#1646](https://github.com/kai-scheduler/KAI-Scheduler/issues/1646)
- Improved solver performance in some large reclaim scenarios [#1627](https://github.com/kai-scheduler/KAI-Scheduler/pull/1627) [itsomri](https://github.com/itsomri)
- Grove grouper now sets `minSubGroup` (equal to the number of child SubGroups) instead of `minMember=0` on parent SubGroups generated from `topologyConstraintGroupConfigs` [#1639](https://github.com/kai-scheduler/KAI-Scheduler/issues/1639) [davidLif](https://github.com/davidLif)
- Fixed Helm chart not wiring `podgrouper.queueLabelKey` into `spec.global.queueLabelKey` on the Config CR, so custom queue label keys were ignored at install time [#1655](https://github.com/kai-scheduler/KAI-Scheduler/pull/1655) [dttung2905](https://github.com/dttung2905)
- Fixed scheduler nil-pointer panic in the preempt scenario builder when a (partial) job has no tasks to allocate (`NewIdleGpusFilter` dereferenced a nil scenario); added the missing nil-guard matching the sibling filters [#1664](https://github.com/kai-scheduler/KAI-Scheduler/issues/1664) [sam-huang1223](https://github.com/sam-huang1223)
- Fixed default node-scale-adjuster image name (`node-scale-adjuster` → `nodescaleadjuster`) so it matches the image published to GHCR
- Fixed duplicate GPU reservation pods being created for a single `gpu-group` on a node (each reserving a different physical GPU), which corrupted the scheduler's fractional-GPU accounting and left devices unschedulable. Reservation pods are now named deterministically per (node, gpu-group) and treat AlreadyExists as success, so concurrent or retried binds collide on one object instead of duplicating [#1673](https://github.com/kai-scheduler/KAI-Scheduler/issues/1673)
- Fixed `kai_pod_group_evicted_pods_total` counter being inflated by gang size. The metric was incremented by `EvictionGangSize` (= N) on every per-pod eviction emit, so an N-pod gang eviction wrote N² to the counter instead of N (and a cross-PodGroup batch of size N inflated each PG's counter by `tasks_in_pg × N`). All eviction-emitting actions (preempt, reclaim, consolidation, stalegangeviction) were affected. [#1620](https://github.com/kai-scheduler/KAI-Scheduler/issues/1620)

## [v0.15.0] - 2026-05-20

### Added
- Added `enabled` Helm values for `binder`, `podgrouper`, `podgroupcontroller`, `queuecontroller`, `admission`, and `scheduler` to allow disabling individual components from values.yaml. Previously these were hardcoded to `true` in the kai-config template.
- Added `prometheus.enabled` and `prometheus.externalPrometheusUrl` Helm values to configure Prometheus from values.yaml [#907](https://github.com/NVIDIA/KAI-Scheduler/issues/907)
- Added validation for `subgroup` name in podgroup [faizanexe](https://github.com/faizan-exe)
- Added memory profile and run duration to snapshot tool [#1411](https://github.com/NVIDIA/KAI-Scheduler/issues/1411)
- Added support for configuring pod and container security contexts on resource reservation pods via CLI flags [AdheipSingh](https://github.com/AdheipSingh)
- Added `operator.logLevel` Helm value to configure the operator log level (maps to `--zap-log-level` when set) [#1446](https://github.com/kai-scheduler/KAI-Scheduler/pull/1446) [dttung2905](https://github.com/dttung2905)
- The scheduler now implements elastic PodGroups on both the subgroup level (`minSubGroup`) and pods (`minAvailable`). This allows for elasticity on all of the podgroup tree hierarchy. [#1416](https://github.com/kai-scheduler/KAI-Scheduler/pull/1416) - [davidLif](https://github.com/davidLif)
- Allow the configuration of plugins in the binder service. [#1480](https://github.com/kai-scheduler/KAI-Scheduler/pull/1480) - [davidLif](https://github.com/davidLif)
- Added support for configuring scheduler log level and custom scheduler args via Helm values (`scheduler.args`) [#1452](https://github.com/kai-scheduler/KAI-Scheduler/pull/1452) [dttung2905](https://github.com/dttung2905)
- Added `global.jsonLog` Helm value to enable JSON-formatted logging for use with log aggregation platforms
- Added `crdupgrader.image.registry` Helm value to override `global.registry` for the `crd-upgrader` pre-install/pre-upgrade hook image, allowing the hook image to be served from a separate mirror without redirecting all chart images. [#1404](https://github.com/kai-scheduler/KAI-Scheduler/issues/1404)
- Added `queue_metadata_name` and `queue_display_name` labels to all queue metrics emitted by both the scheduler (`queue_fair_share_*`, `queue_*_usage`) and the queue-controller (`queue_info`, `queue_deserved_gpus`, `queue_quota_*`, `queue_allocated_*`). `queue_metadata_name` always carries the Queue's `metadata.name` and is the recommended join key between scheduler and queue-controller metrics; `queue_display_name` carries `spec.displayName` (empty when unset). The legacy `queue_name` label is preserved unchanged to keep existing dashboards working. [#1566](https://github.com/kai-scheduler/KAI-Scheduler/issues/1566)
- Added support for externally-created PodGroups. Workloads can opt out of podgrouper mutation with `kai.scheduler/skip-podgrouper: "true"` on the pod or owner chain, join an existing PodGroup via `pod-group-name`, and now get a pod condition when they reference a non-existent subgroup. [#1420](https://github.com/kai-scheduler/KAI-Scheduler/issues/1420)
- Added `--stuck-in-releasing-threshold` scheduler flag (default `2m`) controlling how long a Running pod with a `deletionTimestamp` remains classified as `Releasing` before being reclassified as `StuckInReleasing` and excluded from pipelining. Configurable per shard via `SchedulingShard.spec.args.stuck-in-releasing-threshold`.

### Changed
- **Breaking:** JobSet PodGroups no longer auto-calculate `minAvailable` from `parallelism × replicas`. The default is now 1. Use the `kai.scheduler/batch-min-member` annotation to set a custom value.
- Bumped `k8s.io/*` module group from v0.34.x to v0.35.4, `k8s.io/kubernetes` to v1.35.4, and `sigs.k8s.io/controller-runtime` to v0.23.3, enabling KEP-4671 Workload API types. [#1466](https://github.com/kai-scheduler/KAI-Scheduler/issues/1466)
- Rebuilt the `crd-upgrader` hook image on `alpine:3.20` instead of `ubi9/ubi-minimal`. Image size drops from ~165 MB to ~67 MB uncompressed (~60% reduction), shrinking cold-pull latency on ephemeral CI runners. The image is also reused by the `topology-migration` and `post-delete` hook jobs as a generic `kubectl + bash` toolbox, so bash is preserved on the runtime image. [#1404](https://github.com/kai-scheduler/KAI-Scheduler/issues/1404)

### Fixed
- Account for native sidecar containers (initContainers with `restartPolicy: Always`, KEP-753) in pod resource accounting, matching kubelet's `AggregateContainerRequests`. Previously, native sidecar requests were max'd against regular containers instead of summed with them, causing the scheduler to bind pods that kubelet then rejected at admission with `OutOfCpu`/`OutOfGpu`. [#1556](https://github.com/kai-scheduler/KAI-Scheduler/pull/1556)
- Streaming snapshot JSON directly into the zip writer to avoid OOM on large clusters. The `/get-snapshot` endpoint previously buffered the entire JSON payload in memory (~3x the data size); it now streams per-element, reducing peak memory to ~1x. [#1564](https://github.com/kai-scheduler/KAI-Scheduler/pull/1564)
- Fixed `additionalImagePullSecrets` in Config CR rendering as `map[name:...]` instead of plain strings by extracting `.name` from `global.imagePullSecrets` objects. Also propagated `global.imagePullSecrets` to all Helm hook jobs (`crd-upgrader`, `topology-migration`, `post-delete-cleanup`)
- Added `global.nodeSelector`, `global.tolerations`, `global.affinity`, `global.securityContext` support to the post-delete job hook.
- Fixed Helm template writing `imagesPullSecret` (string) instead of `additionalImagePullSecrets` (array) in Config CR, causing image pull secrets to be silently ignored. Added backward-compatible deprecated `imagesPullSecret` field to CRD schema. [#942](https://github.com/kai-scheduler/KAI-Scheduler/issues/942)
- Fixed `windowSize` field in `SchedulingShard` CR to support Prometheus duration format (e.g. `1w`, `7d`). Previously, using `windowSize: 1w` as shown in the documentation caused the kai-operator to crash-loop with `time: unknown unit "w" in duration "1w"`.
- Race condition where `SyncForGpuGroup` could prematurely delete reservation pods when the informer cache had not yet propagated GPU group labels on recently-bound fraction pods. The binder now checks for active BindRequests referencing the GPU group before deleting a reservation pod.
- Fixed non-preemptible multi-device GPU memory jobs being allowed to exceed their queue's deserved GPU quota. The per-node quota check now correctly accounts for all requested GPU devices. [#1369](https://github.com/kai-scheduler/KAI-Scheduler/issues/1369)
- Added `resourceclaims/binding` RBAC permission to the binder ClusterRole for compatibility with Kubernetes v1.36+, where the `DRAResourceClaimGranularStatusAuthorization` feature gate requires explicit permission on the `resourceclaims/binding` subresource to modify `status.allocation` and `status.reservedFor` on ResourceClaims. [#1372](https://github.com/kai-scheduler/KAI-Scheduler/pull/1372) [praveen0raj](https://github.com/praveen0raj)
- Allow users to override minMember for k8s batch Jobs and JobSets using the `kai.scheduler/batch-min-member` annotation [#1308](https://github.com/kai-scheduler/KAI-Scheduler/pull/1308) [itsomri](https://github.com/itsomri)
- Fixed a bug where nil minMember caused subgroups creation to fail in scheduler [#1407](https://github.com/kai-scheduler/KAI-Scheduler/pull/1407) [itsomri](https://github.com/itsomri)
- Improved performance by evaluating SetNode once per session instead of on each predicate evaluation  [#1421](https://github.com/kai-scheduler/KAI-Scheduler/pull/1421) [itsomri](https://github.com/itsomri)
- Added persistent volumes to cluster snapshot [#1424](https://github.com/kai-scheduler/KAI-Scheduler/pull/1424) [itsomri](https://github.com/itsomri)
- Improved scheduling performance for preempt/reclaim/consolidate actions on jobs with many tasks by replacing per-task linear probing with exponential+binary search in the job solver, reducing the number of scenario simulations from O(n) to O(log n) [#1435](https://github.com/kai-scheduler/KAI-Scheduler/pull/1435) [itsomri](https://github.com/itsomri)
- Avoid expensive solver-backed reclaim/preempt/consolidation work for jobs already blocked by victim-invariant pre-solver failures such as missing PVCs, missing required ConfigMaps, or requests larger than the maximum node size. [#1502](https://github.com/kai-scheduler/KAI-Scheduler/issues/1502)
- Fixed `skipTopOwnerGrouper` not propagating per-type defaults (priority class and preemptibility) for skipped owners (e.g. `DynamoGraphDeployment`), causing PodGroup spec to retain stale values after defaults ConfigMap updates.
- Fixed binder DRA detection on clusters where the upstream `DynamicResourceAllocation` feature gate does not reflect server-side DRA availability. The binder now probes the API server during init (matching the scheduler) so the DRA plugin is gated on the same authoritative decision. [#1481](https://github.com/kai-scheduler/KAI-Scheduler/issues/1481)
- Suppressed noisy `Reconciler error` logs and `PodGrouperWarning` events on transient PodGroup update conflicts. The podgrouper now treats `IsConflict` errors as expected and silently requeues the reconcile instead of surfacing the apiserver's "object has been modified" message.
- Stopped recreating the `kai-config` CR on every `helm upgrade`. The CR is now applied by a post-install/post-upgrade hook Job (`kai-config-deployer`) using `kubectl apply --server-side` instead of being a Helm-managed resource, so its UID stays stable across upgrades. Previously, the default `before-hook-creation` policy deleted and recreated `kai-config` on every upgrade, cascading via `ownerReferences` to all operand `ServiceAccounts` (including `scheduler`). When an upgrade did not change the scheduler Deployment pod template, scheduler pods kept their old projected tokens — bound to the now-deleted SA UID — and failed every API call with `401 Unauthorized` until kubelet rotated the token at ~80% TTL. A matching post-delete Job removes the CR on `helm uninstall`. [#1536](https://github.com/kai-scheduler/KAI-Scheduler/issues/1536)
- Fixed kai-operator not reconciling on Prometheus and ServiceMonitor changes. The Config controller now watches owned `Prometheus` and `ServiceMonitor` resources, so deletions and drift trigger reconciliation. CRD presence is checked at startup against the API server (the scheme-only check used previously could not detect missing CRDs), and the watch is registered only when the CRDs are installed. [#877](https://github.com/kai-scheduler/KAI-Scheduler/issues/877)
- Added `before-hook-creation` to the `crd-upgrader` Helm hook delete policy so failed hook Jobs no longer block subsequent `helm upgrade --install` retries. Aligns with the policy already used by the chart's other hook resources. [#1404](https://github.com/kai-scheduler/KAI-Scheduler/issues/1404)
- Fixed kai-operator leader-election event emission by adding RBAC permission for core `events` (`create`, `patch`, `update`) so operators can publish leadership events instead of logging `events is forbidden`. [#1572](https://github.com/kai-scheduler/KAI-Scheduler/pull/1572) [dttung2905](https://github.com/dttung2905)
- The scheduler's per-shard Service is now populated by an operator-managed `EndpointSlice` pointing at the current leader-election Lease holder, which is connected to the service of the shard's scheduler. This allows the service to route all it's incoming request to the lease-holding pod of the scheduler deployment. [#1593](https://github.com/kai-scheduler/KAI-Scheduler/pull/1593) [davidLif](https://github.com/davidLif)
- Fixed `podgroupcontroller` logging spurious errors on every reconcile for completed/failed pods because it tried to fetch DRA `ResourceClaim` objects that the DRA driver had already deleted. Terminal pods now skip the ResourceClaim lookup entirely, mirroring the scheduler-side fix in [#1456](https://github.com/kai-scheduler/KAI-Scheduler/pull/1456). [#1529](https://github.com/kai-scheduler/KAI-Scheduler/issues/1529)

## [v0.14.0] - 2026-03-30

### Added
- Added queue validation webhook to queuecontroller with optional quota validation for parent-child relationships [AdheipSingh](https://github.com/AdheipSingh)
- Added support for VPA configuration for the different components of the KAI Scheduler - [jrosenboimnvidia](https://github.com/NVIDIA/KAI-Scheduler/pull/1119)
- Users that have VPA installed on their cluster can now utilize it for proper vertical autoscaling
- Added FOSSA scanning for the repository context. Scans will also be performed for submitted PRs. The results can be found [here](https://app.fossa.com/projects/custom%2B162%2Fgit%40github.com%3Akai-scheduler%2FKAI-Scheduler.git). [#1178](https://github.com/kai-scheduler/KAI-Scheduler/pull/1178) - [davidLif](https://github.com/davidLif)
- Added support for Ray subgroup topology-aware scheduling by specifying `kai.scheduler/topology`, `kai.scheduler/topology-required-placement`, and `kai.scheduler/topology-preferred-placement` annotations.
- Allow subgroups to have a 0 value for "minAvailable". This means that all pods in this subgroup are "elastic extra pods". [#1216](https://github.com/NVIDIA/KAI-Scheduler/pull/1216) [davidLif](https://github.com/davidLif)
- Added a display web page for Scale test results for public viewing [#1154](https://github.com/kai-scheduler/KAI-Scheduler/pull/1154) [SiorMeir](https://github.com/SiorMeir)
### Changed
- Auto-enable leader election when `operator.replicaCount` > 1 to prevent concurrent reconciliation [#1218](https://github.com/kai-scheduler/KAI-Scheduler/issues/1218)
- Update go version to v1.26.1, With appropriate upgrades to the base docker images, linter, and controller generator. [#1222](https://github.com/kai-scheduler/KAI-Scheduler/pull/1222) - [davidLif](https://github.com/davidLif)

### Fixed
- Updated resource enumeration logic to exclude resources with count of 0. [#1120](https://github.com/NVIDIA/KAI-Scheduler/issues/1120)
- Fixed scheduler on k8s < 1.34 with DRA disabled.
- Fixed pod group controller failing to track DRA GPU resources on Kubernetes 1.32-1.33 clusters. [#1214](https://github.com/kai-scheduler/KAI-Scheduler/issues/1214)
- Fixed scheduling-constraints signature hashing for `Priority` and container `HostPort` by encoding full `int32` values, preventing byte-truncation collisions and flaky signature tests.
- Fixed rollback in scheduling simulations with DRA [#1168](https://github.com/NVIDIA/KAI-Scheduler/pull/1168) [itsomri](https://github.com/itsomri)
- Fixed a potential state corruption in DRA scheduling simulations [#1219](https://github.com/kai-scheduler/KAI-Scheduler/pull/1219) [itsomri](https://github.com/itsomri)
- Fixed operator reconcile loop caused by status-only updates triggering re-reconciliation. #1229 [cypres](https://github.com/cypres)
- Fixed scheduler not starting on k8s clusters with DRA disabled, due to the ResourceSliceTracker not syncing. #1241 [cypres](https://github.com/cypres)
- Fixed webhook reconcile loop on AKS, by retaining the cloud-provider-injected namespaceSelector rules during reconciliation. #1292 [cypres](https://github.com/cypres)

## [v0.13.0] - 2026-03-02
### Added
- Added `minSubGroup` field to PodGroup and SubGroup API to support specifying the minimum number of child SubGroups required for elastic gang scheduling, along with validation to prevent simultaneous use of `minSubGroup` and `minMember` fields (#TBD) by [KAI Dev Agent](https://github.com/run-ai/KAI-Agents)
- Added `global.nodeSelector` propagation from Helm values to Config CR, ensuring operator-created sub-component deployments (admission, binder, scheduler, pod-grouper, etc.) receive the configured nodeSelector [#1102](https://github.com/NVIDIA/KAI-Scheduler/pull/1102) [yuanchen8911](https://github.com/yuanchen8911)
- Added `plugins` and `actions` fields to SchedulingShard spec, allowing per-shard customization of scheduler plugin/action enablement, priority, and arguments [gshaibi](https://github.com/gshaibi)
- Added support for Kubeflow Trainer v2 TrainJob workloads via skipTopOwner grouper pattern
- Added `binder.cdiEnabled` Helm value to allow explicit override of CDI auto-detection for environments without ClusterPolicy
- Added metric for tracking evicted pods in pod groups, including nodepool, eviction action, and gang size
- Block scheduling of pods with shared (non-template) DRA GPU claims that lack a queue label or have a mismatched queue label [gshaibi](https://github.com/gshaibi)
- Added the option to disable prometheus service monitor creation [#810](https://github.com/NVIDIA/KAI-Scheduler/pull/810) [itsomri](https://github.com/itsomri)
- Fixed prometheus instance deprecation - ensure single instance [#779](https://github.com/NVIDIA/KAI-Scheduler/pull/779) [itsomri](https://github.com/itsomri)
- Added clear error messages for jobs referencing missing or orphan queues, reporting via events and conditions [#820](https://github.com/NVIDIA/KAI-Scheduler/pull/820) [gshaibi](https://github.com/gshaibi)
- Added rule selector for resource accounting prometheus [#818](https://github.com/NVIDIA/KAI-Scheduler/pull/818) [itsomri](https://github.com/itsomri)
- Made accounting labels configurable [#818](https://github.com/NVIDIA/KAI-Scheduler/pull/818) [itsomri](https://github.com/itsomri)
- Added support for Grove hierarchical topology constraints in PodGroup subgroups
- Added support for n-level queue hierarchies [#858](https://github.com/NVIDIA/KAI-Scheduler/pull/858) [gshaibi](https://github.com/gshaibi)
- Added labels and annotations propagation from topOwner in SkipTopOwner grouper [#861](https://github.com/NVIDIA/KAI-Scheduler/pull/861) [SiorMeir](https://github.com/siormeir)
- Added scheduler name match conditions to admission webhooks to improve cluster stability
- Add Gpu Dra claims and resource slices accounting for the purpose of resource management and quota guarantees. *** This change doesn't support shared gpu claims or gpu claims with FirstAvailable *** [#900](https://github.com/NVIDIA/KAI-Scheduler/pull/900) [davidLif](https://github.com/davidLif) 
- Added DRA resources recording to snapshot [#830](https://github.com/NVIDIA/KAI-Scheduler/pull/830)
- Temporarily Prevent device-plugin GPU pods on DRA-only nodes - until translation between device-plugin notation and DRA is implemented
- Implemented subgroups for pytorchjobs [#935](https://github.com/NVIDIA/KAI-Scheduler/pull/935) [itsomri](https://github.com/itsomri)
- Made KAI images distroless [#745](https://github.com/NVIDIA/KAI-Scheduler/pull/745) [dttung2905](https://github.com/dttung2905)
- Allow setting empty gpuPodRuntimeClassName during helm install [#972](https://github.com/NVIDIA/KAI-Scheduler/pull/972) [steved](https://github.com/steved)
- Created scale tests scenarios for running scale tests for KAI [#967](https://github.com/NVIDIA/KAI-Scheduler/pull/967)
- Implemented block-level segmentation for pytorchjobs [#938](https://github.com/NVIDIA/KAI-Scheduler/pull/938) [itsomri](https://github.com/itsomri)
- Added scale test environment setup script and updated service monitors for KAI scheduler [#1031](https://github.com/NVIDIA/KAI-Scheduler/pull/1031)
- Implemented subgroups for leaderworkerset [#1046](https://github.com/NVIDIA/KAI-Scheduler/pull/1046) [davidLif](https://github.com/davidLif) 
- Added discovery data to snapshot for more accurate debugging [#1047](https://github.com/NVIDIA/KAI-Scheduler/pull/1047) [itsomri](https://github.com/itsomri)
- Implemented subgroup segmentation (with topology segment definitions) for leaderworkerset [#1058](https://github.com/NVIDIA/KAI-Scheduler/pull/10586) [davidLif](https://github.com/davidLif)

### Fixed
- Fixed operator status conditions to be kstatus-compatible for Helm 4 `--wait` support: added `Ready` condition and fixed `Reconciling` condition to properly transition to false after reconciliation completes [#1060](https://github.com/NVIDIA/KAI-Scheduler/pull/1060)
- Fixed a bug where the node scale adjuster would not check if a pod was unschedulable before creating a scaling pod leading to unnecessary node scaling [#1094](https://github.com/NVIDIA/KAI-Scheduler/pull/1094) [slaupster](https://github.com/slaupster)
- Fixed admission webhook to skip runtimeClassName injection when gpuPodRuntimeClassName is empty [#1035](https://github.com/NVIDIA/KAI-Scheduler/pull/1035)
- Fixed topology-migration helm hook failing on OpenShift due to missing `kai-topology-migration` service account in the `kai-system` SCC [#1050](https://github.com/NVIDIA/KAI-Scheduler/pull/1050)
- Fixed a bug where queue status did not reflect its podgroups resources correctly [#1049](https://github.com/NVIDIA/KAI-Scheduler/pull/1049)
- Fixed helm uninstall does not remove webhooks [#959](https://github.com/NVIDIA/KAI-Scheduler/pull/959) [faizan-exe](https://github.com/faizan-exe)
- Fixed security vulnerability where PodGang could reference pods in other namespaces, preventing cross-namespace manipulation
- Fixed pod controller logging to use request namespace/name instead of empty pod object fields when pod is not found
- Fixed a bug where topology constrains with equal required and preferred levels would cause preferred level not to be found.
- Fixed GPU memory pods Fair Share and Queue Order calculations
- Interpret negative or zero half-life value as disabled [#818](https://github.com/NVIDIA/KAI-Scheduler/pull/818) [itsomri](https://github.com/itsomri)
- Handle invalid CSI StorageCapacities gracefully [#817](https://github.com/NVIDIA/KAI-Scheduler/pull/817) [rich7420](https://github.com/rich7420)
- Embed CRD definitions in binary for env-test and time-aware-simulations to allow binary portability [#818](https://github.com/NVIDIA/KAI-Scheduler/pull/818) [itsomri](https://github.com/itsomri)
- Fixed missing `podGrouper` configuration in Helm template that prevented podgrouper values from being applied [#860](https://github.com/NVIDIA/KAI-Scheduler/pull/860)
- Fixed rollback for failed bind attempts [#847](https://github.com/NVIDIA/KAI-Scheduler/pull/847) [itsomri](https://github.com/itsomri)
- Fixed missing `namespace`, `serviceAccountName`, and `appLabel` fields in `resourceReservation` section of kai-config Helm template [#860](https://github.com/NVIDIA/KAI-Scheduler/pull/891) [dttung2905](https://github.com/dttung2905)
- If a preferred topology constraint is set, do not try to find a lowest common subtree (as a part of the calculations optimizations) which is lower then the preferred level
- Added dedicated `usage-prometheus` service for scheduler Prometheus access with configurable instance name [#896](https://github.com/NVIDIA/KAI-Scheduler/pull/896) [itsomri](https://github.com/itsomri)
- ClusterPolicy CDI parsing for gpu-operator > v25.10.0
- Fixed missing `repository`, `tag`, and `pullPolicy` fields in `resourceReservationImage` section of kai-config Helm template [#895](https://github.com/NVIDIA/KAI-Scheduler/pull/895) [dttung2905](https://github.com/dttung2905)
- Fixed a bug in ray gang scheduling where not all worker groups' minMember would be respected [#924](https://github.com/NVIDIA/KAI-Scheduler/pull/924) [itsomri](https://github.com/itsomri)
- cpu-only nodes calculation in DRA enabled clusters [#944](https://github.com/NVIDIA/KAI-Scheduler/pull/944)
- enable DRA flag override fix in snapshot-tool [#955](https://github.com/NVIDIA/KAI-Scheduler/pull/955)
- Fixed ConfigMap predicate to respect the Optional field and now considers ConfigMaps in projected volumes and ephemeral containers
- Fixed simulations that failed due to pod capacity on node [#969](https://github.com/NVIDIA/KAI-Scheduler/pull/969) [itsomri](https://github.com/itsomri)
- Fixed a bug where some resource claims would remain marked as bound to devices forever

### Changed
- Removed the constraint that prohibited direct nesting of subgroups alongside podsets within the same subgroupset.
- Fixed plugin server (snapshot and job-order endpoints) listening on all interfaces by binding to localhost only.
- Removed redundant `connection` field from `GlobalConfig` in favor of `Prometheus.ExternalPrometheusUrl` for external Prometheus URL configuration

## [v0.12.0] - 2025-12-24

### Added
- Introduced native KAI Topology CRD to replace dependency on Kueue's Topology CRD, improving compatibility and simplifying installation
- Added support for having the default "preemptibility" per top-owner-type read from the default configs configmap in the pod-grouper
- Added option to profile CPU when running the snapshot tool [#726](https://github.com/NVIDIA/KAI-Scheduler/pull/726) [itsomri](https://github.com/itsomri)
- GPU resource bookkeeping for DRA enabled resources
- Add a "tumbling window" usage configuration - calculate a tumbling window size based on a start timne configuration and a duration config field.
- Added an option to disable prometheus persistency [#764](https://github.com/NVIDIA/KAI-Scheduler/pull/764) [itsomri](https://github.com/itsomri)

### Changed
- If enabled, prometheus storage size is not inferred from cluster objects, but defaults to 50Gi unless explicitly set in KAI config [#756](https://github.com/NVIDIA/KAI-Scheduler/pull/756) [itsomri](https://github.com/itsomri)
- When prometheus is disabled, it will remain in the cluster for a grace period equal to it's retention, unless re-enabled [#756](https://github.com/NVIDIA/KAI-Scheduler/pull/756) [itsomri](https://github.com/itsomri)

### Fixed
- Fixed a bug where the snapshot tool would not load topology objects [#720](https://github.com/NVIDIA/KAI-Scheduler/pull/720) [itsomri](https://github.com/itsomri)
- Operator to conditionally watch ClusterPolicy based on its existence, preventing errors in its absence
- Fixed confusing resource division log message [#733](https://github.com/NVIDIA/KAI-Scheduler/pull/733) [itsomri](https://github.com/itsomri)
- Made post-delete-cleanup resources configurable [#737](https://github.com/NVIDIA/KAI-Scheduler/pull/737) [dttung2905](https://github.com/dttung2905)
- GPU Memory pods are not reclaimed or consolidated correctly
- Added missing leases permission for the operator [#753](https://github.com/NVIDIA/KAI-Scheduler/pull/753) [dttung2905](https://github.com/dttung2905)
- Fixed reclaim/preempt/consolidate actions for topology workloads [#739](https://github.com/NVIDIA/KAI-Scheduler/pull/739)  [itsomri](https://github.com/itsomri)
- Fixed a bug where the scheduler would not consider topology constraints when calculating the scheduling constraints signature [#761](https://github.com/NVIDIA/KAI-Scheduler/pull/766) [gshaibi](https://github.com/gshaibi)
- Fixed Dynamo integration by adding Dynamo GVKs to SkipTopOwner table
- Keep creating service monitors for deprecated prometheus instances [#774](https://github.com/NVIDIA/KAI-Scheduler/pull/774) [itsomri](https://github.com/itsomri)
- Fix retention duration parsing for deprecated prometheus instances [#774](https://github.com/NVIDIA/KAI-Scheduler/pull/774) [itsomri](https://github.com/itsomri)

### Changed
- Renamed the previous "tumbling" option for the scheduler usage window type to "cron".

## [v0.10.2] - 2025-11-24

### Fixed
- Removed the requirement to specify container type for init container gpu fractions [#684](https://github.com/NVIDIA/KAI-Scheduler/pull/684) [itsomri](https://github.com/itsomri)
- When a status update for a podGroup in the scheduler is flushed due to update conflict, delete the update payload data as well [#691](https://github.com/NVIDIA/KAI-Scheduler/pull/691) [davidLif](https://github.com/davidLif)

## [v0.10.1] - 2025-11-23

### Fixed
- Fixed scheduler pod group status update conflict [#676](https://github.com/NVIDIA/KAI-Scheduler/pull/676) [davidLif](https://github.com/davidLif) 
- Fixed gpu request validations for pods [#660](https://github.com/NVIDIA/KAI-Scheduler/pull/660) [itsomri](https://github.com/itsomri)

### Changed
- Dependabot configuration to update actions in workflows [#651](https://github.com/NVIDIA/KAI-Scheduler/pull/651) [ScottBrenner](https://github.com/ScottBrenner)
- optimize dependency management by using module cache instead of vendor directory [#645](https://github.com/NVIDIA/KAI-Scheduler/pull/645) [lokielse](https://github.com/lokielse)

## [v0.10.0] - 2025-11-18

### Added
- Added parent reference to SubGroup struct in PodGroup CRD to create a hierarchical SubGroup structure
- Added the option to configure the names of the webhook configuration resources.
- Option to configure reservation pods runtime class.
- Added a tool to run time-aware fairness simulations over multiple cycles (see [Time-Aware Fairness Simulator](cmd/time-aware-simulator/README.md))
- Added enforcement of the `nvidia` runtime class for GPU pods, with the option to enforce a custom runtime class, or disable enforcement entirely.
- Added a preferred podAntiAffinity term by default for all services, can be set to required instead by setting `global.requireDefaultPodAffinityTerm`
- Added support for service-level affinities
- Added [time aware scheduling](docs/timeaware/README.md) capabilities
- Added option to specify container name and type for fraction containers

### Fixed
- (Openshift only) - High CPU usage for the operator pod due to continues reconciles
- Fixed a bug where the scheduler would not re-try updating podgroup status after failure
- Fixed a bug where ray workloads gang scheduling would ignore `minReplicas` if autoscaling was not set
- KAI Config wrong statuses when prometheus operand is enabled
- GPU-Operator v25.10.0 support for CDI enabled environments

## [v0.9.9] - 20250-12-08

### Added
- Option to configure reservation pods runtime class.

### Fixed
- Fixed Helm chart compatibility with Helm 4 wait logic to prevent indefinite hangs during deployment readiness checks

## [v0.9.1] - 20250-09-15

### Added
- Added the option of providing the podgrouper app a scheme object to use

## [v0.9.0] - 20250-09-10

### Added
- config.kai.scheduler CRD that will describe the installation of all KAI-scheduler services for the operator
- Initial KAI-operator implementation for managing components
- PodGroup Controller, Queue Controller, Admission and Scale Adjuster operands to operator lifecycle management
- Deployment of operator in Helm chart alongside pod group controller
- Deploy PodGroup Controller, Queue Controller, Admission and Scale Adjuster via operator for streamlined deployment
- schedulingshrards.kai.scheduler CRD that describes partitioning the cluster nodes for different scheduling options.

### Changed
- Moved the CRDs into the helm chart so that they are also installed by helm and not only by the crd-upgrader, but removed the external kueue clone of topology CRD from being automatically installed.
- Updated queue controller image name to align with current deployment standards

### Fixed
- Removed webhook manager component as part of operator-based refactoring

## [v0.8.5] - 20250-09-04

### Added
- Added configurable plugins hub for podgrouper using interface and RegisterPlugins

## [v0.8.4] - 20250-09-02

### Added
- Added a plugin to reflect joborder in scheduler http endpoint - Contributed by Saurabh Kumar Singh <singh1203.ss@gmail.com>

### Fixed
- Fixed a bug where workload with subgroups would not consider additional tasks above minAvailable

## [v0.8.3] - 20250-08-31

### Removed
- Removed unused code that required gpu-operator as a dependency

## [v0.8.2] - 2025-08-25

### Fixed
- Fixed wrong GPU memory unit conversion from node `nvidia.com/gpu.memory` labels
- Fixed incorrect MIG GPU usage calculation leading to wrong scheduling decision

## [v0.8.1] - 2025-08-20

### Added
- Added a new scheduler flag `--update-pod-eviction-condition`. When enabled, a DisruptionTarget condition is set on the pod before deletion

### Fixed
- Fixed scheduler panic in some elastic reclaim scenarios

## [v0.8.0] - 2025-08-18

### Added
- Added leader election configuration in all deployments and added global helm value that controls it during installation

## [v0.7.13] - 2025-08-12

### Added
- Separated admission webhooks from binder service to a separate `kai-admission` service

### Fixed
- crd-upgrader respects global values for nodeSelector, affinity and tolerations 
- kai-scheduler will not ignore pod spec.overhead field

## [v0.7.12] - 2025-08-04

### Fixed
- Fixed container env var overwrite to cover possible cases where env var with Value is replaced with ValueFrom or the other way

## [v0.7.7] - 2025-07-16

### Fixed
- Fixed a scenario where only GPU resources where checked for job and node, causing it to be bound instead of being pipelined

## [v0.7.6] - 2025-07-11

### Added
- Added GPU_PORTION env var for GPU sharing pods

## [v0.7.5] - 2025-07-10

### Fixed
- Fixed a miscalculation where cpu/memory releasing resources were considered idle when requesting GPU fraction/memory

## [v0.7.4] - 2025-07-09

### Changed
- Changed RUNAI-VISIBLE-DEVICES key in GPU sharing configmap to NVIDIA_VISIBLE_DEVICES

## [v0.7.3] - 2025-07-08

### Removed
- Removed GPU sharing configmap name resolution from env vars and volumes

## [v0.7.2] - 2025-07-07
### Added
- Added LeaderWorkerSet support in the podGrouper. Each replica will be given a separate podGroup.

## [v0.7.1] - 2025-07-07

### Added
- Added kueue topology CRD to kai installations

### Fixed
- Fixed cases where reclaim validation operated on outdated info, allowing invalid reclaim scenarios

## [v0.7.0] - 2025-07-02

### Added
- Added optional pod and namespace label selectors to limit the scope of monitored pods
- Added a plugin extension point for scheduler plugins to add annotations to BindRequests
- Added support for Grove

### Changed
- Changed `run.ai/top-owner-metadata` to `kai.scheduler/top-owner-matadata`

## [v0.6.0] - 2025-06-16

### Changed
- Changed `runai-reservation` namespace to `kai-resource-reservation`. For migration guide, refer to this [doc](docs/migrationguides/README.md)
- Changed `runai/queue` label key to `kai.scheduler/queue`. For migration guide, refer to [doc](docs/migrationguides/README.md)

### Fixed
- Fixed pod status scheduled race condition between the scheduler and the pod binding
- Removed redundant `replicas` key for binder from `values.yaml` as it is not used and not supported

### Removed
- Removed `runai-job-id` and `runai/job-id` annotations from pods and podgroups

### Added
- Added [minruntime](docs/plugins/minruntime.md) plugin, allowing PodGroups to run for a configurable amount of time without being reclaimed/preempted.
- PodGroup Controller that will update podgroups statuses with allocation data.
- Queue Controller that will update queues statuses with allocation data.


## [v0.5.1] - 2025-05-20

### Added
- Added support for [k8s pod scheduling gates](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/)
- nodeSelector, affinity and tolerations configurable with global value definitions
- Added `PreemptMinRuntime` and `ReclaimMinRuntime` properties to queue CRD
- Scheduler now adds a "LastStartTimestamp" to podgroup on allocation

### Changed
- Queue order function now takes into account potential victims, resulting in better reclaim scenarios.

### Fixed
- Fixed preempt/reclaim of elastic workloads only taking one pod.
- Scheduler now doesn't label pods' nodepool when nodepool label value is empty
