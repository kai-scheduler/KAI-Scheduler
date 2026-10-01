<!--
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# Plugin State Slots: Fast, Typed Per-Object Storage for Plugins

## Overview

This document proposes a `plugin_state` package that lets scheduler plugins attach typed per-object state (per `NodeInfo`, `PodInfo`, `PodGroupInfo`) with O(1) access and without modifying the core info structs. Each plugin registers a `Slot[T]` at init time; reading or writing state on an info object becomes a single array index plus a type assertion.

The goal is to eliminate the two patterns plugins use today — side-table maps keyed by object name/UID, and plugin-specific fields embedded directly in core info types — and replace them with a single uniform mechanism that is faster than the map pattern and cleaner than the embed-into-core pattern.

## Motivation

Plugins routinely need to remember something about an object across multiple framework callbacks within a session (e.g., a precomputed score, a feasibility result, a fingerprint). Today there are two ways to do this, and both are unsatisfying.

### Pattern A: side-table map keyed by object name/UID

Example: the `topology` plugin caches a node-ordering score per `(subgroup, node)` pair and looks it up on every `NodeOrderFn` call.

```go
// pkg/scheduler/plugins/topology/topology_plugin.go:24-30
type topologyPlugin struct {
    TopologyTrees      map[topologyName]*Info
    subGroupNodeScores map[subgroupName]map[string]float64
    session            *framework.Session
}
```

```go
// pkg/scheduler/plugins/topology/node_scoring.go:17-35
func (t *topologyPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
    taskSubGroupInfo, err := t.getTaskSubGroupInfo(task)
    if err != nil { ... }

    relevantNodeScores := t.getRelevantNodeScores(taskSubGroupInfo) // map[string]float64
    if relevantNodeScores == nil {
        return 0, nil
    }

    score, ok := relevantNodeScores[node.Name]  // ← string-keyed map lookup
    if !ok { ... }
    return score, nil
}
```

`NodeOrderFn` is called once per `(task, node)` pair during ordering — for a scheduling cycle over ~2000 nodes and a job of N tasks that is `O(tasks × nodes)` map lookups, each involving a string hash and pointer chase, plus another map traversal up the subgroup ancestor chain in `getRelevantNodeScores`.

The `nodeplacement` plugin uses the same shape with `podAllocatableRange map[string]allocationRange` keyed by pod name, and `minruntime` uses `preemptProtectionCache map[PodGroupID]bool` and a nested `reclaimProtectionCache map[PodGroupID]map[PodGroupID]bool`.

### Pattern B: plugin-specific fields embedded in core info types

Example: `NodeInfo` currently carries several fields whose only readers are specific plugins or features:

```go
// pkg/scheduler/api/node_info/node_info.go:66-92
type NodeInfo struct {
    Name string
    Node *v1.Node

    AllocatableVector resource_info.ResourceVector
    IdleVector        resource_info.ResourceVector
    UsedVector        resource_info.ResourceVector
    ReleasingVector   resource_info.ResourceVector
    VectorMap         *resource_info.ResourceVectorMap

    AccessibleStorageCapacities map[common_info.StorageClassID][]*sc_info.StorageCapacityInfo

    PodInfos               map[common_info.PodID]*pod_info.PodInfo
    MaxTaskNum             int
    MemoryOfEveryGpuOnNode int64
    GpuMemorySynced        bool
    LegacyMIGTasks         map[common_info.PodID]string

    // HasDRAGPUs indicates GPUs were added via DRA ResourceSlices. Temporary fix
    // - remove when device-plugin pods are supported on DRA nodes.
    HasDRAGPUs bool

    PodAffinityInfo pod_affinity.NodePodAffinityInfo

    GpuSharingNodeInfo
}
```

`HasDRAGPUs`, `LegacyMIGTasks`, `PodAffinityInfo`, and the embedded `GpuSharingNodeInfo` are all plugin/feature-specific state that every `NodeInfo` pays for whether or not the cluster uses that feature. The explicit `// Temporary fix - remove when ...` comment on `HasDRAGPUs` is a flag: there is no good place to put this state today, so it landed on the core struct.

Adding a slot for each new plugin concern grows the core type indefinitely and forces unrelated code paths (snapshotting, cloning, tests) to know about every plugin's data.

### What we want

A single mechanism that is:

- **As fast as a field access** — no string hashing, no pointer chase through a map.
- **Type-safe** — no `interface{}` casts at call sites; the wrong type is a compile error.
- **Owned by the plugin** — adding plugin state should not require touching `node_info.go` or `pod_info.go`.
- **Self-contained per object** — slots travel with the info object; cloning/snapshotting is one slice copy.

## Goals / Non-Goals

### Goals

- Provide a `plugin_state` package with `Register[T]() Slot[T]`, `Get[T](carrier, slot)`, `Set[T](carrier, slot, v)` and `Clear[T](carrier, slot)`.
- Add a single backing field to `NodeInfo`, `PodInfo`, and `PodGroupInfo` (one untyped slice each).
- Migrate `topology.subGroupNodeScores` and `nodeplacement.podAllocatableRange` to slots as demonstrations.
- Define a migration path for the plugin-specific fields currently embedded in `NodeInfo` (`HasDRAGPUs`, `LegacyMIGTasks`, `PodAffinityInfo`, `GpuSharingNodeInfo`).

### Non-Goals

- Replace per-session state that is not keyed by an object (e.g., a plugin's own config or global trees like `topologyPlugin.TopologyTrees`). Those stay where they are.
- Provide cross-session persistence. Slot contents live for the lifetime of the info object (one session).
- Provide concurrent-safe access. Plugin callbacks already run inside a single scheduling goroutine; if a plugin parallelises internally, it owns the synchronisation, exactly as it does today with its private maps.

## Proposed Design

### The `plugin_state` package

```go
// pkg/scheduler/api/plugin_state/plugin_state.go
package plugin_state

import "sync/atomic"

// Slot is a typed handle to a position in a Carrier's plugin-state slice.
// Acquire one at plugin init time via Register; pass it to Get/Set/Clear.
type Slot[T any] struct{ idx int }

// Carrier is implemented by info objects that participate in plugin slots.
type Carrier interface {
    pluginSlots() *[]any
}

var nextSlot atomic.Int32

// Register reserves a slot for type T. Must be called before any Carrier is
// constructed (typically from a plugin's package init() or factory).
func Register[T any]() Slot[T] {
    return Slot[T]{idx: int(nextSlot.Add(1) - 1)}
}

// SlotCount returns the number of registered slots. Used by Carrier
// constructors to size their backing slice.
func SlotCount() int { return int(nextSlot.Load()) }

// Get returns the value in slot s on c, and ok=false if unset.
func Get[T any](c Carrier, s Slot[T]) (T, bool) {
    arr := *c.pluginSlots()
    if s.idx >= len(arr) || arr[s.idx] == nil {
        var zero T
        return zero, false
    }
    return arr[s.idx].(T), true
}

func Set[T any](c Carrier, s Slot[T], v T) {
    arr := c.pluginSlots()
    if s.idx >= len(*arr) {
        grown := make([]any, SlotCount())
        copy(grown, *arr)
        *arr = grown
    }
    (*arr)[s.idx] = v
}

func Clear[T any](c Carrier, s Slot[T]) {
    arr := *c.pluginSlots()
    if s.idx < len(arr) {
        arr[s.idx] = nil
    }
}
```

### Changes to core info types

Each carrier gains one private slice and a tiny accessor:

```go
// pkg/scheduler/api/node_info/node_info.go
type NodeInfo struct {
    // ...existing fields...
    pluginSlots []any
}

func (n *NodeInfo) pluginSlots() *[]any { return &n.pluginSlots }
```

`PodInfo` and `PodGroupInfo` get the same treatment. Constructors call `make([]any, plugin_state.SlotCount())` once; deep copies copy the slice. Reset/restore paths (used in scenario simulation) zero the slice without reallocating.

### Hot-path cost model

| Operation         | Map pattern                          | Slot pattern                  |
|-------------------|---------------------------------------|-------------------------------|
| Lookup            | string hash + bucket walk + cast      | one slice index + type assert |
| Memory per entry  | map header + bucket overhead + key    | one `any` (16 B)              |
| Cache locality    | scattered (pointers via map buckets)  | contiguous slice on the info  |
| Cost when unused  | one nil-map check per object          | one nil `any` per object      |

A type assertion on `any` is roughly the cost of a pointer comparison; map access on Go strings is on the order of 20–50× slower. For a `NodeOrderFn` called O(tasks × nodes) times per cycle, this is the difference between scoring being free and scoring being the visible bottleneck.

### Type assertion safety

`Get[T]` performs `arr[s.idx].(T)`. Because slots are typed at the call site and the only writer is `Set[T]` with the same `T`, the assertion can never fail in normal use — the `Slot[T]` value itself guarantees the type. Tests can additionally lock the invariant with a `_, ok := arr[s.idx].(T); !ok` check behind a build tag.

## Worked Examples

### Example 1 — Migrating `topology.subGroupNodeScores`

Today the plugin owns a `map[subgroupName]map[string]float64`. The inner map is the per-node side-table.

After migration, the per-node dimension moves onto the node:

```go
// pkg/scheduler/plugins/topology/topology_plugin.go
type topologyNodeState struct {
    scoreBySubgroup map[subgroupName]float64
}

var nodeSlot = plugin_state.Register[*topologyNodeState]()

type topologyPlugin struct {
    TopologyTrees map[topologyName]*Info
    session       *framework.Session
}
```

```go
// pkg/scheduler/plugins/topology/node_scoring.go
func (t *topologyPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
    sg, err := t.getTaskSubGroupInfo(task)
    if err != nil { ... }

    st, ok := plugin_state.Get(node, nodeSlot)   // ← single slice index
    if !ok {
        return 0, nil
    }
    return st.scoreBySubgroup[sg.GetName()], nil
}
```

`preJobAllocationFn` now iterates over nodes and clears their slot instead of throwing away an outer map; in practice the same `O(nodes)` work, but each clear is a slice store rather than a map allocation.

The win is on the read side: every `(task, node)` evaluation drops the outer `subGroupNodeScores[name]` map lookup entirely, and the surviving inner lookup is over a small `map[subgroupName]float64` (typically size 1–3) rather than `map[string]float64` over all node names.

### Example 2 — Migrating `nodeplacement.podAllocatableRange`

Today: `map[string]allocationRange` keyed by pod name, populated in `nodePreOrderFn` and read in pack/spread scoring.

After:

```go
type allocationRange struct { min, max float64 }

var podRangeSlot = plugin_state.Register[allocationRange]()

func (pp *nodeplacementPlugin) nodePreOrderFn(task *pod_info.PodInfo, ...) {
    plugin_state.Set(task, podRangeSlot, allocationRange{min: ..., max: ...})
}

func nodeResourcePack(task *pod_info.PodInfo, ...) float64 {
    r, _ := plugin_state.Get(task, podRangeSlot)
    // use r.min, r.max
}
```

Because `Slot[allocationRange]` stores the value type directly inside the `any`, no heap allocation per pod is needed beyond the slice grow on first write.

### Example 3 — Cleaning up `NodeInfo` pollution

The fields below are candidates to migrate out of `NodeInfo` into plugin-owned slots:

| Field on `NodeInfo`        | Today's owner (conceptual)            | Proposed slot owner          |
|----------------------------|----------------------------------------|------------------------------|
| `HasDRAGPUs`               | DRA-aware predicate logic              | `predicates` plugin          |
| `LegacyMIGTasks`           | MIG eligibility checks in predicates   | `predicates` plugin          |
| `PodAffinityInfo`          | pod-affinity predicate                 | `podaffinity` plugin         |
| embedded `GpuSharingNodeInfo` | GPU sharing/order plugins           | `gpusharingorder` plugin     |

These are not in scope for the initial implementation — they are existing complexity. But once `plugin_state` exists, removing them becomes a mechanical change: the plugin owns its slot, `NodeInfo` no longer imports `pod_affinity`, and code unrelated to those features stops paying their memory cost.

## Migration Plan

1. Land `pkg/scheduler/api/plugin_state` and wire `pluginSlots []any` into `NodeInfo`, `PodInfo`, `PodGroupInfo`. No behavior change.
2. Migrate `topology` and `nodeplacement` as the two reference cases. Validate via existing unit tests and the scale test (`test/e2e/scale`) — the topology change is expected to show measurable improvement at 1000+ nodes.
3. Migrate `minruntime.preemptProtectionCache` and `reclaimProtectionCache`. PodGroup-keyed maps map cleanly to a `Slot[bool]` on `PodGroupInfo`.
4. (Separate proposal) Move `HasDRAGPUs`, `LegacyMIGTasks`, `PodAffinityInfo`, `GpuSharingNodeInfo` off `NodeInfo` and into their owning plugins.

Each step is independently shippable.

## Alternatives Considered

### Type-keyed `map[reflect.Type]any` on each info

Chromium's `Supplementable` uses this shape. Simpler API (no registration), but each `Get` pays a `reflect.Type` hash and Go's runtime type comparison — measurably slower than slot indexing, and the win over the current `map[string]*state` is smaller.

### Continue with per-plugin maps

Status quo. Acceptable for cold paths (e.g., `minruntime`'s preemption caches), but compounds badly for hot paths like `NodeOrderFn`. Also does not help with the pollution problem on `NodeInfo`.

## References

The slot pattern is well established in systems where pluggable code must attach typed state to shared objects:

- **POSIX thread-local storage** — `pthread_key_create` / `pthread_getspecific` is the canonical "register a slot, then O(1) indexed access" API. Java's `ThreadLocal` and .NET's `AsyncLocal` are the same idea.
- **Chromium / Blink `Supplementable<T>` and `Supplement<T>`** — attaches typed per-DOM-node state without bloating core classes. Type-keyed rather than integer-indexed but solves the identical problem. See [`third_party/blink/renderer/platform/supplementable.h`](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/third_party/blink/renderer/platform/supplementable.h).
- **LLVM `AnalysisManager`** — each analysis pass produces typed results cached on the IR unit, keyed by analysis type. See [`llvm/include/llvm/IR/PassManager.h`](https://llvm.org/doxygen/PassManager_8h_source.html).
- **Linux kernel `netdev_priv()` / cgroup subsystem state** — subsystems register and get a slot in a fixed-size array on `struct net_device` / `struct cgroup`.
- **Entity Component System architectures** — game engines such as [Bevy](https://bevyengine.org/), [Flecs](https://www.flecs.dev/flecs/), and [EnTT](https://github.com/skypjack/entt) give each component type a column indexed by entity ID. Exactly the "plugin owns a typed slot on every object" pattern at scale.
- **Envoy `StreamInfo::FilterState`** — per-stream typed state bag for filters; name-keyed but the same role.
- **Kubernetes scheduler `CycleState`** — the closest cousin in scheduling. Uses a `map[StateKey]StateData` for per-cycle plugin state; this proposal is the per-object analogue with the map replaced by an integer-indexed slice. See [`pkg/scheduler/framework/cycle_state.go`](https://github.com/kubernetes/kubernetes/blob/master/pkg/scheduler/framework/cycle_state.go).

The Go-with-generics formulation (typed `Slot[T]` handles backed by `[]any`) is essentially the ECS / TLS approach ported into idiomatic Go 1.18+.
