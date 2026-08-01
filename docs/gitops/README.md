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

## ArgoCD health check

ArgoCD has no built-in health assessment for the `kai.scheduler/v1` `Config` and `SchedulingShard` resources, so the Application can report `Healthy` while the operator is still reconciling the CR (or failing to). The operator records progress on the CR's status conditions (`Deployed`, `Available`, `DependenciesFulfilled`), which the e2e suite already gates on instead of Application health (see [#1751](https://github.com/kai-scheduler/KAI-scheduler/issues/1751)).

Add a custom health check to `argocd-cm` so ArgoCD reflects the operator's own status. It reports `Healthy` only when all three conditions are `True` for the current `metadata.generation`, and `Degraded` when `DependenciesFulfilled` is `False` for the current generation, since that is the operator's signal that a required dependency is missing and needs attention. A `False` `Deployed` or `Available` is a normal transient state while pods roll out, so it stays `Progressing` to avoid health-degraded notifications on every install or upgrade:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
  namespace: argocd
data:
  resource.customizations.health.kai.scheduler_Config: |
    local hs = {}
    hs.status = "Progressing"
    hs.message = "Waiting for the KAI operator to reconcile the resource"

    if obj.status == nil or obj.status.conditions == nil then
      return hs
    end

    local generation = obj.metadata.generation
    local byType = {}
    for _, condition in ipairs(obj.status.conditions) do
      byType[condition.type] = condition
    end

    -- DependenciesFulfilled=False (reason dependencies_missing) is the only
    -- condition that means a real, user-actionable dependency is absent, so map
    -- it to Degraded. Deployed=False and Available=False are normal transient
    -- states while pods roll out, so they stay Progressing.
    local deps = byType["DependenciesFulfilled"]
    if deps ~= nil and deps.observedGeneration == generation and deps.status == "False" then
      hs.status = "Degraded"
      hs.message = "DependenciesFulfilled: " .. (deps.message or deps.reason or "dependencies are missing")
      return hs
    end

    local required = {"Deployed", "Available", "DependenciesFulfilled"}
    for _, name in ipairs(required) do
      local condition = byType[name]
      if condition == nil or condition.observedGeneration ~= generation then
        hs.status = "Progressing"
        hs.message = name .. " has not been observed for the current generation"
        return hs
      end
      if condition.status ~= "True" then
        hs.status = "Progressing"
        hs.message = name .. " is " .. tostring(condition.status)
        return hs
      end
    end

    hs.status = "Healthy"
    hs.message = "KAI resource reconciled for the current generation"
    return hs
```

The `SchedulingShard` resource reports the same conditions; add an identical block under `resource.customizations.health.kai.scheduler_SchedulingShard` to get the same health assessment for shards.

## OpenShift

OpenShift auto-detection uses a cluster `lookup`, which returns nothing when ArgoCD renders the chart offline. Set it explicitly, otherwise the `SecurityContextConstraints` and the OpenShift hook pod security contexts are not rendered:

```yaml
valuesObject:
  openshift: true
```

## Testing

GitOps mode is covered end-to-end by `test/e2e/suites/gitops`; run it locally with `hack/run-e2e-gitops-kind.sh --local-images-build`.
