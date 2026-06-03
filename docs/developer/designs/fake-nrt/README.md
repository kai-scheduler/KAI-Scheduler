# Fake NRT Simulation Mechanism

## Summary

This document captures the **requirements** for a test-infrastructure component that fakes
NUMA topology in clusters without real NUMA hardware — the NUMA-topology analog of the
[fake-gpu-operator][fgo] (FGO), which simulates NVIDIA GPUs. The mechanism fakes per-node NUMA
zones and publishes [`NodeResourceTopology`][nrt-api] (NRT) objects, simulates the kubelet
Topology Manager's per-pod NUMA allocation (and rejection) for pods that "run" on a fake node,
reflects that consumption in NRT `Available` after a configurable delay, and exposes each pod's
placement for the [placement agent](../numa-placement-agent/README.md) to discover.

It exists so the [NUMA scheduler plugin](../numa-topology/README.md) and the placement agent can
be exercised end-to-end in fake and e2e environments. This is a requirements doc, not a full
design — it enumerates what the mechanism must do and how it relates to FGO, leaving the
implementation (new component vs. FGO extension) open.

## Motivation

The NUMA plugin predicts the kubelet Topology Manager's admission verdict and tracks per-zone
consumption against NRT data. Validating it realistically requires a cluster that (a) has NUMA
topology, (b) publishes NRT, (c) actually rejects pods the way the kubelet would, and (d) lags
NRT updates the way a real exporter does. Real NUMA hardware (multi-socket GPU nodes with the
right kubelet Topology/CPU/Memory Manager policies) is scarce, expensive, and impractical for CI.

The split-brained nature of the feature — the kubelet decides placement, the scheduler predicts
it — means the most important behaviors to test are precisely the ones that need a faithful
kubelet *simulator*:

- The plugin's **prediction accuracy** and its handling of kubelet rejections
  (`TopologyAffinityError`) — testable only if something rejects pods the way the kubelet does.
- The **fingerprint / cross-cycle staleness** path ([Appendix A](../numa-topology/README.md#appendix-a-optional-cross-cycle-staleness-compensation))
  — testable only if NRT `Available` lags binding by a controllable, jittered delay and the pod
  fingerprint attribute is emitted/refreshed.
- The **placement agent** ([Appendix B](../numa-placement-agent/README.md)) and the
  observed-placement path — testable only if there is a podresources-API-equivalent the agent can
  consume to discover per-pod NUMA placement.

FGO already proves this model for GPUs (simulate hardware, watch pods, update a per-node state
object, expose it to consumers). This mechanism is the NUMA-topology counterpart and should reuse
FGO's patterns and, where practical, its components.

## Requirements

### R1 — Fake a configurable NUMA topology per node

Publish, for each fake node, a `NodeResourceTopology` object describing configurable NUMA zones
and per-zone resources (`cpu`, `memory`, `nvidia.com/gpu`, NIC/RDMA resources), with the Topology
Manager **policy** (`single-numa-node` / `restricted` / `best-effort` / `none`) and **scope**
(`container` / `pod`) attributes set per node.

*Enables:* policy/scope parsing, NUMA-relevant-resource inference, and the `single-numa-node` /
`restricted` filter tests against realistic per-zone `Allocatable`/`Available` — across
heterogeneous nodes and policies — without real hardware.

### R2 — Faithfully simulate the kubelet's per-pod NUMA allocation and rejection

When a pod is scheduled/"runs" on a fake node, compute which NUMA zone(s) its exclusive
(Guaranteed, whole-GPU) resources land on the way the real kubelet Topology Manager would for that
node's policy — including **rejecting** what the kubelet would reject (`single-numa-node` /
`restricted` admission failures surface as a `TopologyAffinityError`-equivalent and the pod does
not run) — and decrement the chosen zones' `Available` accordingly.

*Enables:* end-to-end exercise of the plugin's verdict prediction and its
`TopologyAffinityError` / reschedule handling, and measurement of prediction accuracy versus a
kubelet-faithful oracle (so a divergent `pickZone` is caught).

### R3 — Configurable, jittered NRT-update delay

Reflect a pod's consumption in NRT `Available` only after a configurable delay with jitter, to
simulate the real lag between a pod binding and the exporter republishing NRT (event-driven plus
periodic, default ~60s). Be able to emit and refresh the pod-fingerprint attributes
(`nodeTopologyPodsFingerprint` / `nodeTopologyPodsFingerprintMethod`) as the accounted pod set
changes.

*Enables:* testing cross-cycle staleness handling, the clean/dirty fingerprint gate, and the
Appendix A reconstruction path — including the hot-loop the staleness window can produce and its
self-healing once NRT catches up.

### R4 — Expose per-pod placement for the placement agent

Provide a way for the proposed placement-agent DaemonSet (Appendix B) to "discover" each pod's
actual NUMA placement — i.e. fake the kubelet **podresources API** (device IDs with NUMA
`Topology`, `cpu_ids`, memory blocks), or an equivalent the agent can consume — consistent with
the placement computed in R2.

*Enables:* end-to-end testing of the placement agent and the observed-placement path
(observed > predicted > re-derive precedence), including reclaim-simulation accuracy, without a
real kubelet.

[fgo]: https://github.com/run-ai/fake-gpu-operator
[nrt-api]: https://github.com/k8stopologyawareschedwg/noderesourcetopology-api
