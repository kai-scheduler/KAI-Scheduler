# Multi-Level Topology Aware Scheduling

## Overview
Modern distributed workloads often have heterogeneous topology requirements. For instance, large-scale AI inference pipelines, data analytics workflows, or disaggregated microservices may consist of multiple components (or *roles*), each with different locality and resource needs.
The Multi-Level Topology Aware Scheduling mechanism enables fine-grained topology control for such workloads by allowing each SubGroup of a workload to specify its own topology constraints while still adhering to scheduling thresholds defined at each hierarchy level.
This is achieved through an extended PodGroup API that supports nested SubGroups, subgroup-level gang thresholds, and multi-level topology hierarchies.

---

## Key Concepts

### PodGroup
A PodGroup represents a logical collection of pods that should be scheduled in a coordinated manner. It enables gang scheduling semantics, ensuring that the configured minimum threshold is available before the workload starts.
In addition, it allows control over the placement of all the pods within the workload by specifying topology constraints.

A PodGroup can express the threshold in one of two mutually exclusive ways:
- `minMember`: Minimum number of pods required for flat PodGroups.
- `minSubGroup`: Minimum number of direct child SubGroups required for hierarchical or replica-based PodGroups.

### SubGroups
A PodGroup can contain one or more SubGroups, representing logical subsets of pods that share a common role or function within the workload.
Each SubGroup can define:
- A `minMember` value to control gang scheduling at leaf SubGroups.
- A `minSubGroup` value to control how many direct child SubGroups are required at mid-level SubGroups. When `minSubGroup` is omitted, all direct child SubGroups are required.
- A `topologyConstraint` to specify how pods or SubGroups at the lower level should be co-located in the cluster.
- An optional `parent` to establish hierarchical or cross-subgroup relationships.

Pods are assigned only to leaf SubGroups using the `kai.scheduler/subgroup-name` label.

### Topology Constraints
The `topologyConstraint` section defines where and how pods should be placed relative to cluster topology.  
This typically refers to topology domains such as:
- Rack
- Spine
- Zone
- Region

A topology constraint includes:
- `topology` — A reference to the cluster topology resource.
- `requiredTopologyLevel` — The level within the topology hierarchy that pods in the subgroup must share.
- `preferredTopologyLevel` — The level within the topology hierarchy that pods in the subgroup would prefer to share if possible.

---

## Example: Independent Subgroup Constraints
The following example defines a PodGroup with two independent SubGroups (`subgroup-a` and `subgroup-b`).

Each SubGroup:
- Has its own `minMember` requirement for gang scheduling.
- Requires all its pods to be scheduled on the same rack, defined by the topology key `topology/rack`.

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  name: sample1
spec:
  queue: test
  priorityClassName: inference
  minSubGroup: 2
  subGroups:
    - name: subgroup-a
      minMember: 2
      topologyConstraint:
        topology: "cluster-topology"
        requiredTopologyLevel: "topology/rack"
    - name: subgroup-b
      minMember: 3
      topologyConstraint:
        topology: "cluster-topology"
        requiredTopologyLevel: "topology/rack"
```

The desired scheduling result is that both SubGroups are required, and pods of each SubGroup are scheduled on the same rack, possibly on different racks for each SubGroup.

### Example Pods
The following pod examples reference the sample1 PodGroup and are associated with their respective SubGroups:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-a1
  annotations:
    pod-group-name: sample1
  labels:
    kai.scheduler/subgroup-name: subgroup-a
spec:
  containers:
    - name: worker
      image: ubuntu
      command: ["sleep", "infinity"]

---

apiVersion: v1
kind: Pod
metadata:
  name: pod-b1
  annotations:
    pod-group-name: sample1
  labels:
    kai.scheduler/subgroup-name: subgroup-b
spec:
  containers:
    - name: worker
      image: ubuntu
      command: ["sleep", "infinity"]
```

---

## Advanced Example: Cross-SubGroup and Hierarchical Constraints
More complex scheduling scenarios require coordination across SubGroups to enforce locality at multiple topology levels.
For example:
- The entire workload is scheduled within a single zone.
- SubGroups A and B must be co-located under the same spine (assuming spine is a higher-level topology domain).
- Each leaf SubGroup must still ensure its pods are placed within the same rack.
- Another SubGroup (C) can operate independently under a different rack-level constraint.

The example below illustrates this configuration:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  name: sample2
spec:
  queue: test
  priorityClassName: inference
  minSubGroup: 2
  topologyConstraint:
    topology: "cluster-topology"
    requiredTopologyLevel: "topology/zone"
  subGroups:
    - name: subgroup-ab
      minSubGroup: 2
      topologyConstraint:
        topology: "cluster-topology"
        requiredTopologyLevel: "topology/spine"
    - name: subgroup-a
      minMember: 1
      parent: subgroup-ab
      topologyConstraint:
        topology: "cluster-topology"
        requiredTopologyLevel: "topology/rack"
    - name: subgroup-b
      minMember: 2
      parent: subgroup-ab
      topologyConstraint:
        topology: "cluster-topology"
        requiredTopologyLevel: "topology/rack"
    - name: subgroup-c
      minMember: 3
      topologyConstraint:
        topology: "cluster-topology"
        requiredTopologyLevel: "topology/rack"
```

### Example Pods for Hierarchical PodGroup

The following example definitions demonstrate how pods are associated with their respective subgroups within the `sample2` PodGroup.
Each pod specifies:
- The annotation `pod-group-name` to associate with the PodGroup.
- The label `kai.scheduler/subgroup-name` to specify the subgroup membership.

The scheduler activates placement once the PodGroup and SubGroup thresholds are met. In `sample2`, `minSubGroup: 2` at the PodGroup level requires both direct children (`subgroup-ab` and `subgroup-c`), and `minSubGroup: 2` on `subgroup-ab` requires both `subgroup-a` and `subgroup-b`.

```yaml
# Subgroup A
apiVersion: v1
kind: Pod
metadata:
  name: pod-a1
  annotations:
    pod-group-name: sample2
  labels:
    kai.scheduler/subgroup-name: subgroup-a
spec:
  containers:
    - name: worker
      image: ubuntu
      command: ["sleep", "infinity"]

---

# Subgroup B
apiVersion: v1
kind: Pod
metadata:
  name: pod-b1
  annotations:
    pod-group-name: sample2
  labels:
    kai.scheduler/subgroup-name: subgroup-b
spec:
  containers:
    - name: worker
      image: ubuntu
      command: ["sleep", "infinity"]

---

# Subgroup C
apiVersion: v1
kind: Pod
metadata:
  name: pod-c1
  annotations:
    pod-group-name: sample2
  labels:
    kai.scheduler/subgroup-name: subgroup-c
spec:
  containers:
    - name: worker
      image: ubuntu
      command: ["sleep", "infinity"]
```
