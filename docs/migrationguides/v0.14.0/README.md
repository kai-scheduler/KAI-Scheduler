# Migration Guide: v0.13.x → v0.14.0

## 1. What Changed

### Helm values: new `global.vpa` field

v0.14.0 adds a `global.vpa` block to `values.yaml`, enabling
[Vertical Pod Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler)
support for KAI components. It defaults to `enabled: false` and has no effect unless
VPA is installed and explicitly enabled.

### Helm values: new `prometheus` field

v0.14.0 adds a `prometheus` block to `values.yaml` for configuring Prometheus
integration. It defaults to `enabled: false` and has no effect unless explicitly
enabled.

### Operator: leader election auto-enabled when `replicaCount > 1`

The operator now automatically enables leader election when
`operator.replicaCount` is greater than 1. Previously, leader election required
an explicit `global.leaderElection: true`.

**Who is affected:** users running the operator with `operator.replicaCount > 1`
without `global.leaderElection: true`. After upgrade, the operator will enable
leader election automatically. This prevents split-brain reconciliation and is
the correct behavior for multi-replica deployments.

### Queue controller: quota validation (opt-in)

A `--enable-quota-validation` flag is available on the queue-controller binary.
When enabled, it adds warnings when child queue quotas exceed their parent's
quota. This flag is not exposed as a Helm value and defaults to `false`; no
action is required unless you want to opt in.

## 2. Known Issues

### `helm upgrade --reuse-values` fails with nil pointer error

**Symptom:**
```
Error: UPGRADE FAILED: template: kai-scheduler/templates/kai-config.yaml:218:16:
executing "kai-scheduler/templates/kai-config.yaml" at <.Values.prometheus.enabled>:
nil pointer evaluating interface {}.enabled
```

**Root cause:** The v0.13.x chart had no `prometheus` key in `values.yaml`. When
upgrading with `--reuse-values`, Helm replays the stored v0.13.x values, which
carry no `prometheus` key. The v0.14.0 chart template accesses
`.Values.prometheus.enabled` directly, which panics on a nil map.

**Workaround — pass `prometheus.enabled` explicitly:**
```bash
helm upgrade kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
  --version v0.14.0 -n kai-scheduler \
  --reuse-values \
  --set prometheus.enabled=false
```

If you were previously using `prometheus.externalPrometheusUrl`, pass that too:
```bash
  --set prometheus.enabled=true \
  --set prometheus.externalPrometheusUrl=<your-url>
```

### Helm rollback from v0.14.0 fails

`helm rollback` does not work for this chart — **not just across v0.13.x/v0.14.0,
but in general.** This is a pre-existing issue not specific to the v0.14.0
upgrade. See [Helm Rollback Not Supported](../README.md#-helm-rollback-not-supported)
for details and the downgrade procedure.

## 3. Upgrade Procedure

1. **Review custom Helm values.** If you set `operator.replicaCount > 1` and
   relied on leader election being disabled, add `global.leaderElection: false`
   explicitly to preserve the old behavior (not recommended).

2. **Run the upgrade** with the prometheus workaround:
   ```bash
   helm upgrade kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
     --version v0.14.0 -n kai-scheduler \
     --reuse-values \
     --set prometheus.enabled=false
   ```

3. **Verify:**
   ```bash
   # Check all KAI pods are running
   kubectl get pods -n kai-scheduler

   # Verify queues are intact
   kubectl get queues

   # Check scheduler logs for errors
   kubectl logs -n kai-scheduler deployment/kai-scheduler-default --tail=20
   ```

4. **Do not use `helm rollback`.** See [Downgrade Procedure](#4-downgrade-procedure-v0140--v013x).

## 4. Downgrade Procedure (v0.14.0 → v0.13.x)

Since `helm rollback` is not supported, use uninstall + reinstall.

1. **Uninstall v0.14.0:**
   ```bash
   helm uninstall kai-scheduler -n kai-scheduler
   ```
   The following resources are preserved:
   - All Queues and PodGroups (`resource-policy: keep`)
   - All CRDs
   - SchedulingShard "default"
   - `kai-resource-reservation` namespace and reservation pods
   - Running workloads (already bound to nodes)

2. **Reinstall v0.13.x:**
   ```bash
   helm install kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
     --version v0.13.4 -n kai-scheduler
   ```

3. **Verify:**
   ```bash
   kubectl get pods -n kai-scheduler
   kubectl get queues
   kubectl get pods -n <workload-namespace>
   ```
