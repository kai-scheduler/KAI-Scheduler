# Queue Admission Gating

*Status: Proposed*

Related issues: [#1615](https://github.com/kai-scheduler/KAI-Scheduler/issues/1615) (request), [#1783](https://github.com/kai-scheduler/KAI-Scheduler/issues/1783) (quota-change semantics — interacts, see below)

## Motivation

Queue quota and limits are enforced only at allocation time (`capacity_policy`). Pods blocked purely by queue policy are still created, marked `Unschedulable`, and re-examined every session. Two costs follow:

1. **Autoscaler waste.** Cluster autoscalers (Karpenter, CA) treat `Unschedulable` pods as demand and provision nodes for workloads that will never be admitted. On GPU instance types this is expensive, and the nodes sit idle until scale-down. Volcano and Kueue both hold quota-blocked pods back from autoscaler visibility; KAI does not.
2. **Session action cost scales with submissions, not capacity.** Fully gated podgroups are excluded from the input of every action, so job ordering, allocation attempts, and — most significantly — preempt/reclaim scenario generation shrink to the schedulable set. Scope of this claim: **snapshot construction is unchanged** — PodInfos/PodGroupInfos are still built for all pods, gated or not, because the `ungate` action evaluates gated podgroups from the same session snapshot. Snapshot cost stays O(pods submitted), identical to today; the reduction applies to what the actions do with the snapshot, where the expensive work lives. Scheduling signatures already deduplicate repeated *scheduling attempts*, but do not remove over-quota podgroups from job ordering or scenario search, and do not hide pods from autoscalers. (A lighter snapshot representation for gated pods — quota math needs only their resource requests and gate state, not node-fit data — is a possible future optimization, out of scope here.)

The proposal: hold quota-blocked pods with a Kubernetes scheduling gate. Gated pods are invisible to autoscalers (no `Unschedulable` condition is ever written) and are already excluded from session readiness by existing machinery — both improvements reuse mechanisms Kubernetes and KAI already have.

**Core principle: the gate is a cache of the existing quota verdict, never a new enforcement mechanism.** Allocation-time enforcement is unchanged and remains authoritative. Two one-way guarantees define the feature:

1. **Safety**: the gate is removed only after an affirmative admission decision by the quota oracle.
2. **Liveness**: every gated podgroup is re-evaluated each session and ungated once admissible.

The absence of a gate implies nothing — fail-open paths exist by design (webhook outage, manual gate removal, feature disablement), and all of them degrade to exactly today's behavior, never to a quota violation.

## API

Gate on the pod:

```yaml
spec:
  schedulingGates:
    - name: kai.scheduler/queue-admission
```

Opt-in per queue via a label on the Queue CR (no CRD change; graduation to a spec field is an open question):

```yaml
metadata:
  labels:
    kai.scheduler/queue-admission-gating: "true"
```

Global feature toggle in the Config CR, default off. The operator fans one knob out to both sides — the admission flag and the scheduler flag plus the `ungate` action — so webhook and scheduler cannot disagree across upgrades:

```yaml
spec:
  global:
    queueAdmissionGating:
      enabled: false
```

## Semantics

### Gate addition — deliberately dumb

A new mutating-admission plugin in the existing pod-mutator chain appends the gate at pod CREATE (gates are create-only in Kubernetes) to pods with `schedulerName: kai-scheduler` whose queue opts in. The webhook performs **no quota computation** — queue usage is not reliably knowable at admission time (pods may be created before anything in the queue has been scheduled, and the PodGroup may not exist yet). All quota intelligence stays in the scheduler, evaluated on a consistent session snapshot. Cost: admissible pods in opted-in queues pay one extra scheduling cycle before allocation.

If the pod's queue does not exist at admission time, the gate is added anyway — a pod referencing no real queue can never schedule, and leaving it visible to autoscalers would recreate the over-provisioning problem. Removal happens in `ungate` if/when the queue exists. Symmetrically, pods found gated for an existing queue that has **not** opted in are ungated unconditionally by the action — the same self-healing path as feature disablement.

### Gate removal — the `ungate` action

A new scheduler action, appended last in the action list so it observes end-of-session queue state (including this session's allocations and evictions). Per session:

1. Collect podgroups with gated tasks whose **only** gate is the KAI gate. Any foreign gate present → leave the pod untouched (its gate owner keeps control).
2. Order podgroups with the existing queue/job ordering, so fairness ordering is preserved. Podgroups with partially removed gates (see gang semantics) are admitted first.
3. Greedily admit against the quota oracle (below), committing each admission into simulated queue shares before evaluating the next — N admissions never collectively exceed quota.
4. Admitted → enqueue gate-removal patches for the admitted pod set through the scheduler's existing async pod-patch queue (remove only the KAI gate entry, conflict-retried). Not admitted → record the quota reason on the podgroup (conditions below).

Gated pods already flow correctly through the scheduler: they map to the `Gated` task status, are excluded from readiness (`alive − gated ≥ minAvailable`) and from allocation candidates, and all-gated podgroups are filtered out of the input of allocate/preempt/reclaim/consolidation. The `ungate` action is exempt from that readiness filter — it builds its own input by iterating podgroups with KAI-gated tasks directly from the session snapshot; otherwise fully gated podgroups would never reach it. This is also why snapshot construction cannot shrink (see Motivation): gated pods must be present in the snapshot for the ungate decision. The action-cost reduction for the other actions reuses the existing readiness path; no new scheduler fast-path is introduced.

### Quota oracle and the Reserved bucket

Admissibility mirrors the two existing capacity checks (non-preemptible over deserved quota; anyone over limit), with one addition. Quota is consumed only at allocation, so a podgroup ungated in session N that cannot yet allocate (e.g. waiting for node provisioning) would consume nothing in session N+1, and a second podgroup could be admitted against the same quota. To close this, pending **ungated** demand is counted as a `Reserved` bucket in queue attributes:

- non-preemptible: admissible iff `Deserved ≥ AllocatedNotPreemptible + ReservedNotPreemptible + requested`
- limit: admissible iff `MaxAllowed ≥ Allocated + Reserved + requested`

Both checks walk the queue hierarchy exactly as `capacity_policy` does today: `Reserved` aggregates up parent queues the same way `Allocated` does, and the greedy pass uses the same hierarchical queue ordering — admission ordering and accounting see precisely what the capacity checks see.

Reserved is recomputed from cluster state each session ("admitted" ≡ pending without the KAI gate) — no persisted or in-memory-only state, so a scheduler restart loses nothing.

Reserved is an autoscaler-fidelity optimization, not a quota-correctness mechanism — allocation-time enforcement remains authoritative. This permits **bounded reservation**: a pod that remains pending releases its reservation after a configurable expiry (default: a small multiple of the expected node-provisioning time). Without expiry, a single ungated workload that can never schedule for a non-quota reason (unsatisfiable affinity, pending PVC) would hold its reservation forever and head-of-line-block every other gated workload in the queue — a regression against today, where a broken pending pod blocks nothing. After expiry the next podgroup in order can be admitted; if both later become runnable they arbitrate at the allocation-time capacity check, which is exactly today's behavior. The expiry anchor is a `kai.scheduler/queue-admitted-timestamp` annotation stamped on the pod in the same patch that removes the gate — reconstructible from cluster state, mirroring the existing `last-start-timestamp` pattern.

### Autoscaler signal — no reservation pods

Earlier discussion in #1615 proposed dummy "reservation" pods so autoscalers see admissible-but-capacity-starved demand. This design needs none: ungating is decided on **quota** admissibility, not node fit. An ungated pod that lacks node capacity becomes a real `Unschedulable` pending pod — the autoscaler's native signal, with true resources, affinity, and tolerations, and no double-count with a proxy. Gated pods (which could never run) are never marked `Unschedulable`, so autoscalers ignore them — precisely the #1615 fix. `node-scale-adjuster` keys off the `Unschedulable` condition and therefore needs zero changes; its fractional-GPU flow works unchanged after ungating.

Cost: one extra cycle between quota becoming available and ungating, one more to allocate — bounded by the scheduling period.

### Gang and elastic semantics

- The ungate **decision** is per podgroup, all-or-nothing for the gang minimum. **Execution** is per pod and may be torn by a crash; re-decision is deterministic, and partially ungated podgroups are admitted first in the next session's greedy order, so a torn gang always converges instead of being starved by later arrivals.
- A gang below `minAvailable` that is fully gated stays out of sessions entirely (existing readiness filter) — partially created gangs never occupy session time.
- Elastic growth: new pods of a running podgroup arrive gated (webhook is unconditional) while siblings run; readiness holds since `alive − gated ≥ minAvailable`. The increment is admitted per pod — it consumes quota now, so evaluating the delta at ungate time is correct, and over-quota elastic tails no longer generate autoscaler noise.

### Preempt / reclaim soundness

Gating exactly the `IsJobOverQueueCapacity`-rejected set removes no eviction capability: preempt already rejects non-preemptible over-quota preemptors, and preemptible jobs are gated only when over **limit**, where reclaim is equally impossible. Quota freed by reclaim or completion is visible to the ungate pass within one session. Gated pods cannot be victims (not running).

### Fair-share parity

Queue `Request` today counts allocated and pending tasks; gated tasks are skipped. Left as is, gating would silently remove a queue's backlog from fair-share division. Gated demand is therefore counted into `Request` exactly like pending demand — fair-share outcomes are identical with and without gating.

### Observability

A gated-blocked podgroup carries a scheduling condition with the existing quota reasons (`NonPreemptibleOverQuota` / `OverLimit`) plus a distinct gated marker and event, written through the existing status pipeline (updated on change, not re-emitted every cycle). On admission, a superseding condition and an `Admitted` event are written in the same transition — stale gated conditions are never left behind (avoiding the [#1888](https://github.com/kai-scheduler/KAI-Scheduler/issues/1888) pattern). New metrics: gauge of gated pods per queue; ungate-rate counter.

### Failure modes

| Failure | Behavior |
|---|---|
| Webhook unavailable / plugin disabled | Pods created ungated → exactly today's behavior (enforcement is unchanged at allocation) |
| Scheduler crash mid-gang ungate | Torn gang re-decided next session; partial-first ordering guarantees convergence |
| Manual gate removal by user | Harmless bypass of noise suppression only; allocation-time capacity check still enforces quota |
| Quota reduced after ungating | Gates cannot be re-added (Kubernetes one-way ratchet); pods degrade to today's allocation-time blocking. Accepted semantics; composes with #1783 |
| Queue deleted / never existed | Pods stay gated with a distinct condition — ungating them would recreate the autoscaler over-provisioning for pods that can never schedule. Recovery on queue (re)creation or pod deletion |
| Feature disabled with gated pods present | Scheduler mass-ungates all KAI-gated pods — rollback and downgrade need no operator choreography |
| Scheduler restart | All state (gates, phases, specs) reconstructible from cluster state; nothing persisted |

## Decided Points

| # | Decision |
|---|---|
| D1 | The gate caches the allocation-time quota verdict; enforcement never moves to admission. Safety: the gate is removed only after an affirmative admission decision. Liveness: every gated podgroup is re-evaluated each session. Ungated does not imply admissible — allocation-time enforcement is authoritative |
| D2 | Gate addition is unconditional for opted-in queues; no quota computation in the webhook |
| D3 | Gate removal lives in the scheduler (new terminal `ungate` action), not the binder — gated pods can never reach binding, and the scheduler already owns the quota verdict and an async pod-patch channel |
| D4 | No reservation/dummy pods; real ungated pods are the autoscaler signal. `node-scale-adjuster` unchanged |
| D5 | Pod-level scheduling gates, not workload-level `spec.suspend` — suspend is bypassed by workload-internal autoscaling (e.g. RayCluster growing after unsuspension); gates catch every pod at creation |
| D6 | Pending ungated demand reserves quota (Reserved bucket) so cross-session admissions do not collectively exceed quota while reservations hold; expired reservations fall back to allocation-time arbitration |
| D7 | Gated demand counts into queue `Request` — fair-share division identical with and without gating |
| D8 | Strictly opt-in: global Config toggle (default off) fanned out by the operator to webhook and scheduler together, plus per-queue label |
| D9 | Foreign gates are never touched; pods with any non-KAI gate are ignored by the ungate action |
| D10 | Pods whose queue does not exist at admission are gated; removal happens in `ungate` if/when the queue exists. Pods gated for an existing non-opted-in queue are ungated unconditionally |
| D11 | Reserved accounting and greedy ordering walk the queue hierarchy exactly as the capacity checks do |
| D12 | Reservations are bounded: a pod that remains pending releases its reservation after a configurable expiry, preventing head-of-line blocking by workloads unschedulable for non-quota reasons |

## Open Questions

1. Queue opt-in surface: label (no CRD churn) vs `QueueSpec` field?
2. Gate and action naming: `kai.scheduler/queue-admission` / `ungate` vs alternatives (`admit`)?
3. Ungate hysteresis or rate-limiting after a large quota increase (thundering herd of ungates)?
4. Max-gate-age escape hatch for orphaned gated pods (queue deleted, podgroup never created)?
5. Reservation expiry: default value and configuration surface (global vs per-queue)?
6. Scale evidence to attach: session duration and API write volume with a large over-quota backlog (e.g. 50k pods), gated vs ungated, per the scale-test methodology.
