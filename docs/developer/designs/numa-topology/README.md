# NUMA-Aware Scheduling via NodeResourceTopology

## Summary

This document describes a v1 design for making KAI-Scheduler aware of per-NUMA-node
resource topology, so that **Guaranteed-QoS, whole-GPU workloads** are placed only on
nodes where the kubelet's Topology Manager can actually align their GPU, CPU, memory and
NIC resources onto a single NUMA node.

The scheduler consumes the [`NodeResourceTopology`][nrt-api] (NRT) CRD, which is published
per-node by an external exporter (NFD topology-updater or the resource-topology-exporter).
A new `numa` plugin replicates the kubelet's Topology Manager admission check — for both the
`single-numa-node` and `restricted` policies — against the NRT data as a **filter predicate**,
and tracks per-NUMA-zone consumption **within a scheduling cycle** so that multiple pods placed
on the same node in one cycle are not over-committed onto the same zone. Compensating for NRT *staleness across cycles* is an optional extension
([Appendix A](#appendix-a-optional-cross-cycle-staleness-compensation)), not part of v1.

## Motivation

The kubelet's Topology Manager makes the real NUMA-alignment decision at **pod admission
time**, after the scheduler has already chosen a node. When a node is configured with a restrictive
policy like `single-numa-node`  or `restricted` and a Guaranteed pod's resources cannot all be satisfied according to it, the kubelet rejects the pod with a `TopologyAffinityError` and the pod returns to
`Pending`. The scheduler then re-attempts — potentially (in most cases, likely) picking the same bad node again —
producing wasted cycles and, in the worst case, a hot loop, and wasting the workload's time and precious compute resources.

The scheduler cannot *enforce* NUMA alignment (the kubelet owns that), but it can *predict*
it and avoid placing pods where the kubelet will reject them. This is the same role played
by the upstream [`NodeResourceTopologyMatch`][nrt-match] plugin in kubernetes-sigs/scheduler-plugins.

The highest-value case for KAI is GPU locality: strict GPU↔CPU↔NIC NUMA affinity (e.g. for
GPUDirect RDMA) materially affects throughput for AI/ML workloads. That is the `single-numa-node`
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

- **Fractional / MIG GPU sharing.** Only whole-GPU (`RequestTypeRegular`, integer
  `nvidia.com/gpu`) Guaranteed pods are handled. Shared-GPU pods are typically not
  Guaranteed QoS, so the kubelet Topology Manager does not align them.
- **100% prevention of kubelet pod rejections.** The current implementation of NUMA topology is inherently split-brained: the kubelet decides the actual placement of pods, while the scheduler attempts to predict that and match it's decisions. While we can probably approximate it pretty well and cover for some gaps like inter-cycle allocations, some mismatches might still occur, like when foreign (non kai-scheduler) pods are bound to nodes, or many pods are bound concurrently (NUMA allocation can be affected by order). The design aims to mitigate those cases as much as possible, and to be **self-healing**: when mismatches occur, we aim for the scheduler to be **eventually consistent** with the real state, so errors will not be carried for many cycles.

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

The work is staged into two phases (plus a v3 idea, and one optional enhancement —
cross-cycle staleness, [Appendix A](#appendix-a-optional-cross-cycle-staleness-compensation)):

- **v1 — correctness (this section).** A **filter** that predicts the kubelet's admission verdict
  for the two policies that *reject* on topology grounds (`single-numa-node` and `restricted`),
  plus **within-cycle per-zone reservation** so pods placed together in one cycle stay consistent.
  The aim is to prevent the wasted cycles and stranded capacity from *Background* — pods
  land where they can actually run. `best-effort` and `none` are pass-through.
- **Observed placement (v1).** A per-node agent publishes each pod's *actual* NUMA placement; the
  scheduler consumes it for exact per-zone accounting (and accurate reclaim) when available, and
  **falls back to its own prediction when the agent is absent or lagging**. The agent ships with
  v1, but deploying it is optional — the scheduler degrades gracefully without it. See *Observed
  placement: the per-node agent*.
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

### Plugin-local per-zone data model

The plugin builds its own working state at `OnSessionOpen` from the snapshot's NRT data and
mutates it during the cycle. The resource vectors are **not** involved.

```go
type tmPolicy int // none | bestEffort | restricted | singleNUMANode
type tmScope  int // container | pod

// One NUMA node's working headroom, seeded from NRT zone Available,
// decremented as tasks commit in-cycle, restored on rollback/eviction.
type numaZone struct {
    id        string
    available map[v1.ResourceName]resource.Quantity
}

type nodeTopology struct {
    policy        tmPolicy
    scope         tmScope
    zones         []*numaZone               // NRT zones of Type == "Node"
    topologyAware sets.Set[v1.ResourceName] // resources this node reports per-zone, minus denylist
}

type numaPlugin struct {
    denylist  sets.Set[v1.ResourceName]      // optional; resources reported per-zone but NOT aligned
                                             // (e.g. cpu/memory when their manager is off). Default empty.
    nodes     map[string]*nodeTopology       // rebuilt each OnSessionOpen; nil entry ⇒ pass
    reserved  map[common_info.PodID][]string // task UID → charged zone id(s); 1 for single-numa, ≥1 for restricted
}
```

The scheduler instantiates a **fresh plugin instance every cycle** (`OpenSession` calls the
builder then `OnSessionOpen`), so all plugin state is per-cycle: `nodes` is rebuilt from the
snapshot's NRT data each cycle, and `reserved` tracks only the current cycle's in-flight
allocations. v1 keeps no cross-cycle state (see
[Appendix A](#appendix-a-optional-cross-cycle-staleness-compensation)).

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
- **Optional denylist**: an operator who knows a reported resource is
  *not* aligned on their nodes (e.g. `memory` with Memory Manager `None`, or `cpu` without
  `static`) lists it, excluding it from per-zone reasoning and recovering the over-rejected
  capacity. Default is empty.

(The QoS gate still applies — `cpu`/`memory`/`hugepages` constrain only Guaranteed pods, matching
the kubelet, which aligns them only for Guaranteed QoS.)

> **Possible future work:** upstream a `cpuManagerPolicy` / `memoryManagerPolicy` NRT attribute (none
> exists today — exporters publish only the Topology Manager policy/scope). With it, `cpu`/`memory`
> alignment becomes inferable per node and the denylist can be dropped.

### `shouldHandle` gate

The plugin engages for a task only when **all** hold (otherwise the predicate passes
through):

- node has a `nodeTopology` entry whose policy is `singleNUMANode` or `restricted`, and
- `task.Pod.Status.QoSClass == Guaranteed`, and
- whole-GPU request: `task.ResourceRequestType == RequestTypeRegular` and integer
  `nvidia.com/gpu` (i.e. `!IsFractionCandidate() && !IsMigCandidate()`).

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
`Preferred=true` on those using the **minimum** number of NUMA nodes the request physically
needs. A hint is a candidate grouping, **not** an allocation — it names no specific device/core.

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

The merge + `Preferred`/admit rule — is small (the admit short-circuit is a
few dozen lines). Per-resource hint generation (enumerate NUMA-node subsets from per-zone
`Available`, mark minimal-width preferred) is generic; there seems to be **no vendor-specific hint code**
in the kubelet (device hints are driven by per-device NUMA affinity, which NRT already encodes as
per-zone counts). Importing `k8s.io/kubernetes/.../topologymanager` (an internal kubelet package)
would couple KAI to kubelet internals; upstream scheduler-plugins itself imports only
`bitmask` and reimplements the rest. v1 does the same.

### In-cycle reservation (EventHandler)

Within-cycle correctness rides the existing session `EventHandler`
(`framework.Event{Task}`), which fires symmetrically on commit and on rollback/undo:

```
AllocateFunc(e):
    nt = nodes[e.Task.NodeName]
    if !shouldHandle(e.Task, nt): return
    zones = evaluate(nt, requests(e.Task)).zones   // 1 zone for single-numa, ≥1 for restricted
    charge zones by requests(e.Task)               // restricted: split across the masked zones
    reserved[e.Task.UID] = ids(zones)

DeallocateFunc(e):
    zones = reserved[e.Task.UID]; if none: return
    credit back zones; delete reserved[e.Task.UID]
```

For `single-numa-node` this charges exactly one zone. For `restricted`, the chosen mask `M` may
span several zones; the kubelet does not fix the per-zone split at admission, so the plugin uses
an **approximate greedy split** across `M`'s zones (internal accounting only — see the
reservation-split caveat in *Known Limitations*).

Because the statement's undo path fires `DeallocateFunc` on rollback (and `AllocateFunc` on
redo), preemption/reclaim scenario probing — which speculatively allocates and `Discard()`s —
stays consistent automatically, with **no manual clone/restore**. Recording the charged zone(s)
(rather than recomputing them) guarantees the restore targets the exact zones even though
headroom changed in between. The chosen zones are internal accounting only; they are never sent
to the kubelet, which independently re-derives placement.

This layer is *within-cycle* and in-memory: speculative allocations from preemption probing must
never leak into long-lived state. Only a **committed** bind persists its chosen zone — as the
scheduler-predicted placement record, next.

### Scheduler-predicted placement record

`pickZone` produces a prediction of each pod's NUMA zone. v1 keeps that prediction in memory
only (the `reserved` map) for the current cycle. Persisting it turns it into a durable, per-pod
**zone ledger** that survives across cycles and across scheduler restarts:

- **On commit only**, the chosen zone(s) are carried in the `BindRequest` (a new field, exactly
  like `SelectedGPUGroups` / `ResourceClaimAllocations`), and the binder writes them to a pod
  annotation (`kai.scheduler/numa-placement-predicted`). This piggybacks on the bind the binder
  already performs — **no extra API writes** — and the `BindRequest` is added to the snapshot
  store synchronously, so the prediction is readable the very next cycle. Speculative
  (probed-then-discarded) allocations are never persisted.
- **On later cycles**, the plugin reads each pod's recorded prediction instead of re-deriving
  its zone. This is what makes the Appendix A reconstruction and the reclaim eviction-crediting
  **stable**: a recorded prediction never drifts (a re-derived one does, and a restart re-derives
  inconsistently). It is the persistent form of the per-pod ledger those mechanisms need. 

**Precedence: observed > predicted > re-derive.** This record is the scheduler's *prediction*,
not ground truth. When the per-node placement agent (next) has published a pod's *observed*
placement, that supersedes this predicted one; when the agent is absent or hasn't reported a pod
yet, the predicted record is the best available per-pod zone.

### Observed placement: the per-node agent

Prediction is only as good as the scheduler's `pickZone` matching the kubelet's actual choice. To
make per-zone accounting (and especially reclaim) *exact*, v1 also consumes the **observed**
placement produced by a per-node agent — a DaemonSet that reads the kubelet **podresources API**,
derives each pod's actual per-NUMA-zone resource placement, and publishes it as a pod annotation
(`kai.scheduler/numa-placement-observed`). When present, the plugin uses observed placement
directly: occupancy is exact, victim evictions credit the *real* zone, and reclaim simulation is
accurate. When absent or not-yet-reported (agent undeployed, lagging, or pod just bound), the
plugin falls back to the predicted record, then to re-derivation — so the agent is **purely
additive**: it improves accuracy without being a hard dependency, and the scheduler is built to
handle its input from day one.

The agent ships with v1, and the operator deploys it automatically when the `numa` plugin is
enabled (see *Operator integration*), but a cluster can run without it on the prediction
fallback. Full design: [Per-Node NUMA Placement Agent](../numa-placement-agent/README.md).

### Policy evaluator seam

Both policies' admit / zone-selection logic is isolated behind one interface, so the predicate
and the reservation are policy-agnostic:

```go
// evaluate returns whether the pod can be NUMA-aligned on this node, and the
// zone(s) the in-cycle reservation should charge — one zone for single-numa-node,
// one or more for a restricted merge.
type numaEvaluator interface {
    evaluate(nt *nodeTopology, req resourceRequests) (zones []*numaZone, admit bool)
}
```

v1 ships **two** evaluators, selected per node by its Topology Manager policy:
- `singleNUMAEvaluator` — the bitmask intersection (`single-numa-node`); always returns one zone.
- `restrictedEvaluator` — the hint merge (`restricted`); returns the chosen mask's zones.
  It builds per-resource hints from per-zone `Available` via a small `resourceHinter` registry
  (one generic counting hinter covers `nvidia.com/gpu` and `memory`; `cpu` needs care — see
  *Known Limitations*) and searches for a common minimal-width mask. If some requested
  topology-aware resource has no registered hinter, it falls back to `singleNUMAEvaluator` (a
  safe, stricter rejection).

The predicate and the `AllocateFunc`/`DeallocateFunc` reservation both route through `evaluate`
and charge whatever zones it returns. v2's scoring layer reuses the same evaluators and per-zone
model — it only adds ranking, never changes the admit decision.

### Registration

Register the builder in `pkg/scheduler/plugins/factory.go`:

```go
framework.RegisterPluginBuilder("numa", numa.New)
```

and enable it in the scheduler plugin configuration. The only argument is the optional resource
**denylist** (see *NUMA-relevant resources*), read from `PluginArguments`.

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
  [Appendix A](#appendix-a-optional-cross-cycle-staleness-compensation) is ever needed.

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
- **Reclaim-simulation accuracy depends on the placement agent.** NRT is aggregate per-zone only,
  so without observed placement the scheduler *predicts* each pod's zone; reclaim/preemption then
  runs on predicted victim zones and can occasionally waste an eviction when the pending pod needs
  multiple per-zone-scarce resources co-located (GPU-bound pods with abundant per-zone CPU are
  largely immune). With the [per-node placement agent](../numa-placement-agent/README.md) deployed
  (a v1 component — see *Observed placement*), victim zones are *observed* and reclaim is accurate;
  **when the agent is absent or lagging the scheduler falls back to prediction**, where the worst
  case is a wasted eviction and a bounce, never a loop.

## Testing

- **Unit**: policy/scope parsing from NRT attributes (and legacy `TopologyPolicies`); the
  `single-numa-node` bitmask filter across single/multi-zone fits; QoS gating; per-node
  NUMA-relevant inference (resource constrains iff reported per-zone) and denylist exclusion; pod-
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
  (simulated) kubelet would reject, and it converges once NRT catches up; with Appendix A enabled,
  that the fingerprint-driven reservation corrects the stale view rather than hot-looping.
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
  (jittered) update delay and refreshes the pod fingerprint, and exposes each pod's placement for
  the [placement agent](../numa-placement-agent/README.md) to discover. This lets the plugin's
  prediction/`TopologyAffinityError` handling, the fingerprint/staleness path (Appendix A), and the
  agent be tested without real NUMA hardware. Requirements:
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
  the users express their wokrloads' requirements, instead of having the admin config this globally.
- **Per-resource granularity.** A workload could ask to align only what it cares about (e.g. GPU
  and NIC, not CPU), sidestepping the node-level all-resource merge that drives `restricted`'s
  request-inflation quirk.

**Honest limitation — not a hard guarantee.** Because a `best-effort` kubelet never rejects, the
scheduler cannot provide a *kubelet-enforced* guarantee; it offers a strong placement preference
(place only where alignment is achievable, plus reservation) and the kubelet best-effort path
delivers it. A true "align or don't run" guarantee would additionally need the
[placement agent](../numa-placement-agent/README.md) to observe actual placement and re-place on a
miss (verify-and-heal).

This also lines up with where Kubernetes is heading — **DRA**, where workloads express device and
topology constraints and the scheduler allocates against them; v3 is a KAI-native precursor to that
model.

## Appendix A: (optional) cross-cycle staleness compensation

**Status: optional, not part of v1, and the *second* line of defense.** First apply the
operational mitigation in *Deployment guidance* (event-driven NRT exporter + longer
`--schedule-period`), which closes the staleness window in the common case with no in-plugin
state. Implement this appendix only if the observability signal shows the bounded reschedule
hot-loop still matters in practice. It does not affect correctness (the kubelet is the
backstop) — only scheduling efficiency during the NRT refresh window.

### The problem

The schedule period is **1s** (`defaultSchedulerPeriod`). NRT is republished by the exporter
near-real-time on allocation changes (event-driven), but can lag up to its **periodic** refresh
(default 60s, configurable; see *Deployment guidance*) if event updates are disabled or delayed.
During any such lag, NRT `Available` still shows the pre-binding state: a second NUMA pod can be
placed on the same node off stale data; under packing pressure the kubelet rejects it
(`TopologyAffinityError`), and since the next cycle sees the same stale NRT the scheduler
re-picks the same node — a hot-loop until NRT catches up. With event-driven updates active this
window is small; this appendix matters only when it is not.

### The clean signal: the NRT pod fingerprint

The hard part of any cross-cycle cache is *eviction* — knowing when NRT has caught up so the
cache can stop compensating. There is a deterministic signal for this: the **pod fingerprint**
([`podfingerprint`](https://github.com/k8stopologyawareschedwg/podfingerprint)). The exporter
hashes the set of pods (by `namespace+name`) whose resources it accounted for when building the
NRT object and publishes it on the object:

- attribute **`nodeTopologyPodsFingerprint`**, with **`nodeTopologyPodsFingerprintMethod`** =
  `all` or `with-exclusive-resources` (legacy annotation `topology.node.k8s.io/fingerprint`).

It lets the scheduler answer *"does this NRT object already reflect the pods I know about?"*
exactly:

1. List the pods the scheduler sees on the node (it already exposes its own just-bound pods to
   the next snapshot). Use the **`with-exclusive-resources`** subset to match the exporter's
   method — that subset is exactly our Guaranteed whole-GPU pods.
2. Compute their fingerprint and compare to the NRT object's `nodeTopologyPodsFingerprint`.
3. **Match** → NRT accounts for exactly that pod set → it is current → trust `Available`.
   **Mismatch** → a pod the scheduler knows (e.g. a just-bound one) is not yet reflected → do
   not trust `Available` for this node.

This is the mechanism upstream's production NRT cache uses (`OverReserve.Resync`).

### Serving a dirty node: never skip — reconstruct

A first instinct is to **skip** a dirty node (fail the NUMA predicate on it until it goes
clean). Do **not**: it breaks multi-cycle reclaim. A reclaim/preempt decision spans cycles —
victims drain over their `terminationGracePeriod` while the pending pod is *pipelined* onto the
node. If the node disappears from NUMA consideration mid-drain, the solver re-plans onto a
different node and **evicts a second set of victims** while the first set is already dying.
Staleness would thus actively *multiply* evictions. The node must stay a candidate.

So a dirty node is served a **reconstructed** view rather than being dropped:

- **Clean node (fingerprint match):** use NRT `Available` directly — ground truth, already
  reflecting *every* pod on the node (ours or not). No prediction.
- **Dirty node (mismatch):** reconstruct per-zone availability from the snapshot,
  `available[zone] = capacity[zone] − Σ predicted_occupancy[zone]` over **all** NUMA pods on the
  node (`capacity` = static per-zone NRT `Allocatable`; each pod's zone taken from its persisted
  *scheduler-predicted placement record* where available — stable across cycles and restarts —
  else re-derived via the evaluator). Used only while dirty; the next match reverts to NRT.

The fingerprint gate is what makes reconstruction safe: it is **transient**, so it cannot drift
permanently the way an *ungated* reconstruction would (one that never defers to ground truth and
strands capacity under fragmentation).

**No foreign-pod special case.** Upstream rejects nodes carrying pods it did not schedule,
because its cache is built only from its own `Reserve` calls and has no record of a foreign pod
to subtract. KAI's snapshot already contains *every* pod on the node (it must, for whole-node
`IdleVector`), and no pod's zone is ever *observed* anyway — ours included, all zones are
predictions. So reconstruction treats foreign and self-scheduled pods identically; there is
nothing special about a pod we did not place.

**Eviction credits the freed zone.** Because reconstruction assigns every live NUMA pod a
predicted zone, the in-cycle `DeallocateFunc` credits a victim's zone back when a reclaim
scenario (speculatively) evicts it — so NUMA-pod reclaim scenarios succeed without re-planning.
The prediction need only be **internally consistent**, not match the kubelet: a wrong victim
zone just means the pending pod is pipelined onto a zone label differing from where the kubelet
actually frees a GPU — but a GPU *did* free, so the kubelet still admits it. Mispredicted zones
cost internal precision, never correctness. The
[per-node placement agent](../numa-placement-agent/README.md) (v1 — see *Observed placement*)
removes the prediction entirely by reporting each pod's *observed* zone, making reclaim simulation
exact.

### Caveats and the no-fingerprint fallback

- **Requires a fingerprint-emitting exporter** (RTE publishes it; plain NFD topology-updater may
  not). No attribute → no clean/dirty signal → fall back to the operational mitigation, or the
  gap-bounded correction below.
- **v1 fingerprint is `namespace+name`, not UID** — aliases only if *naked* pods are recreated
  with the same name under churn; a non-issue with controllers (unique generated names).
- **Transient over-report during a dirty window.** Reconstruction predicts zones, so it can
  briefly over-report a zone and earn a kubelet rejection — bounded to the window and caught by
  the kubelet backstop. This is the accepted cost of keeping the node usable (vs. skip) for
  reclaim stability.
- **No fingerprint? Gap-bounded fallback.** Without the clean/dirty gate, anchor on NRT
  `Available` and subtract only up to the measured lag `gap = Σ_zone Available −
  KAI_exact_node_free` (KAI's whole-node free never lags); it auto-decays as NRT catches up.
  The drift warning applies specifically to *ungated* reconstruction — with no fingerprint to
  snap back to NRT, predicting all pods' zones every cycle never defers to ground truth.

## Operator integration (intent)

The KAI operator should make the placement agent zero-touch: **detect whether the `numa` plugin
is enabled and, if so, deploy the placement agent automatically** (unless an operator has
explicitly disabled it). The plugin works without the agent (predicted placement), so this is a
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
