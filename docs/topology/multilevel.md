# Multi-Level Topology Aware Scheduling

## Overview
Modern distributed workloads often have heterogeneous topology requirements. For instance, large-scale AI inference pipelines, data analytics workflows, or disaggregated microservices may consist of multiple components (or *roles*), each with different locality and resource needs.
The Multi-Level Topology Aware Scheduling mechanism enables fine-grained topology control for such workloads by allowing each subgroup of a workload to specify its own topology constraints while still adhering to an overall scheduling policy defined at the workload level.
This is achieved through an extended PodGroup API that supports nested subgroups, cross-subgroup constraints, and multi-level topology hierarchies.

---

## Key Concepts

### PodGroup
A PodGroup represents a logical collection of pods that should be scheduled in a coordinated manner. It enables gang scheduling semantics, ensuring that a defined minimum number of pods (`minMember`) are gang-scheduled before any are launched.
In addition, it allows control over the placement of all the pods within the workload by specifying topology constraints.

### Subgroups
A PodGroup can contain one or more subgroup, representing logical subsets of pods that share a common role or function within the workload.
Each subgroup can define:
- A `minMember` value to control gang scheduling at the subgroup level (on the lowest level of the subgroup hierarchy).
- A `topologyConstraint` to specify how pods or subgroups at the lower level that are associated with this subgroup should be co-located in the cluster.
- An optional `parent` to establish hierarchical or cross-subgroup relationships.

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
The following example defines a PodGroup with two independent subgroups (`subgroup-a` and `subgroup-b`).

Each subgroup:
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
  subgroups:
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

The desired scheduling result is that pods of each subgroup are scheduled on the same rack, possibly on different racks for each subgroup.

### Example Pods
The following pod examples reference the sample1 PodGroup and are associated with their respective subgroups:

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

## Advanced Example: Cross-Subgroup and Hierarchical Constraints
More complex scheduling scenarios require coordination across subgroups to enforce locality at multiple topology levels.
For example:
- The entire workload is scheduled within a single zone.
- Subgroups A and B must be co-located under the same spine (assuming spine is a higher-level topology domain).
- Each subgroup must still ensure its pods are placed within the same rack.
- Another subgroup (C) can operate independently under a different rack-level constraint.

The example below illustrates this configuration:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  name: sample2
spec:
  queue: test
  priorityClassName: inference
  topologyConstraint:
    topology: "cluster-topology"
    requiredTopologyLevel: "topology/zone"
  subgroups:
    - name: subgroup-ab
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

The scheduler activates placement once the `minMember` condition for each subgroup is met.

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