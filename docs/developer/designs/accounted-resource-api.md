# AccountedResource API Proposal for KAI

This document proposes a phase-1 API for limiting arbitrary resources in KAI
Scheduler. It focuses on the API shape and user-facing behavior. Low-level
scheduler implementation details are intentionally left for a later design.

## Summary

KAI currently has a fixed queue resource model through `spec.resources`, which
is centered on the built-in CPU, memory, and GPU fields already understood by
the scheduler. That model is not flexible enough for clusters that need limits
on arbitrary Kubernetes resources, GPU types, DRA-selected devices, or
capacity-like dimensions such as GPU memory.

The proposal is to add a cluster-scoped `AccountedResource` CRD. An
`AccountedResource` defines how KAI should recognize and count one logical
resource. Queues then reference these resources through
`spec.accountedResources` and set per-queue limits.

Phase 1 is limit-only. It does not add arbitrary-resource fair share, reclaim,
deserved quota, over-quota weight, usage status, or usage metrics.

## Background

KAI users want to express limits on resources that are not cleanly represented
by the current queue fields:

- extended resources such as EFA;
- generic GPU count;
- specific GPU products such as H200 or GB200;
- GPU memory across full GPUs and fractional GPUs;
- DRA-selected device attributes and capacities;
- legacy or runtime-provided pod annotations such as KAI GPU memory requests.

These resources can be discovered in different ways. Some are normal pod
resource requests. Some depend on the selected node. Some depend on the selected
DRA device. Some are exposed through annotations or driver-specific DRA config.

The API should let administrators define these accounting rules once, then let
each queue opt into the resources it wants to limit.

## Design Goals

- Support arbitrary resource names, including CPU, memory, pods, Kubernetes
  extended resources, and GPU resources.
- Support placement-derived accounting, such as counting `nvidia.com/gpu` as
  `gpu-h200` only when the selected node has `nvidia.com/gpu.product: H200`.
- Support DRA resources through device class and selected device attributes.
- Support capacity-style accounting, such as charging selected GPU memory
  instead of only counting devices.
- Keep queue configuration concise.
- Preserve the existing `spec.resources` behavior.
- Avoid ResourceGroup-style registration requirements in phase 1.
- Keep fairness and reclaim out of phase 1 so the limit API can be reviewed
  independently.

## Use Cases

### Generic GPU Limit Plus Per-Type Limits

An administrator wants queues to keep using generic GPU accounting for existing
fairness behavior, but also wants to limit how many H200 or GB200 GPUs each
project can consume.

The same pod allocation should be able to charge multiple logical resources. For
example, a pod that requests `nvidia.com/gpu: 1` and lands on an H200 node can
charge both `gpu` and `gpu-h200`.

The API solves this by defining a generic GPU resource and a placement-derived
GPU-type resource:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: nvidia.com/gpu
---
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-h200
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: nvidia.com/gpu
    match:
      nodeLabels:
        nvidia.com/gpu.product: H200
```

The queue can then set a broad GPU limit and a narrower H200 limit:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: Queue
metadata:
  name: team-a
spec:
  accountedResources:
  - resourceRef: gpu
    limit: "16"
  - resourceRef: gpu-h200
    limit: "4"
```

### GPU Memory Limit

A project should not consume more than a configured amount of total GPU memory,
regardless of how many GPU devices it uses.

This needs to work for DRA-selected device capacity, driver-specific DRA
configuration, and existing KAI GPU memory annotations.

The API solves this with one logical `gpu-memory` resource that can read the
quantity from multiple integrations:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-memory
spec:
  defaults:
    limit: "-1"
  sources:
  - dra:
      deviceClassName: gpu.nvidia.com
    quantity:
      dra:
        selectedCapacity:
          capacityName: memory
  - dra:
      deviceClassName: gpu.nvidia.com
    quantity:
      dra:
        opaqueConfigField:
          driver: gpu.nvidia.com
          path: sharing.mpsConfig.defaultPinnedDeviceMemoryLimit
          unit: Mi
  - podAnnotation:
      key: gpu-memory
      unit: Mi
```

The queue only needs to reference the logical resource:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: Queue
metadata:
  name: team-a
spec:
  accountedResources:
  - resourceRef: gpu-memory
    limit: "320Gi"
```

### Fractional GPU Limit

A project should be limited by the effective fraction of GPU it consumes. For
example, KAI fractional GPU, GPU-memory pods, and full GPU pods should all
contribute to GPU-backed accounted resources using KAI's accepted allocation
view.

The API does not expose a separate fractional-GPU source. Instead, KAI treats
GPU-backed `AccountedResource` objects specially. A resource such as `gpu-h200`
is still configured from the normal GPU request and placement:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-h200
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: nvidia.com/gpu
    match:
      nodeLabels:
        nvidia.com/gpu.product: H200
```

When a workload uses KAI fractional GPU APIs and lands on an H200 node, KAI
charges the accepted GPU fraction against `gpu-h200`. A queue can therefore
limit effective H200 consumption without adding fractional fields to the public
API:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: Queue
metadata:
  name: team-a
spec:
  accountedResources:
  - resourceRef: gpu-h200
    limit: "2"
```

### GPU Memory And GPU Compute Limits

Future GPU integrations may expose both GPU memory and GPU compute as
independent dimensions. A queue may need a limit on either dimension or both.
A workload should be rejected if it would exceed any configured finite limit.

The API solves this by defining separate logical resources for each dimension.
Each resource can use the integration that exposes that quantity:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-memory
spec:
  defaults:
    limit: "-1"
  sources:
  - podAnnotation:
      key: gpu-memory
      unit: Mi
---
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-compute
spec:
  defaults:
    limit: "-1"
  sources:
  - podAnnotation:
      key: gpu-compute
      unit: percent
```

The queue can limit either dimension independently:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: Queue
metadata:
  name: team-a
spec:
  accountedResources:
  - resourceRef: gpu-memory
    limit: "320Gi"
  - resourceRef: gpu-compute
    limit: "400"
```

If a candidate allocation would exceed either finite limit, KAI rejects it
through the same over-limit scheduling path.

### Extended Resource Limit

An administrator wants to limit resources such as EFA without adding a special
hard-coded field to the Queue API.

The API solves this by mapping the Kubernetes extended resource name directly:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: efa
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: vpc.amazonaws.com/efa
```

Queues can then set an EFA limit without any EFA-specific queue field:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: Queue
metadata:
  name: team-a
spec:
  accountedResources:
  - resourceRef: efa
    limit: "32"
```

## Proposed API

### Queue API

Add `spec.accountedResources` to Queue:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: Queue
metadata:
  name: team-a
spec:
  accountedResources:
  - resourceRef: gpu
    limit: "16"
  - resourceRef: gpu-h200
    limit: "4"
  - resourceRef: gpu-memory
    limit: "320Gi"
```

Suggested type shape:

```go
type QueueSpec struct {
    Resources          *QueueResources           `json:"resources,omitempty"`
    AccountedResources []QueueAccountedResource `json:"accountedResources,omitempty"`
}

type QueueAccountedResource struct {
    ResourceRef string            `json:"resourceRef"`
    Limit       resource.Quantity `json:"limit"`
}
```

Queue semantics:

- `resourceRef` references a cluster-scoped `AccountedResource`.
- `limit` is required for explicit phase-1 entries.
- `-1` means unlimited.
- Duplicate `resourceRef` entries in the same queue are rejected.
- Existing `spec.resources` keeps its current meaning.
- `spec.accountedResources` is additive. If both old and new limits apply to a
  workload, both limits are enforced.
- `deserved` and `overQuotaWeight` are intentionally not exposed in phase 1.

### Hierarchical Queue Semantics

`spec.accountedResources` may be configured on any queue in the hierarchy,
including parent queues. Parent queue limits act as aggregate ceilings for the
subtree rooted at that queue. Leaf or child queue limits act as local ceilings.

When KAI evaluates a candidate allocation for a workload in a queue, the
allocation must fit the effective limit of that queue and every ancestor queue.
For each queue in the path, the effective value is the explicit queue entry when
present, otherwise `AccountedResource.spec.defaults.limit`.

For example, if parent queue `research-team` has `gpu-h200` limit `10`, and
children `ml-team` and `cv-team` each have `gpu-h200` limit `8`, each child is
capped at `8`, while the combined subtree usage is capped at `10`.

### AccountedResource CRD

An `AccountedResource` defines how one logical resource is charged.

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-h200
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: nvidia.com/gpu
    match:
      nodeLabels:
        nvidia.com/gpu.product: H200
```

Core semantics:

- `spec.sources` is required.
- Each source sets exactly one of `resourceName`, `dra`, or `podAnnotation`.
- `match` decides when the source applies to a candidate placement.
- `quantity` decides how much to charge when the default quantity is not enough.
- Sources are additive.
- If multiple sources in the same `AccountedResource` match, KAI sums them.
- If multiple `AccountedResource` objects match the same allocation, KAI charges
  all of them.
- `spec.defaults.limit` is optional and defaults to unlimited (`-1`).

Suggested type shape:

```go
type AccountedResourceSpec struct {
    Defaults *AccountedResourceDefaults `json:"defaults,omitempty"`
    Sources  []AccountedResourceSource  `json:"sources"`
}

type AccountedResourceDefaults struct {
    Limit *resource.Quantity `json:"limit,omitempty"`
}

type AccountedResourceSource struct {
    ResourceName  *corev1.ResourceName `json:"resourceName,omitempty"`
    DRA           *DRASource           `json:"dra,omitempty"`
    PodAnnotation *PodAnnotationSource `json:"podAnnotation,omitempty"`

    Match    *AccountedResourceMatch    `json:"match,omitempty"`
    Quantity *AccountedResourceQuantity `json:"quantity,omitempty"`
}

type DRASource struct {
    DeviceClassName string `json:"deviceClassName"`
}

type PodAnnotationSource struct {
    Key  string `json:"key"`
    Unit string `json:"unit,omitempty"`
}

type AccountedResourceMatch struct {
    NodeLabels map[string]string `json:"nodeLabels,omitempty"`
    DRA        *DRAMatch         `json:"dra,omitempty"`
}

type DRAMatch struct {
    DeviceAttributes map[string]DRAAttributeValue `json:"deviceAttributes,omitempty"`
}

type DRAAttributeValue struct {
    String  *string `json:"string,omitempty"`
    Int     *int64  `json:"int,omitempty"`
    Bool    *bool   `json:"bool,omitempty"`
    Version *string `json:"version,omitempty"`
}

type AccountedResourceQuantity struct {
    DRA           *DRAQuantitySource           `json:"dra,omitempty"`
    PodAnnotation *PodAnnotationQuantitySource `json:"podAnnotation,omitempty"`
}

type DRAQuantitySource struct {
    SelectedCapacity *DRASelectedCapacityQuantity   `json:"selectedCapacity,omitempty"`
    OpaqueConfigField *DRAOpaqueConfigFieldQuantity `json:"opaqueConfigField,omitempty"`
}

type DRASelectedCapacityQuantity struct {
    CapacityName string `json:"capacityName"`
}

type DRAOpaqueConfigFieldQuantity struct {
    Driver string `json:"driver"`
    Path   string `json:"path"`
    Unit   string `json:"unit,omitempty"`
}

type PodAnnotationQuantitySource struct {
    Unit string `json:"unit,omitempty"`
}
```

## Example AccountedResources

Generic GPU count:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: nvidia.com/gpu
```

H200 GPUs through device plugin resource plus node label:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-h200
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: nvidia.com/gpu
    match:
      nodeLabels:
        nvidia.com/gpu.product: H200
```

GB200 GPUs through DRA:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-gb200
spec:
  defaults:
    limit: "-1"
  sources:
  - dra:
      deviceClassName: gpu.nvidia.com
    match:
      dra:
        deviceAttributes:
          productName:
            string: GB200
```

EFA extended resource:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: efa
spec:
  defaults:
    limit: "-1"
  sources:
  - resourceName: vpc.amazonaws.com/efa
```

GPU memory from DRA selected capacity, DRA opaque config, or pod annotation:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: AccountedResource
metadata:
  name: gpu-memory
spec:
  defaults:
    limit: "-1"
  sources:
  - dra:
      deviceClassName: gpu.nvidia.com
    quantity:
      dra:
        selectedCapacity:
          capacityName: memory
  - dra:
      deviceClassName: gpu.nvidia.com
    quantity:
      dra:
        opaqueConfigField:
          driver: gpu.nvidia.com
          path: sharing.mpsConfig.defaultPinnedDeviceMemoryLimit
          unit: Mi
  - podAnnotation:
      key: gpu-memory
      unit: Mi
```

## Why This Shape

### Separate Resource Definition From Queue Policy

`AccountedResource` defines how to detect and count a resource. Queue entries
define each queue's policy for that resource. This keeps complex detection logic
out of every queue spec.

### Count All Matching Resources

KAI should not choose one best matching resource. It should evaluate all
matching `AccountedResource` objects independently. This is necessary for common
cases like charging both `gpu` and `gpu-h200` for the same selected H200 GPU.

### Keep Source And Match Separate

The source says where the base request comes from. The match says when the
source applies to a selected placement. For example, a source with
`resourceName: nvidia.com/gpu` can become `gpu-h200` only when the selected node
has an H200 label.

### Keep Phase 1 Limit-Only

Limits can be enforced without deciding fairness and reclaim semantics for
queues that do not participate in every resource. Fairness for partial
participation has gaming and reciprocal reclaim risks. Those rules should be
designed separately after the accounting API is reviewed.

## Built-In GPU Accounting

Pods do not request `AccountedResource` objects directly. KAI evaluates matching
resources against the accepted allocation data.

Phase 1 should reuse KAI's existing GPU accounting logic:

- `gpu-memory` is reserved by name and uses KAI's existing GPU memory logic.
- GPU-backed resources such as `gpu`, `gpu-h200`, and `gpu-gb200` are charged
  from accepted allocation data, not only from raw pod requests.
- GPU-backed detection is implicit:
  - name-based for `gpu-memory`;
  - resource-name based for canonical GPU resources such as `nvidia.com/gpu`;
  - DRA-class based for recognized or configured GPU DRA classes.
- Whole GPU requests, KAI fractional GPU requests, KAI GPU-memory requests, and
  DRA-selected GPU devices should all charge the matching GPU-backed resources.

No `AccountedResource.status` field is required in phase 1 to expose this
classification.

## Validation And User Feedback

Validation is layered:

- CRD schema and CEL validate structure, quantity syntax, key syntax, and basic
  required fields.
- Admission webhook validates cross-field and cross-object rules, including
  source one-of rules, source/quantity compatibility, duplicate queue refs, and
  existing `AccountedResource` references when visible.
- Scheduler validates runtime and cache-dependent state.

Scheduler behavior:

- If a queue references an `AccountedResource` that the scheduler cannot
  resolve, jobs in that queue do not schedule.
- Those jobs should get a fit-error event.
- The queue should get a condition such as
  `AccountedResourcesResolved=False`, reason `AccountedResourceNotFound`.
- This applies even if the queue's configured limit is `-1`.
- If an existing `AccountedResource` cannot be evaluated for a candidate because
  selected runtime data is missing or unparsable, emit an event and skip
  enforcement for that unresolved source or candidate in phase 1.
- If usage can be evaluated and exceeds a finite limit, reject the candidate
  through the existing `OverLimit` path. The message should identify the queue,
  `resourceRef`, current usage, candidate request, and limit.

## Queue Admission Signaling

This design must integrate with the queue-admission signaling work tracked in
https://github.com/kai-scheduler/KAI-Scheduler/issues/1615.

Issue #1615 covers a scheduler behavior problem: pods that cannot run because of
queue quota or capacity limits should not look like ordinary node-resource
failures. If KAI marks those pods as unschedulable in the same way it marks pods
that lack cluster capacity, autoscalers such as Karpenter may provision nodes
even though the workload is blocked by queue policy and cannot be admitted by
adding capacity.

`AccountedResource` limits introduce new queue-policy rejection reasons. Any
solution for #1615, whether it uses scheduling gates or another admission
signal, must apply globally to all queue-capacity failures, including:

- current CPU, memory, and GPU limits from `spec.resources`;
- every finite limit in `spec.accountedResources`;
- default limits from `AccountedResource.spec.defaults.limit`;
- validation failures where a queue references an unresolved
  `AccountedResource` and jobs in that queue are blocked from scheduling.

The important boundary is that AccountedResource over-limit failures are not
node-fit failures. They should be surfaced through the same global
queue-admission mechanism that #1615 defines, so AccountedResource limits do not
create a new path that accidentally triggers scale-up for jobs blocked by queue
policy.

## Phase Boundaries

Phase 1 intentionally stops at limit enforcement. The proposal defines how to
name, detect, count, and limit accounted resources, but it does not decide how
those resources participate in fairness or reclaim.

The deferred work falls into three groups:

- Fairness policy: per-`AccountedResource` deserved quota, over-quota weight,
  borrowing, lending, and arbitrary-resource fair share.
- Reclaim policy: same-resource reclaim evidence, reciprocal reclaim
  prevention, and placement-aware reclaim for topology-derived resources.
- Operational visibility and migration: usage status, metrics,
  `AccountedResource.status`, and any explicit bridge from existing
  `spec.resources` fields into `spec.accountedResources`.

Keeping these out of phase 1 makes the first API review about accounting and
limits only. Fairness and reclaim can then be designed with explicit
participation rules instead of being inferred from limit configuration.
