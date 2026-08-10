# In-Place Pod Resize

KAI Scheduler supports Kubernetes [in-place pod resize](https://kubernetes.io/docs/tasks/configure-pod-container/resize-container-resources/) (KEP-1287): changing a running pod's CPU and memory requests without restarting it. KAI keeps queue accounting consistent throughout a resize, and applies a best-effort admission check of resize requests against queue limits and quota — a guardrail rather than a strict guarantee (see [Best-effort semantics](#best-effort-semantics)).

Requires Kubernetes 1.33 or newer (the `InPlacePodVerticalScaling` feature gate is enabled by default since 1.33 and GA since 1.35).

## Resizing a pod

Resizes go through the `pods/resize` subresource:

```bash
kubectl patch pod my-pod --subresource resize \
  -p '{"spec":{"containers":[{"name":"main","resources":{"requests":{"cpu":"2"}}}]}}'
```

Only CPU and memory can be resized; GPU resources cannot be changed in place.

## Accounting: what a resizing pod is charged

During a resize, a pod's desired requests (spec) and its actual running size can differ. KAI charges the **effective request** per container and resource:

| Resize state | Charged amount |
|---|---|
| No resize in progress | spec |
| Resize pending or in progress | max(spec, actual) |
| Resize marked **Infeasible** by the kubelet | actual only — the unreachable spec is not charged |

"Actual" is the value the kubelet reports in the container status. This model matches the upstream Kubernetes scheduler and kubelet, and applies consistently across:

- scheduler decisions (fair share, node fit, preemption and reclaim),
- queue status (`Queue.status.allocated`, `Queue.status.allocatedNonPreemptible`),
- resize admission (below).

The practical consequences:

- A pod mid-downsize keeps its current (larger) charge until the kubelet completes the downsize — the freed capacity is never promised before it exists.
- A pod whose upsize the kubelet rejected as Infeasible does not hold queue capacity for a size it will never reach.

## Resize admission

A validating webhook on `pods/resize` checks every upsize of a KAI-scheduled pod against the pod's queue hierarchy, before the resize is persisted:

- **Limit** (all workloads): the increase must not push any queue's `allocated` over its CPU or memory `limit`.
- **Quota** (non-preemptible workloads only): the increase must also fit within `quota`, checked against `allocatedNonPreemptible`.

The increase is computed at the pod level, so moving CPU or memory between containers of the same pod is not counted as growth, and downsizes are always admitted.

A rejected resize returns an error such as:

```
resize rejected: pod team-a/trainer (PodGroup pg-trainer) CPU upsize would push
queue team-a Allocated (1000m + 2000m) over limit (2000m)
```

The pod keeps running at its current size; nothing is restarted or evicted.

### Best-effort semantics

Admission is a guardrail, not a transactional guarantee:

- The webhook is registered with `failurePolicy: Ignore` — if the admission service is unavailable, resizes are admitted rather than blocked.
- Lookup failures (queue, podgroup) admit the resize and log an error.
- The check reads `Queue.status.allocated`, which trails actual scheduling decisions through the controller reconcile chain. A resize can therefore race concurrent activity on a nearly-full queue — another resize, or the scheduler placing new pods — and together exceed the limit. The reverse also holds: a recently freed queue may transiently over-deny a valid resize. The scheduler's own accounting stays correct either way and stops further allocation once over the limit.
- A resize issued while the same pod's previous resize is still being enacted is checked against the transient charge. In particular, a pending downsize is still charged until the kubelet enacts it, so a follow-up upsize within that window can reclaim the not-yet-released capacity and settle above the limit.

For workloads where the race above is unacceptable, see `blockUpsizeOnBoundedQueues` below.

## Configuration

Resize admission is configured in the KAI `Config` resource:

```yaml
apiVersion: kai.scheduler/v1
kind: Config
metadata:
  name: kai
spec:
  admission:
    inPlacePodResize:
      validateQuota: true              # default
      blockUpsizeOnBoundedQueues: false # default
```

| Field | Default | Effect |
|---|---|---|
| `validateQuota` | `true` | Master switch. When `false`, the webhook admits all resizes without any checks, and `blockUpsizeOnBoundedQueues` is ignored. |
| `blockUpsizeOnBoundedQueues` | `false` | Strict mode: reject **any** upsize on a queue (or ancestor) that has a finite CPU or memory limit — regardless of free headroom — and, for non-preemptible workloads, on any queue with a finite quota. Closes the concurrent-resize race at the cost of disallowing upsizes on bounded queues entirely. |

## Interaction with queue limits

`limit: -1` on a queue resource means unbounded; any other value (including `0`) is a hard bound enforced on resize. The same convention applies to `quota` for non-preemptible workloads. See [Queues](../queues/README.md) for how limits and quota are configured.

Because checks walk the full queue hierarchy, an upsize must fit under every ancestor's limit, not just the leaf queue's.
