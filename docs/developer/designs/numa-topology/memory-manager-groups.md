<!--
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# Modeling Memory Manager Groups from Observed Pod Placement

*Status: Draft*

## Summary

Kubelet's static Memory Manager maintains disjoint NUMA cell groups which are not represented by
per-zone resource quantities. A NUMA node with an active singleton allocation cannot join a wider
group, while members of an active cross-NUMA group cannot be allocated independently or as part of
a different group. Ignoring this state can make KAI rank a node using the wrong locality or bind a
pod which kubelet later rejects.

The kubelet podResources API already reports each container's Memory Manager blocks, including the
complete NUMA affinity and reserved size. The NUMA placement exporter (NPE) will aggregate those
blocks into a pod-level list of memory groups and amounts. The existing observed-placement
annotation remains unchanged.

The NUMA plugin will use the group masks to enforce Memory Manager compatibility and their reported
amounts to reconstruct aggregate free memory within each group. Ownership remains pod-level because
KAI allocates, evicts, and rolls back whole pods.

Related material:

- [KAI issue #2013](https://github.com/kai-scheduler/KAI-Scheduler/issues/2013)
- [NUMA topology design](README.md)
- [NUMA placement exporter design](../numa-placement-exporter/README.md)

## Problem

Linux can constrain a container to a set of NUMA memory nodes, but it cannot enforce an independent
memory limit for every node in that set. Static Memory Manager therefore treats an active affinity
mask as one allocation unit:

- a singleton such as `{0}` prevents a new cross-NUMA allocation containing node 0;
- a group such as `{0,1}` prevents new `{0}`, `{1}`, `{0,1,2}`, or other overlapping allocations;
- an existing group may be reused by another allocation with exactly the same mask;
- a group remains active until its last owning pod is removed.

NRT reports capacity and availability independently for each zone. The current observed-placement
annotation reports a pod's per-zone resource totals, but it cannot represent a cross-NUMA Memory
Manager affinity. NPE therefore drops a memory block whose podResources topology contains more than
one NUMA node.

A mask alone is also insufficient. To decide whether another allocation fits in an existing group,
the scheduler needs the aggregate amount reserved from that group. Inferring it from PodSpec
requests is unsafe because a reported block can have zero reserved bytes while the assignment
continues to keep the group active.

## Goals

- Keep `kai.scheduler/numa-placement-observed` backward compatible.
- Report every active singleton and cross-NUMA Memory Manager group at pod granularity.
- Report the Memory Manager-reserved amount for every memory resource in each group.
- Reconstruct aggregate group availability from NRT `Allocatable` without inventing a per-zone byte
  split.
- Remove a group only after its final pod owner is removed.
- Filter modeled nodes when known Memory Manager state makes allocation impossible. Supported
  Topology Manager policies are `single-numa-node`, `restricted`, and `best-effort`.
- Score using the final Memory Manager mask rather than only zones with positive byte attribution.
- Update group capacity and ownership atomically during allocation, preemption, uneviction, and
  rollback.
- Reject managed-memory allocations explicitly when the effective group state is unknown.

## Non-Goals

- Reading or parsing the kubelet `memory_manager_state` checkpoint.
- Reporting physical page residency or a per-zone byte split for a cross-NUMA block.
- Changing the existing observed-placement annotation or its per-zone aggregation.
- Changing kubelet allocation behavior or making exporter, scheduler, and kubelet updates atomic.
- Applying static Memory Manager rules to Burstable or BestEffort QoS memory.
- Modeling ordinary init-container Memory Manager allocations and reservation reuse.

## NPE Reporting

### podResources Source

The podResources `List` response contains a list of `ContainerMemory` records for each represented
container. Each record contains:

- `memory_type`, such as ordinary memory or a hugepage size;
- `size`, representing the amount reserved by Memory Manager;
- `topology.nodes`, containing the complete Memory Manager NUMA affinity.

NPE currently accepts a memory record only when `topology.nodes` contains exactly one node. The new
path retains the full mask and size before the existing pod-level zone aggregation loses them.

NPE must retain zero-sized records. A reported zero-sized block still records assignment ownership
and contributes to the group's lifetime; its mask must not be discarded or its amount inferred
from PodSpec requests.

The podResources API reports application containers and restartable init containers, but omits
ordinary init containers. Memory Manager can therefore have state which NPE cannot observe through
this API. Ordinary init-container memory placement is outside this design's scope; NPE checks
completeness only for application containers and restartable init containers (native sidecars).

Every completeness decision joins podResources data with a node-scoped pod informer cache. NPE does
not publish group state until that cache has synchronized and contains the matching pod. Pod events
and podResources polling both trigger reconciliation.

### Existing Placement Annotation

The existing annotation remains byte-for-byte compatible:

```yaml
kai.scheduler/numa-placement-observed: |-
  [
    {"zone":"node-0","amount":{"cpu":"20","nvidia.com/gpu":"2","memory":"42949672960"}}
  ]
```

It continues to contain per-zone totals aggregated across the pod's containers. Singleton memory
blocks may continue contributing to `amount`. Cross-NUMA blocks remain absent because podResources
does not report their per-zone split.

### Memory Groups Annotation

NPE adds a separate pod annotation:

```yaml
kai.scheduler/numa-memory-groups-observed: |-
  [
    {
      "memoryNodes": ["node-0", "node-1"],
      "amount": {
        "memory": "161061273600",
        "hugepages-2Mi": "2147483648"
      }
    },
    {
      "memoryNodes": ["node-2"],
      "amount": {
        "memory": "0"
      }
    }
  ]
```

NPE aggregates all blocks belonging to the pod by canonical mask. For blocks with the same mask,
it sums sizes by memory resource. It publishes each distinct mask once, even when several
containers use it. The pod UID is the implicit owner.

`memoryNodes` uses the same durable NRT zone identifiers as the existing placement record. NPE
sorts and deduplicates masks, sorts groups deterministically, and patches an annotation only when
its canonical serialized value changes.

The scheduler resolves each pod's group ownership using this precedence:

| Observed annotation | Group ownership |
| --- | --- |
| Absent | Use a complete prediction if available; otherwise unknown. |
| `[]` | Complete observation: the pod owns no groups. Replaces prediction. |
| Group list | Complete observation. Replaces prediction, never adds to it. |
| `null`, malformed, or invalid | Unknown; do not fall back to prediction. |

For prediction fallback, prefer the active BindRequest over the predicted pod annotation, matching
the existing zone-placement precedence.

NPE writes the literal annotation string `"null"` when it detects incomplete or invalid state,
including when an earlier complete observation regresses. JSON null in a metadata merge patch would
delete the key and incorrectly enable prediction fallback. Before NPE has made a decision, the key
may remain absent so prediction bridges the bind-to-observation interval.

Normal pre-start delay with no observed allocation leaves the key absent. Once partial
long-running managed-memory allocation appears or a complete observation regresses, write
`"null"` until a complete list is available.

### Completeness

NPE determines completeness using the podResources response together with the PodSpec and current
PodStatus:

- on every reconciliation, every expected application container and restartable init container
  requesting managed memory must have started and be represented; missing containers make the
  observation incomplete, including after an earlier complete list;
- every requested managed memory resource for each represented Guaranteed container must have a
  corresponding block, even if its size is zero.

NPE publishes a complete group list only after these checks pass. A node's group state is known only
when every active Guaranteed pod which requests managed memory has a complete observed or predicted
group record. This includes pods not managed by KAI.

NPE validates pod-local completeness and block structure. The scheduler validates node-wide mask
compatibility and aggregate owner amounts when reconstructing the ledger.

### Compatibility and Rollout

NPE, the scheduler, binder, and CRD changes are released together. The existing placement wire
format does not change; older schedulers ignore the additional annotation.

Predictions are persisted in BindRequest and copied by the binder to
`kai.scheduler/numa-memory-groups-predicted`. The new optional BindRequest field requires an upgraded
CRD schema; an older schema prunes it. An older binder still binds pods but does not copy the new
annotation. In that case, the scheduler can use the persisted BindRequest while it exists.

`pod_info.NUMAPlacement` is internal to the scheduler binary, not a wire format. Explicit conversions
produce the existing zone records and separate group records, so changing its Go shape does not
require matching binder or NPE versions.

## Scheduler Data Model

### Durable Group Placement

The API representation adds one pod-level type:

```go
type NUMAMemoryGroupPlacement struct {
    MemoryNodes []string        `json:"memoryNodes"`
    Amount      v1.ResourceList `json:"amount"`
}
```

`NUMAZonePlacement` and both existing placement annotations remain unchanged. BindRequest gains a
`PredictedNUMAMemoryGroups []NUMAMemoryGroupPlacement` field.

The internal `PodInfo` representation stores these groups in `NUMAPlacement.MemoryGroups`, using
NUMA zone indices, not in another parallel field. Groups are deduplicated by mask and amounts are
aggregated by resource. Container identity is needed only in the request solver's temporary state.

Resolve observed versus predicted groups using the precedence table, then store one complete
pod-level result. Do not retain separate observed and predicted owners in the node ledger.

### Node Memory Group State

Each node topology gains a cloneable Memory Manager ledger:

```go
type MemoryGroupState struct {
    Status MemoryGroupStatus
    Groups map[NUMAMask]*MemoryGroup
}

type MemoryGroup struct {
    Owners map[types.UID]v1.ResourceList
}
```

`NUMAMask` is a canonical bitmask of topology zone indices. A pod may own several
disjoint groups. If several containers in the same pod use one mask, their amounts are summed into
one owner entry.

For a group and memory resource:

```text
capacity  = sum(zone.Allocatable for every zone in the mask)
used      = sum(owner amount for every pod in the group)
available = capacity - used
```

Derive capacity and availability from topology and owner amounts rather than maintaining another
mutable counter. Removing the final owner deletes the group.

The calculation trusts NRT `Allocatable`, as the NUMA plugin already does for physical capacity and
preferred-width decisions. Equality with Memory Manager's internal allocatable values is a
deployment assumption rather than a new NPE validation step. Group usage and deallocation remain
exact relative to that capacity; kubelet admission remains the backstop if the sources diverge.

The effective state is:

| State | Scheduler behavior |
| --- | --- |
| Known | Enforce group compatibility and capacity. An empty `Groups` map means no active constraints. |
| Unknown | Reject requests for managed memory with an explicit unknown-state error. CPU/device-only requests do not require group state. |

Known-empty is not a separate status. `MemoryGroupStatus` needs only `Unknown` and `Known`, with
`Unknown` as the zero value.

On an eligible NUMA node with no active managed-memory consumers, initialize the ledger as known
empty. The deployment must configure Memory Manager's static policy on nodes whose reported memory
is modeled, unless those resources are ignored; NRT supplies topology and capacity, not proof of
the kubelet policy. The first allocation can then
create a group and constrain later allocations in the cycle and through its BindRequest. Any
active relevant pod without a complete observed or predicted record makes the ledger unknown.
Include `Releasing` and `StuckInReleasing` pods while their allocation remains active.

Predicted groups created during the current scheduling cycle are added to a known ledger and
constrain later allocations. Predictions do not turn an initially unknown ledger into known state.

### Internal NUMA Placement

`pod_info.NUMAPlacement` changes from a slice of zone placements into the complete internal
allocation identity returned by the solver and stored on the task:

```go
type MemoryGroupPlacement struct {
    Mask   NUMAMask
    Amount v1.ResourceList
}

type NUMAPlacement struct {
    Zones        []ZonePlacement
    MemoryGroups []MemoryGroupPlacement
}
```

`Zones` preserves the existing pod-level per-zone quantities for CPU, devices, and persistence.
`MemoryGroups` contains the final pod-level masks and reserved amounts needed to
mutate the group ledger and persist the Memory Manager prediction. Keeping both in one value makes
clone, equality, snapshot, rollback, allocation, and deallocation operate on one atomic placement
instead of parallel fields that could diverge.

The solver validates group compatibility and capacity before returning a placement; event
handlers apply already validated ownership without cloning or validating the node ledger again.
Seeding validates externally supplied records before applying them.

Hint enumeration supports at most eight NUMA zones, matching kubelet's default maximum.
Larger modeled topologies are rejected explicitly rather than attempting exponential enumeration.

Both fields use canonical ordering. Zones remain sorted by zone index. Memory groups contain one
entry per mask and are sorted by the canonical mask value; amounts for duplicate masks are merged.
`Clone` deep-copies every resource list, and `Equal` compares masks and resource quantities
semantically rather than depending on Go map iteration order. This prevents equivalent group
allocations from appearing to be different moves during pipeline deduplication.

Generated hints, per-container scratch allocations, preferred widths, and Topology Manager's merged
affinity remain temporary solver values.

## Scheduling Rules

### Group Compatibility

For a candidate memory mask `M` and every active group `G`, the mask is compatible only when:

```text
M does not intersect G, or M equals G
```

Equal masks reuse a group; disjoint masks create a group. Any other overlap is invalid.

### Group Availability

Group compatibility and group capacity are separate checks.

If the candidate mask exactly equals an active group, the solver compares each requested memory
resource with that group's aggregate `available` amount. Kubelet performs the equivalent check by
summing `Free` across every NUMA cell in the selected mask.

If the candidate mask is disjoint from all active groups, its cells have no static Memory Manager
assignments. Its available amount is therefore the sum of their per-zone `Allocatable` values. A
successful allocation creates a new group and records the pod's effective reserved amounts.

For example:

```text
node-0 allocatable:       100 GiB
node-1 allocatable:       100 GiB
pod-a in group {0,1}:     120 GiB
pod-b in group {0,1}:      30 GiB
group {0,1} available:     50 GiB
```

A new 40 GiB allocation fits in `{0,1}`; a 60 GiB allocation does not. No per-zone split is needed.

### Memory Hint Generation

Static Memory Manager jointly considers ordinary memory and every requested hugepage size when it
builds its mask family.

For each Memory Manager admission unit, the solver:

1. Enumerates every non-empty NUMA mask.
2. Keeps masks whose summed `Allocatable` satisfies every requested memory resource.
3. Records the smallest allocatable width before considering current group availability.
4. Removes masks which conflict with active groups.
5. Removes masks whose aggregate group availability cannot satisfy the request.
6. Marks a remaining hint preferred when its width equals the physical preferred width from step 3.

Filtering active groups must not recompute the preferred width. Otherwise a group-forced wider mask
could incorrectly become preferred under `restricted`.

### Topology Manager Merge and Final Memory Mask

Nodes with policy `none` are outside this plugin's scope: they bypass evaluation, memory-state
validation, group reconstruction, and allocation/deallocation accounting. Their locality score
uses an unmodeled full-node span rather than a predicted placement. Kubelet handles admission;
the scheduler does not prevent conflicting delayed-bind memory allocations on these nodes.

The solver generates separate CPU, device, and memory hints from simulated scheduler state, then
passes them to the upstream Kubernetes Topology Manager policy's `Merge` method. The upstream
policy handles hint intersection, tie-breaking, single-NUMA filtering, and admission rather than
forcing every resource through one shared candidate mask.

After selecting the merged Topology Manager affinity, the solver predicts Memory Manager's final
allocation mask:

- when the merged affinity has enough group availability, Memory Manager uses it directly;
- otherwise Memory Manager selects the best emitted memory hint containing the merged affinity;
- the final mask must satisfy group compatibility and capacity.

The merged affinity and final memory mask can differ. For example, CPU and GPU may remain on `{0}`
while memory reuses `{0,1}`.

### Scope and Allocation Order

The request model preserves container identity and order while solving:

- **Container scope:** calculate memory hints and allocate each native sidecar and application
  container, updating private evaluation state between allocations.
- **Pod scope:** calculate one pod-level Topology Manager affinity, then simulate each long-running
  container's Memory Manager allocation using that affinity. The memory request used for pod hints
  sums application containers and native sidecars, not ordinary-init peak memory.
- **Ordinary init containers:** retain existing CPU/device request handling and whole-pod resource
  and QoS accounting, but do not contribute memory hints or predicted groups.

After the admission sequence, the solver aggregates all still-active application and restartable
init allocations by mask. Only that pod-level result is committed and persisted.

An ordinary init container can temporarily create groups different from the predicted long-running
placement. The scheduler does not reserve those temporary groups or block a node solely because
an ordinary init container is present. Such groups can cause unexpected kubelet NUMA admission
failures even when the predicted placements fit. The cause may be transient, but an admission
failure can leave the pod failed and require recreation; automatic recovery is not guaranteed.
Generic incomplete observations still make group state unknown, including partial startup of
application containers or native sidecars.

Scratch state is private to one evaluation. Parallel node scoring never mutates session state.

## Predicate and Scoring

Topology Manager admission and Memory Manager allocation are separate decisions:

- `single-numa-node` and `restricted` continue rejecting invalid Topology Manager merges;
- `best-effort` normally passes topology alignment, but known Memory Manager impossibility
  rejects the node because provider allocation would fail after the merge;
- unknown or missing group state explicitly rejects requests for managed memory under every modeled policy;
- only Guaranteed containers using statically managed memory are constrained.

`ignoreList` continues controlling speculative per-zone alignment. Complete observations of actual
Memory Manager groups remain authoritative and enforce compatibility and capacity even for an
ignored memory resource. Predictions alone do not override an ignored resource on a node without
observed evidence of Memory Manager allocations.

Memory Manager conflicts use a distinct fit reason from insufficient group capacity and Topology
Manager alignment failures.

Scoring uses the union of resource placement masks and final memory masks. A small allocation forced
onto `{0,1}` has width two even though its reserved bytes are tracked only as a group aggregate.
This makes a clean singleton placement rank above a retained cross-NUMA group.

Predicate, scoring, and final placement share one solver: admission requests, resource hints,
upstream Topology Manager merge, and simulated allocations. There is no legacy per-zone memory
solver or unknown-state scoring fallback. CPU/device-only requests use the same solver without
memory-group accounting. Final placement is recomputed after node selection against the current
ledger. Only long-running memory allocations are modeled and persisted.

Generate preferred hints first. Rejecting policies need no non-preferred candidates. For
`best-effort`, a preferred upstream merge is already optimal; only a non-preferred result requires
generating the full hint sets and rerunning the same upstream merge. This avoids large Cartesian
products in the common case without introducing another solver or a custom ranking algorithm.

## Allocation, Deallocation, and Preemption

After node selection, the scheduler solves once more against current node state. The framework must
expose an error-returning operation such as
`SolveNUMAPlacement(task, node) (pod_info.NUMAPlacement, error)`, or an equivalent pre-allocation
hook. A re-solve failure aborts before statement mutation.

Solve into a local value and assign the task's placement only after success, preserving its prior
placement if another candidate must be tried. Validate all masks and amounts before applying any
ledger mutation so the existing non-error-returning allocation handlers remain infallible. The
Memory Manager feasibility remains enforced under `best-effort` despite permissive alignment.

Allocation atomically:

1. applies the existing per-zone resource placement, excluding statically managed memory when the
   group ledger is known;
2. creates or reuses every final memory group;
3. stores the pod UID and aggregated reserved amount as the group's owner entry.

Deallocation atomically:

1. reverses the same per-zone resource placement applied during allocation;
2. removes the pod UID from every group it owns;
3. deletes a group only when its owner map becomes empty.

In the 200 GiB example above, removing pod-a leaves pod-b's 30 GiB ownership and restores
availability to 170 GiB. Removing pod-b deletes the group, allowing cells 0 and 1 to be used
independently again.

Statement rollback and uneviction apply the inverse operation using the group allocations stored on
the task. Both group lifetime and aggregate reclaimed capacity are exact when observations are
complete; no per-zone byte distribution is required.

Memory groups also become part of the task's allocation identity. Framework statement snapshots,
restores, and compares the complete `NUMAPlacement`, including both `Zones` and `MemoryGroups`. A
task whose CPU/device zones are unchanged but whose memory mask or reserved amount changed must not
have its move cancelled as a duplicate.

### Interaction with Per-Zone Availability

The old placement annotation can contain singleton memory which is also present in the group
annotation. The scheduler must not account for it twice.

When `MemoryGroupState` is known:

- reconstruct CPU and device availability from the existing per-zone placement records;
- exclude ordinary memory and hugepages from generic per-zone reconstruction and mutation;
- evaluate and mutate all statically managed memory through the group ledger.

When group state is unknown, managed-memory requests are rejected rather than evaluated from
per-zone memory availability. CPU and device accounting remain usable independently.

`reconstructAvailable=false` controls generic per-zone reconstruction only. Known Memory Manager
group capacity continues using `Allocatable` minus owner amounts; NRT per-zone `Available` cannot
replace that aggregate ledger.

## Failure Handling and Observability

- Reconstruction rejects unknown zones, overlapping distinct masks, and owner totals exceeding
  capacity. These invalidate the known ledger; use the unknown-state rules above.
- Log unknown state at debug level; invalid records include the pod and offending mask or amount.
- Fit reasons distinguish incompatible groups, insufficient group capacity, and Topology Manager
  alignment failure. Include the conflicting mask or requested/available amounts as applicable.
- Exporter and informer lag can miss or retain groups. There is no freshness timestamp: structurally
  complete stale records remain usable until reconciliation. Kubelet admission is the backstop;
  stronger liveness detection is out of scope.

## Testing

### NPE

- Parse singleton, cross-NUMA, ordinary-memory, hugepage, and zero-sized podResources blocks.
- Aggregate containers by canonical mask and sum sizes by resource.
- Publish the existing placement annotation unchanged.
- Produce deterministic group JSON and avoid unchanged writes.
- Publish explicit empty group lists only for complete observations.
- Withhold a complete list until every expected long-running managed-memory container has started.
- Publish `null` when an observation becomes incomplete or invalid.
- Join every completeness decision with a synchronized node-scoped pod informer cache.
- Verify ordinary-init status does not independently invalidate long-running observations.

### State Reconstruction

- Cover known-empty, known-groups, predicted, missing, malformed, and unknown-zone observations.
- Start from a clean known-empty node and retain its first predicted group across scheduling cycles.
- Include releasing and stuck-in-releasing owners until their allocation is removed.
- Combine equal masks from several pods while retaining every pod owner and amount.
- Reject partial overlaps between distinct groups.
- Reject owner totals which exceed group capacity.
- Include pods outside KAI PodGroups.
- Verify only a complete observed record replaces predicted groups.
- Verify explicit `null` overrides persisted predictions.
- Exclude memory from generic per-zone reconstruction when group state is known.

### Solver and Predicate

- Cover equal, disjoint, singleton, subset, superset, and partial-overlap masks.
- Calculate active-group availability as capacity minus reported owner amounts.
- Evaluate ordinary memory and hugepages independently within the same mask.
- Preserve physical preferred width after group and capacity filtering.
- On a two-zone `restricted` node, reject a singleton-sized request when an active `{0,1}` group
  leaves only a non-preferred wide memory hint.
- Cover `single-numa-node`, `restricted`, and `best-effort`; verify `none` bypasses the plugin even
  with unknown, incompatible, or exhausted memory groups.
- Verify unknown and missing state reject managed-memory requests under every modeled policy, while
  CPU/device-only and non-Guaranteed device requests do not require memory-group state.
- Cover pod scope, container scope, and native sidecars.
- Verify ordinary-init memory requests do not contribute hints or predicted group reservations,
  while existing CPU/device request and QoS handling remains intact.
- Enforce observed groups for ignored memory resources and with per-zone reconstruction disabled.

### Lifecycle

- Create and reuse a group within one scheduling cycle.
- Aggregate several containers from one pod into one group owner.
- Let one pod own several disjoint groups.
- Remove one of several pod owners and restore its aggregate amount.
- Remove the final owner and allow the cells to form a different group.
- Cover allocation rollback, preemption, uneviction, and statement discard.
- Include masks and amounts in allocation-identity and pipeline-dedup comparisons.
- Reconstruct a binding pod's predicted groups from its BindRequest while the binder is delayed and
  before NPE can publish an observation.
- Verify predicted-to-observed handoff does not double-count ownership or amounts.
- Verify singleton memory is not counted by both zone placement and group state.
- Retain BindRequest predictions with an upgraded CRD and an older binder that does not copy the
  group annotation.

### Issue Reproductions

- Rank a clean singleton-capable node above a node retaining `{0,1}` through a small follower pod.
- Reject a wide request requiring `{0,1}` when an active singleton owns `{0}`, including under
  `best-effort`.
- On a two-zone `restricted` node with 100 GiB per zone, leave a 120 GiB pod waiting for binding,
  rebuild state from its predicted `{0,1}` group, and reject a 40 GiB NUMA-sensitive pod even though
  their combined 160 GiB request fits the node. Without the BindRequest group, the approximate
  per-zone split would incorrectly admit both and leave one to fail kubelet admission.
- Reclaim an observed cross-NUMA victim's aggregate amount without inventing a per-zone split.

## Implementation Areas

The main changes are expected in:

- `pkg/npe/placement`: aggregate per-container `ContainerMemory` blocks into pod-level groups.
- `pkg/npe/exporter`: publish, clear, and reconcile the group annotation independently from the
  existing placement annotation.
- `pkg/apis/scheduling/v1alpha2`: add group placement types and BindRequest prediction fields.
- `pkg/scheduler/api/pod_info`: expand `NUMAPlacement` to store, clone, and compare zone and group
  allocations as one value.
- `pkg/scheduler/api/node_info`: store and clone the node Memory Manager group ledger.
- `pkg/scheduler/plugins/numa/seed_placements.go`: reconstruct groups from all active node pods.
- `pkg/scheduler/plugins/numa/requests.go`: cache ordered admission units and the pod-scope request.
- `pkg/scheduler/plugins/numa/evaluator.go`: solve CPU/device and memory placement through one
  hint-based pipeline, with explicit errors for unknown required memory ownership.
- `pkg/scheduler/plugins/numa/topology_hint_merge.go`: delegate merging and admission to upstream
  Topology Manager policy implementations.
- `pkg/scheduler/plugins/numa/evaluator.go`: shared capacity, resource drawing, and conversion helpers.
- `pkg/scheduler/plugins/numa/numa.go`: predicate gating, scoring, and atomic group commit/rollback.
- `pkg/scheduler/plugins/numa/reconstruct.go`: keep memory out of per-zone reconstruction when group
  state is known.
- `pkg/scheduler/framework` and the BindRequest path: persist selected groups and restore them
  during statement rollback.
