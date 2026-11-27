# Usage Database Authentication

*Status: Proposed*

Related: [#2639](https://github.com/kai-scheduler/KAI-Scheduler/issues/2639) (request), [kai-scheduler/api#34](https://github.com/kai-scheduler/api/pull/34) and [#2640](https://github.com/kai-scheduler/KAI-Scheduler/pull/2640) (prototype of this design, draft)

> Goals and background of time-based fair share are in [`time-based-fairshare.md`](time-based-fairshare.md), and the way the scheduler fetches usage is in [`data-fetching.md`](data-fetching.md). This document covers one thing: how the usage database client authenticates.

## Motivation

Time-based fair share reads queue usage from a Prometheus-compatible store. The store can be external, and a shared metrics store is usually not open: it sits behind basic auth or a token. Examples are VictoriaMetrics behind vmauth, Mimir, Thanos behind a gateway, and managed Prometheus services.

The use case that drives this: several clusters each run KAI and order their queues by the usage of the same teams across all clusters. The usage is aggregated in one central store, so every scheduler queries that store instead of an in-cluster Prometheus.

Today there is no way to give the client a credential:

- The Prometheus client is built from the address alone ([`prometheus.go`](../../../../pkg/scheduler/cache/usagedb/prometheus/prometheus.go), `NewPrometheusClient`).
- `connectionString` is a field of the SchedulingShard, and the operator copies it into the scheduler ConfigMap. A credential in the URL is plain text in both objects, and in Git for a GitOps install. The scheduler also prints the connection string at log level 3 ([`hub.go`](../../../../pkg/scheduler/cache/usagedb/hub.go), `GetClient`).
- `connectionStringEnvVar` names an environment variable, but nothing can set it. The operator builds the environment of the scheduler container itself ([`resources_for_shard.go`](../../../../pkg/operator/operands/scheduler/resources_for_shard.go), `deploymentForShard`), and neither Config nor SchedulingShard has a field for extra environment variables or Secret references.

The workaround is an authenticating reverse proxy next to the scheduler, restricted by a NetworkPolicy, with `connectionString` pointing at it. That is one more component to run for a single HTTP header.

## Goals

- The usage database client can send basic auth or an `Authorization` header.
- A credential lives only in a Secret. It is not in the SchedulingShard, the scheduler ConfigMap, the pod spec or the logs.
- A rotated Secret is picked up without a scheduler restart.
- With `auth` unset, nothing changes: the operator renders the same Deployment and ConfigMap as today.

## Non-Goals

- TLS settings: a private CA, client certificates, `insecureSkipVerify`. They fit under the same `auth` block later.
- OAuth2, SigV4 and other schemes that need a token exchange or request signing.
- Authentication for the operator's own probe of `spec.prometheus.externalPrometheusUrl` (see [Interaction with `externalPrometheusUrl`](#interaction-with-externalprometheusurl)).
- A general way to add environment variables or volumes to the scheduler pod.

## API

A new optional `auth` block on `usageDBConfig`, with Secret references in the shape the Prometheus Operator uses for `basicAuth` and `authorization`:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
spec:
  usageDBConfig:
    clientType: prometheus
    connectionString: https://metrics.example.com/prometheus
    auth:
      basicAuth:
        username:
          name: kai-usage-db
          key: username
        password:
          name: kai-usage-db
          key: password
      # or, instead of basicAuth:
      authorization:
        type: Bearer          # default
        credentials:
          name: kai-usage-db
          key: token
```

The types go in `github.com/kai-scheduler/api/usagedb`, next to `UsageDBConfig`:

```go
type UsageDBConfig struct {
    // ...
    // Auth configures how the client authenticates to the usage database.
    // If not set, requests carry no credentials.
    Auth *Auth `json:"auth,omitempty"`
}

// BasicAuth and Authorization are mutually exclusive.
type Auth struct {
    BasicAuth     *BasicAuth     `json:"basicAuth,omitempty"`
    Authorization *Authorization `json:"authorization,omitempty"`
}

type BasicAuth struct {
    Username *corev1.SecretKeySelector `json:"username,omitempty"`
    Password *corev1.SecretKeySelector `json:"password,omitempty"`
    // Set by the operator in the scheduler configuration.
    UsernameFile string `json:"usernameFile,omitempty"`
    PasswordFile string `json:"passwordFile,omitempty"`
}

type Authorization struct {
    // Defaults to "Bearer". "Basic" is not accepted: use BasicAuth.
    Type        string                    `json:"type,omitempty"`
    Credentials *corev1.SecretKeySelector `json:"credentials,omitempty"`
    // Set by the operator in the scheduler configuration.
    CredentialsFile string `json:"credentialsFile,omitempty"`
}
```

Rules, checked by one `Auth.Validate()` that both the operator and the scheduler call:

- `basicAuth` and `authorization` are mutually exclusive. With neither, there is no authentication.
- Each value has exactly one source: the Secret reference or the file path, not both.
- `basicAuth` needs a username. The password is optional.
- `authorization` needs credentials. `type: Basic` is rejected.
- A Secret reference needs both `name` and `key`.

`UsageDBConfig` is one Go type with two uses: it is the `usageDBConfig` field of the SchedulingShard, and the operator serializes it into the scheduler configuration file. `Auth` follows that. A user sets the Secret references on the SchedulingShard, and the scheduler configuration carries the `*File` fields. See [open question 1](#open-questions).

## Semantics

### Operator

For a SchedulingShard whose `auth` has Secret references, the operator does three things.

**Mounts the keys.** It adds one projected volume, `usage-db-auth`, to the scheduler pod, mounted read-only at `/etc/usage-db-auth`. Each referenced Secret key becomes one file: `username`, `password` or `credentials`. The Secrets are read from the namespace KAI runs in. A `SecretKeySelector` has no namespace field, and the kubelet can only mount a Secret from the pod's own namespace.

**Rewrites the scheduler configuration.** In the ConfigMap, every Secret reference is replaced by the path of its mounted file:

```yaml
usageDBConfig:
  clientType: prometheus
  connectionString: https://metrics.example.com/prometheus
  auth:
    basicAuth:
      usernameFile: /etc/usage-db-auth/username
      passwordFile: /etc/usage-db-auth/password
```

The ConfigMap holds no credential and no Secret name. The scheduler needs no RBAC on Secrets, because the kubelet delivers the files.

**Checks that the Secrets exist.** Before it renders the Deployment, the operator gets each referenced Secret and checks the key. If one is missing, the reconcile of the shard fails with an error that names the field, the Secret and the key, and the running scheduler is left as it is. The reason is that the scheduler Deployment uses the `Recreate` strategy: a new pod that cannot mount its volume stays in `ContainerCreating` after the old pod is gone, so a mistyped Secret name would leave the shard with no scheduler. The reconcile is retried, so creating the Secret afterwards is enough. A reference with `optional: true` is not checked and is mounted as optional. The operator's ClusterRole already has `get` on Secrets.

Changing `auth` on the shard changes the Deployment and the ConfigMap, and the scheduler restarts, as it does for other shard changes. Changing only the content of a Secret restarts nothing.

### Scheduler

`GetClientFn` gains an `auth` argument. The Prometheus client wraps its transport in a `RoundTripper` from `github.com/prometheus/common/config`, which is already a dependency: `NewBasicAuthRoundTripper` or `NewAuthorizationCredentialsRoundTripper`, each given file-backed secrets (`NewFileSecret`). The fake clients ignore the argument.

- **Rotation.** The file is read on every request. After a Secret changes, the kubelet updates the mounted file within its sync period, and the next usage fetch sends the new value. Surrounding whitespace in the file, such as a trailing newline, is trimmed.
- **The scheduler reads files only.** If its configuration still has a Secret reference, client creation fails with a message that says to set the `*File` field or let the operator mount the Secret. The scheduler does not call the API server for credentials.
- **Failures are fetch failures.** A file that cannot be read, a 401 or a 403 fail that fetch, not the scheduler. The existing handling applies: the scheduler keeps the last usage until `stalenessPeriod` and then treats usage as unavailable.
- **Userinfo and `auth` together are rejected.** Go's HTTP client turns the userinfo of a URL into an `Authorization` header before the transport runs, and the `prometheus/common` round trippers leave an existing `Authorization` header alone. A connection string with userinfo would therefore silently win over `auth`. Client creation fails instead when both are set.

### Logging

The scheduler stops logging the raw connection string. A URL is logged with the password of its userinfo redacted (`url.URL.Redacted()`). A string that is not a URL may be a DSN with a credential anywhere in it, so it is logged as `<redacted>`. This applies whether or not `auth` is set, and also to a connection string that comes from `connectionStringEnvVar`.

### Interaction with `externalPrometheusUrl`

When `usageDBConfig` has no connection string, the operator fills it from `spec.prometheus.externalPrometheusUrl` of the Config. That field also starts a health probe in the operator, which requests `<url>/api/v1/status/config` without credentials and sets the Config's `Available` condition to false on any non-2xx answer. Against a store that requires authentication the probe fails.

This design does not change the probe. For an authenticated store, set `usageDBConfig.connectionString` on the shard and leave `externalPrometheusUrl` unset. Giving the probe the same credentials is possible later, but the Config is cluster-wide and `auth` is per shard, so it needs its own decision.

### Compatibility

- `auth` is optional. A shard without it gets the same Deployment and ConfigMap as before, so upgrading the operator does not restart schedulers because of this feature.
- `connectionString` and `connectionStringEnvVar` keep their meaning.
- `GetClientFn` changes its signature. A client registered through `NewClientResolver` from outside this repository must add the argument.
- The log line for the connection string changes as described above.

## Alternatives Considered

| Alternative | Why not |
|---|---|
| Credential in the connection string (`https://user:pass@host`) | Plain text in the SchedulingShard, the ConfigMap and Git. |
| `connectionStringEnvVar`, plus a new `env` / `envFrom` field on the shard | An environment variable is fixed at pod start, so rotation needs a restart. The whole URL becomes the secret. Arbitrary environment variables for the scheduler are a much wider API than one header. |
| The scheduler reads the Secret through the API server | The scheduler would need RBAC on Secrets and a watch for rotation. Mounting keeps Secret access with the kubelet. |
| The operator copies the values into the ConfigMap or a Secret it generates | A second copy of the credential to keep in sync and to protect. |
| An authenticating proxy in front of the store | Works today, and is the workaround. It is an extra Deployment, Service and NetworkPolicy per cluster. |
| `auth` on the Config (`spec.prometheus`) instead of the shard | The connection string is per shard, and shards can point at different stores. The credential belongs next to the address it is for. |

## Open Questions

| # | Question | Proposal |
|---|---|---|
| 1 | One `Auth` type for both the SchedulingShard and the scheduler configuration, or two? With one type the `*File` fields also appear in the SchedulingShard CRD, where they are only useful for a scheduler that runs without the operator. With two types, the CRD side could reuse `monitoringv1.BasicAuth` and `monitoringv1.SafeAuthorization` directly, but `UsageDBConfig` itself would have to be split. | One type, as in the prototype. It is the smaller change and keeps `UsageDBConfig` shared. |
| 2 | Fail the reconcile on a missing Secret, or render the pod and let it wait in `ContainerCreating`? | Fail the reconcile, because of the `Recreate` strategy. |
| 3 | Validate only in Go (`Auth.Validate()`), or also with CEL rules in the CRD so the API server rejects an invalid shard? | Start with Go. Add CEL for the mutual exclusions if reviewers want admission-time errors. |
| 4 | Should the failed reconcile also be visible as a condition on the SchedulingShard, not only in the operator log? | Yes. The SchedulingShard status already has `conditions`; whether to do it in the same pull request is up to reviewers. |

## Test Plan

- Unit tests for `Auth.Validate()` in the api repository.
- Unit tests for the operator: the projected volume, the rewrite to file paths, the Secret check, and that a shard without `auth` renders no volume.
- Unit tests for the scheduler: the header on every usage query against an `httptest` server, a rotated file, an unreadable file, a configuration the scheduler must reject, and the log redaction.
- One end-to-end case in the time-aware suite: a Prometheus behind basic auth, a shard with `auth`, and usage that reaches the scheduler.

## Rollout

Two pull requests, because the API types live in their own repository:

1. `kai-scheduler/api`: the `Auth` types and `Validate()`.
2. `kai-scheduler/KAI-Scheduler`: bump the api module, the operator and scheduler changes above, the regenerated SchedulingShard CRD, and a section in [`docs/time-based-fairshare/README.md`](../../../time-based-fairshare/README.md).
