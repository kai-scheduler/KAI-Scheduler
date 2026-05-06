# Migration Guide: v0.14.x → v0.15.0

This guide applies to anyone upgrading from any earlier release line that has the `kai-config` Helm hook (v0.9.4 and later) to v0.15.0.

If you are installing v0.15.0 fresh, this guide does not apply.

## 1. What Changed

v0.15.0 manages the `kai-config` resource as a regular chart resource instead of a Helm `pre-install,pre-upgrade` hook. This fixes [#1536](https://github.com/kai-scheduler/KAI-Scheduler/issues/1536), where the default `before-hook-creation` policy deleted and recreated the CR on every upgrade — cascading via `ownerReferences` to all operand `ServiceAccounts` and leaving scheduler pods with stale projected tokens until kubelet rotated them at ~80% TTL.

The chart change is a one-line removal of the `helm.sh/hook` annotations from `templates/kai-config.yaml`. The catch on first upgrade: the existing `kai-config` CR on the cluster was originally created as a Helm hook, and Helm-created hooks do not carry the standard release-ownership labels and annotations that Helm requires for adopting a regular release resource. Without intervention the upgrade fails with:

```
Error: UPGRADE FAILED: unable to continue with update: Config "kai-config" in
namespace "" exists and cannot be imported into the current release: invalid
ownership metadata; label validation error: missing key
"app.kubernetes.io/managed-by": must be set to "Helm"; annotation validation
error: missing key "meta.helm.sh/release-name": must be set to "kai-scheduler";
annotation validation error: missing key "meta.helm.sh/release-namespace": must
be set to "kai-scheduler"
```

## 2. Required Steps

Pass `--take-ownership` on the **first** `helm upgrade` to v0.15.0:

```bash
helm upgrade kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  --version v0.15.0 --namespace kai-scheduler --take-ownership
```

`--take-ownership` instructs Helm to adopt existing resources whose ownership labels and annotations don't match the release. As part of the upgrade, Helm stamps the missing labels and annotations on the existing `kai-config` CR. After this single upgrade, the CR is properly tracked in the release manifest, and subsequent upgrades work without the flag.

`--take-ownership` was introduced in Helm 3.17.0 ([helm/helm#13439](https://github.com/helm/helm/pull/13439)). Helm 3.16 or older does not support the flag — upgrade your Helm CLI before running the command.

## 3. Verification

After the upgrade, confirm the `kai-config` CR carries the release-ownership labels and annotations:

```bash
kubectl get config kai-config -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}'
# Expected: Helm

kubectl get config kai-config -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}'
# Expected: kai-scheduler   (or your release name if non-default)
```

Confirm the scheduler `ServiceAccount` UID is unchanged across the upgrade (capture before and after):

```bash
kubectl get sa scheduler -n kai-scheduler -o jsonpath='{.metadata.uid}'
```

The scheduler `Deployment` should continue running without a rolling restart, and the pod logs should be free of `Unauthorized` and `error retrieving resource lock` lines after the upgrade.

## 4. Cross-References

- [#1536](https://github.com/kai-scheduler/KAI-Scheduler/issues/1536) — root-cause analysis of the SA-recreation behavior this release fixes.
- [v0.9.0 Migration Guide](../v0.9.0/) — required pre-step (manual CRD apply) for direct upgrades from v0.6.x.
