# NUMA-Aware Scheduling — Implementation Follow-ups

Open questions and design refinements surfaced while implementing
[the design](./README.md). Each item either diverges from the current design text or
extends it, and should be folded back into the design README once confirmed.

## 1. `shouldHandle` gate is QoS-based, not whole-GPU-based

**Divergence from design.** The README's *Non-Goals* and *`shouldHandle` gate* sections say
only **whole-GPU** Guaranteed pods are handled. The implementation engages for **any
Guaranteed pod** on a modeled-policy node (`single-numa-node` / `restricted`).

**Why.** The kubelet's Topology Manager runs its admission check for every Guaranteed pod,
aligning whichever hint providers participate. A fractional/MIG pod's GPU is shared and not
device-plugin-aligned, but if the pod is Guaranteed its **cpu/memory are still aligned** and
can trigger a `TopologyAffinityError`. Excluding fractional/MIG pods drops exactly the
rejection the plugin exists to prevent. The per-resource model handles this safely without a
whole-GPU gate: `topologyAware = {resources reported per-zone} ∩ {resources the pod requests}`
naturally excludes `nvidia.com/gpu` for a fractional pod (it requests no integer device), so
such a pod is evaluated on cpu/memory only — precisely what the kubelet aligns for it.

**Action:** update *Non-Goals* (fractional/MIG GPU *sharing* is still out of scope, but
fractional/MIG *pods* are handled for their cpu/memory) and the *`shouldHandle` gate* to state
the criterion as **Guaranteed QoS + modeled policy**.

## 2. `reserved` ledger: amounts, indices, seeding, and runtime authority

The design sketches `reserved map[PodID][]string` (zone ids) populated by `AllocateFunc`.
Implementation refines this in four ways:

- **Store per-zone amounts, not just ids.** `zoneReservation{zoneIndex, amount}`. Credit-back
  on rollback/eviction must restore the exact charged amounts; re-deriving the split on
  deallocate is wrong under `restricted`, whose greedy split depends on headroom *at allocate
  time* (which changes before deallocate, e.g. during preemption probing).
- **Reference zones by index** into the (now deterministically ordered) `zones` slice — O(1)
  credit-back. The durable `zone.id` is retained for the cross-cycle placement record and the
  observed-placement agent, recovered via `zones[zoneIndex].id`.
- **Seed `reserved` with all live NUMA pods at `OnSessionOpen`** (not only this-cycle
  allocations), so a pre-existing running victim's zone is known when reclaim/preemption
  (un)evicts it.
- **Precedence applies only to seeding.** `observed > predicted > re-derive` establishes a
  pod's *initial* placement. At runtime `reserved` is the single source of truth:
  `DeallocateFunc` always credits `reserved[uid]` and never re-consults the observed
  annotation. This is required for evict → re-allocate → re-evict, where the latest in-cycle
  zone must be credited, not the stale observed one.

**Action:** update the *Plugin-local per-zone data model* and *In-cycle reservation* sections.

## 3. Deterministic NUMA-zone ordering (guarantee)

`nodeTopology.zones` is sorted by NUMA-node id (numeric suffix of the NRT zone name;
`node-2` before `node-10`). This gives stable, meaningful zone indices, makes
`single-numa-node` selection prefer the lowest NUMA node (matching the kubelet), and gives the
`restricted` greedy split a stable tie-break. The design references picking the "lowest" zone
but does not state the ordering guarantee.

**Action:** document the ordering guarantee in the design.

## 4. NUMA-aware eviction dedup (framework change)

The solver's virtual-eviction dedup (`framework/statement.go`, `Pipeline` → `Unevict`) cancels
an eviction when a pod is re-pipelined to the same node. Its only "don't dedup" gate is
`isSharedAndMoveToDifferentGPU`, which requires `task.IsSharedGPUAllocation()` — **always false
for our whole-GPU Guaranteed pods**. So today such a pod's eviction is unconditionally
cancelled regardless of which NUMA zone it was re-placed on, causing (a) ledger drift (the pod
stays kubelet-pinned to its old zone while accounting believes it moved) and (b) silent failure
of any scenario that needed the pod on a different zone.

**Required addition (beyond the current design):**
- Carry the predicted zone(s) as **allocation identity on `PodInfo`** (`NUMAZones`), set by
  `pickZone` during the allocate step (before `Pipeline`'s dedup check), mirroring `GPUGroups`.
  The framework snapshots/restores it on evict undo/redo (like `previousGpuGroups`).
- Add a `numaMovesToDifferentZone` gate to the dedup, analogous to
  `isSharedAndMoveToDifferentGPU`, and restore `NUMAZones` on unevict.

This shares the predicted-zone field with the cross-cycle placement record (design's
*Scheduler-predicted placement record*).

**Action:** add an *Interaction with eviction dedup* subsection to the design.

## 5. NRT activation via API discovery, not plugin-enablement

The NRT informer is wired only when the cluster serves the `topology.node.k8s.io` API group
(discovery-based feature gate), mirroring how DRA is gated — rather than keying off the `numa`
plugin being enabled in scheduler config (which is not known at cache-construction time). Worth
recording in the design's deployment/operator notes; revisit if tighter coupling to
plugin-enablement is wanted.

## 6. Non-`Node` NRT zone types — ignored, or is there value? (open question)

`buildZones` keeps only zones with `Type == "Node"` and ignores all others (e.g. `Socket`,
`Die`, `Core`). This matches the kubelet's Topology Manager, which aligns at **NUMA-node
granularity** — its hints are `NUMANodeAffinity` bitmasks defined over NUMA nodes only.

**Confirmed against the kubelet source** (`k8s.io/kubernetes/pkg/kubelet/cm/topologymanager`,
local checkout line numbers — verify against the target release):

- `numa_info.go` — `NewNUMAInfo(topology []cadvisorapi.Node, …)` builds the manager's entire
  NUMA universe `NUMAInfo.Nodes` by appending one entry per cadvisor NUMA `node.Id`
  (`numa_info.go:33-54`). There is no socket/die/core concept anywhere in the Topology Manager;
  the universe is NUMA nodes, full stop.
- `topology_manager.go` — `NewManager` takes those cadvisor NUMA nodes and rejects machines with
  more than `MaxAllowableNUMANodes` *NUMA Nodes* (`topology_manager.go:165,188`).
- `policy.go` — the hint merge operates purely on `hint.NUMANodeAffinity` bitmasks
  (`mergePermutation`, `policy.go:45-54`), i.e. over NUMA-node ids.

So ignoring non-Node NRT zones for the **admit decision** is correct, not just convenient — they
are NRT-API hierarchy (sockets/dies) that the kubelet's admission path never consults. The NRT
`Type == "Node"` zone corresponds 1:1 to a kubelet NUMA node.

**Still open:**
- **Inter-node distance is the one cross-zone signal the kubelet uses.** `NewNUMAInfo` also reads
  NUMA distances and `Narrowest`/`Closest` (`numa_info.go:40-75`) consume them — but only under
  the optional `prefer-closest-numa-nodes` policy option, and only to pick *among* equally-valid
  NUMA-node masks, never to change the admit verdict. Worth evaluating whether ingesting the NRT
  inter-zone `Zone.Costs` (distances) adds value for `restricted` minimal-span selection or v2
  scoring (prefer closer NUMA nodes). This would ingest cost data while still keying admission on
  Node zones.
- Decide whether to emit observability when an exporter publishes only non-Node zones for a
  rejecting-policy node (a potential exporter/config gap).

## 7. Per-zone resources as vectors, not cloned maps (potential optimization)

**Not premature — defer until measured, but likely the first hotspot.** The evaluator
represents each zone's headroom as `map[v1.ResourceName]resource.Quantity` and clones it
constantly: `cloneScratch` rebuilds every zone map on each `evaluate` call, `splitAcrossMask`
and `subtract` allocate more maps, and `amountOf` `DeepCopy`s quantities. That's
O(zones × resources) map allocations + quantity deep-copies per candidate node, per request
unit. Today the plugin only filters, so it runs once per (task, node); **once NUMA scoring
lands it will call `evaluate` for every (task, node) pair**, multiplying the map churn across
the concurrent scoring fan-out (item 8) — exactly where it will hurt.

KAI already solved the equivalent problem for whole-node accounting with `ResourceVector`
(`pkg/scheduler/api/resource_info`): a flat slice indexed by a shared `ResourceVectorMap`, so
cloning is a slice copy and arithmetic is index ops — no map allocation, no `Quantity`
deep-copy. **Potential optimization:** model per-zone `available` (and projected requests) as
fixed-width vectors over the node's small `topologyAware` resource set, making scratch clones a
slice copy and `subtract`/`compare`/`split` index operations.

**Action:** revisit after profiling the scoring path; if map churn dominates, port the per-zone
model to a vector representation (ideally reusing `ResourceVector`/`ResourceVectorMap`). Keep
the read-only-during-scoring discipline from item 8 when doing so.

## 8. Scoring must stay concurrency-safe (design constraint)

KAI scores nodes **concurrently**: `Session.OrderedNodesByTask`
(`pkg/scheduler/framework/session.go`) launches one goroutine per node calling
`NodeOrderFn(task, node)` and joins them with a `WaitGroup` before returning. A NUMA
`AddNodeOrderFn` we add will therefore run in parallel across nodes for the same task.

The current design is compatible, by construction:

- **State is partitioned per node** (`pp.nodes[node.Name]`), so concurrent node goroutines
  never touch the same `nodeTopology` / `numaZone`.
- **`evaluate` is pure**: it clones `nt.zones` into a scratch and mutates only locals; it never
  writes `nodeTopology` or `reserved`. (Concurrent *reads* of the same maps would be safe in Go
  anyway, but the partitioning means it doesn't even arise.)
- **The only writers — `AllocateFunc` / `DeallocateFunc`** — run at commit/rollback. The action
  loop scores (parallel, joined) *then* commits (serial); the phases never overlap.

**Invariant to preserve when implementing scoring:** the score fn MUST remain read-only on
shared state — compute via `evaluate`/clone, never mutate `nt` or `reserved`. If per-node
memoization is ever wanted, compute it in the **serial** `NodePreOrderFn` (which runs once
before the goroutines) or guard it with a lock; never lazily cache onto shared structs from
inside the concurrent score fn. The vector port (item 7) must keep this discipline — a shared
mutable vector written during scoring would be a data race.

**Action:** note this constraint in the design's *Future Work → NUMA scoring* section.
