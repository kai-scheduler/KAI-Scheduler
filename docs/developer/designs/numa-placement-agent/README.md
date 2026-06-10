# Per-Node NUMA Placement Agent

## Summary

An optional per-node DaemonSet that reads the kubelet's **podresources API**, derives the
actual NUMA-node placement of each pod's exclusive resources (GPUs, CPUs, NICs, memory), and
publishes that attribution back onto the pod (as an annotation). When the agent is deployed,
the [NUMA scheduler plugin](../numa-topology/README.md) consumes this *observed* placement
instead of *predicting* it — making its per-zone accounting, and in particular its
reclaim/preemption simulation, accurate.

This component is **not part of the NUMA plugin v1**. The plugin works without it (using
predicted placement); the agent is an accuracy upgrade that can be enabled later.

## Motivation

The `NodeResourceTopology` (NRT) CRD the scheduler consumes is **aggregate per zone** — it
reports "zone 0 has 2 free GPUs," never "pod V holds a GPU in zone 0." But the kubelet's
Topology Manager makes the real per-pod NUMA assignment at admission, and the scheduler never
observes it. The NUMA plugin therefore *predicts* each pod's zone.

Prediction is adequate for filtering (the kubelet backstops admission), but it is a genuine
problem for **reclaim simulation**: to free an aligned slot for a pending pod, the scheduler
must know which zone evicting a victim opens up. If it mispredicts the victim's zone, it can
evict a victim that does *not* create a usable aligned slot — a wasted eviction plus a
`TopologyAffinityError` bounce. (This bites specifically when the pending pod needs multiple
per-zone-scarce resources co-located; GPU-bound pods with abundant per-zone CPU are largely
immune — see the plugin doc's *Reclaim-simulation accuracy* note.)

The information the scheduler is missing exists on the node: the kubelet podresources API
reports, per container, the exact device IDs (with their NUMA `Topology`) and CPU IDs that were
allocated. Surfacing it removes the prediction entirely.

## Goals

- Publish, per pod, the actual NUMA-node assignment of its topology-aligned resources.
- Let the NUMA scheduler plugin consume observed placement when available, and fall back to
  prediction when not — so the agent is purely additive.
- Keep the scheduler's consumption cheap (no new informer, no new CRD if avoidable).

## Non-Goals

- Replacing the NRT exporter. NRT (aggregate per-zone availability) is still required; this
  agent is complementary (per-pod attribution).
- Influencing the kubelet's placement. The agent is read-only with respect to allocation; it
  observes and reports, it does not hint or pin.
- Supporting pods the kubelet does not NUMA-align (non-Guaranteed). Those have no exclusive
  placement to report.

## Design Details

### Architecture

A DaemonSet on each NUMA/GPU node. Each instance:

1. Connects to the local kubelet podresources gRPC socket
   (`/var/lib/kubelet/pod-resources/kubelet.sock`, hostPath-mounted, read-only).
2. Calls `List` (and watches, where supported) to get per-pod, per-container resource
   allocations: device IDs with `Topology.Nodes` (NUMA affinity), `cpu_ids`, and memory blocks
   with their NUMA node.
3. Maps each allocation to a NUMA node:
   - **Devices** (GPU/NIC): the NUMA node is in the podresources `Topology` field directly.
   - **CPUs**: map `cpu_ids` → NUMA node using the node's CPU topology (from `/sys` or the NRT
     zones already on the node).
   - **Memory**: the podresources memory blocks carry their NUMA node.
4. Writes the result onto the pod as an annotation (only for pods holding aligned resources,
   only when the value changes).

### Published format

A single annotation on the pod, resource → {NUMA node → quantity}:

```
kai.scheduler/numa-placement-observed: |
  [{"zone":"node-0","amount":{"nvidia.com/gpu":"2","cpu":"8","memory":"17179869184"}}]
```

This represents multi-zone placement too (a `restricted`/multi-NUMA pod would list more than
one node), so it is not specific to `single-numa-node`.

### Scheduler consumption

The NUMA plugin, when building its per-zone model:

- **If a pod carries `kai.scheduler/numa-placement`** → use the observed per-zone quantities
  directly. Occupancy is now *exact*, not predicted: per-zone availability can be reconstructed
  as `capacity[zone] − Σ observed_placement[zone]`, and a victim's eviction credits the *real*
  zone. Reclaim simulation becomes accurate.
- **If a pod lacks the annotation** (agent absent, pod not yet observed, or non-aligned) → fall
  back to the prediction path described in the plugin design.

No new informer or CRD is needed — the scheduler already watches pods, so the annotation rides
the existing pod cache.

### Lifecycle and freshness

Placement is **stable**: the kubelet pins a pod's exclusive resources for the pod's lifetime,
so once written the annotation does not change until the pod ends. The agent therefore writes
once per pod (shortly after the pod starts running) and on the rare re-allocation. There is a
small initial lag between a pod starting and the annotation appearing — during that window the
plugin falls back to prediction for that pod, exactly as if the agent were absent.

### RBAC and security

- **Agent → kubelet podresources:** read-only access to the podresources socket via a hostPath
  mount. This is the same surface NRT exporters use.
- **Agent → API server:** `patch` on pods (annotations only). Scoped to the annotation key.
- **Scheduler:** no new permissions — it already lists/watches pods.

## Interaction with the NUMA plugin and NRT

- **NRT** remains the source for aggregate per-zone *availability* and Topology Manager
  policy/scope.
- **This agent** supplies per-pod *attribution*, which the plugin layers on top to turn
  predicted occupancy into observed occupancy.
- With the agent present, the plugin's prediction-based reconstruction (and its drift/accuracy
  caveats) is bypassed for annotated pods; the fingerprint freshness signal is still useful for
  the aggregate `Available` numbers but is no longer load-bearing for reclaim accuracy.

## Limitations and Caveats

- **Initial-observation lag:** a just-started pod is unannotated until the agent observes it;
  the plugin falls back to prediction meanwhile.
- **Agent must be deployed on every relevant node**, or coverage is partial (mixed
  observed/predicted placement — still correct, just less accurate on un-covered nodes).
- **Annotation write load:** bounded by writing only for aligned pods and only on change;
  placement stability keeps this to roughly one write per pod lifetime.

## Alternatives Considered

- **Per-node CRD instead of pod annotations.** More expressive but adds a new informer and CRD;
  pod annotations are lighter for scheduler consumption and reuse the existing pod cache.
- **Extend the resource-topology-exporter (RTE).** RTE already reads podresources to build NRT;
  per-pod attribution could be contributed upstream rather than shipped as a separate agent.
  Worth pursuing as an upstream contribution, but a self-contained agent decouples KAI from that
  timeline.

## Future Work

- Upstream the per-pod attribution into a standard surface (NRT extension or RTE) so it is not
  KAI-specific.
- Use observed placement for NUMA *scoring*, not just feasibility/reclaim.
