# Installing KAI Scheduler with ArgoCD (GitOps)

KAI Scheduler can be installed and managed by GitOps tools such as ArgoCD using the same Helm chart, with two values changed:

```yaml
kaiConfigDeployer:
  enabled: false
kaiConfig:
  render: true
```

This document explains why these values are needed, provides a full `Application` example, and covers migration, uninstall and known limitations.

> **Requires ArgoCD >= 2.10** (for `PostDelete` hook support used by the chart's cleanup job).

## Why the default install mode doesn't fit GitOps

By default, the chart does not render the `kai-config` Config CR (the operator's input configuration) as a release resource. Instead, a post-install/post-upgrade hook Job (`kai-config-deployer`) applies it out-of-band with `kubectl apply --server-side`. This works well for plain Helm, but under ArgoCD the CR is invisible to the GitOps engine:

- It is not part of the rendered manifests, so ArgoCD does not track it — no drift detection, no `selfHeal`, not shown in the application tree.
- If the CR is deleted or modified, ArgoCD still reports the application as `Synced`/`Healthy` while the operator degrades and KAI components stop functioning (see incident [#1751](https://github.com/kai-scheduler/KAI-scheduler/issues/1751), addressed by [#1794](https://github.com/kai-scheduler/KAI-scheduler/issues/1794)).

Setting `kaiConfig.render=true` renders the CR inline as a regular chart resource, so ArgoCD tracks, drift-detects and self-heals it. Because the KAI operator sets `ownerReferences` from the Config CR to every component it creates, the whole KAI component tree becomes visible in the ArgoCD UI under the Config resource.

### Install mode matrix

| `kaiConfigDeployer.enabled` | `kaiConfig.render` | Result |
|---|---|---|
| `true` (default) | `false` (default) | Hook Job applies the CR out-of-band (plain Helm installs) |
| `false` | `true` | CR rendered inline as a tracked release resource — **GitOps/ArgoCD mode** |
| `false` | `false` | Chart never creates `kai-config` — managed externally |
| `true` | `true` | Template rendering fails with a clear error (mutually exclusive) |

## Application example

Register the OCI registry as a Helm repository (`enableOCI: "true"`), then:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kai-scheduler
  namespace: argocd
spec:
  project: default
  source:
    repoURL: ghcr.io/kai-scheduler/kai-scheduler
    chart: kai-scheduler
    targetRevision: <VERSION>
    helm:
      valuesObject:
        kaiConfigDeployer:
          enabled: false
        kaiConfig:
          render: true
  destination:
    server: https://kubernetes.default.svc
    namespace: kai-scheduler
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
    retry:
      limit: 3
      backoff:
        duration: 10s
        factor: 2
```

Notes:

- `retry` covers transient first-sync errors while the Config CRD is being established. The Config CR itself carries `argocd.argoproj.io/sync-options: SkipDryRunOnMissingResource=true,ServerSideApply=true`, so a fresh install where the CRD does not exist yet does not fail the dry-run.
- The chart's Helm hooks (CRD upgrader, topology migration) are translated by ArgoCD to `PreSync` hooks automatically. The post-delete cleanup Job carries an explicit `argocd.argoproj.io/hook: PostDelete` annotation and runs when the application is deleted.
- Default Queues, the default SchedulingShard and the resource-reservation namespace are annotated with `argocd.argoproj.io/sync-options: Prune=false` (matching their `helm.sh/resource-policy: keep`), so user-modified scheduling configuration is not pruned on sync.

## OpenShift

The chart normally auto-detects OpenShift with a cluster `lookup`, which returns nothing under offline rendering (ArgoCD's repo-server runs `helm template` without cluster access). On OpenShift, set it explicitly:

```yaml
valuesObject:
  openshift: true
```

Without it, the `SecurityContextConstraints` and the OpenShift-specific hook pod security contexts are not rendered.

## Migrating an existing hook-managed install

**To ArgoCD:** create the `Application` above pointing at the existing installation. With `ServerSideApply=true`, ArgoCD adopts the live `kai-config` CR (previously field-managed by `kai-config-deployer`) on the first sync — no manual steps required. Fields that were set by the deployer but are absent from the chart values may remain co-owned by the stale `kai-config-deployer` field manager; inspect with `kubectl get config kai-config --show-managed-fields -o yaml` if leftover fields need cleanup.

**Plain Helm, switching to `kaiConfig.render=true`:** Helm refuses to adopt a resource it did not create (`invalid ownership metadata ... cannot be imported`). Adopt the CR once before upgrading:

```sh
kubectl annotate config kai-config meta.helm.sh/release-name=<RELEASE_NAME> meta.helm.sh/release-namespace=<NAMESPACE>
kubectl label config kai-config app.kubernetes.io/managed-by=Helm
```

## Uninstall

Deleting the `Application` (with prune) removes the Config CR; all operator-created components are garbage-collected through their `ownerReferences`, and the chart's `PostDelete` cleanup Job removes the remaining operator-managed resources. Default Queues, the default SchedulingShard and the resource-reservation namespace are intentionally kept (`Prune=false` / `resource-policy: keep`).

## Known limitations under offline rendering

Helm `lookup` calls return nothing when ArgoCD renders the chart, so:

- OpenShift is never auto-detected — set `openshift: true` explicitly (see above).
- The resource-reservation namespace and ServiceAccount are always rendered. If they are managed elsewhere, set `global.resourceReservation.createNamespace=false` / `global.resourceReservation.createServiceAccount=false`.
- The scaling-pod namespace (`nodescaleadjuster.scalingPodNamespace`) is always rendered when `global.clusterAutoscaling=true`, and ArgoCD adopts a pre-existing one.
- The Config CR has no custom ArgoCD health check; the application can report `Healthy` before the operator finishes reconciling the CR.
