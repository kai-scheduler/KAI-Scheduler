# Node Assignments

*Status: Proposed*

Related issues: [#2464](https://github.com/kai-scheduler/KAI-Scheduler/issues/2464)

## Summary

Node assignments aim to provide better guarantees for queues. Today, while the deserved and fair share resources are a strong guarantee the scheduler attempts to provide for queues, fragmentation often makes that difficult or impossible: consider a 2-node, 8-gpu-each cluster, with two queues with 8 gpu quota - while both queues expect to be able to run an 8-gpu non-preemptible job, it's possible (and, in some cases, likely) to get to a situation where a non-preemptible 1-gpu pods of one queue runs on each of the node, preventing the other queue from running a full-node job.

Node assignments let admins assign nodes to queues. Once a node is assigned, the owning queue's pods will prefer it, foreign pods will prefer to not run on it, and foreign, non-preemptible pods will not be allowed on it. This guarantees that this node's resources will always be available to it's queue.

Admins do not have to assign every node, and not every queue has to own nodes.

## Motivation

While the quota guarantee by the scheduler is useful, it's still mainly a nominal resource guarantee - an 8 GPU quota guarantees the scheduler will find 8 GPUs, but it doesn't guarantee their shape - 2*4, 6+2, or a full 8-GPU node. Clusters get naturally fragmented over time, and large jobs especially get hurt: the larger a job is, the more likely it is to be unschedulable due to fragmentation. 

One especially important use case are "hero jobs": while not a clearly defined term in the industry, they generally refer to large, high-priority, long-running jobs. Admins would want to guarantee their capacity whenever they require it, but today's scheduler logic cannot always provide that guarantee without human intervention, when a reclaim scenario isn't possible.

## Goals

- Let cluster admins assign nodes to queues which will stay non-fragmented
- Assign nodes stay clean of non-preemptible squatter pods

## Non-Goals

- (At the kai level): assert a "good" division of the cluster
  - At this point, we do not aim to enforce the assignment of nodes in the cluster - over-provisioned nodes, under-provisioned queues, leftover squatter pods, etc.

## User Stories

<!--
Optional.
Short scenarios that show who needs this and what they do with it.
Useful for user-facing features; often unnecessary for internal refactors.
-->

### Story 1: <title>

## Background

<!--
Optional.
Existing KAI or Kubernetes behavior the reader needs before the design makes sense (e.g. how kubelet decides X, how the current action does Y).
Link to code or upstream docs instead of restating them at length.
-->

## Design

Provide a new API: a node label `kai.scheduler/queue` (key is configurable) that assigns a node to a queue. We will refer to this as a queue "owning" a node or a node being "assigned" to a queue. In the context of a owned node, we will refer to pods from its owner queue as "native" and to pods from other queues as "foreign".

Native pods will score owned nodes higher than others, and will score foreign nodes lower. Non-preemptible pods will not be allowed to run on foreign nodes at all ("squat").

These guarantee that a reclaim solution is always possible in the domain of the assigned nodes. 

The global single-label key, like in scheduling shards, guarantee that each node is not assigned to more than one queue (if at all). At this point, we do not aim to add further assertions over that: for example, an admin could theoretically assign all nodes to a single queue, thus preventing all others from running non-preemptible jobs. Preventing misconfigurations like that will be left for admin discresion or for more opinionated projects like KRM.

As a general rule, the sum of resources of unassigned nodes should be at least the sum of queues' resources which are not satisfied by assigned nodes, to ensure that queues that don't own nodes can run their non-preemptible jobs on this unassigned space.

In some cases, admins might want to over-provision node assignemts to queues: for example, a hero queue that's meant to run critical high-profile jobs on a large number of jobs might need 1-2 spare nodes assigned to it, to ensure that even in cases of unavailable nodes due to maintenance, it's quota will be available.

### Squatter pods

It is theoretically possible for an admin to change a node's assignment while non-preemptible pods are running on it, causing non-preemptible "squatters" to remain on the node. At this stage, we will not attempt to forcibly evict them, but report these cases as warnings.

### Solver

In large-scale clusters, it could make sense to implement a Scenario Generator that will take into account owned nodes - attempt to start finding a solution in the assigned-nodes space. This could potentially significantly improve performance and even prevent cases where the usual solvers miss a valid reclaim scenario. We will leave this to a separate design.

### API

The only new API surface is the assignment label: `kai.scheduler/queue` on nodes. At this stage, nodes can only be assigned to leaf queues.

### Observability

Potential interesting metrics:
- The queue controller can publish the total assigned resources per queue to help admins ensure the cluster is properly configured.
- Indicating squatter pods somehow could be useful - maybe as a metric per pod, distinguishing preemptible and non-preemptible pods

### Overriding fairness

While the described implementation greatly strengthens the guarantees, there could be cases where a preemptible, in-quota foreign pods are squatting on an assigned node - either because of fragmentation in the unassigned node space, or unavailable nodes, or misconfigurations. An open question is wether we want to allow quota guarantees to be overridden in these cases.

## Alternatives Considered

<!--
Other approaches and why they were not chosen.
Recording rejected options saves reviewers from re-proposing them.
Some designs list these as "Design Decisions" with the reasoning for each choice instead.
-->

## Risks and Known Limitations

- Due to the lack of safeguards, it's the admin's responsibility to configure the cluster "correctly", and avoid misconfigurations, for example - not leaving enough unassigned nodes to handle the over-quota resources in the cluster
- Misconfigurations should be indicated clearly to the admin when possible - for example, alerting on non-preemptible squatter pods
- Semi-preemptible jobs will be considered non-preemptible for the purposes of this feature, since the "core pods" set can change and cause squatter pods


## Test Plan

<!-- Unit, integration (envtest), and e2e coverage; scale or benchmark tests if performance is a concern. -->

## Implementation Plan

At this stage, the feature will be implemented in a separate plugin, turned off by default, and will be considered an alpha feature, until we settle the open questions and depending on user feedback.
<!--
Optional.
Phases or milestones if the work ships in pieces, with what each phase delivers on its own.
-->

## Open Questions

As described in the doc:

- Do we want to allow non-preemptible squatter pods to be evicted, even when non-preemptible?
- Do we want preemptible squatter pods to be evicted, even when in-quota?

## Future Work

As described in the doc:

- A dedicated solver for assigned nodes

### Full-hierarchy assignments

A useful possible generalization of the feature could be hierarchical assignments: for example, assign some nodes to a parent queue, and some of those could be assigned to child queues. This could allow admins to provide a sophisticated layered guarantee do different levels of hierarchy, but greatly increases the complexity of the feature, mainly on the API and admin burden side. At this stage, we will leave this as potential future work, and the scheduler will only consider leaf-queue assignments.

## References

<!-- Optional. Upstream KEPs, related designs in this directory, external projects, discussions. -->
