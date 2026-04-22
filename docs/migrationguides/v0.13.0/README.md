# Migration Guide: v0.12.x → v0.13.0

## 1. What Changed

### Queue Controller: `--queue-label-key` flag removed

The `--queue-label-key` CLI flag was removed from the queue-controller binary in v0.13.0
([#1049](https://github.com/NVIDIA/KAI-Scheduler/pull/1049)). The queue-controller now uses a
field indexer on `Spec.Queue` instead of relying on a pod label key for resource aggregation.

The operator's args builder was updated in the same commit, so the flag is no longer passed to the
queue-controller deployment. **No user action is required** for standard installations managed by
the KAI operator.

**Who is affected:**
- Users who launch the queue-controller binary directly with custom arguments (outside the operator).
  If `--queue-label-key` is passed to the v0.13.0 binary, it will exit with:
  ```
  flag provided but not defined: -queue-label-key
  ```
- The `podgrouper` and `scheduler` binaries still accept `--queue-label-key`. Only the
  queue-controller binary was changed.

### Helm values: new `plugins` and `actions` fields

v0.13.0 adds `plugins: {}` and `actions: {}` to `values.yaml`, allowing per-shard customization of
scheduler plugins and actions. These default to empty maps and do not change behavior unless
explicitly configured.

### Helm values: image `pullPolicy` added

An explicit `pullPolicy: IfNotPresent` was added to the default image configuration.

### Global config: `ExternalTSDBConnection` field removed

The `connection` field was removed from `GlobalConfig` in favor of
`Prometheus.ExternalPrometheusUrl`.

### Global config: `NodeSelector` field added

A new `global.nodeSelector` field propagates from Helm values to the Config CR, ensuring
operator-created sub-component deployments receive the configured `nodeSelector`.

## 2. Known Issues

### Helm rollback from v0.13.0 to v0.12.x fails

`helm rollback` does not work for this chart — **not just across v0.12.x/v0.13.0, but in general.**
This is a pre-existing issue not specific to the v0.13.0 upgrade.

**Symptoms (vary depending on cluster state):**
```
Error: no Namespace with the name "kai-resource-reservation" found
```
```
Error: failed to create resource: serviceaccounts "kai-resource-reservation" already exists
```
```
Error: no Queue with the name "default-parent-queue" found
```

**Root cause:** The chart uses conditional rendering (`lookup` guards, `.Release.IsInstall`) to
create resources only when they don't already exist. At install time, these resources are rendered
into the stored manifest. On rollback, Helm replays the stored manifest verbatim and attempts to
re-create these resources, conflicting with their current state in the cluster.

Affected resources:
- `kai-resource-reservation` namespace — `lookup`-guarded, with `helm.sh/resource-policy: keep`
- `kai-resource-reservation` service account — created by both Helm and the operator
- Default queues (`default-parent-queue`, `default-queue`) — `.Release.IsInstall`-guarded, with `helm.sh/resource-policy: keep`

Manually deleting resources before rollback does not help: removing the namespace triggers a
cascade of follow-on errors (service account conflicts, missing queues), and the operator
re-creates resources concurrently with the rollback, causing further race conditions.

**Impact:**
- The Helm release is left in a `failed` state.
- Running workloads are **not** affected — all pods continue running.
- Recovery requires an uninstall + reinstall cycle. See [Downgrade Procedure](#4-downgrade-procedure-v0130--v012x).

**Workaround — uninstall + reinstall instead of `helm rollback`:**
```bash
# 1. Uninstall the current release
helm uninstall kai-scheduler -n kai-scheduler

# 2. Reinstall the target version
helm install kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
  --version v0.12.1 -n kai-scheduler
```
All queues, workloads, CRDs, and PodGroups are preserved across this cycle.
See [Downgrade Procedure](#4-downgrade-procedure-v0130--v012x) for full details.

### Helm uninstall from v0.13.0

Helm uninstall works cleanly. The following resources are intentionally preserved due to
`helm.sh/resource-policy: keep`:
- `SchedulingShard` "default"
- `kai-resource-reservation` namespace and its reservation pods
- All CRDs (Queues, PodGroups, BindRequests, etc.)
- Existing workload pods continue running (already bound to nodes)

All KAI controller pods are removed. No new scheduling occurs until KAI is reinstalled.

## 3. Upgrade Procedure

1. **Review custom queue-controller arguments.** If you pass `--queue-label-key` outside the
   operator, remove it before upgrading.

2. **Review custom Helm values.** If you override `global.connection`, migrate to
   `prometheus.externalPrometheusUrl`.

3. **Run the upgrade:**
   ```bash
   helm upgrade kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
     --version v0.13.0 -n kai-scheduler --reuse-values
   ```

4. **Verify:**
   ```bash
   # Check all KAI pods are running
   kubectl get pods -n kai-scheduler

   # Check queue-controller logs for errors
   kubectl logs -n kai-scheduler -l app=queue-controller --tail=50

   # Verify queues are healthy
   kubectl get queues
   ```

5. **Do not use `helm rollback`.** See [Known Issues](#helm-rollback-from-v013x-to-v012x-fails).
   If you need to revert to v0.12.x, use the downgrade procedure below.

## 4. Downgrade Procedure (v0.13.0 → v0.12.x)

Since `helm rollback` is not supported, use uninstall + reinstall to downgrade. This procedure
preserves all queues, workloads, CRDs, and PodGroups.

1. **Uninstall v0.13.0:**
   ```bash
   helm uninstall kai-scheduler -n kai-scheduler
   ```
   The following resources are preserved after uninstall:
   - All Queues and PodGroups (CRD instances with `resource-policy: keep`)
   - All CRDs (Queues, PodGroups, BindRequests, Configs, etc.)
   - SchedulingShard "default"
   - `kai-resource-reservation` namespace and reservation pods
   - Running workloads (already bound to nodes)

   The Config CR (`kai-config`) **is deleted** by uninstall, but it will be
   recreated with defaults when v0.12.x is reinstalled.

2. **Reinstall v0.12.x:**
   ```bash
   helm install kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
     --version v0.12.1 -n kai-scheduler
   ```
   The `kai-scheduler` namespace already exists so `--create-namespace` is not needed.

3. **Verify:**
   ```bash
   # All controllers should be running
   kubectl get pods -n kai-scheduler

   # Queues should be intact
   kubectl get queues

   # Existing workloads should still be running
   kubectl get pods -n <workload-namespace>

   # Check scheduler is actively scheduling
   kubectl logs -n kai-scheduler deployment/kai-scheduler-default --tail=20
   ```

**What is preserved across the uninstall/reinstall cycle:**

| Resource | Preserved | Notes |
|---|---|---|
| Queues | ✅ | `resource-policy: keep` |
| PodGroups | ✅ | CRD instances, not managed by Helm directly |
| Running workloads | ✅ | Already bound to nodes |
| Jobs | ✅ | Owned by workloads namespace |
| Deployments | ✅ | Owned by workloads namespace |
| CRDs | ✅ | Not deleted by Helm uninstall |
| SchedulingShard | ✅ | `resource-policy: keep` |
| Reservation namespace | ✅ | `resource-policy: keep` |
| Config CR (`kai-config`) | ❌ | Recreated with defaults on reinstall |
| Custom Helm values | ❌ | Must be re-supplied via `--values` or `--set` |
