# Karpenter Interaction

KAI Scheduler can run on clusters that use Karpenter for node provisioning and disruption.
The main interaction points are GPU-sharing scale-up and Karpenter node disruption.

For the general KAI autoscaling behavior, see [Cluster Autoscaling](./README.md).
That document explains the same behavior used with the Kubernetes Cluster Autoscaler:
GPU-sharing requests are stored in annotations, so they are not directly visible as normal GPU requests in the pod spec.
When GPU-sharing pods are unschedulable, KAI's `node-scale-adjuster` creates temporary utility pods that request full GPUs and allow the cluster autoscaler to provision suitable nodes.

## Gpu Fractional Pods

For fractional GPU workloads, KAI creates GPU reservation pods in the `kai-resource-reservation` namespace.
These reservation pods have a normal `nvidia.com/gpu` request and are used by KAI to "reserve" gpus consumed by the fractional pods. They perevent double-booking of gpu devices. KAI annotates these reservation pods with Karpenter's `karpenter.sh/do-not-disrupt` annotation.

The annotation prevents Karpenter from disrupting nodes that host those pods in most voluntary disruption flows.
Karpenter's disruption documentation states that pods that cannot be evicted cause Karpenter to ignore the node and try later during candidate selection.
While there are exceptions, in all of them all of the pods are removed from the node. This means that we won't reach a case where there are reservation pods without their matching fractional pods nither there will be fracrtional pods without the reservation pods.

The exceptions are:
* A configured NodePool `terminationGracePeriod` can eventually allow disruption to proceed even when pods have `karpenter.sh/do-not-disrupt`.
* Forceful disruption paths, such as some interruption or node-repair cases, are not equivalent to ordinary consolidation decisions.
* If the node is already being terminated, Kubernetes and Karpenter termination behavior can still delete pods as part of node shutdown.

See Karpenter's [`TerminationGracePeriod`](https://karpenter.sh/docs/concepts/disruption/#terminationgraceperiod) and [`Disruption Controller`](https://karpenter.sh/docs/concepts/disruption/#disruption-controller) documentation for the exact Karpenter-side behavior.

## Scheduling And Binding Ownership

Karpenter does not schedule KAI workload pods and does not bind KAI reservation pods.
Its disruption controller uses scheduling simulation to decide whether pods from a candidate node can run elsewhere, and whether replacement nodes are required.
After that, Karpenter taints the candidate node, creates replacement NodeClaims when needed, and deletes the disrupted NodeClaim or node.

Actual pod placement is still owned by the scheduler and binder path:

* Karpenter models whether capacity can exist for the pods.
* KAI Scheduler chooses the node for KAI-managed workloads.
* KAI Binder creates and binds fractional GPU reservation pods and then binds the workload pod.

This distinction matters when debugging incidents.
If a reservation pod is observed on a node, that placement came from KAI's binding flow, not from Karpenter setting `spec.nodeName` on the pod.
Karpenter can indirectly cause movement by disrupting nodes and creating replacement capacity, but it is not the component that assigns KAI pods to nodes.
