<!--
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# Deferred Resize Preemption

_Status: Implemented (opt-in, scheduler flag `deferred-resize-preemption`, off by default)_

Related issue: [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872).
Parent design: [In-Place Pod Resize Accounting and Queue Admission](README.md).

## Problem

When a node has no room for an in-place upsize, the kubelet sets
`PodResizePending` with reason `Deferred` and retries later. KAI already
charges the pod at `max(spec, allocated, actual)`, so the node's idle CPU or
memory goes negative by the shortfall and nothing else is placed there. But
nothing frees room either, so the pod can stay at its old size forever.

## Mechanism

This follows kube-scheduler's
[KEP-5836](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/5836-scheduler-preemption-for-ippr).
The resizing pod is never re-placed; only victims are.

1. Jobs with a deferred upsize enter the preempt and reclaim job queues even
   without pending pods. When a job is popped, its pending pods are tried
   first, as before. Then, if its gang is secured, each deferred pod is tried
   in name order.
2. If the pod's node has `idle + releasing >= 0` for CPU and memory, nothing is
   done: the resize fits, or victims of an earlier round are still
   terminating.
3. Otherwise the action solves a copy of the job that holds its allocated
   pods and, instead of the resizing pod, a pending **stand-in**
   (`pod_info.NewResizeStandIn`). The stand-in requests nothing,
   because the node already charges the pod at its target size.
   Pending pods are left out of the copy.
4. The regular `JobSolver` runs with the action's validator and a victim
   queue limited to jobs with a pod on that node. "Allocating" the stand-in
   (`AllocateJob`) is only a fit check: `node.IsInPlaceResizeAllocatable`,
   which passes when the node is no longer over-committed for CPU and memory.
   Nothing is bound or pipelined for it. Victims may be placed back or moved
   to any node, as in the regular path.
5. On success the solver's statement holds only victim evictions and moves,
   and it is committed as is.

The stand-in requests zero, so the increase is never counted twice: the
sub-scenario emitter, reclaim's fair-share checks and the validators all see
no extra demand. The solver's scenario filters (node affinity, idle GPUs,
topology idle GPUs) are skipped for a stand-in, since the fit check above is
the only test that applies.

## Design notes

- Pending pods first, then resizes, in the same pop, so the job-queue order
  is kept. Resize-only jobs skip the "easier to schedule" signature cache,
  which is keyed on pending tasks.
- A job whose gang is not secured gets no evictions for its resizes.
- Resource fit only, as in KEP-5836: node health, affinity, taints, ports,
  volumes and CSI capacity, ConfigMaps, topology, NUMA zones and resource
  claims are not checked for the resizing pod. A resize does not change its
  claims.

## Known limitations

- Other deferred pods on the same node stay charged at their target size, so
  evictions happen only when every deferred resize on the node can be met.
- NUMA zones are ignored: only node-level room is freed, so the kubelet may
  still defer the resize if the room is in another zone (matches upstream).
- A pod bound to the node by another party can race on the freed room before
  the kubelet retries the resize.
