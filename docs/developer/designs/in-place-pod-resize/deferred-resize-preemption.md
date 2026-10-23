<!--
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
-->

# Deferred Resize Preemption

*Status: Implemented (opt-in, scheduler flag `deferred-resize-preemption`, off by default)*

Related issue: [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872).
Parent design: [In-Place Pod Resize Accounting and Queue Admission](README.md).

## Problem

When a node has no room for an in-place upsize, the kubelet sets
`PodResizePending` with reason `Deferred` and retries later. KAI already
charges the deferred target (`max(spec, allocated, actual)`), so the node
shows the shortfall and freed room is not backfilled. But nothing frees room,
so an important workload can stay at its old size forever.

## Upstream alignment (KEP-5836)

[KEP-5836](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/5836-scheduler-preemption-for-ippr)
(kube-scheduler, alpha in v1.37) solves the same gap:

| KEP-5836 | KAI |
| --- | --- |
| Deferred pod enters the normal scheduling queue | Deferred job enters the normal preempt/reclaim job queues |
| Restricted cycle: own node, resource fit only | Solver pinned to the pod's node; full predicates for the pod at its desired size |
| Standard preemption picks victims | Standard preempt/reclaim victim selection, validators and budgets |
| Reservation via `max(spec, allocated, actual)` | Same effective-request accounting (parent design) |
| Kubelet retries once victims are gone | Same; KAI never touches the resizing pod |
| Node opt-out: `spec.podPreemptionPolicy.disableResizePreemption` | Not supported yet (see [Future work](#future-work)) |

## Mechanism: virtual self-eviction

Jobs with a deferred upsize enter the action's job queue even without
pending pods (`NonPendingJobFilter`, set only when the flag is on). When any
job is popped, after its pending pods (if any) were tried:

0. **Scan the job.** Nothing is cached from the start of the action. If the
   job's gang is not secured (its allocated pods, i.e. running, bound or
   pipelined, do not meet `minAvailable` at every level), nothing is
   attempted: a job that is not working yet must not evict others so one of
   its pods can grow. Otherwise its pods are scanned once, now: pods that are
   still `Running` with a deferred upsize, on a known node, sorted by name.
   Pods evicted or moved earlier in the session (for example by
   consolidation) are no longer `Running` and drop out. The job's own
   attempts never evict its own pods and discard their statements, so the
   scanned pods stay current while the job's attempts run.

Then, one deferred pod at a time:

1. **Skip if the node already has room.** If `idle + releasing ≥ 0` for cpu and
   memory, the kubelet will enact the resize on its own (for example, victims
   of an earlier round are still terminating). This prevents repeat evictions.
2. **Virtually evict the resizing pod** inside a statement. It becomes
   `Releasing` with a virtual status, so `ShouldAllocate` treats it as
   pending at its charged (desired) size, and the queue is credited back.
3. **Run the action's eligibility checks** on the job in that state:
   `IsNonPreemptibleJobOverQueueQuota` for the resizing pod (preempt) or `CanReclaimResources` on a copy of the
   job holding only its allocated pods and the resizing pod (reclaim), so the
   demand of its pending pods is not counted. Reclaim's victim filter gets
   the same copy, since it judges candidate queues by the reclaimer's demand.
4. **Run the action's `JobSolver`** for exactly that pod
   (`SolveTasksWithResult`). It is pinned to the pod's node
   (`PinFeasibleNodes`) and uses victims from that node only. The pod fits
   back iff `used − releasing − victims ≤ allocatable`, which is exactly the
   resize shortfall. Pipelining it back consumes the releasing room, so the
   solver's reprieve step cannot give that room back to a victim. Victims may
   still be placed back or moved to any node, as in the regular path.
5. **On success**, undo only the resizing pod's placement in the solver
   statement (`Statement.Unpipeline`) and commit it, so victims are evicted,
   or moved where the solver placed them back, as in the regular path. The
   outer statement is then discarded, so the resizing pod is never evicted.

One pod per attempt is enough: other deferred pods on the node are already
charged at their desired size, so the pod fits back only once the whole node
shortfall is covered, and the next pod on that node then skips at step 1. It
also avoids the solver's elastic task selection, which adds pods above a
gang's `minAvailable` one at a time.

Framework changes that support this:

- `Statement.Pipeline` used to treat a task already on the target node as one
  it had evicted itself, and undo that eviction. It now does so only when the
  eviction is in the same statement. The resizing pod is evicted in an outer
  statement, because the solver opens and discards its own statements per
  scenario and would otherwise undo it. So the solver's statement pipelines it
  on its node normally; this case previously always failed.

## Ordering

Both preempt and reclaim run the deferred attempt after the regular attempt
for the same popped job, each with its own statement and commit, so the two
are never solved together. Pending pods get the first pick of victims; the
resize is then tried whether or not they were placed, as long as the gang is
secured. The resizing pod is already charged at its desired size, so pending
pods can only take room beyond what the resize needs. Resize-only jobs skip
the "easier to schedule" signature cache, which is keyed on pending tasks.

## Design choices

- Node-local. An in-place resize cannot move a pod, so victims are only
  searched on its node.
- Full predicates, not resource fit only (unlike KEP-5836). This falls out of
  reusing the preempt and reclaim solver; a resource-only check would need a
  separate allocation mode in shared code. It also avoids pointless evictions:
  on a cordoned or not-ready node nothing is evicted. The cost is that a pod
  whose node changed under it (node affinity labels removed, a `NoSchedule`
  taint added after it started, a referenced ConfigMap deleted) gets no
  evictions, where upstream would preempt (see [Future work](#future-work)).
- A job whose gang is not secured gets no evictions for its resizes.

## Known limitations

- Other deferred pods on the same node stay charged at their desired size
  during the search, so evictions happen only when every deferred resize on
  the node can be met; there is no partial relief.
- GPUs are never part of a resize delta.
- Freed room is reserved by charging the resizing pod at its desired size.
  KAI and kube-scheduler (which uses the same effective-request accounting)
  do not place other pods into it. A pod bound to the node directly (static
  pod, or `spec.nodeName` set by a controller) can still take it before the
  kubelet retries; the shortfall then reappears and KAI evicts other KAI-
  scheduled pods for the same resize. KAI never evicts pods it does not
  schedule.

## Future work

- Resource fit only, as in KEP-5836: skip start-time checks (ConfigMap,
  volumes, node affinity, taints) for the resizing pod on its own node, so a
  pod whose node or references changed after it started can still get room.
  Open question: whether a cordoned node should still block evictions (the
  kubelet still resizes there).
- Node opt-out: honor `spec.podPreemptionPolicy.disableResizePreemption` once
  KAI's `k8s.io/api` includes it (alpha in Kubernetes 1.37).
