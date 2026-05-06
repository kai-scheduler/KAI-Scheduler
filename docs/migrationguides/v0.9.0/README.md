# Migration Guide: v0.6.x → v0.9.0+

This guide applies to anyone upgrading directly from a v0.6.x release to v0.9.0 or later (v0.12.x, v0.13.x, v0.14.x, ...).

If you are already on v0.9.0 or later, this guide does not apply.

## 1. What Changed

v0.9.0 introduced the `kai.scheduler/Config` CRD and the `kai-config` resource that the operator reconciles. v0.6.x clusters do not have this CRD installed.

The chart ships the CRD under its `crds/` directory, but Helm only installs files from `crds/` on `helm install` — **never on `helm upgrade`**. A direct `helm upgrade` from v0.6.x to v0.9+ therefore reaches the upgrade with no `Config` CRD on the cluster, and Helm fails with:

```
Error: UPGRADE FAILED: resource mapping not found for name: "kai-config"
namespace: "kai-scheduler" from "": no matches for kind "Config" in version "kai.scheduler/v1"
ensure CRDs are installed first
```

## 2. Required Steps

Apply the chart's CRDs to the cluster **before** running `helm upgrade`:

```bash
# 1. Pull the target chart locally
helm pull oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
  --version <target-version> --untar

# 2. Apply every CRD shipped by the chart (idempotent)
kubectl apply -f kai-scheduler/crds/

# 3. Run the upgrade as usual
helm upgrade kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
  --version <target-version> --namespace kai-scheduler

# 4. Optional cleanup
rm -rf kai-scheduler kai-scheduler-*.tgz
```

`kubectl apply -f kai-scheduler/crds/` is safe to run on a cluster that already has some of these CRDs — `apply` patches existing CRDs in place without disrupting workloads.

## 3. Verification

After step 2, confirm the Config CRD is present:

```bash
kubectl get crd configs.kai.scheduler
```

A successful `helm upgrade` then leaves the operator reconciling a single `kai-config` resource in the release namespace:

```bash
kubectl get config kai-config -n kai-scheduler
```

## 4. Cross-References

If you are upgrading from a release older than v0.6.0, also review the [v0.6.0 Migration Guide](../v0.6.0/) for the resource-reservation namespace and queue label-key changes that affect this same upgrade path.
