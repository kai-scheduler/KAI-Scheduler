# Deferred In-Place Resize Preemption

## Problem

Kubernetes In-Place Pod Vertical Scaling (KEP-1287, GA in 1.35) lets a running pod's
CPU/memory requests be changed via the `pods/resize` subresource without recreating the
pod. When the pod's node does **not** have enough free capacity to actuate an increase,
the kubelet does not evict anything — it records a `PodResizePending` condition with
reason `Deferred` and leaves the pod at its current size until capacity appears.

This is a problem for the pattern the feature in [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872)
targets: an external right-sizer (e.g. a VPA-style controller) shrinks less-important
workloads to pack a node densely, but relies on being able to scale an **important**
workload back **up** when it needs more. On a full node that scale-up silently sits
`Deferred`, and the important workload is starved or throttled. Vanilla Kubernetes never
preempts to satisfy a resize.

## Goal

Make a `Deferred` in-place resize a scheduling event KAI acts on: free room on the
resizing pod's node so the kubelet can actuate the resize on its next attempt — **treating
the growth like any other pending pod in the resizing pod's queue**, so KAI's existing
preemption and reclaim rules apply to it unchanged, with no resize-specific gate. Enabled by
default; a no-op (no disruption) when the queue is not entitled to the growth or no eligible
victim exists — exactly as a plain pending pod of that size would be.

## Non-goals

- KAI never calls the `pods/resize` subresource. It only **frees capacity**; the kubelet
  actuates the resize on its own retry loop once the room exists.
- `Infeasible` resizes (a resize the node could not satisfy even when empty) are ignored —
  freeing capacity cannot help them.
- GPU-count in-place resize is not a supported Kubernetes operation and is out of scope; the
  implementation is nonetheless resource-agnostic (it acts on whatever CPU/memory/scalar
  delta the resize requests).

## Approach

Rather than adding a bespoke eviction path, a deferred resize is turned into a first-class
**pending demand** that the *existing* `allocate` → `preempt` → `reclaim` machinery already
knows how to satisfy. The whole feature is: represent the resize's growth as a synthetic,
node-pinned pending pod for the delta, and let the normal actions free room for it. This
means quota, fair share, victim selection, and scenario validation are all reused **for
free** — the resize competes for its growth exactly like a new workload of that size on that
node. There is no new action, action-priority, config, CRD, or Helm/values change.

Three pieces make it work, wired together when the scheduler snapshot is built.

### 1. Detection (`pkg/scheduler/api/pod_info/resize.go`)

- **`IsResizeDeferred`** — true iff the pod has a `PodResizePending` condition with reason
  `Deferred`.
- **`resizeDeferredDeltaList` / `ResizeDeferredDelta`** — the per-resource positive difference
  between the *desired* requests (`spec.containers[].resources.requests`) and the *actual*
  granted resources (`status.containerStatuses[].resources`). The diff is taken over raw
  `ResourceList`s so that an unchanged resource — notably an explicit `nvidia.com/gpu: 0` —
  contributes a zero delta and is dropped rather than being misread as a fractional device;
  the implicit `pods` resource is ignored.

### 2. Charge the resizing pod at its *actual* size

KAI normally charges a pod its `spec` requests. For a deferred resize that would be the
*desired* size, which would make the node look over-allocated by the delta and would
double-count once the delta is also represented as a pending demand. Instead,
`regularContainerRequests` charges a deferred-resize pod at **`desired − delta`** — i.e. the
resources the kubelet has actually granted. This keeps the node's view truthful (it reflects
the pod's real current footprint) and preserves the invariant:

```
charge (actual)  +  reservation (delta)  =  desired
```

so the growth is represented exactly once. The subtraction reuses the same delta
`ResizeDeferredDelta` computes, so charge and reservation can never disagree.

### 3. Inject a node-pinned reservation (`cluster_info.InjectResizeReservations`)

While building the snapshot (`ClusterInfo.Snapshot()`, after PodGroups are built), for every
running pod with a deferred resize KAI adds a synthetic `PodGroupInfo` — the *reservation* —
standing in for the growth (`pod_info.NewResizeReservationTask`):

- a single **pending** task requesting the resize **delta**;
- pinned to the resizing pod's node with a required node affinity on the well-known
  `kubernetes.io/hostname` label — the growth must be actuated where the pod already runs;
- flagged **`IsResizeReservation`** (see the no-bind guard below);
- in the resizing pod's **queue, priority, and preemptibility**, so it is subject to the same
  fairness the pod itself is;
- given a minimal non-nil `PodGroup` so every action that assumes `job.PodGroup != nil` is
  safe;
- charged the delta only, with the implicit `pods: 1` dropped — it is not a real pod and must
  not consume a pod-count slot.

The normal actions then run. `allocate` tries to place the reservation on its node; if the
node is full, `preempt` (same queue, lower priority) and `reclaim` (cross queue) free room —
under the **same preemption and reclaim rules KAI applies to any pending pod**, because the
reservation flows through the identical eligibility, quota, and scenario-validation code,
unchanged. With an eligible lower-priority victim and the queue entitled, a victim is evicted
and the reservation is *pipelined* onto the node; otherwise nothing is evicted and the resize
stays `Deferred` — exactly as that pending pod would stay `Pending`.

### No-bind guard (`Session.BindPod`)

The reservation has no real pod behind it — it exists only to reserve room and drive eviction.
`Session.BindPod` returns early for an `IsResizeReservation` task, so it is never bound and no
`BindRequest` is ever emitted; it only ever reaches the `Pipelined` state, holding the freed
capacity in KAI's model. The kubelet actuates the real resize on its own retry loop once the
room exists, and on the next snapshot — with the resize actuated — the delta is zero and no
reservation is produced.

### Claiming the freed space

Because the resizing pod is charged at its actual size and the reservation carries the pod's
own priority, the reservation takes the freed capacity **ahead of any lower-priority pod** —
`allocate`/`preempt` process higher-priority demands first, so lower-priority work cannot take
the room the resize freed, and the reservation never displaces higher-priority or
non-preemptible pods.

Against **equal-priority** same-queue demand the reservation gets **no special precedence**: it
inherits the resizing pod's own creation timestamp (see `InjectResizeReservations`), so it
competes by the pod's seniority under KAI's normal equal-priority tiebreak (creation timestamp,
then UID) — exactly as the pod itself would. It may win or lose that ordering; if it loses, the
resize simply retries on the next snapshot. This is a deliberate choice for strict parity with an
ordinary pending pod; the alternative — always preferring an in-flight resize by leaving it a
zero timestamp so it sorts first — was considered and declined.

Note this is only the *equal-priority* tiebreak. A non-preemptible resize still cannot grow its
queue's non-preemptible usage beyond the queue's deserved share (KAI's standard cap on
unreclaimable usage), and it never displaces higher-priority or non-preemptible neighbours —
again, exactly as any pending pod of that priority and preemptibility.

## Limitations

- **No special treatment — it behaves exactly like a pending pod.** The reservation carries the
  resizing pod's queue/priority/preemptibility and flows through the *unchanged* actions, so it
  competes under KAI's normal rules: it can preempt only strictly-lower-priority work in its own
  queue and reclaim from other queues only within fair share (a non-preemptible resize's
  same-queue preemption is additionally bounded by the queue's entitlement, like any
  non-preemptible pod). If the queue is not entitled, or only higher-priority/non-preemptible
  neighbours exist, the resize stays `Deferred` — precisely as such a pending pod would stay
  `Pending`. This is standard KAI fairness, not a resize-specific limit (maintainer guidance on
  [#1872](https://github.com/kai-scheduler/KAI-Scheduler/issues/1872)).
- **Higher-priority / non-preemptible neighbours block the resize.** The reservation carries
  the resizing pod's priority, so it can only preempt strictly-lower-priority same-queue work.
  If the node holds only higher-priority or non-preemptible neighbours, the resize stays
  `Deferred`.
- **Trusts the kubelet's `Deferred` signal**, and acts only on the delta the kubelet reports
  in `status` vs `spec`.

## Testing

- **Unit — detection & accounting** (`pkg/scheduler/api/pod_info/resize_test.go`): deferred vs
  infeasible vs absent; cpu-only, memory-only, multi-container, zero deltas; `nvidia.com/gpu: 0`
  produces no spurious GPU delta; a deferred pod is charged at its actual size and
  `charge + delta = desired`; `NewResizeReservationTask` builds a pending, node-pinned,
  `IsResizeReservation` task requesting the delta (and returns nil when unassigned or not
  growing).
- **Unit — no-bind guard** (`pkg/scheduler/framework/bind_resize_test.go`): `BindPod` on a
  reservation emits no `BindRequest`.
- **Unit — injection** (`pkg/scheduler/cache/cluster_info/resize_injection_test.go`):
  `InjectResizeReservations` adds exactly one reservation (right queue/priority, flagged) for a
  deferred-resize job and none for a plain job.
- **Integration — Snapshot path** (`pkg/scheduler/cache/cluster_info/resize_snapshot_test.go`):
  the real `Snapshot()` both charges the resizing pod at its actual size *and* injects the
  reservation — chaining the accounting and injection through the production code path.
- **Composition — preempt** (`pkg/scheduler/actions/preempt/composition_resize_test.go`): with
  the reservation injected, the normal `preempt` action evicts a lower-priority same-queue
  victim and pipelines the reservation (never binding it) **when the queue is entitled to the
  growth**, and evicts nothing **when it is not**.
- **Composition — reclaim** (`pkg/scheduler/actions/preempt/composition_resize_reclaim_test.go`):
  with the reservation injected *before* the session builds (via
  `test_utils.BuildSessionWithSnapshotMutator`, reproducing the production `Snapshot()` ordering so
  the proportion plugin counts its pending demand in the queue's fair share), the normal `reclaim`
  action evicts a **cross-queue** victim from an over-fair-share queue when the resizing pod's queue
  is within its fair share (`CanReclaimResources` true), and evicts nothing when it is over — the
  counterfactual (`CanReclaimResources` false) that proves the eviction is genuinely fair-share
  gated, not incidental.
- **e2e (CI / Kind only)** (`test/e2e/suites/...`): fills a node, triggers a resize the kubelet
  defers, and asserts KAI frees room so the resize actuates when the queue is entitled to the
  growth and not otherwise. Guarded on `InPlacePodVerticalScaling` / k8s 1.35; it cannot run in
  the unit environment.
