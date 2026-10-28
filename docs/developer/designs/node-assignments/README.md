# Node Assignments

*Status: Proposed*

Related issues: [#2464](https://github.com/kai-scheduler/KAI-Scheduler/issues/2464) (design), [#2465](https://github.com/kai-scheduler/KAI-Scheduler/issues/2465) (implementation)

## Summary

Node assignments aim to provide better guarantees for queues.
Today, while the deserved and fair share resources are a strong guarantee the scheduler attempts to provide for queues, fragmentation often makes that difficult or impossible.
Consider a cluster of two 8-GPU nodes and two queues with an 8-GPU quota each: both queues expect to be able to run an 8-GPU non-preemptible job, but it's possible (and, in some cases, likely) that a non-preemptible 1-GPU pod of one queue runs on each node, preventing the other queue from running a full-node job.

Node assignments let admins assign nodes to queues.
Once a node is assigned, the owning queue's pods will prefer it, foreign pods will prefer not to run on it, and foreign non-preemptible pods will not be allowed on it.
This guarantees that the node's resources will be available to its queue (see [Risks and Known Limitations](#risks-and-known-limitations) for the cases where the guarantee is weaker).

Admins do not have to assign every node, and not every queue has to own nodes.

## Motivation

While the quota guarantee by the scheduler is useful, it's still mainly a nominal resource guarantee: an 8-GPU quota guarantees the scheduler will find 8 GPUs, but it doesn't guarantee their shape (for example, 2x4, 6+2, or a full 8-GPU node).
Clusters naturally get fragmented over time, and large jobs get hurt the most: the larger a job is, the more likely it is to be unschedulable due to fragmentation.
Reclaim cannot always fix this: when the free capacity is spread across nodes, or held by non-preemptible pods, there may be no valid reclaim scenario even though the queue is below its quota ([#2285](https://github.com/kai-scheduler/KAI-Scheduler/issues/2285)).

One especially important use case is "hero jobs".
While not a clearly defined term in the industry, it generally refers to large, high-priority, long-running jobs, such as pretraining a flagship model.
Hero jobs are usually planned ahead rather than urgent: they are tested at smaller scales, their capacity is planned in advance, and they are closely monitored.
Admins want to guarantee their capacity whenever they require it, but today's scheduler logic cannot always provide that guarantee without human intervention when a reclaim scenario isn't possible.

## Goals

- Let cluster admins assign nodes to queues, keeping those nodes unfragmented for their owning queues
- Keep assigned nodes clean of foreign non-preemptible pods
- Keep assigned nodes usable by other queues while their owner doesn't need them
- Keep the assignment "soft" - allow non-preemptible foreign pods to use assigned resources opportunistically, when possible

## Non-Goals

- Asserting a "good" division of the cluster at the KAI level
  - We do not aim to validate or enforce the assignment of nodes in the cluster: over-provisioned nodes, under-provisioned queues, leftover squatter pods, etc.
- Preventing a queue from fragmenting its own assigned nodes
- Changing how quota, deserved share or fair share are calculated

## User Stories

### Story 1: Hero job

An organization plans a multi-week pretraining run on 64 full nodes, submitted to a dedicated `pretraining` queue with a 64-node quota.
Until the run starts, and between its stages, the rest of the organization should be able to use those nodes.
The admin labels 66 nodes with `kai.scheduler/assigned-queue=pretraining`, keeping two spare nodes for maintenance and node failures.
In the meantime, other queues borrow these nodes only with preemptible pods, and only when the unassigned nodes are full.
When the run is submitted, it reclaims the borrowed pods and starts, without an admin having to drain nodes or evict non-preemptible jobs by hand.

### Story 2: Full-node inference replicas

An inference team runs 8-GPU replicas that must not be preempted, next to research teams that submit many small, non-preemptible jobs.
The admin assigns enough full nodes to the inference queue for its quota, so a scale-up always finds whole free nodes, while research teams can still run preemptible work on them when inference traffic is low.

### Story 3: Shared ownership

Several teams fund parts of a shared cluster.
Each team's queue owns the nodes it paid for, and unassigned nodes form a shared pool.
Teams can always run non-preemptible work on their own nodes, and any team can borrow idle nodes from others with preemptible work.

## Background

- **Preemptibility**: a workload is preemptible or non-preemptible according to its priority class or its explicit preemptibility setting.
  See [Priority](../../../priority/README.md) and [Priority and Preemptibility Separation](../priority-preemptibility-separation/README.md).
  [Semi-preemptible](../semi-preemptible/README.md) workloads have a non-preemptible core and preemptible extra pods.
- **Reclaim**: a queue below its fair share can evict over-fair-share preemptible workloads of other queues.
  In-quota workloads are protected unless `queuePriorityInQuotaReclaim` is enabled.
  See [Fairness](../../../fairness/README.md).
- **Scheduling shards**: nodes and queues can be partitioned between scheduler instances by a label.
  Shards are hard partitions: idle capacity in one shard cannot be used by queues of another.
  See [Scheduling Shards](../../../operator/scheduling-shards.md).
- **Predicates**: plugins register node filters with `AddPredicateFn`, and `Session.PredicateFn` ([`session_plugins.go`](../../../../pkg/scheduler/framework/session_plugins.go)) runs them on every node a task is placed on, including the simulations of preempt, reclaim and consolidation.
  A failing predicate rejects the node, and its error is reported as one of the pod's fit errors.
- **Node scoring**: plugins score nodes in fixed tiers ([`scores.go`](../../../../pkg/scheduler/plugins/scores/scores.go)), so a plugin's weight decides which preferences it can override.

## Design

Provide a new API: a node label `kai.scheduler/assigned-queue` (key is configurable) that assigns a node to a queue.
We will refer to this as a queue "owning" a node, or a node being "assigned" to a queue.
For an owned node, pods of the owning queue are "native" and pods of other queues are "foreign".
Foreign pods running on an owned node are "squatters".

The rules:

- **Scoring**: pods prefer nodes in this order:
  1. Nodes owned by the pod's queue
  2. Unassigned nodes
  3. Nodes owned by other queues
- **Filtering**: foreign non-preemptible pods are not allowed on owned nodes.

These guarantee that a reclaim solution is always possible in the domain of the assigned nodes (see [Risks and Known Limitations](#risks-and-known-limitations)).

The single global label key, like in scheduling shards, guarantees that each node is assigned to at most one queue.
At this point, we do not aim to add further assertions over that: for example, an admin could theoretically assign all nodes to a single queue, thus preventing all others from running non-preemptible jobs.
Preventing misconfigurations like that will be left to admin discretion or to more opinionated, high level projects like [KRM](https://github.com/kai-scheduler/kai-resource-management).

As a general rule, the sum of resources of unassigned nodes should be at least the sum of queue quotas that are not covered by assigned nodes, so that queues that don't own nodes can run their non-preemptible jobs in the unassigned space.

In some cases, admins might want to over-provision node assignments: for example, a hero queue that's meant to run critical, high-profile jobs on a large number of nodes might need 1-2 spare assigned nodes, so that its quota is still available when some nodes are unavailable due to maintenance or failures.

### Which pods the rules apply to

The rules apply to all pods the same way, whether they request whole GPUs, GPU fractions, or only CPU: any of them can fragment a node.
Semi-preemptible workloads are treated as non-preemptible (see [Risks and Known Limitations](#risks-and-known-limitations)).

### Invalid assignments

A label is ignored, with a warning in the scheduler log, when its value is:

- A queue that does not exist
- A non-leaf queue
- A queue in a different scheduling shard than the node

Whether invalid labels should be ignored at all is an [open question](#open-questions).

### Squatter pods

It is possible for an admin to change a node's assignment while non-preemptible pods are running on it, leaving non-preemptible squatters on the node.
We could either be conservative and ignore them (maybe add a warning in logs), or we could decide to give more weight to the admin's intent on node assignments, and make them eligible for preemptions/reclaims. My proposal is to start conservative and add a more aggressive (configurable?) behavior based on user feedback.

### Solver

Especially for large-scale clusters, it could make sense to implement a scenario generator that takes owned nodes into account and starts searching for a solution in the assigned-nodes space.
This could significantly improve performance and even prevent cases where the usual generators miss a valid reclaim scenario.
It is related to the `FullNodeFirst` generator proposed in [#1790](https://github.com/kai-scheduler/KAI-Scheduler/issues/1790).
We will leave this to a separate design.

### API

The only new API surface is the assignment label, `kai.scheduler/assigned-queue`, on nodes.
At this stage, nodes can only be assigned to leaf queues.
The label key is configurable in the scheduler configuration.

### Observability

- The queue controller can publish the total assigned resources per queue, to help admins check the cluster is configured properly.
  - Total assigned resources? Node count?
- Squatter pods are reported, distinguishing preemptible from non-preemptible squatters, for example as a metric per queue and node.
  - Does in make sense to publish events on squatters? 
- Pods rejected by the filtering rule get a clear reason in their scheduling condition, for example "node is assigned to queue X; non-preemptible pods of other queues are not allowed".
- Invalid assignment labels are reported as warnings in the scheduler log.

### Overriding fairness

While the described implementation greatly strengthens the guarantees, there could be cases where preemptible, in-quota foreign pods squat on an assigned node, either because of fragmentation in the unassigned node space, unavailable nodes, or misconfigurations.
An open question is whether we want to allow quota guarantees to be overridden in these cases.

## Alternatives Considered / Adjacent Proposals

- **Taints, tolerations and node affinity**: admins can taint a queue's nodes and add tolerations and affinity to its workloads.
  This dedicates the nodes completely, so their capacity is idle when the owner doesn't use them, and it requires every workload (or an admission webhook) to carry the right tolerations.
  It is still a useful recipe to document for users who want hard dedication.
- **Scheduling shards (node pools) per queue**: also a hard partition, with the same idle-capacity problem. In this proposal, we will allow non-preemptible workloads to get scheduled across assigned and non-assigned nodes.
- **Queue-level node selector** ([#133](https://github.com/kai-scheduler/KAI-Scheduler/issues/133), [#1723](https://github.com/kai-scheduler/KAI-Scheduler/issues/1723)): restricts where a queue's pods run, but does not keep other queues' non-preemptible pods off those nodes.
- **Periodic defragmentation** ([#1311](https://github.com/kai-scheduler/KAI-Scheduler/issues/1311), [#1500](https://github.com/kai-scheduler/KAI-Scheduler/issues/1500)): increases the chance of free full nodes but guarantees nothing, and cannot move non-preemptible pods.
- **Resource reservation and backfill** ([#1969](https://github.com/kai-scheduler/KAI-Scheduler/issues/1969), [#1987](https://github.com/kai-scheduler/KAI-Scheduler/issues/1987), [#2348](https://github.com/kai-scheduler/KAI-Scheduler/issues/2348)): holds capacity for a specific pending or planned job.
  It addresses starvation of large jobs in general, but depends on jobs declaring an expected run time, and leaves reserved capacity idle unless it is backfilled.
  It is complementary to node assignments rather than a replacement.

## Risks and Known Limitations

- Due to the (intentional) lack of safeguards, it's the admin's responsibility to configure the cluster correctly and avoid misconfigurations, for example not leaving enough unassigned nodes for the other queues' non-preemptible workloads.
- Misconfigurations should be indicated clearly to the admin when possible, for example by alerting on non-preemptible squatter pods.
- Semi-preemptible jobs will be considered non-preemptible for the purposes of this feature, since their core pod set can change and cause non-preemptible squatter pods.
- The owning queue can still fragment its own nodes with non-preemptible native pods: node assignments do not attempt to prevent this kind of fragmentation.
- Preemptible squatters are only evicted under the usual reclaim rules: the owning queue must be eligible to reclaim, and in-quota squatters are protected (see [Overriding fairness](#overriding-fairness)).
- Squatters protected by [min-runtime](../../../plugins/minruntime.md) or preemption delay delay the native job until their protection ends.
- When an owned node is unavailable (failure, maintenance), the owning queue has fewer resources on its owned nodes, and the scheduler does not assign other nodes as a replacement.
  It is up to the admin to decide whether to over-assign nodes to a queue for redundancy, weighing that against other considerations, like leaving enough unassigned nodes for other queues.
- Jobs with topology constraints only benefit if enough owned nodes are in the same topology domain; admins should assign nodes accordingly.

## Test Plan

- Unit tests for the plugin: scoring order, filtering rule, invalid labels, semi-preemptible jobs.
- Action tests (allocate, reclaim, preempt, consolidation) with owned nodes and squatters, including the fragmentation scenario from the [Summary](#summary).
- E2E test: queues fill unassigned nodes, preemptible pods borrow owned nodes, a foreign non-preemptible pod stays pending, and a large native job reclaims the borrowed pods.
- A scale-test scenario based on the existing hero-job reclaim scenario ([#1981](https://github.com/kai-scheduler/KAI-Scheduler/issues/1981)).

## Implementation Plan

At this stage, the feature will be implemented in a separate plugin, turned off by default, and will be considered an alpha feature until we settle the open questions, depending on user feedback.

1. Plugin: label parsing and validation, scoring, filtering, and scheduling reasons
2. Observability: assigned resources per queue, squatter metrics
3. Documentation, including the taints and tolerations recipe for hard dedication
4. Dedicated solver (separate design)

## Open Questions

- Do we want to allow non-preemptible squatter pods to be evicted?
- Do we want preemptible squatter pods to be evicted even when in-quota?
- What about min-runtime guarantees? Do we consider protected pods as non-preemptible?
- How significant should the node scoring be compared to other plugins?
  For example, should it override topology preferences, or only bin-packing and spreading?
- Should reclaim prefer squatters as victims over pods running on unassigned nodes?
- Should nodes with [invalid assignments](#invalid-assignments) still be treated as owned, instead of ignoring the label?
  For example, a node may be assigned to a queue that doesn't exist yet but will be created shortly; ignoring the label lets foreign non-preemptible pods land on the node in the meantime, fragmenting it before its queue exists.

## Future Work

- A dedicated solver for assigned nodes
- Configurable behavior toward "protected" squatter pods (min runtime, non-preemptible, or in-quota pods)

### Full-hierarchy assignments

A useful possible generalization of the feature is hierarchical assignments: for example, assigning some nodes to a parent queue, and some of those to its child queues.
This could allow admins to provide layered guarantees at different levels of the hierarchy, but greatly increases the complexity of the feature, mainly on the API and admin burden side.
At this stage, we will leave this as potential future work, and the scheduler will only consider leaf-queue assignments.
