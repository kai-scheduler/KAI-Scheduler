# Multi workload gang scheduling

(not to be confused with [hierarchical-PodGroups](../hierarchical-podgroup/README.md))

As use cases become more complex, and different workload types get more specialized, it becomes necessary to manage the lifecycle of several workloads together. Maybe the clearest use case is reinforcement learning (RL), which can require a complex set of workloads all operating together, but there are also simpler use cases, like some model serving patterns which require auxiliary deployment, e.g for authentication, routing, caching and so on. All these use cases require coupling the lifecycle of more than one workload together, but the users would benefit from having each workload managed by its own domain-specific controller.

KAI's PodGroup API provides the necessary building block to support gang-of-gangs, due to its hierarchical structure and per-hierarchy-level minima and topology constraints - but today, a user has no way to group multiple workloads with different parent CRDs together, unless they build the entire PodGroup themselves - which is usually an unreasonable requirement from most researchers or engineers, as the grouping logic itself can be quite complicated and specialized, especially in complex use cases like disaggregated inference.

## Background

### Reinforcement learning workloads

Modern reinforcement-learning post-training is a heterogeneous, cyclic application rather than a single distributed job. Policy trainers, rollout/inference servers, reward or critic models, replay buffers, and agent environments have different resource and lifecycle requirements, but useful progress requires the complete loop to be available.

While these patterns can sometimes be expressed as a single hierarchical workload (like a Ray job with several worker groups), as models and RL environments become more complicated, it becomes harder to express the entire RL stack as a single workload. For example, disaggregated LLM serving is a complex problem on its own, tackled by frameworks like Dynamo/Grove. Users will benefit from being able to deploy each part of the RL workload as its own entity with its own CRD/controller, as long as they can express the scheduling requirements between these workloads.

## Goals

- Allow several independent workloads to be coupled together as a gang
- Support all existing podgrouper-supported tupes
- Support externally-created podgroups
- Maintain explainability as much as possible; if workload A can theoretically be scheduled but is blocked by workload B, make it clear to the user

## Non-Goals

- KAI should not own the individual workload objects beyond scheduling them. Users create workload objects directly or through third parties, the podgrouper may create ordinary input PodGroups for them, and the `WorkloadGang` controller composes those inputs.

## Design

The hierarchical structure of PodGroups already provides the scheduling primitive needed to compose several gangs. This design will propose a mechanism to group several independent workloads together reliably.

To provide a versatile solution that will work with both "internal" (PodGrouper-created PodGroups) and "external" (PodGroups created by users or 3rd party systems), this new flow will group PodGroup objects from the cluster. To prevent race conditions, several alternative data models are proposed. A new CRD, `WorkloadGang`, will represent the coupling of these PodGroups, including potential overrides to the workloadGroup's scheduling properties like preemptiblity and priority, topology constraints, etc. It will also help prevent race conditions by specifying the number of PodGroups expected to participate in the gang.

The controller creates a **rendered PodGroup** containing the merged hierarchy and effective scheduling policy. Issues in the grouping process will be reported in `WorkloadGang.status` - for example, if some expected PodGroups are missing. A successful grouping will result in an actual PodGroup object in the cluster which the scheduler will treat like any other PodGroup.

### Proposed flow

1. A user creates several workloads they want to be grouped together - for example, a `RayCluster` and a `DynamoGraphDeployment`. The user creates a `WorkloadGroup` instance with the expected number of participating PodGroups (2). The individual workloads submitted by the user include an annotation `kai.scheduler/workload-gang: <WorkloadGang object name>` to let the podgrouper / Dynamo to create their podgroups with a workloadGroupRef in their spec.
2. Two podgroups are created alongside the objects mentioned before: one by the podgrouper for the RayCluster, the other by Grove for Dynamo. Both podgroups are created with the `workloadGroupRef` in spec, so the scheduler knows not to handle them prematurely - even if the `WorkloadGroup` creation is delayed.
3. When the `WorkloadGang` object is created, the new WorkloadGangController in the podgrouper starts reconciling it. If the necessary podgroups don't exist yet, it updates the `WorkloadGang` status accordingly. Once both Podgroups exist, it merges their specs and creates a new `PodGroup` object in the cluster. It updates the existing PodGroups with a reference to the new podgroup in the status, and to their respective subgroups within it.
4. The scheduler sees the new PodGroup and the existing pods. The scheduler's snapshot follows the new refs in the PodGroups' status to create the correct structure in-memory: each pod is assigned to the correct subgroup within the new root `PodGroup`.
5. The scheduler acts on the new PodGroup as usual, including error handling, SchedulingConditions etc. (We can probably update the individual podgroups' status as well - need to consider).

## Relationship and data model

The relationship between pods, input PodGroups, the `WorkloadGang`, and the rendered PodGroup must satisfy the following requirements regardless of its API representation:

- All objects participating in a group are in the same namespace. Same-namespace actors are assumed to be mutually trusted.
- An input PodGroup belongs to at most one `WorkloadGang`.
- Grouping intent must hold an input PodGroup from the moment it is created, before it can schedule independently, including when the `WorkloadGang` or rendered PodGroup has not appeared yet. Adding the gate later cannot restore gang atomicity.
- The controller must be able to determine when the input set is complete. Observing the currently available PodGroups is not sufficient by itself.
- The relationship must resolve to a particular rendered PodGroup revision before the scheduler follows it.
- The controller must publish a mapping from each input PodGroup and local SubGroup to a rendered SubGroup. The mapping must be deterministic and stable across reconciles; the scheduler must not reproduce the controller's prefix, truncation, or hashing logic.
- Input PodGroups keep their original ownership. Only the rendered PodGroup has a controller owner reference to the `WorkloadGang`.

Two main data models are under consideration.

### Option A: input PodGroups point to the WorkloadGang

Each input PodGroup identifies its `WorkloadGang` through a typed field in its spec:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  name: policy-trainer
  namespace: research
spec:
  workloadGroupRef:
    name: rl-post-training
```

For KAI-generated PodGroups, grouping intent can be placed on the workload's top owner and propagated by the podgrouper into `spec.workloadGroupRef`. An externally created PodGroup that supports this API sets the field directly. A non-empty reference is both the membership declaration and the scheduling gate: the PodGroup must not be scheduled independently while its referenced `WorkloadGang` is unresolved or inactive.

As a future compatibility option, an annotation could express the same relationship for external PodGroup producers that are not aware of `WorkloadGang` or the new PodGroup spec field. This option remains out of scope until such a use case is identified.

Top-owner propagation would need to become authoritative reconciliation rather than creation-time copying. The podgrouper would need to watch relevant owner changes and set, replace, or remove its owned relationship field on the generated PodGroup. Otherwise late association, detachment, and movement between groups can leave stale membership.

The `WorkloadGang` does not list member identities. It declares the exact number of input PodGroups expected:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: WorkloadGang
metadata:
  name: rl-post-training
  namespace: research
spec:
  expectedPodGroups: 3
```

The controller indexes PodGroups by this relationship. Fewer than `expectedPodGroups` leaves the group pending. Equality freezes the exact input UIDs and generations for a rendered revision and permits rendering. A larger set is a conflict, and a late input after activation remains held rather than silently joining the active revision. This is an exact expected count, not a scheduling threshold. If `N of M` scheduling is desired later, it should use a separate field.

This model follows the existing pod-to-PodGroup direction: the smaller scheduling unit points to the larger unit. Membership has one identity source, and externally created PodGroups fit naturally. Its disadvantages are that status can report only how many members are missing rather than which ones, and late PodGroups that point to an already active group must remain held and conflicted until a new input set is explicitly activated.

### Option B: the WorkloadGang lists input PodGroups

The `WorkloadGang` explicitly lists its expected PodGroups:

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: WorkloadGang
metadata:
  name: rl-post-training
  namespace: research
spec:
  podGroupRefs:
    - apiVersion: scheduling.run.ai/v2alpha2
      kind: PodGroup
      name: policy-trainer
    - apiVersion: scheduling.run.ai/v2alpha2
      kind: PodGroup
      name: rollout-workers
    - apiVersion: scheduling.run.ai/v2alpha2
      kind: PodGroup
      name: reward-service
```

The explicit list closes membership and lets status identify a missing or invalid member. It also avoids needing a grouper-specific representation: references point to the PodGroup boundary regardless of who created each object.

This model still needs a child-side scheduling gate. Otherwise an input PodGroup can be created and scheduled before the `WorkloadGang` exists or before its controller has rendered the aggregate. A generic hold is sufficient because the explicit `podGroupRefs` list is the membership source of truth:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  name: policy-trainer
  namespace: research
spec:
  hold: true
```

The hold does not get released when rendering completes. It means that the input PodGroup must never be scheduled as an independent job. Once an active rendered revision exists, the scheduler associates the held PodGroup's pods with that rendered PodGroup instead. The controller records the input PodGroups in the rendered revision, allowing the scheduler to build the reverse input-to-rendered relationship without a parent reference on each input.

If no active rendered revision claims a held PodGroup, its pods remain held. If multiple active rendered revisions claim it, all conflicting claims remain unschedulable and the controllers report the conflict. Deleting the `WorkloadGang` therefore leaves the input PodGroups held unless a user or their owning controller explicitly removes the hold.

This option has a clear definition of completeness and a simple creation-order gate. The `WorkloadGang` list defines membership; `hold` only prevents standalone scheduling and carries no membership identity.

### Activation requirements

The exact API is undecided, but activation must be fail-closed:

1. Grouping intent holds every input PodGroup from standalone scheduling.
2. The controller resolves the complete input set and validates it.
3. The controller persists an inactive rendered PodGroup and the complete input-to-rendered SubGroup mapping.
4. The `WorkloadGang` publishes the active rendered PodGroup reference last, including enough identity and revision information to reject stale or partially updated objects.
5. The scheduler follows the relationship only when the active reference, rendered object, input identities, and SubGroup mapping agree. Otherwise the input pods remain held.

When an active group changes, the controller must make the current revision inactive before replacing its hierarchy or mapping, and publish the new active revision only after all related state is ready. The concrete revision and activation fields depend on the selected data model.

Joining a workload group after an input PodGroup has already allocated pods cannot provide retroactive gang atomicity. Whether this is rejected, supported through eviction and restart, or treated as best effort remains open. If an active rendered revision later becomes invalid while descendant pods are already allocated, the scheduler still needs a valid aggregate job identity for those pods. The design must choose between retaining the last valid root while blocking new allocations, constructing a non-schedulable aggregate for existing allocations, or evicting the aggregate before deactivation. Running pods must never fall back to independent child jobs implicitly.

## Rendering and pod resolution

Each input PodGroup becomes one direct child of the rendered PodGroup. Its existing hierarchy is preserved below a deterministic wrapper:

```text
rendered PodGroup root
├── flat input PodGroup wrapper (preserved root minMember and topology)
└── hierarchical input PodGroup wrapper (preserved root minSubGroup and topology)
    └── prefixed copy of the input SubGroup tree
```

The root uses `minSubGroup` to express how many input PodGroup wrappers are required. For an all-or-nothing group, this is the number of inputs. A future `N of M` API can select a lower scheduling threshold without changing how completeness is determined.

Rendered SubGroup names must be globally unique within the rendered PodGroup. Names should be derived from the input PodGroup name and original SubGroup name, with deterministic truncation and a hash suffix when needed. Original parent relationships, minima, and topology constraints are rewritten under the input wrapper rather than flattened. The controller records the resulting local-to-rendered leaf mapping as part of the rendered revision.

Pods retain their existing annotations and labels. During snapshot construction, the scheduler must resolve:

```text
input PodGroup UID + local leaf name + rendered revision
                            ↓
rendered PodGroup UID + rendered leaf name
```

An empty local SubGroup maps to the flat input wrapper. A named local SubGroup must identify a leaf and maps to its recorded rendered copy; a missing or internal SubGroup is invalid. Every lookup validates the input UID and rendered UID/revision so deleting and recreating an object under the same name cannot reuse stale mappings. This remapping applies to pending and already allocated pods. In the scheduler snapshot, the effective job identity becomes the rendered PodGroup and the effective SubGroup identity becomes the mapped rendered leaf; the pod object is not patched. The scheduler discovers pods through the input PodGroups because the rendered PodGroup has no directly annotated pods. It must not expose an input PodGroup as an independent job while that input is held for grouping.

The relationship API must make this traversal cheap and unambiguous. Possible implementations include a rendered-PodGroup reference and SubGroup mapping on the input PodGroup, or traversal through the active reference and mapping in `WorkloadGang.status`. Storing the relationship only in `WorkloadGang.status` requires the scheduler to watch and cache another resource and synchronize it with PodGroup snapshots. Storing it on PodGroup objects keeps traversal within an API the scheduler already consumes. This integration cost is part of the data-model decision.

## Rendered PodGroup and status

The rendered PodGroup is a real `scheduling.run.ai/v2alpha2.PodGroup` object. Storing only a rendered spec in `WorkloadGang.status` is not sufficient: the scheduler consumes PodGroup objects, and duplicating the full spec in status would create another stale representation. Status instead references the rendered object and records its inputs.

An illustrative status is:

```yaml
status:
  observedGeneration: 1
  renderedPodGroupRef:
    apiVersion: scheduling.run.ai/v2alpha2
    kind: PodGroup
    name: wg-rl-post-training-8293b681
    uid: 102884d1-6c33-41d7-934c-c618eee595a9
  renderedRevision: 7
  inputSetDigest: sha256:8293b6813bc1
  observedPodGroups:
    - name: policy-trainer
      uid: 9b41aa46-18ab-4b33-b804-5997c0e05013
      observedGeneration: 2
      rootSubGroup: policy-trainer
    - name: rollout-workers
      uid: 80947a48-a889-4634-a459-5b7c810aa85f
      observedGeneration: 4
      rootSubGroup: rollout-workers
    - name: reward-service
      uid: 0fbc89fa-5e4f-4214-bb66-af411453a7db
      observedGeneration: 1
      rootSubGroup: reward-service
  conditions:
    - type: InputsResolved
      status: "True"
      observedGeneration: 1
      reason: AllInputsResolved
      message: All expected PodGroups are present and valid
      lastTransitionTime: "2026-08-12T08:00:00Z"
    - type: Rendered
      status: "True"
      observedGeneration: 1
      reason: PodGroupRendered
      message: Rendered PodGroup research/wg-rl-post-training-8293b681
      lastTransitionTime: "2026-08-12T08:00:01Z"
    - type: Active
      status: "True"
      observedGeneration: 1
      reason: RenderedRevisionActive
      message: Rendered revision 7 is active
      lastTransitionTime: "2026-08-12T08:00:02Z"
```

The rendered PodGroup should expose the effective priority, preemptibility, queue, topology, and hierarchy directly in its spec. The rendered revision must also expose the input identities and complete leaf mapping needed by the scheduler, although the exact API location remains open. `WorkloadGang.status` should explain merge conflicts and, where possible, which input prevents the aggregate from scheduling.

## Merge behavior

The controller merges actual input PodGroup objects, not the podgrouper's internal `podgroup.Metadata`. Identity and ownership come from the `WorkloadGang`; PodGroup-wide scheduling policy resolves to one effective root value; gang shape and topology are preserved under input wrappers.

The merger must be deterministic and must not use first-writer or last-writer wins. The rendered result is visible so users can inspect every resolved value. The initial merge policy is still partly open:

| Field | Proposed behavior or decision required |
| --- | --- |
| `metadata.name` | Derive a scheduler-global collision-resistant name from the `WorkloadGang`, for example `wg-<name>-<short-uid>`. The scheduler currently identifies PodGroups by name rather than namespaced name, so namespace uniqueness alone is insufficient. Alternatively, changing the scheduler to use namespaced or UID-based PodGroup identity must be part of the implementation. |
| `metadata.namespace` | Use the `WorkloadGang` namespace and require every input PodGroup and pod to use it. |
| `metadata.ownerReferences` | Set the `WorkloadGang` as the sole controller owner. Never adopt or modify ownership of input PodGroups. |
| Annotations and labels | Start with `WorkloadGang` metadata and add controller-owned provenance. Do not arbitrarily union input metadata. Resolve scheduler-significant labels such as node pool, user, tenant, and project explicitly and reject unsupported conflicts. |
| `queue` | Prefer requiring all inputs to use the same queue. Decide whether a group-level value is only an assertion or may override input queues. Cross-queue grouping must not happen silently. |
| `priorityClassName` | Use an explicit group value when configured. Otherwise define an aggregation rule for differing priorities. Choosing the maximum promotes every input to the highest member priority; choosing the minimum makes the entire aggregate wait at the lowest member priority. The selected rule must be documented and visible in the rendered spec. |
| `preemptibility` | Use an explicit group value when configured. Otherwise define how mixed values collapse to one root policy; per-input preemptibility cannot be represented by the rendered PodGroup. |
| `preemptionDelay` | Prefer an explicit group value. If inferred, the maximum prevents the aggregate from triggering eviction earlier than any input requested. |
| `stalenessGracePeriod` | Prefer an explicit group value. If inferred, the longest grace period preserves every input's requested recovery window; a negative value should dominate finite values. |
| `minMember` and `minSubGroup` | Use root `minSubGroup` for the input-wrapper threshold. Preserve each input root's effective minimum on its wrapper and preserve descendant minima. Do not sum input `minMember` values into the rendered root. |
| `subGroups` | Deep-copy each input tree, prefix names, rewrite parents, preserve minima and topology, and sort deterministically. Reject invalid trees and name collisions that cannot be resolved deterministically. |
| Root topology | Do not infer an aggregate topology constraint from one input. Preserve each input's root constraint on its wrapper. A constraint spanning all inputs must be explicit on the `WorkloadGang`. |
| `markUnschedulable` and `schedulingBackoff` | Define whether these are group overrides or merged values. They apply to the rendered scheduling unit and cannot retain different per-input semantics. |

An input PodGroup with both root `minMember` and explicit SubGroups needs special validation. In the current scheduler, a PodGroup with SubGroups is represented by its SubGroup tree, and the root `minMember` is not an additional constraint. The renderer must preserve the scheduler's effective semantics and reject shapes it cannot normalize safely.

The current generic PodGroup handler cannot be reused unchanged for the rendered object. It preserves the existing queue and node-pool on update, preserves old root topology when the newly generated topology is empty, and updates annotations and labels additively. The rendered PodGroup needs explicit controller field ownership so that removed or changed rendered values reconcile correctly.

## Status and accounting

Input PodGroups continue to exist, so resource and scheduling status need a single authority to avoid misleading duplicate information.

Today, PodGroup resource status is calculated from pods whose `pod-group-name` directly names that PodGroup. Under this design, input PodGroups therefore naturally retain resource totals while the rendered PodGroup has no directly annotated pods. The queue controller currently sums every PodGroup, so copying aggregate resources to the rendered object without filtering would double-count them.

Two accounting models are possible:

- Input PodGroups remain authoritative for resource status and queue accounting; the rendered PodGroup is excluded from resource summation and owns aggregate scheduling conditions. If the rendered priority or preemptibility differs from the inputs, queue accounting must reclassify input resources using the effective rendered policy rather than summing each input's `allocatedNonPreemptible` value.
- The rendered PodGroup reports aggregate resources and input PodGroups are excluded from queue accounting while grouped.

The selected model must also account for the rendered priority and preemptibility. If these differ from an input PodGroup, resource classification based on the input policy can disagree with the scheduler's effective policy.

Regardless of which object is authoritative, every pod's resources must be counted exactly once. An inactive rendered revision must not be counted in addition to its inputs, and activating a rendered revision must switch accounting without treating both representations as independent workloads. The queue controller needs one canonical activation predicate: for example, count the rendered root only when its UID/revision is active and all recorded inputs match that revision; otherwise count the inputs and ignore the inactive root. Status propagation remains eventually consistent, but one reconcile must never deliberately sum both representations.

If the rendered PodGroup owns aggregate resources, the PodGroup status controller must become relationship-aware because today it finds only pods directly annotated to the object. It should aggregate the actual descendant pods or their input statuses and patch only `resourcesStatus`, leaving scheduler-owned scheduling conditions untouched.

For explainability, the rendered PodGroup should carry the scheduler's aggregate scheduling conditions, and the `WorkloadGang` should project those conditions into per-input status. A user inspecting the group should be able to tell that one input is locally ready but blocked by another input. Existing input scheduling conditions from a previous standalone period must be cleared or explicitly marked as superseded, and the input status should identify the rendered PodGroup under which it is scheduled. Controllers must patch only the status fields they own.

## Constraints

- A `WorkloadGang`, every input PodGroup, every participating pod, and the rendered PodGroup must be in the same namespace.
- Input PodGroups remain owned and reconciled by their original creators. The `WorkloadGang` owns only the rendered PodGroup.
- A grouped input PodGroup must not be scheduled independently while the relationship is unresolved or invalid.
- The renderer, not the scheduler, owns all merge policy. The scheduler performs only relationship and SubGroup resolution.

## Open questions

- Which relationship model should be used: child discovery with `expectedPodGroups`, an explicit list with a child-side gate, or a hybrid?
- Where should the active rendered reference, input identities, and local-to-rendered SubGroup mapping be stored?
- When does the observed input set become immutable, and what happens when another input appears after activation?
- May an already allocated PodGroup join a workload group, and if so, how is gang atomicity restored?
- How should differing priority and preemptibility values be merged when the `WorkloadGang` does not override them?
- Should `queue` be inferred and required to match, or explicitly configured as an assertion or override?
- Which object is authoritative for resource status and queue accounting?
- Which input and group policy changes are allowed after the rendered PodGroup has allocated pods?
- For Option A, who removes `workloadGroupRef` after the `WorkloadGang` is deleted or becomes invalid? Option B remains held until `hold` is explicitly removed.
- What aggregate identity owns already allocated pods while a rendered revision is inactive or being replaced?
- Should the MVP require every input, or support a separate `N of M` scheduling threshold?
