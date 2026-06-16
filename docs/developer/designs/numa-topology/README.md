# NUMA-Aware Scheduling via NodeResourceTopology

## Summary

This document describes a v1 design for making KAI-Scheduler aware of per-NUMA-node
resource topology, so that **Guaranteed-QoS workloads** are placed only on nodes where the
kubelet's Topology Manager can actually align their resources.

The scheduler consumes the [`NodeResourceTopology`][nrt-api] (NRT) CRD, which is published
per-node by an external exporter (NFD topology-updater or the resource-topology-exporter).
A new `numa` plugin replicates the kubelet's Topology Manager admission check — for both the
`single-numa-node` and `restricted` policies — against the NRT data as a **filter predicate**,
and tracks per-NUMA-zone consumption **within a scheduling cycle** so that multiple pods placed
on the same node in one cycle are not over-committed onto the same zone. Compensating for NRT
 *staleness across cycles* is discussed in ([Appendix A](#appendix-a-cross-cycle-staleness-compensation)).

## Motivation

The kubelet's Topology Manager makes the real NUMA-alignment decision at **pod admission
time**, after the scheduler has already chosen a node. When a node is configured with a restrictive
policy like `single-numa-node`  or `restricted` and a Guaranteed pod's resources cannot all be satisfied according to it, the kubelet rejects the pod with a `TopologyAffinityError` and the pod returns to
`Pending`. The scheduler then re-attempts — potentially (in most cases, likely) picking the same bad node again —
producing wasted cycles and, in the worst case, a hot loop, and wasting the workload's time and precious compute resources.

The scheduler cannot *enforce* NUMA alignment (the kubelet owns that), but it can *predict*
it and avoid placing pods where the kubelet will reject them. This is the same role played
by the upstream [`NodeResourceTopologyMatch`][nrt-match] plugin in kubernetes-sigs/scheduler-plugins.

The highest-value case for KAI seems to be GPU locality: strict GPU↔CPU↔NIC NUMA affinity materially affects throughput for AI/ML workloads. That is the `single-numa-node`
scenario (everything on one NUMA node) and, for workloads larger than one NUMA node, the
`restricted` scenario (the minimal NUMA span) — both of which the kubelet enforces by rejecting
mismatched placements, and which this plugin therefore predicts.

## Usage Stories

### GPU + NIC locality for distributed training

A training pod requests one whole GPU, a block of CPUs, memory, and an RDMA NIC. For best
performance all four must sit on the same NUMA node. The cluster runs the kubelet with
`topologyManagerPolicy: single-numa-node`. Today KAI may place the pod on a node whose free
GPU is on a different NUMA node than its free CPUs, and the kubelet rejects it. With this
design KAI filters such nodes out up front.

### Packing many single-GPU pods onto a multi-NUMA node

A node has 8 GPUs split 4+4 across two NUMA nodes, but limited CPUs per NUMA node. KAI places
several single-GPU Guaranteed pods on it in one scheduling cycle. Without per-zone tracking,
KAI's whole-node accounting can approve a layout the kubelet cannot honor. In-cycle NUMA-zone
reservation ensures each successive pod sees the reduced per-zone headroom.

### Full-node workloads that span multiple NUMA nodes

A large training pod requests most or all of a node — e.g. all 8 GPUs (with matching CPU and
memory) on a node whose 8 GPUs are split 4+4 across two NUMA nodes. It physically cannot fit on a
single NUMA node, so `single-numa-node` would reject it everywhere. The node is configured
`restricted`, under which the kubelet admits it pinned to the *minimal* NUMA span (here, both
nodes) — the correct and performant placement for a full-node job. KAI must predict that
`restricted` verdict to place the pod without wasted scheduling cycles. This is why v1 models
`restricted` faithfully (the hint merge) rather than treating it as `single-numa-node`: full-node
GPU workloads are common, and they are inherently multi-NUMA.

## Goals

These are the objectives of NUMA-aware scheduling as a whole; The implementation will be done in stages, described later in the document.

- **Prevent wasted scheduling from NUMA mismatches.** Don't place a pod on a node where the
  kubelet's Topology Manager will reject it on topology grounds — eliminating the `Pending`
  bounce and reschedule hot-loop that follow.
- **Enable NUMA locality for performance on `best-effort` nodes where achievable.** For nodes
  with the kubelet **`best-effort`** policy — which never rejects on topology grounds but may
  silently run workloads *unaligned* when resources cannot co-locate on one NUMA node — steer
  topology-sensitive pods (e.g. GPU↔CPU↔NIC) toward nodes where alignment can succeed, preferring
  alignable placements over ones that would not, without ever blocking when locality is
  unachievable ([v2](#v2-optimization--scoring); v1 leaves `best-effort` nodes as pass-through).
- **Remain a safe optimization layer; never compromise correctness.** The kubelet stays the
  source of truth and the enforcement point; this feature only reduces churn and improves
  placement, and attempts to never cause an incorrect or mis-pinned placement.
- **Stay correct under concurrency and preemption.** Concurrent placement decisions must not
  over-commit a NUMA zone, and preempting or reclaiming for a topology-sensitive pod must avoid
  evicting victims that would not actually free a usable aligned slot.
- **Keep adoption cost low.** Build on the standard `NodeResourceTopology` tooling already common
  in the ecosystem; require no mandatory new cluster components, keeping richer accuracy and
  broader policy coverage as opt-in enhancements.

## Non-Goals

- **Aligning a fractional / MIG GPU with the pod's other resources.** A shared (fractional or MIG)
  GPU is not a device-plugin NUMA-aligned resource, so the plugin does not try to co-locate the GPU
  *fraction* with the pod's CPU/memory. This is **not** a gate on the pod: a fractional/MIG pod that
  is **Guaranteed QoS** still has its `cpu`/`memory` aligned by the kubelet, and the plugin accounts
  for those — the GPU simply drops out of the per-resource intersection (see *`shouldHandle` gate*
  and *NUMA-relevant resources*). Only the GPU-fraction alignment itself is out of scope.
- **100% prevention of kubelet pod rejections.** The current implementation of NUMA topology is inherently split-brained: the kubelet decides the actual placement of pods, while the scheduler attempts to predict that and match its decisions. While we can probably approximate it pretty well and cover for some gaps like inter-cycle allocations, some mismatches might still occur, like when foreign (non kai-scheduler) pods are bound to nodes, or many pods are bound concurrently (NUMA allocation can be affected by order). The design aims to mitigate those cases as much as possible, and to be **self-healing**: when mismatches occur, we aim for the scheduler to be **eventually consistent** with the real state, so errors will not be carried for many cycles.

## Background: who decides NUMA alignment

The **kubelet Topology Manager** implements every policy (`none`, `best-effort`,
`restricted`, `single-numa-node`) and enforces it at admission, independently of the
scheduler. So with zero scheduler support the kubelet still guarantees *correctness* — no pod is
ever NUMA-misaligned.

But correctness is not usability. The kubelet only *rejects*; it never *finds* a valid
placement. Without a NUMA-aware scheduler the failure mode potentially severely degrades the cluster usability:

- A pod whose node can't NUMA-align it bounces to `Pending`, and the scheduler — seeing that node
  as fine by whole-node accounting — keeps re-selecting it, so the pod **hot-loops or stays
  Pending indefinitely even though the cluster has capacity**.
- GPUs that are free by count but not NUMA-placeable become **stranded** — effective capacity
  loss on the most scarce and expensive resource in the cluster.
- The repeated bind → reject → reschedule traffic is **scheduler/binder thrash** that degrades
  scheduling latency for *all* workloads, not just the NUMA-sensitive ones.
- To users it looks like a pod that "should fit" mysteriously won't run, with an opaque
  `TopologyAffinityError` — hard to diagnose, and corrosive to trust in the scheduler.

The scheduler plugin's job is to restore usability on top of the kubelet's correctness: predict
the kubelet's verdict so pods land where they can actually run, and free capacity is actually
usable.

## Design Details

The work is staged into two phases (plus a v3 idea, and an opt-in, exporter-trusted cross-cycle
staleness correction, [Appendix A](#appendix-a-cross-cycle-staleness-compensation)):

- **v1 — correctness (this section).** A **filter** that predicts the kubelet's admission verdict
  for the two policies that *reject* on topology grounds (`single-numa-node` and `restricted`),
  plus **within-cycle per-zone reservation** so pods placed together in one cycle stay consistent.
  The aim is to prevent the wasted cycles and stranded capacity from *Background* — pods
  land where they can actually run. `best-effort` and `none` are pass-through.
- **Observed placement (v1).** A per-node exporter publishes each pod's *actual* NUMA placement; the
  scheduler consumes it for exact per-zone accounting (and accurate reclaim) when available, and
  **falls back to its own prediction when the exporter is absent or lagging**. The exporter ships with
  v1, but deploying it is optional — the scheduler degrades gracefully without it. See *Observed
  placement: the per-node exporter*.
- **v2 — optimization & scoring** ([Optimization & scoring](#v2-optimization--scoring)). Adds
  *performance*: ranks feasible nodes (least fragmentation / fewest NUMA nodes) and steers
  `best-effort` workloads toward nodes where alignment will actually succeed. It reuses v1's
  evaluators and per-zone model and only **ranks** — it never changes the admit decision.

The rest of this section describes **v1**.

### Policy handling

| Kubelet Topology Manager [policy][tm] on node (via NRT) | v1 behavior |
| --- | --- |
| [`single-numa-node`][tm-single-numa-node] | Fully modeled: require **one** NUMA zone to satisfy all the pod's NUMA-relevant requests (the `\|M\|=1` case of the merge below). |
| [`restricted`][tm-restricted] | Fully modeled: admit iff a common minimal-width NUMA mask satisfies all the pod's NUMA-relevant requests (the general merge — see *Modeling `restricted`*). |
| [`best-effort`][tm-best-effort] | Pass (kubelet never rejects on topology grounds). [v2](#v2-optimization--scoring) adds node scoring to steer toward alignable placements. |
| [`none`][tm-none] | Pass (plugin no-op; Topology Manager performs no alignment). |
| No NRT object for node | Pass (cluster without NRT is unaffected). |

Both modeled policies are different cases of the same admit question and are implemented behind
a single `numaEvaluator` seam (see *Policy evaluator seam*): `single-numa-node` is the
single-zone special case; `restricted` allows the minimal multi-zone span the kubelet would.

### NRT ingestion

1. Add a `NodeResourceTopology` lister to the data-lister interface
   (`pkg/scheduler/cache/cluster_info/data_lister`) and register the informer in
   `kubernetes_lister.go`.
2. In `cluster_info.Snapshot()`, attach the raw `*NodeResourceTopology` (matched by node
   name) to the corresponding `NodeInfo` as a pointer field.

This keeps ingestion consistent with KAI's deterministic, snapshot-based scheduling and
testability, while leaving the vector model untouched.

### NUMA data model: on `NodeInfo` and `PodInfo`

NUMA state lives on the existing snapshot objects:

- **Node topology on `NodeInfo`.** Each node's NRT object is parsed once at snapshot build into a
  `NumaTopology` attached to its `NodeInfo` (alongside the raw NRT): the Topology Manager
  policy/scope, the per-zone `Available` (dynamic — decremented as tasks commit in-cycle, restored
  on rollback) and `Allocatable` (static per-zone capacity), and the set of resources the node
  reports per zone.
- **Per-task placement on `PodInfo`.** A task carries its NUMA placement — the zone(s) it was
  allocated to *and the exact per-zone amount* (if known) — on `PodInfo`. Storing the exact
  amount enables simulating NUMA allocations on allocation rollback/eviction, which allows for 
  consolidate/preempt/reclaim simulations.

A running pod's placement is rebuilt each cycle from its durable record (precedence
**observed > predicted**; see *Observed placement* and *Scheduler-predicted placement record*),
parsed onto `PodInfo` at snapshot build exactly as `GPUGroups` is. A running pod with no record 
(for example, if the NUMA exporter is missing or stuck) has an empty placement and is simply **not credited on
virtual eviction**. Its consumption is already netted out of NRT `Available` (the occupancy ledger
is seeded from `Available`), so the only effect is that evicting it frees no zone in the ledger —
which matters *only* to a NUMA-sensitive preemptor on that exact zone; non-NUMA-sensitive preemption
is unaffected.

### NUMA-relevant resources

Which resources constrain zone selection is decided **per node**, by what that node's NRT object
reports per-zone, intersected with what the pod requests:

```
topologyAware(node) = { r : some zone of node reports r }  ∩  { r : pod requests r }
```

- **Devices (GPU, NICs):** fully inferred. A device appears per-zone in NRT *only because* its
  plugin emitted NUMA topology — exactly when the kubelet will align it — so per-zone reporting is
  a faithful signal, with no configuration. Heterogeneous clusters work automatically: a device is
  NUMA-constrained on nodes that report it per-zone and ignored on nodes that don't (correct —
  those nodes won't NUMA-align it either). *Caveat:* if a node should publish per-zone device
  topology but doesn't (exporter gap), the plugin reverts to no per-zone prediction there and
  relies on the kubelet backstop — an observability concern (alert on rejecting-policy nodes with
  no per-zone device data), not a correctness one.
- **`cpu` / `memory`:** reported per-zone *unconditionally*, but the kubelet only aligns `cpu` when
  its [CPU Manager policy is `static`][cpu-mgr] and `memory` when the [Memory Manager is
  enabled][mem-mgr] (`Static`, not the default `None`). **NRT exposes neither manager's policy**
  (only the Topology Manager policy/scope), so the plugin cannot infer whether they are actually
  aligned. It therefore treats `cpu`/`memory` as aligned **by default** — the admission-error-safe
  choice (under-including a resource the kubelet *does* align would cause rejections). The cost is
  over-rejection on nodes whose manager is off; because **Memory Manager defaults to `None`**, a
  `single-numa-node` node that aligns CPU+devices but lets memory float is a real case where
  treating `memory` as aligned over-rejects.
- **Optional ignoreList**: an operator who knows a reported resource is
  *not* aligned on their nodes (e.g. `memory` with Memory Manager `None`, or `cpu` without
  `static`) lists it, excluding it from per-zone reasoning and recovering the over-rejected
  capacity. Default is empty.

(The QoS gate still applies — `cpu`/`memory`/`hugepages` constrain only Guaranteed pods, matching
the kubelet, which aligns them only for Guaranteed QoS.)

> **Possible future work:** upstream a `cpuManagerPolicy` / `memoryManagerPolicy` NRT attribute (none
> exists today — exporters publish only the Topology Manager policy/scope). With it, `cpu`/`memory`
> alignment becomes inferable per node and the ignoreList can be dropped.

### `shouldHandle` gate

The plugin engages for a task only when **both** hold (otherwise the predicate passes
through):

- the node has a `NumaTopology` whose policy is `single-numa-node` or `restricted`, and
- `task.Pod.Status.QOSClass == Guaranteed` — the QoS for which the kubelet Topology Manager runs
  alignment at all.

At this stage, aligning the GPU *fraction* (where relevant) itself is out of scope (see *Non-Goals*),
but is feasible in the future.

### Filter algorithm: `single-numa-node`

`single-numa-node` is the simple case — a bitmask intersection (the `|M|=1` special case of the
general merge in *Modeling `restricted`*). Following the upstream approach:

```
resourcesAvailableInAnyZone(nt, req):       // req limited to nt.topologyAware
    mask = { all zones set }
    for r, qty in req:
        if qty == 0: continue
        zmask = { zone z : suitable(qos, r, qty, z.available[r]) }
        mask = mask AND zmask
        if mask empty: return (nil, false)
    return (lowest set zone, true)           // kubelet picks narrowest/lowest

suitable(qos, r, qty, avail):
    if qos != Guaranteed and r in {cpu, memory, hugepages}: return true  // kubelet won't align
    return avail >= qty
```

**Scope split** (read from the node's NRT attributes):

- **`pod` scope** → align the whole pod to one zone. Use KAI's effective-pod-request
  computation (which already accounts for init containers and native sidecars), projected
  onto the NUMA-relevant set, and run `resourcesAvailableInAnyZone` once.
- **`container` scope** → align each container independently but sharing zone headroom. Run
  the check per container on a scratch copy of the zones, subtracting the chosen zone's
  resources after each container (greedy, first-fit lowest zone). This matches the upstream
  `singleNUMAContainerLevelHandler`. Init containers run serially and are checked but not
  accumulated.

The predicate is **pure** (read-only); it never mutates `nodes`. It also runs only on nodes
that already passed the existing whole-node vector gate, so it is naturally late in the
funnel.

### Modeling `restricted`: the hint merge

`restricted` lets a pod span more than one NUMA node, but only when the alignment is the
*minimal* one possible. To predict the kubelet's verdict, we reproduce its hint merge.

A **hint** is `{NUMANodeAffinity bitmask, Preferred bool}` — a candidate set of NUMA nodes a
hint provider (CPU/Memory/Device Manager) can satisfy its slice of the request from. Each
provider lists the NUMA-node subsets that can supply its requested amount, marking
`Preferred=true` on those using the **minimum** number of NUMA nodes sufficient to satisfy
the request from their **Allocatable** capacity (total installed devices; `m.allDevices` in the
kubelet device manager). Feasibility — whether a given mask actually has enough free devices —
is checked against **Available** (currently unallocated). This two-pass structure means that
when some devices are already placed, a single-zone placement can be *preferred* by capacity but
*infeasible* by availability; the only feasible mask (multi-zone) is then non-preferred →
`restricted` rejects. A hint is a candidate grouping, **not** an allocation — it names no
specific device/core.

The Topology Manager merges one hint per provider (`mergePermutation`): merged affinity is the
**bitwise-AND** of the picked affinities, and is `Preferred` **iff all picked affinities are
equal *and* all are individually preferred**. `restricted` admits **iff the best merged hint is
`Preferred`**, which reduces to a clean, short-circuitable rule:

> **`restricted` admits ⟺ there exists a NUMA-node mask `M` such that, for every NUMA-relevant
> resource the pod requests, `M` is a preferred (minimal-width) satisfying hint for it.**

`single-numa-node` is the special case `|M| = 1`. The kubelet's full
`compare`/`BestNonPreferredAffinityCount` machinery only picks *which* non-preferred hint wins
for `best-effort`; it is not needed for the `restricted` admit decision. On admission the kubelet
stores `M` and each provider allocates **within** `M` — the per-zone split is not fixed, so any
allocation drawing every resource from nodes in `M` is acceptable.

**Worked examples** (node has 2 NUMA nodes):

| Per-node capacity | Pod (Guaranteed) | Per-resource preferred masks | Common mask? | `restricted` verdict |
| --- | --- | --- | --- | --- |
| 4 GPU, 16 CPU | 6 GPU + 10 CPU | GPU `{0,1}`; CPU `{0}`/`{1}` | none (GPU needs 2, CPU needs 1) | **reject** |
| 4 GPU, 16 CPU | 6 GPU + 24 CPU | GPU `{0,1}`; CPU `{0,1}` | `{0,1}` | **admit on `{0,1}`** |
| 2 GPU, many CPU | 4 GPU + 1 CPU | GPU `{0,1}`; CPU `{0}`/`{1}` | none | **reject** |

The third row is an instructive footgun: a 4-GPU pod that *could* run 2+2 with its single CPU
anywhere is **rejected by the kubelet itself** under `restricted`, because the CPU's minimal
width (1) disagrees with the GPU's (2). The only ways to run it are to raise the CPU (or memory)
request above one node's capacity, or to use `best-effort`. The plugin faithfully reproduces this
rejection — it does not (and must not) "fix" it.

#### Reimplement the merge, don't import it

The merge + `Preferred`/admit rule is small (the admit short-circuit is a few dozen lines).
Importing `k8s.io/kubernetes/.../topologymanager` (an internal kubelet package) would couple KAI to
kubelet internals; upstream scheduler-plugins itself imports only `bitmask` and reimplements the
rest. v1 does the same.

**One generic counting rule covers GPU, CPU and memory.** Per-resource hint generation is
*identical* across all three kubelet hint providers, so a single generic rule over `resource.Quantity`
reproduces them — there is no per-resource hinter and no vendor-specific hint code:

- **Device Manager** — `generateDeviceTopologyHints`
  (`k8s.io/kubernetes/pkg/kubelet/cm/devicemanager/topology_hints.go`): preferred width =
  fewest NUMA nodes whose **total** device count (`m.allDevices`) covers the request; a mask is
  feasible iff its **available** device count covers it; `Preferred` iff the mask's width equals the
  minimal width (`minAffinitySize`).
- **CPU Manager** — `generateCPUTopologyHints` (`.../cpumanager/policy_static.go`): the same,
  counting **CPUs** (`CPUDetails.CPUsInNUMANodes` for capacity, `availableCPUs` for feasibility).
- **Memory Manager** — `calculateHints` (`.../memorymanager/policy_static.go`): the same, summing
  **memory bytes** (`Allocatable` for capacity, `Free` for feasibility).

All three reduce to one two-pass rule — *preferred width = fewest zones whose summed `Allocatable` ≥
request; feasible = summed `Available` ≥ request; `Preferred` iff width = the minimal width* —
differing only in the **unit** (device count, CPU count, memory bytes), each just a
`resource.Quantity`. So the plugin runs one generic counting hinter for every NUMA-relevant resource.

**Is there any deviation between the providers?** Only in edge cases, all captured in *Known
Limitations* and none changing the common-case verdict: the Memory Manager's multi-NUMA *group*
bookkeeping (consistency of already-grouped NUMA nodes); the CPU Manager's alpha `align-by-socket` /
`prefer-align-cpus-by-uncore-cache` options (off by default, and *relaxations* — so the generic rule
is only ever conservative there); and devices whose topology spans multiple NUMA nodes (rare).
Absent those, the providers' hint generation and the generic rule are identical.

### In-cycle reservation (EventHandler)

Within-cycle correctness rides the existing session `EventHandler` (`framework.Event{Task}`), which
fires symmetrically on commit and on rollback/undo. On allocate, the task's chosen placement is
charged against the node's per-zone `Available`; on deallocate (rollback, or virtual eviction during
preempt/reclaim probing), the exact per-zone amounts are credited back. A task with no placement is
not accounted (no re-derive).

For `single-numa-node` this charges exactly one zone. For `restricted`, the chosen mask `M` may
span several zones; the kubelet does not fix the per-zone split at admission, so the plugin uses
an **approximate greedy split** across `M`'s zones (internal accounting only — see the
reservation-split caveat in *Known Limitations*).

The placement (zones **and** amounts) rides `PodInfo`, set during the allocate step before
`Pipeline` like `GPUGroups`, so the copy the statement clones onto the node carries it and the dedup
can compare it. Because it rides `PodInfo`, the statement's existing undo machinery **snapshots the
previous placement on virtual eviction and restores it on rollback** (exactly as for
`GPUGroups`/`previousGpuGroups`), so preemption/reclaim scenario probing — which speculatively
allocates and `Discard()`s — stays consistent with **no plugin-side bookkeeping**. The chosen zones
are internal accounting only; they are never sent to the kubelet, which independently re-derives
placement.

This restore-by-snapshot is necessary but **not sufficient**: the solver's *eviction dedup* can
cancel a victim's eviction outright. That interaction is handled via the same `NUMAPlacement`
identity — see *Interaction with eviction dedup*.

This layer is *within-cycle*: only a **committed** bind persists its chosen placement durably — as
the scheduler-predicted placement record, next.

### Interaction with eviction dedup

The solver de-duplicates virtual evictions: when a task is re-pipelined to a node it was already
evicted from in the same scenario, the statement (`Pipeline` → `Unevict`) **cancels** the pending
eviction instead of double-counting, and restores the task's allocation identity from the copy on
the node. Its only existing "don't dedup" exception is a *shared-GPU-moved-to-a-different-GPU*
check, which is **always false for whole-GPU / NUMA pods**. So without change, such a pod's
eviction is unconditionally cancelled regardless of which NUMA zone the scenario would move it to,
which (a) **drifts the ledger** — accounting believes the pod moved zones while the kubelet keeps
it pinned to the old one — and (b) **silently defeats any scenario that needed the victim on a
different zone** (e.g. consolidating a victim off the exact zone the pending pod needs).

v1 closes this by giving the chosen placement the same first-class allocation-identity treatment
GPU sharing already gets:

- **`NUMAPlacement` on `PodInfo`** (defined in *NUMA data model*) — the task's
  chosen zone(s) and per-zone amounts, set during the allocate step (before `Pipeline`'s dedup
  check), mirroring `GPUGroups`. It is the same placement the record persists, so the in-memory
  identity and the durable annotation agree.
- The framework **snapshots the previous `NUMAPlacement` on virtual eviction and restores it** on
  evict undo / pipeline undo, exactly as it already does for `GPUGroups` (`previousGpuGroups`).
- A **`numaPlacementChanged` gate** is added to the dedup, analogous to
  `isSharedAndMoveToDifferentGPU`: when the task's new placement differs from the copy on the
  node, the eviction is *not* deduped, so the move is realized. The comparison is the **full
  placement — zones *and* per-zone amounts**, not zone identity alone. The per-zone split is a
  free variable (it depends on evaluation-time headroom), so a consolidation/rebalance can
  deliberately re-lay-out a pod onto the *same* zone set; deduping that on zone identity would
  silently restore the old split and desync the ledger. (Unlike GPU, where per-task memory is
  fixed and group identity is a sufficient move key, NUMA needs the amounts.) An ordinary victim
  that stays put carries its placement unchanged, so it still dedups.

This is a small, mechanical extension of the existing GPU-sharing dedup path; it is the one piece
of v1 that touches shared framework code (`pkg/scheduler/framework/statement.go`,
`pkg/scheduler/api/pod_info`) rather than the plugin alone.

### Scheduler-predicted placement record

The evaluator produces a prediction of each pod's NUMA placement (`NUMAPlacement`). Within a cycle
it rides `PodInfo`; persisting it on commit turns it into a durable, per-pod **placement record**
that survives across cycles and scheduler restarts. **This record is part of v1.**

- **On commit only**, the chosen zone(s) are carried in the `BindRequest` (a new field, exactly
  like `SelectedGPUGroups` / `ResourceClaimAllocations`), and the binder writes them to a pod
  annotation (`kai.scheduler/numa-placement-predicted`). This piggybacks on the bind the binder
  already performs — **no extra API writes** — and the `BindRequest` is added to the snapshot
  store synchronously, so the prediction is readable the very next cycle. Speculative
  (probed-then-discarded) allocations are never persisted.
- **On later cycles**, each pod's `NUMAPlacement` is populated from this recorded prediction at
  snapshot build (when no observed annotation supersedes it). This is what makes the reclaim
  eviction-crediting **stable**: a recorded prediction never drifts, whereas guessing would
  (and a restart would guess inconsistently). It is the persistent form of the per-pod placement
  the eviction-crediting needs.

**Precedence: observed > predicted.** This record is the scheduler's *prediction*, not ground
truth. When the per-node placement exporter (next) has published a pod's *observed* placement, that
supersedes this predicted one; when the exporter is absent or hasn't reported a pod yet, the
predicted record is the best available placement. When **neither** exists, the pod has no
`NUMAPlacement` and is not accounted on virtual eviction — v1 never *guesses* a zone.

### Observed placement: the per-node exporter

Prediction is only as good as the scheduler's evaluator matching the kubelet's actual choice. To
make per-zone accounting (and especially reclaim) *exact*, v1 also consumes the **observed**
placement produced by a per-node exporter — a DaemonSet that reads the kubelet **podresources API**,
derives each pod's actual per-NUMA-zone resource placement, and publishes it as a pod annotation
(`kai.scheduler/numa-placement-observed`). When present, the plugin uses observed placement
directly: occupancy is exact, victim evictions credit the *real* zone, and reclaim simulation is
accurate. When absent or not-yet-reported (exporter undeployed, lagging, or pod just bound), the
plugin falls back to the predicted record — and when that is also absent, the pod is simply not
accounted on virtual eviction (no guessing). So the exporter is **purely additive**: it improves
accuracy without being a hard dependency, and the scheduler is built to consume its input from day
one. **Scope:** the *scheduler-side* consumption of the observed annotation is part of v1; the
per-node exporter's own implementation and delivery are tracked separately (not in this PR).

When the exporter is deployed, the operator auto-enables observed-based reconstruction (see *Operator
integration*); without it, the scheduler runs on the prediction fallback. Full design:
[Per-Node NUMA Placement Exporter](../numa-placement-exporter/README.md).

### Policy evaluator seam

Both policies' admit / zone-selection logic is isolated behind one interface, so the predicate
and the reservation are policy-agnostic:

```go
// evaluate returns whether the pod can be NUMA-aligned on this node, and the
// zone(s) the in-cycle reservation should charge — one zone for single-numa-node,
// one or more for a restricted merge.
type numaEvaluator interface {
    evaluate(nt *NumaTopology, req resourceRequests) (zones []*NumaZone, admit bool)
}
```

v1 ships **two** evaluators, selected per node by its Topology Manager policy:
- `singleNUMAEvaluator` — the bitmask intersection (`single-numa-node`); always returns one zone.
- `restrictedEvaluator` — the hint merge (`restricted`); returns the chosen mask's zones. It builds
  per-resource hints with the single generic counting rule (`Allocatable` for preferred-width,
  `Available` for feasibility; see *Reimplement the merge*) and searches for a common minimal-width
  mask — one rule covers GPU, CPU and memory, so there is no per-resource registry.

The predicate and the `AllocateFunc`/`DeallocateFunc` reservation both route through `evaluate`
and charge whatever zones it returns. v2's scoring layer reuses the same evaluators and per-zone
model — it only adds ranking, never changes the admit decision.

### Registration

Register the builder in `pkg/scheduler/plugins/factory.go`:

```go
framework.RegisterPluginBuilder("numa", numa.New)
```

and enable it in the scheduler plugin configuration. The only argument is the optional resource
**ignoreList** (see *NUMA-relevant resources*), read from `PluginArguments`.

### Deployment guidance: NRT freshness vs. schedule period

The cross-cycle staleness window (see *Known Limitations*) is an **operational** concern
before it is a code concern. The recommended deployment mitigates it without any cross-cycle
state in the plugin:

- **Keep the exporter's event-driven updates enabled (the default).** Both exporters — NFD's
  topology-updater ([nfd-tu]) and the resource-topology-exporter (RTE, [rte]) — watch the kubelet
  state directory (`cpu_manager_state`, `memory_manager_state`, `kubelet_internal_checkpoint`)
  via fsnotify and republish NRT immediately on an allocation change, *in addition to* a periodic
  refresh (`-sleep-interval`/`--sleep-interval`, default **60s**, configurable to any duration or
  to `0` to disable periodic updates). So NRT is normally fresh within ~sub-second to a few
  seconds of a pod start/stop. Use caution when setting the *periodic* interval very
  low — that is a per-node-per-interval write storm at fleet scale; the **event** path is what
  delivers freshness. (RTE rate-limits event scans via `--max-events-per-second`, default 1.)
- **Raise `--schedule-period`** (default `1s`) to, e.g., `5s`. This gives the full
  bind → kubelet-admit → exporter → apiserver → informer pipeline time to reflect a binding
  before the next cycle, so prior binds are visible and the hot-loop does not form. Note this
  is a **global** knob — it raises scheduling latency for *all* pods, which is generally
  acceptable for AI/ML batch workloads but should be weighed for latency-sensitive ones.
- **Observe it.** Emit a metric/log when the kubelet rejects a NUMA pod
  (`TopologyAffinityError`) or when the scheduler re-selects a node it just failed on. This
  reveals whether the timing assumption actually holds in a given fleet — and therefore whether
  [Appendix A](#appendix-a-cross-cycle-staleness-compensation) is ever needed.

This is a timing assumption, not a guarantee: under bind bursts, kubelet admission lag, or
exporter backlog the window can still exceed a cycle. The kubelet preserves correctness
regardless; Appendix A is the in-plugin fallback if the assumption proves insufficient.

## Correctness and Known Limitations

- **The kubelet is the backstop.** Any divergence between this plugin and the kubelet costs
  extra reschedules.
- **Provider-participation divergence.** NRT reports `cpu`/`memory` per zone even when the
  kubelet's CPU Manager is not `static` (in which case CPU is not actually hint-aligned).
  `single-numa-node` deployments almost always run CPU Manager `static` + Memory Manager, so
  the assumption holds in practice; documented as a divergence source.
- **Greedy container-scope packing** is order-sensitive and an approximation of the kubelet's
  per-container hint merge. Exact in the common single-GPU-container case.
- **Reclaim-simulation accuracy depends on the placement source.** NRT is aggregate per-zone only,
  so the victim's zone comes from its placement record (observed > predicted). Reclaim/preemption
  runs on those zones and can occasionally waste an eviction when the pending pod needs multiple
  per-zone-scarce resources co-located (GPU-bound pods with abundant per-zone CPU are largely
  immune). With the [per-node placement exporter](../numa-placement-exporter/README.md) deployed, victim
  zones are *observed* and reclaim is accurate; with only the predicted record the worst case is a
  wasted eviction and a bounce, never a loop. A victim with **no** placement record (neither
  observed nor predicted) is **not credited** on virtual eviction — so a NUMA-sensitive preemptor
  may miss it, but accounting never drifts on a guess. v1 never re-derives a zone.
- **Allocatable-vs-available split in preferred-width computation.** The kubelet device manager
  computes `minAffinitySize` (which governs `Preferred`) from total device capacity (`m.allDevices`),
  not from currently-free devices. When zone capacity is partially allocated, a single-zone placement
  may be preferred by capacity yet infeasible by availability — making the only feasible mask
  (multi-zone) non-preferred → `restricted` rejects. This is correct kubelet behavior; the plugin
  matches it by using `Allocatable` for preferred-width and `Available` for feasibility. Confirmed
  against kubelet source: `pkg/kubelet/cm/devicemanager/topology_hints.go` lines 176–184
  (`m.allDevices` pass) vs. 201–210 (available-device feasibility pass). See FOLLOWUPS item 10.

## Testing

- **Unit**: policy/scope parsing from NRT attributes (and legacy `TopologyPolicies`); the
  `single-numa-node` bitmask filter across single/multi-zone fits; QoS gating; per-node
  NUMA-relevant inference (resource constrains iff reported per-zone) and ignoreList exclusion; pod-
  vs container-scope; `shouldHandle` rejection of fractional/MIG/non-Guaranteed pods.
- **`restricted` merge**: the worked examples above (admit on a common minimal-width mask;
  reject when per-resource minimal widths disagree, incl. the 4-GPU+1-CPU footgun); hinter-
  coverage fallback to `singleNUMAEvaluator`; multi-zone mask selection.
- **Reservation**: in-cycle multi-pod placement on a multi-NUMA node (single- and multi-zone
  charges); rollback consistency through allocate → discard (preemption probing).
- **In-cycle consistency** (scheduler integration tests): on a single multi-NUMA node, schedule a
  set of pods that *would* all fit by whole-node accounting but cannot under the per-zone
  constraint, and assert only the NUMA-feasible subset is placed. Example: two 4-core NUMA zones
  (8 cores total) with three pods requesting 3, 3, and 2 cores — whole-node capacity admits all
  three, but after two 3-core pods each zone has only 1 free core, so the 2-core pod cannot be
  aligned and exactly two schedule. (The same scenario doubles as a consolidation test.)
- **Stale-node behavior** (scheduler integration tests): using the fake-NRT update delay, feed
  NRT whose `Available` lags recent binds and assert the documented behavior — in-cycle
  reservation prevents over-commit within a cycle, the scheduler does not place pods the
  (simulated) kubelet would reject, and it converges once NRT catches up; with Appendix A enabled
  (the exporter present), that reconstruction from observed placements corrects the stale view
  immediately rather than hot-looping.
- **NUMA-aware preemption, reclaim, and consolidation** (integration tests and e2e): verify these
  actions respect per-zone constraints — evicting/reclaiming a victim actually frees a *usable
  aligned* slot for the pending pod (eviction-zone crediting), a multi-cycle reclaim plan stays
  stable on its target node, and consolidation relocates pods while preserving NUMA feasibility,
  without wasted evictions.
- **E2E** (with a Kind node exposing synthetic NRT objects): a Guaranteed whole-GPU pod is
  filtered off a node whose free GPU/CPU cannot co-locate, and placed on one where they can.
- **Fake-NRT test mechanism.** Realistic fake/e2e coverage needs a NUMA-topology analog of the
  [fake-gpu-operator][fgo] — a component that fakes per-node NUMA topology and NRT objects (with
  the Topology Manager policy/scope attributes), simulates the kubelet-like per-pod NUMA allocation
  and rejection for bound pods, reflects that consumption in NRT `Available` after a configurable
  (jittered) update delay, and exposes each pod's observed placement for the
  [placement exporter](../numa-placement-exporter/README.md) to discover. This lets the plugin's
  prediction/`TopologyAffinityError` handling, the reconstruction/staleness path (Appendix A), and
  the exporter be tested without real NUMA hardware. Requirements:
  [Fake NRT Simulation Mechanism](../fake-nrt/README.md).

## v2: Optimization & scoring

v1 decides *feasibility* — can this node host the pod without a `TopologyAffinityError`. v2
decides *which feasible node is best*, via a node score (`AddNodeOrderFn`, a new band in
`scores/scores.go`). It reuses v1's evaluators and per-zone model unchanged: it only **ranks**
nodes, never alters the admit decision.

### What scoring adds

- **Optimize `best-effort` performance.** On a `best-effort` node the kubelet never rejects — it
  silently runs the pod *unaligned* when it can't fit a NUMA node, costing throughput. v1 does
  nothing for `best-effort` (there is no admission error to prevent). v2 **scores** `best-effort`
  nodes by whether the pod's resources *can* be aligned there, steering it toward a node where
  the kubelet's best-effort alignment will actually succeed — turning a silent performance loss
  into a good placement. This is the primary motivation for v2.
- **Prefer tighter, less-fragmented fit** on feasible `single-numa-node` / `restricted` nodes, so
  later pods still find aligned room, and multi-NUMA pods span the fewest zones.

### Scoring strategies

Reusing the upstream NodeResourceTopology scoring vocabulary, computed over the plugin's per-zone
model:

- **LeastNUMANodes** (policy-agnostic) — prefer nodes where the pod spans the fewest NUMA nodes
  (ideally one). This is the core `best-effort` steering and the multi-NUMA-span minimizer.
- **LeastAllocated / MostAllocated / BalancedAllocation** — spread vs. bin-pack vs. balance
  per-zone utilization, for fragmentation control on the aligned policies; selectable via config.

### Notes

- Scoring runs on the same predicted per-zone state as v1, so the prediction caveats carry over —
  but a score is only a *preference*, so a misprediction costs ranking quality, never correctness.
- `best-effort` scoring is the one place the plugin touches `best-effort` nodes at all; v1 leaves
  them untouched, and the admit decision for `single-numa-node` / `restricted` is unchanged.

## v3: Pod-level NUMA policy (scheduler-enforced)

*A direction for future discussion — not yet designed. API and mechanics are open; this section
only records the idea.*

Today NUMA intent is a property of the **node**: the kubelet's Topology Manager policy applies to
every pod on it, all-or-nothing. That leaves two gaps — `best-effort` gives no alignment guarantee
to workloads that want one, while `restricted` / `single-numa-node` force alignment on *every*
Guaranteed pod (including ones that don't care) and carry the request-inflation quirk.

v3 would make NUMA intent a property of the **workload**: on a permissive (`best-effort` / `none`)
node, a performance-sensitive pod declares its own NUMA constraint and the scheduler enforces it by
placement, while other pods on the same node stay unconstrained. Mechanically this reuses v1's
machinery — drive the evaluator from a per-pod declaration instead of the node policy — and relies
on the kubelet's `best-effort` aligner (which still aligns when it can) to deliver the pinning.

Two properties make it attractive:

- **Softer failure mode than v1.** A `best-effort` kubelet never rejects, so a scheduler
  misprediction yields an *unaligned* pod (a throughput hit), never a `TopologyAffinityError` or a
  stuck `Pending`.
- **Pod-granularity NUMA requirements.** Like network topology, the sensitivity to NUMA placement 
  should be a property of the workload, and specifically, of the pod. This implementation lets
  the users express their workloads' requirements, instead of having the admin config this globally.
- **Per-resource granularity.** A workload could ask to align only what it cares about (e.g. GPU
  and NIC, not CPU), sidestepping the node-level all-resource merge that drives `restricted`'s
  request-inflation quirk.

**Honest limitation — not a hard guarantee.** Because a `best-effort` kubelet never rejects, the
scheduler cannot provide a *kubelet-enforced* guarantee; it offers a strong placement preference
(place only where alignment is achievable, plus reservation) and the kubelet best-effort path
delivers it. A true "align or don't run" guarantee would additionally need the
[placement exporter](../numa-placement-exporter/README.md) to observe actual placement and re-place on a
miss (verify-and-heal).

This also lines up with where Kubernetes is heading — **DRA**, where workloads express device and
topology constraints and the scheduler allocates against them; v3 is a KAI-native precursor to that
model.

## Appendix A: cross-cycle staleness compensation

**Status: part of the design, opt-in via a boolean plugin flag, auto-configured by the operator.**
NRT `Available` is republished by the exporter and **lags across cycles**. When the per-node
[placement exporter](#observed-placement-the-per-node-exporter) is deployed, the scheduler can ignore the
laggy `Available` entirely and **reconstruct** each zone's free capacity from data that is always
fresh. The operator enables this automatically when the exporter is present (overridable); without the
exporter the scheduler trusts NRT `Available` and relies on the operational mitigation in *Deployment
guidance*. Correctness never depends on this — the kubelet is the backstop — but on packed or
single-node clusters the stale window is hit on nearly every bind, so the correction matters in
practice.

### The problem

NRT is republished near-real-time on allocation changes (event-driven) but can lag up to its
periodic refresh (default 60s; see *Deployment guidance*) when events are disabled or delayed.
During any such lag NRT `Available` is stale in **both** directions:

- **A just-bound pod is missing** → `Available` over-reports free capacity → a second NUMA pod is
  placed on the same zone, the kubelet rejects it (`TopologyAffinityError`), and the next cycle
  re-picks the same node off the same stale data — a hot-loop until NRT catches up.
- **A just-deleted pod still lingers** → `Available` under-reports → a freed zone looks occupied.
  This is especially harmful under preemption: after evicting a victim on one NUMA node and
  pipelining the preemptor onto it, a stale `Available` that still shows the victim's zone occupied
  can drive the scheduler to preempt a *second* victim on another zone — over-evicting.

### The mechanism: reconstruct `Available` from `Allocatable` minus known placements

The plugin already separates the two roles of per-zone data — `Allocatable` (static capacity, drives
preferred width) and `Available` (free space, drives feasibility). A boolean flag changes only the
**source of `Available`**:

```
Available[zone] = Allocatable[zone] − Σ placement[zone]   over every pod the scheduler sees on the node
```

where each pod's placement is resolved by the precedence already used for eviction crediting —
**observed (exporter) > predicted (BindRequest / annotation)**. The evaluator, predicate and merge are
unchanged; they consume whatever `Available` the topology carries. This reads from three sources,
**none of which is the laggy NRT `Available`**:

1. **`Allocatable`** — static per-zone capacity; never changes within a node's lifetime.
2. **The set of pods on the node** — from the scheduler's own snapshot, which sees binds *and
   deletions* immediately, long before the NRT exporter republishes.
3. **Each pod's zone** — the exporter's **observed** placement (ground truth, read from the kubelet
   podresources API), with the scheduler's own **predicted** placement as a fallback for the brief
   window between a bind and the exporter's first report.

### Why anchor on *observed*, not predictions

Reconstructing from *predicted* placements alone was rejected earlier: predicted zones often
disagree with the kubelet's actual choice, and the error would scale with the whole pod count. The
exporter removes that objection — observed placement is the kubelet's real per-zone assignment, so the
reconstruction is **exact for every pod the exporter has reported**. Prediction survives only as a
fallback for a just-bound pod the exporter has not yet observed (seconds), for that one pod, and is the
scheduler's own prediction — internally consistent (the pod was pipelined onto the zone it
predicts). The exporter annotates **all** pods with exclusive NUMA allocations — KAI-scheduled *and
foreign* — so the subtraction is complete for `cpu`/`memory` (every exclusive consumer is
accounted), not only for GPUs.

### Why it beats trusting NRT `Available`

Because it never reads NRT `Available`, it is immune to exporter lag in both directions:

- **Additions**: a just-bound pod is in the snapshot immediately and subtracted (observed once
  reported, predicted until then) — no over-allocation window.
- **Deletions**: a deleted/Releasing pod leaves the snapshot immediately, so its zone is credited at
  once (`Allocatable − Σ(remaining)`), with no dependence on the exporter noticing the deletion.
- **Preemption continuation**: a Releasing victim still running is charged (still consuming); a
  Pipelined preemptor is charged on its predicted zone; once the victim deletes it drops out and the
  zone frees — all from the fresh snapshot, so the over-eviction scenario above cannot arise.

It is also simpler than any scheme that keeps NRT `Available` as the baseline and patches it: there
is no staleness *detection* step, because the laggy source is not used at all.

### Operator integration

The flag is **auto-enabled when the placement exporter is deployed**, and can be explicitly overridden.
The common path is zero-touch: deploy the exporter (the operator does this when the `numa` plugin is
enabled — see *Operator integration*) and the scheduler switches to reconstruction automatically;
remove the exporter (or override the flag) and it reverts to trusting NRT `Available`.

### Caveats

- **Accuracy depends on a healthy exporter.** With the flag on but the exporter absent or badly lagging,
  reconstruction degrades to *predicted-only* — the very mode this avoids. The operator only enables
  the flag alongside the exporter; the plugin should additionally treat a pod running well beyond the
  exporter's report interval with no observed annotation as an exporter-health signal (log/metric), so the
  degradation is visible rather than silent.
- **A pod with neither observed nor predicted placement** is omitted from the subtraction (never
  guess a zone) → a transient per-zone over-report on its zone. With the exporter covering all pods this
  is limited to the bind→observe window of KAI's own pods, where the predicted record covers it.
  *Potential follow-up mitigation:* on a node where any consuming numa-sensitive pod still lacks a placement,
  **defer** (pipeline) numa-sensitive allocations rather than binding them — keeping the node a
  candidate while waiting until the per-zone data is trustworthy — instead of risking a bind the
  kubelet rejects.
- **`Allocatable` already nets out reserved capacity** (kube/system-reserved), so
  `Allocatable − Σ exclusive` is the correct free-for-alignment figure; no separate reserved
  handling is needed.

## Operator integration (intent)

The KAI operator should make the placement exporter zero-touch: **detect whether the `numa` plugin
is enabled and, if so, deploy the placement exporter automatically** (unless an operator has
explicitly disabled it). The plugin works without the exporter (predicted placement), so this is a
convenience/accuracy default, not a hard dependency. Design details (how detection works, the
disable switch, lifecycle) are deferred.

[tm]: https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/
[tm-none]: https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#policy-none
[tm-best-effort]: https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#policy-best-effort
[tm-restricted]: https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#policy-restricted
[tm-single-numa-node]: https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#policy-single-numa-node
[cpu-mgr]: https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/
[mem-mgr]: https://kubernetes.io/docs/tasks/administer-cluster/memory-manager/
[nrt-match]: https://github.com/kubernetes-sigs/scheduler-plugins/blob/master/pkg/noderesourcetopology/README.md
[nrt-api]: https://github.com/k8stopologyawareschedwg/noderesourcetopology-api
[nfd-tu]: https://github.com/kubernetes-sigs/node-feature-discovery/blob/master/pkg/nfd-topology-updater/kubeletnotifier/kubeletnotifier.go
[fgo]: https://github.com/run-ai/fake-gpu-operator
[rte]: https://github.com/k8stopologyawareschedwg/resource-topology-exporter/blob/main/pkg/notification/notification.go
