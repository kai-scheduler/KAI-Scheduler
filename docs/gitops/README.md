# Installing KAI Scheduler with ArgoCD (GitOps)

KAI Scheduler can be installed and managed by GitOps tools such as ArgoCD using the same Helm chart, with two values changed:

```yaml
kaiConfigDeployer:
  enabled: false
kaiConfig:
  render: true
```

> **Requires ArgoCD >= 2.10** (for the `PostDelete` hook used by the chart's cleanup job).

## Why

By default the `kai-config` Config CR (the operator's input configuration) is applied out-of-band by a post-install hook Job, so ArgoCD does not track it: no drift detection, no `selfHeal`, and if the CR is deleted the application stays `Synced`/`Healthy` while KAI degrades (see [#1751](https://github.com/kai-scheduler/KAI-scheduler/issues/1751)).

With `kaiConfig.render=true` the CR is rendered inline as a tracked release resource. Because the operator sets `ownerReferences` from the CR to every component it creates, the whole KAI component tree becomes visible in the ArgoCD UI.

| `kaiConfigDeployer.enabled` | `kaiConfig.render` | Result |
|---|---|---|
| `true` (default) | `false` (default) | Hook Job applies the CR out-of-band (plain Helm installs) |
| `false` | `true` | CR tracked inline — **GitOps/ArgoCD mode** |
| `false` | `false` | `kai-config` managed externally |
| `true` | `true` | Rendering fails (mutually exclusive) |

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

- `retry` and the CR's `SkipDryRunOnMissingResource` sync-option cover the first sync, where the Config CRD is not established yet.
- Default Queues, the default SchedulingShard and the resource-reservation namespace carry `Prune=false` (matching their `helm.sh/resource-policy: keep`), so user-modified scheduling configuration is not pruned on sync.

## OpenShift

OpenShift auto-detection uses a cluster `lookup`, which returns nothing when ArgoCD renders the chart offline. Set it explicitly, otherwise the `SecurityContextConstraints` and the OpenShift hook pod security contexts are not rendered:

```yaml
valuesObject:
  openshift: true
```

## Testing

GitOps mode is covered end-to-end by `test/e2e/suites/gitops`; run it locally with `hack/run-e2e-gitops-kind.sh --local-images-build`.
