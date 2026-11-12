# Reservation API

Status: draft for iteration. Not implemented.

This document is the core design: what a Reservation is, its API, who may
consume it, and how it lives and dies. [mechanics.md](mechanics.md) says
how the scheduler implements it, with durability, crash recovery and edge
cases.

A `Reservation` holds node capacity for a pod that does not exist yet.
Later, a pod from another PodGroup takes over the held capacity on the same
node.

## Why

- **Allocate before creating pods.** A controller must secure capacity
  before it creates the pods, because how many to create, or where, depends
  on a decision made elsewhere. The simplest case: a user wants a training
  job not to wait for capacity once created, and reserves it first. This is
  the first version.
- **Multi-cluster placement.** An orchestrator asks whether a workload fits
  a cluster before creating it there, and needs the answer to stay true
  while it creates the workload.
- **Standing queue pools, later.** A queue keeps capacity ready for whatever
  it runs next: interactive and debug sessions that must start at once, a
  scarce hardware kind kept for the team that owns it, a group's guaranteed
  share held idle rather than lent out. Same object, one more value of one
  field.
- **Pre-allocation, later.** A hold on capacity that is still busy, taken as
  it frees. Three uses:
  - A large gang that never sees its capacity free at once accumulates holds
    node by node instead of losing each freed GPU to a smaller job.
  - Safe preemption: a hold on the victims' releasing capacity guarantees
    the preemptor gets what was freed for it.
  - Replacing a pod in place: a rolling update, a restart from a local
    checkpoint, or a planned move holds the slot a pod still occupies for
    its replacement, so nothing else takes it in between.

In every case the hold behaves like a scheduled pod, or for pre-allocation
like a pipelined one: placed under the same rules, charged to the queue,
evictable under the same preemption rules, and durable across a restart,
which pipelined pods are not today.

## Terms

| Term                      | Meaning                                                                                                                                     |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| **Reservation**, **hold** | One object, one held slot of node capacity, described by a pod template. "Hold" is the same thing seen from the scheduler                   |
| **Holder**                | The PodGroup a Reservation belongs to. It has holds and no pods, and gives them queue, priority, preemptibility, gang minimums and topology |
| **Consumer**              | A pod that takes a hold, and the ordinary PodGroup it belongs to. Always a different PodGroup than the holder                               |
| **Creator**               | Whoever creates the holder and its Reservations: a user, a CLI, a controller. Never KAI                                                     |
| **Eligible**              | A pod that may take a given hold: same namespace, holder's queue, priority class and preemptibility, matches the hold's selector            |
| **Placement**             | The node a hold is on, `status.placement`. Written by the scheduler when the hold is reserved                                               |
| **Consumption**           | A pod committed onto its hold, recorded in `status.podRefs`. Written by the scheduler before the pod's BindRequest; the hold is `Consumed` from then on |

## Scope

First version: single-use holds whose consumer's request vector equals
the hold's; whole GPUs and other extended resources on both sides; holder
and consumer with equal priority class and preemptibility, neither
semi-preemptible.

Not in scope: domain enumeration and per-domain counts, which the holder's
tree expresses and a controller builds; a hold that counts toward the
consuming gang; reviving an expired hold.

Later:

- **`Shared` holds** for standing queue pools: many pods take parts of one
  hold until it is used up, a finished pod gives its part back. One more
  enum value, an `allocated` status field, and a virtual task that shrinks
  and grows between cycles. The only non-obvious rule: a pool must be
  evicted before its consumers, or it re-absorbs the capacity the eviction
  freed.
- **Time windows and workload run times**, one effort. A window is a start
  time and a duration; the duration is a TTL counted from the start. The
  start time is the hard part: without knowing when running jobs end,
  "start at T" can only mean "begin trying at T", no better than creating
  the Reservation at T. The guaranteed form, where nothing that would
  overrun T is allowed to start, needs an expected run time on every
  PodGroup, declared as Slurm's time limit is or estimated from history.
  That same field lets pre-allocation plan when held capacity frees.
- **Pre-allocation.** Reserving on a node whose capacity is not free yet
  and taking it as running pods finish, the hold equivalent of pipelining.
  Uses:
  - A large gang that never sees its capacity free at once, because every
    GPU that frees is taken by a smaller job first, accumulates holds node
    by node instead.
  - A preemptor waiting out its victims' grace period keeps its placement
    across a scheduler restart, which pipelining does not.
  - A pod replaced in place, by a rolling update, a restart from a local
    checkpoint or a planned move, hands its slot to its replacement with
    nothing else taking it in between.

  Needs a waiting state, so clients must tolerate states they do not know,
  and a durable record, since pipelined pods get no BindRequest today.
- **Looser matching.** A consumer fitting within a larger hold, and the
  `Aligned` overflow taking the rest from the node.
- **Fractional holds and consumers**, which need a GPU group for the held
  remainder, recorded in `placement` so that only fractions can take it.
- **DRA-backed holds**, in the direction of KEP-5729.
- **Reservations without `podGroupRef`**, with the podgrouper creating a
  one-hold holder as for bare pods. The field is already optional; the
  podgrouper plugin and holder resolution through the annotation come
  later.
- **Non-aggressive holders**, taking free capacity only: a negative
  `preemptionDelay` meaning never, a small generic PodGroup change.

What the first version already provides for these: `podRefs` is a list,
`sharing` is an enum with one value, and `placement` is a struct that can
grow.

## Principles

1. One object is one hold on one node, described by a pod template.
2. Every Reservation belongs to a holder PodGroup, which holds and never
   runs. Queue, priority, preemptibility, gang rules and topology come from
   it.
3. The Reservation declares who may consume it, by label selector. Pods
   carry nothing and are matched to holds by fit. No pinning.
4. A hold is a task: charged, placed, evicted and counted like a pod,
   present in every snapshot as a virtual task. No new action.
5. Nothing is counted twice. A hold counts capacity until it is consumed;
   from then on its pod counts, through its BindRequest, as any pod does.
6. An eligible pod sees one hold's capacity as free, evaluated per hold;
   everyone else sees it as taken.
7. The scheduler is the only writer of a Reservation's status, and nothing
   in KAI deletes one. The binder never sees a hold.
8. Placement and consumption are both recorded on the Reservation,
   synchronously, before any dependent bind.
9. No new PodGroup status. The creator learns that the capacity is secured
   from its Reservations' states. Gang placement is all-or-nothing up to
   each subgroup's `minMember`, so those holds turn `Reserved` together;
   holds beyond it are elastic and are placed one by one.
10. A hold ends when it is consumed and expires if nobody consumes it. Both
    are terminal.

## Objects

| Object                 | Role                                                                                         | Spec written by          | Status written by             |
| ---------------------- | -------------------------------------------------------------------------------------------- | ------------------------ | ----------------------------- |
| PodGroup, unchanged    | Holder: gang tree, queue, priority, preemptibility, topology. Consumer: the running workload | Controller or podgrouper | Scheduler, podgroupcontroller |
| Reservation, new       | One hold: template, consumers, lifetime; its placement and consumer in status                   | Controller               | Scheduler only                |
| BindRequest, unchanged | The consumer pod's ordinary placement record                                                 | Scheduler                | Binder                        |

### PodGroup, unchanged

| Field                                                         | Role for a holder PodGroup                                                                                                                                                                                    |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `spec.queue`, `spec.priorityClassName`, `spec.preemptibility` | Holds are charged, ranked and evicted like pods. A non-preemptible holder is admitted only within deserved quota and never evicted. Consumers must have the holder's queue, priority class and preemptibility |
| `spec.subGroups[].minMember`                                  | How many holds a subgroup needs. A consumed hold still counts                                                                                                                                                 |
| `spec.subGroups[].minSubGroup`                                | Floor over children: "at least N of these"                                                                                                                                                                    |
| `spec.subGroups[].parent`, `topologyConstraint`               | Tree and co-location rules apply to holds as to pods                                                                                                                                                          |
| `spec.preemptionDelay`                                        | Unchanged: minimal pending time before the holder may evict others for its holds                                                                                                                              |
| `spec.stalenessGracePeriod`                                   | How long a holder may sit below a minimum before its remaining `Reserved` holds expire, like a gang of pods                                                                                                   |

A holder has Reservations and no pods. It waits for its holds exactly as a
workload waits for its pods: the first `minMember` of a subgroup are placed
together or not at all, the rest as capacity allows.

### Reservation

```go
// Reservation holds capacity on one node for a pod of another PodGroup that
// does not exist yet.
type Reservation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReservationSpec   `json:"spec"`
	Status ReservationStatus `json:"status,omitempty"`
}

// ReservationSpec is immutable after creation.
type ReservationSpec struct {
	// PodGroupRef names the holder PodGroup, in the same namespace, and the
	// subgroup this hold belongs to; no subgroup means the PodGroup's default
	// one, as for pods. Queue, priority, preemptibility, gang rules and
	// topology come from it. Omitted, the podgrouper creates a holder for
	// this Reservation alone, as it does for a bare pod.
	// +optional
	PodGroupRef *PodGroupSubGroupRef `json:"podGroupRef,omitempty"`

	// Consumers says which pods may take the hold.
	Consumers ReservationConsumers `json:"consumers"`

	// Sharing is Single: one pod takes the whole hold. The only value in
	// the first version; Shared, many pods taking parts, comes later.
	// +kubebuilder:validation:Enum=Single
	// +kubebuilder:default=Single
	// +optional
	Sharing ReservationSharing `json:"sharing,omitempty"`

	// Template describes the hold the way a pod is described. Only
	// scheduling-relevant fields are honored: resources, overhead,
	// nodeSelector, affinity, tolerations, labels and annotations. Images,
	// commands and env are ignored. Containers may carry only a name and
	// resources. A consumer takes the hold when its effective request vector
	// equals the template's; nothing else is compared. The consumer's own
	// node selector, affinity and tolerations are evaluated on the hold's
	// node as predicates, as for any pod.
	// Carries the full PodTemplateSpec schema: a malformed template is
	// rejected by the API server instead of breaking every list of
	// Reservations the scheduler depends on.
	Template corev1.PodTemplateSpec `json:"template"`

	// TTL bounds the hold's life, counted from creation. Zero disables.
	// Defaults to 24h.
	// +kubebuilder:default="24h"
	// +optional
	TTL *metav1.Duration `json:"ttl,omitempty"`
}

type PodGroupSubGroupRef struct {
	Name string `json:"name"`
	// +optional
	SubGroup string `json:"subGroup,omitempty"`
}

type ReservationConsumers struct {
	// PodSelector selects the eligible pods among those in the Reservation's
	// namespace whose PodGroup is in the holder's queue and has the holder's
	// priority class and preemptibility. Required; an empty selector means
	// any such pod. Holds are never consumers of other holds.
	PodSelector metav1.LabelSelector `json:"podSelector"`
}

// ReservationStatus is written by the scheduler only.
type ReservationStatus struct {
	// ObservedGeneration is the generation this status reflects.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions report object-wide problems: PodGroupFound and
	// SubGroupFound, false while the holder or subgroup does not exist yet.
	// The hold waits, as a pod waits for its PodGroup.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Placement is the hold's placement record. Written by the scheduler
	// before any dependent bind; absent while Pending. This is what a
	// restart replays.
	// +optional
	Placement *ReservationPlacement `json:"placement,omitempty"`

	// State is Pending, Reserved, Consumed or Expired. Consumed and Expired
	// are terminal.
	State ReservationState `json:"state,omitempty"`

	// Reason and Message explain the state, for example WaitingForCapacity.
	// +optional
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`

	// LastTransitionTime is when State last changed.
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// PodRefs records the consuming pod by name and UID, written
	// synchronously with Consumed, before the consumer's BindRequest exists.
	// At most one entry for Single; a list so that Shared needs no API
	// change.
	// +listType=map
	// +listMapKey=uid
	// +optional
	PodRefs []ReservationPodRef `json:"podRefs,omitempty"`
}

type ReservationPlacement struct {
	// NodeName is the node the capacity is held on.
	NodeName string `json:"nodeName"`
}

type ReservationPodRef struct {
	Name string    `json:"name"`
	UID  types.UID `json:"uid"`
}
```

The Reservation is namespaced, next to its holder, and serves only pods of
its namespace. Controllers find a holder's Reservations by the names they
gave them or by filtering on `spec.podGroupRef`.

A Reservation without `podGroupRef` is treated like a bare pod. The
podgrouper creates a holder PodGroup with `minMember: 1`, owned by the
Reservation, with the queue from the Reservation's queue label and the
priority from the template's `priorityClassName`, and records it in the
`pod-group-name` annotation on the Reservation, since the spec is immutable.
The scheduler resolves the holder from `spec.podGroupRef` when set, else
from the annotation, and the hold waits with `PodGroupFound` false until the
PodGroup exists, as a pod does. Deleting the Reservation deletes its
PodGroup. This needs a podgrouper plugin and is built after the first
version; see [Scope](#scope).

### Example: one pod, by hand

A user reserves one 8-GPU node slot, then runs a pod on it. KAI never
creates holders; whoever wants the capacity held does.

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup # holder: holds, no pods
metadata: { name: dev-1-hold, namespace: team-a }
spec:
  queue: team-a
  priorityClassName: build
  minMember: 1 # how many holds
---
apiVersion: scheduling.run.ai/v2alpha2
kind: Reservation
metadata:
  name: dev-1
  namespace: team-a
  ownerReferences:
    [
      {
        apiVersion: scheduling.run.ai/v2alpha2,
        kind: PodGroup,
        name: dev-1-hold,
        uid: "<uid>",
      },
    ]
spec:
  podGroupRef: { name: dev-1-hold } # default subgroup
  consumers:
    podSelector: { matchLabels: { app: dev-1 } }
  template:
    spec:
      containers:
        - name: main
          resources: { requests: { nvidia.com/gpu: 8 } }
```

KAI places the hold and writes `status.placement.nodeName` and `Reserved`.
The user then creates a PodGroup and a pod by hand, opting out of the
podgrouper (see the external PodGroups design):

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup # consumer
metadata: { name: dev-1, namespace: team-a }
spec:
  queue: team-a # the holder's queue
  priorityClassName: build # the holder's priority class and preemptibility
  minMember: 1
---
apiVersion: v1
kind: Pod
metadata:
  name: dev-1
  namespace: team-a
  labels: { app: dev-1 } # matches the hold's selector
  annotations:
    pod-group-name: dev-1
    kai.scheduler/skip-podgrouper: "true"
spec:
  containers:
    - name: main
      image: ...
      resources: { requests: { nvidia.com/gpu: 8 } } # equals the template
```

The pod is eligible, its request vector equals the template's, and it lands
on the held node. The hold is written `Consumed`, with the pod, before the
pod's ordinary BindRequest is created:

```yaml
status:
  placement: { nodeName: gpu-b07 }
  state: Consumed
  podRefs:
    - { name: dev-1, uid: "<pod-uid>" }
```

When the user is done, deleting the holder removes the Reservation; the pod
is untouched.

### Example: PyTorchJob

A distributed training job, one master and three workers in one rack, that
must not wait for capacity once created. Same flow; what differs:

- **Two roles, two subgroups.** The holder has a `master` subgroup with
  `minMember: 1` and a `worker` subgroup with `minMember: 3` and a rack
  `topologyConstraint`, and the Reservations in each copy the resources of
  that replica's template.
- **One selector per subgroup.** Worker Reservations select the job name and
  `replica-type: worker`, the master's selects `replica-type: master`. This
  is what ties each role to its own subgroup's holds. Request vectors alone
  cannot: if master and worker had the same shape, the master could take a
  worker hold and leave a worker on the master's node, outside the rack the
  worker subgroup was constrained to.
- **Consumer PodGroup from the podgrouper.** The PyTorchJob is created with
  the same queue label and priority class; the podgrouper builds its
  PodGroup as today, with a subgroup per replica type. The four pods consume
  the four holds and the job is satisfied at once, its capacity already held
  and already charged.

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup # holder
metadata: { name: train-1-holds, namespace: team-a }
spec:
  queue: team-a
  priorityClassName: train
  subGroups:
    - { name: master, minMember: 1 }
    - {
        name: worker,
        minMember: 3,
        topologyConstraint:
          { topology: datacenter, requiredTopologyLevel: rack },
      }
---
apiVersion: scheduling.run.ai/v2alpha2
kind: Reservation
metadata: { name: train-1-worker-0, namespace: team-a, ownerReferences: [...] }
spec:
  podGroupRef: { name: train-1-holds, subGroup: worker }
  consumers:
    podSelector:
      matchLabels:
        training.kubeflow.org/job-name: train-1
        training.kubeflow.org/replica-type: worker
  ttl: 24h
  template:
    spec: # copied from the PyTorchJob worker template
      containers:
        - name: pytorch
          resources:
            { requests: { nvidia.com/gpu: 8, cpu: "32", memory: 256Gi } }
# ... train-1-worker-1, train-1-worker-2; train-1-master-0 in subgroup master,
#     with the master template and replica-type: master
```

## States

`status.state` is the hold's lifecycle. Every transition is written by the
scheduler.

| State      | Meaning                                                                                                                         |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `Pending`  | No placement; holds nothing; tried every cycle                                                                                  |
| `Reserved` | Capacity held on `placement.nodeName`; counts toward the queue; satisfies the holder                                            |
| `Consumed` | A pod was committed onto the hold; its BindRequest places it. Terminal: the hold holds no capacity and still satisfies the holder, until its creator deletes it |
| `Expired`  | TTL passed; placement cleared; terminal; a running pod is untouched                                                             |

```text
Pending -> Reserved -> Consumed (terminal)
Pending | Reserved -> Expired (TTL)
Reserved -> Pending (hold evicted, node gone)
```

Consumption is final at commit. If the pod's bind fails afterwards, or the
scheduler crashed between writing `Consumed` and creating the BindRequest,
the pod schedules as an ordinary pod and the hold is spent. Nothing is
double-counted and no second pod can take the hold; the pod has lost its
reservation, which the creator can make again. See
[mechanics.md](mechanics.md#consuming).

Nothing in KAI deletes a Reservation. `Consumed` and `Expired` holds hold
no capacity, like finished pods, and their creator deletes them: directly,
or by deleting the holder, which cascades through the owner reference every
Reservation should carry. A hold whose holder or subgroup is gone is
reported through its conditions and otherwise ignored.

## Eligibility and consumption

One function decides who may take a hold:

```text
eligible(pod, hold) =
     namespace(pod) == namespace(hold)
  && queue(pod) == queue(hold.holder)
  && preemptibility(podGroup(pod)) == preemptibility(hold.holder)
  && priorityClass(podGroup(pod)) == priorityClass(hold.holder)
  && !isHold(pod)                           # holds never consume holds
  && !fractional(pod)                       # first version
  && hold.consumers.podSelector matches pod
```

The queue is always the holder's queue, so the charge and the consumers can
never drift apart. Preemptibility must match: a non-preemptible hold
consumed by a preemptible pod would lose its guarantee when the scheduler
evicts the pod, and a preemptible hold consumed by a non-preemptible pod
would grant protection that was never admitted. Priority must match too:
otherwise a `Pending` hold of a higher-priority holder could evict the pods
consuming its sibling holds. The holder stands in for the workload, so it
carries the workload's queue, priority and preemptibility, nothing else.
Semi-preemptible PodGroups are excluded on both sides in the first version.

Fractional GPU reservations are not supported yet, on either side:
templates and consumers are whole GPUs. See [Scope](#scope).

Eligibility is by labels the consuming controller owns: PodGroup membership
is an annotation and cannot be selected on. A workload in the same queue with
the same labels could take the holds, a misconfiguration risk inside one
queue, never across queues.

A pod takes the whole hold, and in the first version its effective request
vector must equal the hold's for every resource: the hold's virtual task is
removed from the node and the pod is allocated in its place, so nothing leaks
to the node and nothing is drawn from it. Only the request vectors are
compared. The pod's node selector, affinity and tolerations are its own and
run as predicates on the hold's node, as for any pod; a consumer whose
constraints exclude that node does not fit that hold and tries the next
candidate. Fit is evaluated per hold, never against the sum of several: two
one-GPU holds do not make a two-GPU pod fit.

Holds come first. A pod with matching holds is placed on one of their nodes
whenever any of them fits; free capacity elsewhere is used only when none
does. The hold was paid for and shaped for this pod, so there is no reason
to leave it idle while the queue's general share is spent. If the pod does
land elsewhere, the hold stays `Reserved` until its TTL, an eviction, or its
creator deletes it.

## Lifecycle

### Expiry

`ttl` counts from `metadata.creationTimestamp`, default 24h, `0` disables.
The timer runs while `Pending` or `Reserved`, so a hold that never fits is
bounded too, and stops at consumption. Expiry is
terminal and clears the placement; no edit revives the hold. There is no
absolute deadline field: the spec is immutable, so it could not extend a
hold, and a controller computes a TTL at creation.

A holder can fall below a minimum without eviction: one hold expires while
another, with a different TTL, is still `Reserved`, or the creator deletes
one. The gang then cannot complete on its own, so the holder is stale after
`stalenessGracePeriod`, counting `Reserved` and `Consumed` holds
against each subgroup's `minMember` exactly as pods are counted today. A
stale holder has its `Reserved` holds expired. The grace period is the
window in which the creator can repair the gang with a replacement
Reservation and keep the surviving holds where they are.

### Eviction

| Event                                          | Effect                                                                                                                                                   |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Consumer pod ends or is evicted                | Nothing. The hold ended at consumption; a replacement pod schedules normally                                                                             |
| Scheduler evicts an unconsumed hold            | Placement cleared; back to `Pending`; gang rules below                                                                                                   |
| The hold's node disappears                     | As eviction                                                                                                                                              |
| Holder deleted                                 | Reservations deleted through the owner reference; consumers unaffected                                                                                   |

A hold is a task of its holder, so preempt and reclaim see it with the
holder's queue, priority and preemptibility, and the holder gets KAI's gang
eviction unchanged: extras beyond `minMember` and `minSubGroup` are taken one
at a time, and a victim that would leave the gang below a minimum takes the
whole gang with it. One rule covers the state no PodGroup of pods can be in,
a holder that is partly consumed:

> `Consumed` holds count toward the minimums, hold no capacity, and are
> never victims. Victims are chosen among `Reserved` holds only.

So with `minMember: 3` and four holds, two `Consumed` and two `Reserved`,
one reserved hold can be evicted alone and the second cannot: it would break
the minimum and takes the holder's remaining `Reserved` holds with it. The
pods those holds were meant for schedule normally.

Evicting a hold is a status change with no pod to kill and no grace period:
the capacity is free when the patch lands. Holds are ranked as victims by the
same rules as pods, queue and priority first, so a lower-priority running pod
is evicted before a higher-priority hold. Only among victims of equal rank
do holds go first, because they are the cheaper eviction. Consolidation
treats holds like pods too: it may move a `Reserved` hold to another node,
and the creator sees the node change.

Whether a holder may evict others for its holds is decided as for any
PodGroup: by its queue's share, its priority and its `preemptionDelay`,
which preempt, reclaim and consolidation already honor.

## Validation

Admission rejects a Reservation when:

- the holder PodGroup is semi-preemptible (first version);
- `consumers.podSelector` is missing; an explicit empty selector `{}` is
  allowed and means every eligible pod of the namespace;
- the template has no container, or no resource requests;
- the template requests a GPU fraction (`gpu-fraction`, `gpu-memory`
  annotations) or `resourceClaims` (first version);
- `ttl` is negative;
- an empty or broad selector is set by a principal not allowed to reserve
  for the whole queue (a policy rule, not an object-scope question);
- the spec changes on update.

The template is validated by the CRD schema, the full `PodTemplateSpec`,
so a malformed template never reaches the cluster. The CRD is large as a
result, several hundred kilobytes, which exceeds the limit of client-side
`kubectl apply`; it is installed with server-side apply or `create`, as the
chart does.

Consumer pods and consumer PodGroups need no admission change.

## Open: one object for the whole gang

Proposed in review: a compound Reservation that carries the templates with
counts and the gang requirements, so a user creates one object instead of a
holder PodGroup and N Reservations.

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: Reservation
metadata: { name: train-1, namespace: team-a }
spec:
  queue: team-a
  priorityClassName: train
  consumers:
    podSelector: { matchLabels: { training.kubeflow.org/job-name: train-1 } }
  ttl: 24h
  subGroups:
    - name: master
      minMember: 1
      template: { spec: { containers: [{ name: pytorch, resources: { requests: { nvidia.com/gpu: 1 } } }] } }
    - name: worker
      minMember: 3
      topologyConstraint: { topology: datacenter, requiredTopologyLevel: rack }
      template: { spec: { containers: [{ name: pytorch, resources: { requests: { nvidia.com/gpu: 8 } } }] } }
status:
  subGroups:
    - name: worker
      holds:
        - { id: "0", state: Reserved, placement: { nodeName: gpu-b07 } }
        - { id: "1", state: Consumed, placement: { nodeName: gpu-b08 }, podRefs: [...] }
```

Recommended as a layer: the podgrouper, or a small controller, expands it
into a holder PodGroup and per-hold Reservations, and the scheduler keeps
the per-hold API. Consumed directly by the scheduler it would repeat the
PodGroup's gang spec in a second schema, turn every placement and
consumption into a patch on entries of one object, and need a mutable spec
to release some holds. Not decided.

## Prior art

- Koordinator `Reservation`: pod template, `owners`, TTL from creation,
  `allocateOnce`, `Aligned` policy, placement in `status.nodeName` written
  by the scheduler, consumer tracked by a pod annotation set in PreBind,
  reserve pod kept full-size in the scheduler cache and restored per matched
  pod. This design takes the unit, the expiry, the status-held placement,
  single use as the default, and the owner-declares-consumers model; it
  differs in gang and queue integration and in where consumption is
  recorded.
- Slurm advance reservations: named nodes for named users in a time
  window. `MAGNETIC` is selector-based eligibility, `PURGE_COMP` is the TTL,
  `REPLACE` is the `Shared` pool growing back, and backfill's planned starts
  are pre-allocation. No template, no gang, no consumption.
- Kueue `Workload.spec.podSets`: the shape of the template-list alternative.
- KEP-4671 gang scheduling names a future upstream `Reservation` as the
  output of a PodGroup placement; this is a KAI-local form of it.
- KEP-5729 ResourceClaim per PodGroup: the direction for DRA-backed holds.
