# Semi-Preemptible Mode

## Overview

In v0.10 we separated Priority and Preemption to allow users to control the two parameters independently, where Preemption has 2 modes (values) - **preemptible** and **non-preemptible**.

We want to add a new 3rd mode, named **semi-preemptible**, where the podgroup will be non-preemptible up to the `min-members`, and any extra pods are "elastic" pods and preemptible.

## Use Cases

This value means a workload with `minReplicas` such as Inference and Elastic Distributed Training can request to be non-preemptible up to its `minReplicas` and then other pods above `min-replicas` are preemptible. This allows to run critical workload with some assured resources and some on-demand and availability based.

## Quota Requirements

The `min-replicas` must be in-quota when allocated. Any "extra" pods can be allocated over-quota. All the pods must respect the Limit setting for the job's queue.
With this requirements, we can see that: 
- A semi-preemptible podgroup where the amount of pods equal to minMember == non-preemptible podgroup
- A semi-preemptible podgroup where minMember is set to 0 == preemptible podgroup

## Subgroups

Subgroups inherit the preemption mode from the podGroup - For example, if the podgroup is **semi-preemptible**, then all the subgroups are **semi-preemptible**.

Under the current implementation, the `minMember` behavior is relevant only for the leaf subgroups and the top podgroup (calculated based on the pod sum). If the top `minMember` equals to the sum of the leaf `minMembers`, all the requirements will remain satisfied under a **semi-preemptible** mode.

For podgroups without any subgroups, we are still dependant on the pod ordering plugin in the scheduler, just like before.

## Simulation Considerations

In simulations, I would consider "possible victims" only the last `n-m` ("the extra") pods. This approach might miss some solutions, but I don't think that checking all $\binom{n}{m}$ options is important enough to make the code less readable and more complicated.

## Implementation Notes

- **Over-quota checks are different**: base in quota, extra can be over-quota
- **For podgroup and queue statuses**: consider only the `min-member` resources for the non-preemptible counting. Like the scheduler, they should know the pod order to know which pods are considered "core", and which pods are "extra".
- "Fully preemptible" representative job for solver simulations containing the "extra" pods of a semi-preemptible job.

