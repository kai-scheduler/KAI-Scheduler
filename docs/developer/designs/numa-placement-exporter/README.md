# Per-Node NUMA Placement Exporter (NPE)

## Summary

The **NUMA Placement Exporter (NPE)** is an optional per-node DaemonSet that reads the kubelet's **podresources API**, derives the
actual NUMA-node placement of each pod's exclusive resources (GPUs, CPUs, NICs, memory), and
publishes that attribution back onto the pod (as an annotation). When the exporter is deployed,
the [NUMA scheduler plugin](../numa-topology/README.md) consumes this *observed* placement
instead of *predicting* it — making its per-zone accounting, and in particular its
reclaim/preemption simulation, accurate.

This exporter is **part of the NUMA plugin v1**, and the scheduler is built to consume its input from
day one — but **deploying it is optional**: the plugin works without it on a predicted-placement
fallback, so the exporter is an enabler of reclaim/preempt action rather than a hard dependency (the KAI operator
deploys it automatically when the `numa` plugin is enabled).

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
  prediction when not — so the exporter is purely additive.
- Keep the scheduler's consumption cheap (no new informer, no new CRD if avoidable).

## Non-Goals

- Replacing the NRT exporter. NRT (aggregate per-zone availability) is still required; this
  exporter is complementary (per-pod attribution).
- Influencing the kubelet's placement. The exporter is read-only with respect to allocation; it
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

### Lifecycle and freshness

Placement is **stable**: the kubelet pins a pod's exclusive resources for the pod's lifetime,
so once written the annotation does not change until the pod ends. The exporter therefore writes
once per pod (shortly after the pod starts running) and on the rare re-allocation. There is a
small initial lag between a pod starting and the annotation appearing — during that window the
plugin falls back to prediction for that pod, exactly as if the exporter were absent.

### Drift reconciliation against the API server

The fast path is driven by the podresources `List` and an in-memory cache of the last value
written per pod, so a pod is patched only when its computed placement changes. That cache
assumes the exporter's last successful patch still reflects what is actually on the pod object —
an assumption that breaks if the annotation is removed or mutated externally.

To catch the out-of-band case, a second, slower pass reconciles against the API server. On its
own interval it lists the pods assigned to this node (`spec.nodeName` field selector), compares
each pod's **live** annotation value to the value the exporter computes from podresources, and
patches the ones that have **drifted** (missing, stale, or externally modified).

The interval is configurable via `--drift-resync-interval`, **defaulting to `60s`**, and is
**disabled by `0`** (relying solely on the in-memory cache). The pass is read-mostly: it lists
pods (one paginated `List` per interval, scoped to the node) and patches only the drifted
subset, so steady-state cost is one list per interval and zero writes once placement is stable.

### RBAC and security

- **Exporter → kubelet podresources:** read-only access to the podresources socket via a hostPath
  mount. This is the same surface NRT exporters use.
- **Exporter → API server:** `patch` on pods (annotations only), scoped to the annotation key, plus
  `list` on pods for the drift-reconciliation pass (scoped to this node via field selector).
- **Scheduler:** no new permissions — it already lists/watches pods.

## Interaction with the NUMA plugin and NRT

- **NRT** remains the source for aggregate per-zone *availability* and Topology Manager
  policy/scope.
- **This exporter** supplies per-pod *attribution*, which the plugin layers on top to turn
  predicted occupancy into observed occupancy.
- With the exporter present, the plugin's prediction-based reconstruction (and its drift/accuracy
  caveats) is bypassed for annotated pods; the fingerprint freshness signal is still useful for
  the aggregate `Available` numbers but is no longer load-bearing for reclaim accuracy.

## Limitations and Caveats

- **Initial-observation lag:** a just-started pod is unannotated until the exporter observes it;
  the plugin falls back to prediction meanwhile.
- **Exporter must be deployed on every relevant node**, or coverage is partial (mixed
  observed/predicted placement — still correct, just less accurate on un-covered nodes).
- **Annotation write load:** bounded by writing only for aligned pods and only on change;
  placement stability keeps this to roughly one write per pod lifetime.

## Superseded long-term by DRA

Under **Dynamic Resource Allocation** ([KEP-3063][kep3063], GA-track) the scheduler itself
allocates devices and records them in `ResourceClaim` status, and DRA drivers can expose NUMA
node as a device attribute — so the scheduler knows real placement with no scraping. This exporter
is therefore a stopgap for the legacy device-plugin + Topology Manager world (and for CPU/memory
NUMA, which DRA does not yet manage — [KEP-3695][kep3695] tracks bridging podresources/DRA). As
workloads move to DRA, the need for it fades.

The topology-aware WG is building per-container NUMA-placement feedback from the same podresources
data: the [`numaplacement`][numaplacement] encoding and resource-topology-exporter PRs 
([#390][rte390]/#396) derive each container's actual NUMA affinity and publish it — but as 
**NRT CRD node-level attributes** (alongside the fingerprint), *not* as pod annotations. Assuming this capability
is adopted by the community, it could replace our own implementation.

## Future Work

- Consume the upstream `numaplacement` NRT attribute (RTE) instead of self-published pod
  annotations, once available.
- Use observed placement for NUMA *scoring*, not just feasibility/reclaim.
- Revisit/retire as workloads migrate to DRA.

[kep119]: https://github.com/kubernetes-sigs/scheduler-plugins/tree/master/kep/119-node-resource-topology-aware-scheduling
[numaplacement]: https://github.com/k8stopologyawareschedwg/numaplacement
[rte390]: https://github.com/k8stopologyawareschedwg/resource-topology-exporter/pull/390
[kep1926]: https://github.com/kubernetes/enhancements/pull/1926
[kep3063]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation
[kep3695]: https://github.com/kubernetes/enhancements/issues/3695
