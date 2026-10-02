# Background Pods

Background pods are maintenance and infrastructure workloads a cluster operator has to run but that should never get in a user's way: node health checks, device diagnostics, monitoring agents, etc.
They occupy real resources, so without special handling the scheduler treats the nodes they sit on as partly full, and that capacity is effectively lost to user workloads.

Marking such a workload as *background* tells KAI to plan as if it were not there.
Its capacity counts as available, and it is evicted on demand when a user workload is placed on top of it.

## When to use it

Use it for pods that should not interfere with real user workloads, usually non-business pods like healthcheck and maintenance pods, that are needed for cluster operations but need to stay out of the way from value-creating pods, such as AI training and inference.
The scheduler will perform its scheduling decisions as if they weren't there, and evict any background pods necessary to perform the allocations.

### When not to use it

Do not use it for anything a user depends on being alive.
It's not recommended for anything whose restart is expensive.
KAI will not try to minimize the number of evictions for background pods, to avoid complex scenario simulations.

## How it works

At the start of every scheduling cycle the scheduler evicts background pods from its in-memory view of the cluster.
Placement decisions, including topology domain selection and bin-packing, are then computed against nodes that look as empty as they would be without the maintenance workloads.
At the end of the cycle each background pod is offered its place back.
Those that still fit stay exactly where they were and are not disturbed.
Those whose capacity was taken are evicted in the cluster.

A user workload that lands on capacity a background pod is holding waits for that eviction to complete, so it binds one scheduling cycle later.
A user workload that fits in capacity that was already free binds immediately and is unaffected.

## Setting up

### 1. Create a queue for the background workloads

Background pods still need a queue, because KAI only manages pods it scheduled.
Give it zero quota and zero over-quota weight so it doesn't claim a share of the cluster.

```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: maintenance
spec:
  resources:
    gpu:
      quota: 0
      overQuotaWeight: 0
      limit: -1
    cpu:
      quota: 0
      overQuotaWeight: 0
      limit: -1
    memory:
      quota: 0
      overQuotaWeight: 0
      limit: -1
```

`limit: -1` leaves the queue unbounded, so background pods are still placed while the cluster has room.

### 2. Mark the workload's pods

Four things go on the pod template.

```yaml
metadata:
  labels:
    kai.scheduler/background: "true"      # tells the scheduler to treat this pod as a background pod
    kai.scheduler/queue: maintenance      # the zero-quota queue
spec:
  schedulerName: kai-scheduler            # KAI must be the one scheduling it
  priorityClassName: train                # must be preemptible, see below
  terminationGracePeriodSeconds: 5        # keep short
```

`kai.scheduler/background: "true"` explicitly tells the plugin to treat this pod as a background pod, and virtually evict it at the beginning of a cycle.

`schedulerName: kai-scheduler` is required.
KAI can only evict a pod it scheduled, so a pod handled by another scheduler is ignored by this feature entirely.

The priority class must be **preemptible**, meaning a value below 100.
`train` (50) is the usual choice.
This matters because a non-preemptible workload may only run inside its queue's quota, and the maintenance queue's quota is zero, so a non-preemptible background pod would be rejected.
Deployments and Knative services default to `inference` (125), which is non-preemptible, so set the priority class explicitly rather than relying on the default.
See [workload priority](../priority/README.md) for the full list.

A short `terminationGracePeriodSeconds` is recommended because a user workload displacing a background pod waits for that pod to actually terminate before it binds.
Every second of grace period is a second the user's workload spends pending.
Make sure the container handles `SIGTERM` promptly.

### 3. Apply it

A node diagnostics agent, eight replicas, each holding one GPU:

```bash
kubectl apply -f maintenance-queue.yaml
kubectl apply -f healthcheck.yaml
```

Both manifests are in this directory.

## What to expect

Background pods do not appear in any queue's fair-share arithmetic while the scheduler is planning, so they cannot push a user's queue over its share or trigger reclaim.

When one is displaced, its PodGroup gets a normal eviction event:

```bash
kubectl get events --field-selector reason=Evict
```

The message is `Evicted to make room for a workload on this node`, and the event carries the action name `backgroundpods`, which distinguishes these evictions from preemption and reclaim.

A controller-owned background pod is recreated immediately after eviction.
It is placed wherever there is room, and stays `Pending` if there is none, since the maintenance queue gets nothing back when the cluster is full.
This is the intended steady state: maintenance work runs on whatever the users are not using.

All else being equal, the scheduler still prefers a node with no background pod on it, so it does not disturb one for no reason.
A stronger signal overrides that preference: a workload with a topology constraint takes the domain it needs and displaces the background pods there.

## Configuring the plugin

The feature is on by default and needs no configuration.
To change which pods it selects, set arguments on the `backgroundpods` plugin in the SchedulingShard:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: default
spec:
  plugins:
    backgroundpods:
      arguments:
        labelSelector: "workload-type in (healthcheck,diagnostics)"
```

`labelSelector` takes any Kubernetes label selector expression and replaces the default `kai.scheduler/background=true`.
An unparsable selector disables the plugin and logs an error rather than failing the scheduler.

To turn the feature off:

```yaml
spec:
  plugins:
    backgroundpods:
      enabled: false
```

## Limitations

**Dynamic resource allocation.** Deleting a pod does not synchronously free a `ResourceClaim`, so background pods holding DRA resources are not supported yet.

**Pods KAI does not schedule.** Anything without `schedulerName: kai-scheduler`, including most vendor and cloud-provider daemons, cannot be marked background today.

**Critical pods.** Do not label pods with `system-node-critical` or `system-cluster-critical` priority.
They are meant never to be evicted, which is the opposite of what this feature does.

**Anti-affinity between background pods.** Background pods are invisible to each other while the scheduler is planning, so a required pod anti-affinity among them is not enforced during placement and cannot be used to spread them one per node.
Use a DaemonSet when exactly one pod per node is a requirement, and reserve anti-affinity for keeping background pods away from user workloads.

## Verifying

Pick a node holding a background pod and submit a workload that needs the whole node.
The user workload should be scheduled onto that node, the background pod should get an `Evict` event, and its replacement should come back `Pending` or land elsewhere.
Without the feature the same submission leaves the user workload pending, or places it on a worse node.
