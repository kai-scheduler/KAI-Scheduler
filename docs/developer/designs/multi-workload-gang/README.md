# Multi workload gang scheduling

(not to be confused with [hierarchical-podgroups](../hierarchical-podgroup/README.md))

As use cases become more complex, and different workload types get more specialized, it becomes necessary to manage the lifecycle of several workloads together. Maybe the clearest use case is reinforcement learning (RL), which can require a complex set of workloads all operating together, but there are also simpler use cases, like some model serving patterns which require auxiliary deployment, e.g for authentication, routing, caching and so on. All these use cases require coupling the lifecycle of more than one workload together, but the users would benefit from having each workload managed by it's own domain-specific controller.

KAI's PodGroup API provide the necessary building block to support multiple gangs, due to it's hierarchical structure and per-hierarchy-level constraints and priorities - but today, a user has no way to group multiple workloads with different parent CRDs together, unless they build the entire podgroup themselves - which is usually an unreasonable requirement from most researchers or engineers, as the grouping logic itself can be quite complicated and specialized, especially in complex types like Grove and others.

## Background

### Reinforcement learning workloads
Modern reinforcement-learning post-training is a heterogeneous, cyclic application rather than a single distributed job. Policy trainers, rollout/inference servers, reward or critic models, replay buffers, and agent environments have different resource and lifecycle requirements, but useful progress requires the complete loop to be available. 

While these patterns can sometimes be expressed as a single hierarchical workload (like a Ray job with several worker groups), as models and RL environments become more complicated, it becomes harder to express the entire RL stack as a single workload. For example, disaggregated LLM serving is a complex problem on it's own, tackled by frameworks like Dynamo/Grove. Users will benefit from being able to deploy each part of the RL workload as it's own entity with it's own CRD/controller, as long as they can express the scheduling requirements between these workloads.

## Goals
- Allow several independent workloads to be coupled together as a gang
- Support any pod-grouper supported type, don't require specific grouping logics to be aware of gang-of-gangs
- Maintain explainability as much as possible; If workload A can theoretically be scheduled but is blocked by workload B - make it clear to the user

## Non-Goals
- KAI should not own the individual workload objects beyond scheduling them; Like the existing pod-grouping pattern - users create workload objects (directly or through third-parties), the pod-grouper is responsible to group them together (according to users' instructions)

## Design
The hierarchical structure of podgroups already solves most of the problem; theoretically, any podgroup we create today can be expressed as a sub-group. What's missing is a pod-grouper mechanism to detect such groups. While this can be probably achieved with annotations on the top-owner, we propose a new CRD to express that, given the following:
- The different workloads will be created by different controllers. To prevent race conditions and inconsistencies, the final podgroup should only be available once the podgrouper saw all the relevant workloads. To do that, the grouper needs to know how many workloads to expect. This can be expressed by a minWorkloads field (or something similar)
- A CRD can report conditions and errors in it's status, helping manage some of the complexity of this multi-workload orchestration
- A user might want to override the priority/preemptibility or other values

Note: this new CRD is only meant to express the coupling of these workloads, not to own them: it does not need to own them, and doesn't need to create them or manage their lifecycle.

The proposed flow is:
- A user wants to gang a RayCluster and a Dynamo workload together. The user creates an instance of the new CRD, and the relevant Dynamo and Ray objects, which are annotated (or, have an owner reference to the CRD?) to indicate they're part of a bigger gang. The annotation lets the podgrouper know to hold off on creating individual podgroup objects for each workload; this means we aren't sensitive to the order of creation.
- The pod-grouper watches for the CRD and pods. On every workload object, it knows not to create a podgroup object because of the annotation. If the CRD exists but not all workloads do, reflect that in the status, as a condition maybe.
- Pods that are created are assigned to the podgroup even before the podgroup is created (could happen if one workload creation is delayed) - we need to verify that the scheduler handles this gracefully. This is to prevent the need to go back and patch a bunch of pods once the full podgroup is created.
- Once all workloads and the new CRD exist, the podgrouper will run the internal plugin per workload object.
    - If all are successful, it will merge them: one podgroup object with the output of each plugin as a subgroup
    - If any plugin returned an error, or some other issue occurred - show it in the CRD's status
- Once a podgroup spec is successfully created - create it as a podgroup in the cluster
    - The owner of this podgroup will be the CRD, which is owned by the user - this is to help with garbage collection
- Pod-grouper continues to reconcile pods as usual. Changes in the owners' spec should update the relevant subgroup

### Proposed API

The names below are provisional. Workloads are listed explicitly so membership is closed and the controller can report which member is missing or invalid. All references are local to the `WorkloadGang` namespace because a PodGroup cannot contain pods from multiple namespaces.

```yaml
apiVersion: scheduling.run.ai/v1alpha1
kind: WorkloadGang
metadata:
  name: rl-post-training
  namespace: research
spec:
  members:
    - name: policy-trainer
      workloadRef:
        apiVersion: ray.io/v1
        kind: RayJob
        name: policy-trainer
    - name: rollout-workers
      workloadRef:
        apiVersion: ray.io/v1
        kind: RayCluster
        name: rollout-workers
    - name: reward-service
      workloadRef:
        apiVersion: apps/v1
        kind: Deployment
        name: reward-service

  # These values are applied to the generated root PodGroup and override
  # values inferred independently by the workload-specific groupers.
  schedulingPolicy:
    queue: research
    priorityClassName: train
    preemptibility: non-preemptible
    preemptionDelay: 5m
    stalenessGracePeriod: 10m
```

Each `members` entry becomes one direct child of the generated PodGroup. The entry's `name` is the prefix for that workload's generated SubGroups, preventing collisions when two grouper plugins produce the same SubGroup names. The generated PodGroup requires all direct workload SubGroups, preserving each workload's internal `minMember`, `minSubGroup`, and topology constraints below its parent.

Each referenced top owner also carries a membership annotation. The reference is authoritative; the annotation lets the pod-grouper suppress standalone grouping and route pods before every member is available. An owner reference must not be used for membership because it would give the `WorkloadGang` garbage-collection control over the workload.

```yaml
metadata:
  annotations:
    kai.scheduler/workload-gang: rl-post-training
    kai.scheduler/workload-gang-member: policy-trainer
```

The CRD reports both aggregate and per-workload reconciliation state. An illustrative status after successful grouping is:

```yaml
status:
  observedGeneration: 1
  podGroupRef:
    apiVersion: scheduling.run.ai/v2alpha2
    kind: PodGroup
    name: rl-post-training
    uid: 102884d1-6c33-41d7-934c-c618eee595a9
  memberStatuses:
    - name: policy-trainer
      uid: 9b41aa46-18ab-4b33-b804-5997c0e05013
      state: Grouped
      grouper: ray
      rootSubGroup: policy-trainer
    - name: rollout-workers
      uid: 80947a48-a889-4634-a459-5b7c810aa85f
      state: Grouped
      grouper: ray
      rootSubGroup: rollout-workers
    - name: reward-service
      uid: 0fbc89fa-5e4f-4214-bb66-af411453a7db
      state: Grouped
      grouper: deployment
      rootSubGroup: reward-service
  conditions:
    - type: MembersResolved
      status: "True"
      observedGeneration: 1
      reason: AllMembersResolved
      message: All referenced workloads exist and have supported groupers
      lastTransitionTime: "2026-08-10T08:00:00Z"
    - type: PodGroupCreated
      status: "True"
      observedGeneration: 1
      reason: PodGroupCreated
      message: Generated PodGroup research/rl-post-training
      lastTransitionTime: "2026-08-10T08:00:01Z"
```

`podGroupRef` is a status field rather than a spec field because the generated PodGroup is an implementation detail owned by the `WorkloadGang`. The controller sets the CRD as the PodGroup's controller owner reference so deleting the CRD cleans up only the generated PodGroup, not the referenced workloads.


## Constraints
- Grouping should be queue-scoped: workloads in different queues cannot be grouped together
- Priority and preemptiblity: different workloads have different default priority and preemptiblity values, and they can all be overridden by a user using annotations. The preemptibility can be inferred by the podgrouper (if all sub-workloads have the same value we can just use that. If they differ, use semi-preemptible)
