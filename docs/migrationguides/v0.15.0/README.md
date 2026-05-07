# Migration Guide: v0.15.0

## Summary

**No manual steps required.** Standard `helm upgrade` works.

## What Changed

The `kai-config` Config CR is no longer part of the Helm release manifest. It is now applied by a post-install/post-upgrade hook Job (`kai-config-deployer`) that uses `kubectl apply --server-side` to create or update the CR. This fixes [#1536](https://github.com/kai-scheduler/KAI-Scheduler/issues/1536), where the prior Helm-hook lifecycle deleted and recreated `kai-config` on every upgrade — cascading via `ownerReferences` to all operand `ServiceAccounts` and leaving the scheduler's projected token bound to a now-deleted SA UID until kubelet rotated it (~48 minutes of `401 Unauthorized` on every API call).

The deployer hook also strips the leftover `helm.sh/hook` annotations from any pre-existing `kai-config` so previous chart state stops looking like a Helm hook.

## What you might notice

- On `helm install`, `helm upgrade`, and `helm uninstall`, two new Jobs run as hooks (`kai-config-deployer` and, on uninstall, `kai-config-deleter`). Both use the existing `crd-upgrader` image and complete in seconds.
- After `helm upgrade` returns, the `kai-config` CR reflects the new chart values. The operator may take a few extra seconds to roll operand Deployments in place.
- `helm rollback` does not touch `kai-config`. Rollback was already unsupported for this chart (see the [migration index](../README.md)); this is unchanged.

## Cross-References

- [#1536](https://github.com/kai-scheduler/KAI-Scheduler/issues/1536) — root-cause analysis of the SA-recreation behavior this release fixes.
