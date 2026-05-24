# Karta PodGroup Scheduling Design

## Field Model
Evaluation context for new dynamic fields:

| Field | Meaning |
|---|---|
| `.root` | The root workload object described by this Karta, such as a PyTorchJob, RayCluster, or LeaderWorkerSet. |
| `.pod` | The pod currently being mapped to a PodGroup or SubGroup. |
| `.sources` | Related objects loaded through `sources`, mainly for RayJob/RayService resolving a RayCluster. |
| `.vars` | Previously evaluated variables from `gangScheduling.vars` and `podGroups[].vars`. |
| `.group` | The current logical PodGroup instance after `groupByKeyPaths` are evaluated. |
| `.component` | The current Karta component or component instance when evaluating component-local subgroup fields. |

`groupByKeyPaths` decides which concrete PodGroup instance a pod belongs to. The evaluated values become grouping keys, so pods with the same key values join the same gang.

## PodGroup Fields
```yaml
optimizationInstructions:
  gangScheduling:
    vars: {}
    podGroups:
      - name: job
        sources: {}
        vars: {}
        podGroupName: ""
        minMember: ""
        minSubGroup: ""
        queue: ""
        priorityClassName: ""
        preemptibility: ""
        topologyConstraint:
          topology: ""
          requiredTopologyLevel: ""
          preferredTopologyLevel: ""
        members:
          - componentName: worker
            groupByKeyPaths: []
```

| Field | Explanation |
|---|---|
| `gangScheduling.vars` | Optional shared jq variables available to all PodGroup definitions in this Karta. Use only for values reused by multiple PodGroups. |
| `podGroups` | List of PodGroup templates this Karta can create for the workload’s pods. |
| `podGroups[].name` | Karta-local identifier for the PodGroup rule. |
| `podGroups[].sources` | Optional related-object lookups. Used when PodGroup calculation needs another object, for example RayJob/RayService reading the generated RayCluster. |
| `sources.<name>.kind` | GVK of the related object to load. |
| `sources.<name>.namespace` | jq expression returning the namespace of the related object. |
| `sources.<name>.name` | jq expression returning the name of the related object. |
| `podGroups[].vars` | PodGroup-local jq variables. Preferred place for calculations used only by this PodGroup. |
| `podGroupName` | jq expression for the actual Kubernetes PodGroup object name. If omitted, implementation keeps current Karta naming behavior. |
| `minMember` | jq expression for PodGroup `spec.minMember`. Mutually exclusive with `minSubGroup`. |
| `minSubGroup` | jq expression for PodGroup `spec.minSubGroup`, used when direct child SubGroups determine schedulability. Mutually exclusive with `minMember`. |
| `queue` | jq expression for PodGroup `spec.queue`. |
| `priorityClassName` | jq expression for PodGroup `spec.priorityClassName`. |
| `preemptibility` | jq expression returning `preemptible`, `non-preemptible`, or empty/default behavior. |
| `topologyConstraint` | Optional PodGroup-level topology constraint block. |
| `members` | Existing Karta member list selecting which components participate in the PodGroup. |
| `members[].componentName` | Existing literal component name from `structureDefinition`. |
| `members[].groupByKeyPaths` | Existing pod-rooted jq paths used to split included pods into logical PodGroup instances. |

## Component SubGroup Fields
Component-specific SubGroup fields live directly under the relevant component.

```yaml
structureDefinition:
  childComponents:
    - name: worker
      gangScheduling:
        subGroup:
          when: ""
          forEachInstance: false
          name: ""
          minMember: ""
          minSubGroup: ""
          parent: ""
          topologyConstraint:
            topology: ""
            requiredTopologyLevel: ""
            preferredTopologyLevel: ""
```

| Field | Explanation |
|---|---|
| `component.gangScheduling` | Component-local scheduling contribution. Keeps component-specific subgroup logic near the component definition. |
| `subGroup` | Describes the SubGroup emitted by this component. If this section is not defined, no subgroup will be created for this component.  |
| `subGroup.when` | Optional jq boolean. If false, this component does not emit this SubGroup. |
| `subGroup.forEachInstance` | When true, emit one SubGroup per component instance, using `.component.instanceID` and instance-local scale. Defaults to `false`. |
| `subGroup.name` | jq expression for SubGroup `name`. |
| `subGroup.minMember` | jq expression for SubGroup `minMember`. Mutually exclusive with `minSubGroup`. |
| `subGroup.minSubGroup` | jq expression for SubGroup `minSubGroup`, for hierarchical subgroup trees. Mutually exclusive with `minMember`. |
| `subGroup.parent` | jq expression for parent SubGroup name. Omit for direct children of the PodGroup. |
| `subGroup.topologyConstraint` | Optional SubGroup-level topology constraint block. Same fields as PodGroup topology. |

## Examples
PyTorch:

```yaml
optimizationInstructions:
  gangScheduling:
    podGroups:
      - name: job
        vars:
          specs: ".root.spec.pytorchReplicaSpecs"
          masterReplicas: "(.vars.specs.Master.replicas // 0) | tonumber"
          totalReplicas: "([(.vars.specs // {})[] | (.replicas // 1) | tonumber] | add) // 0"
          minAvailable: "(.root.spec.runPolicy.schedulingPolicy.minAvailable // .root.spec.elasticPolicy.minReplicas // .vars.totalReplicas) | tonumber"
        minMember: ".vars.minAvailable"
        members:
          - componentName: master
            groupByKeyPaths: ['.metadata.labels["training.kubeflow.org/job-name"]']
          - componentName: worker
            groupByKeyPaths: ['.metadata.labels["training.kubeflow.org/job-name"]']
```

```yaml
childComponents:
  - name: master
    gangScheduling:
      subGroup:
        when: ".vars.specs.Master != null and .vars.masterReplicas > 0"
        name: '"master"'
        minMember: ".vars.masterReplicas"
  - name: worker
    gangScheduling:
      subGroup:
        when: ".vars.specs.Worker != null"
        name: '"worker"'
        minMember: "if (.vars.minAvailable - .vars.masterReplicas) < 0 then 0 else (.vars.minAvailable - .vars.masterReplicas) end"
```

Ray:

```yaml
optimizationInstructions:
  gangScheduling:
    podGroups:
      - name: cluster
        sources:
          cluster:
            kind: { group: ray.io, version: v1, kind: RayCluster }
            namespace: ".root.metadata.namespace"
            name: ".root.metadata.name"
        vars:
          workerGroups: ".sources.cluster.spec.workerGroupSpecs // []"
          workerMin: >-
            ([.vars.workerGroups[] | select((.suspended // false) | not) |
              (if ((.minReplicas // 0) | tonumber) > 0
               then ((.minReplicas // 0) | tonumber)
               else ((.replicas // 0) | tonumber)
               end) * ((.numOfHosts // 1) | tonumber)] | add) // 0
        minMember: "1 + (.vars.workerMin | tonumber)"
        priorityClassName: '.root.metadata.labels["ray.io/priority-class-name"] // ""'
        members:
          - componentName: head
            groupByKeyPaths: ['.metadata.labels["ray.io/cluster"]']
          - componentName: worker
            groupByKeyPaths: ['.metadata.labels["ray.io/cluster"]']
```

```yaml
childComponents:
  - name: head
    gangScheduling:
      subGroup:
        name: '"headgroup"'
        minMember: "1"
  - name: worker
    instanceIdPath: '.spec.workerGroupSpecs | to_entries[] | (.value.groupName // "worker-group-\(.key)")'
    scaleDefinition:
      replicasPath: '.spec.workerGroupSpecs[] | ((.replicas // 0) * (.numOfHosts // 1))'
      minReplicasPath: '.spec.workerGroupSpecs[] | ((.minReplicas // 0) * (.numOfHosts // 1))'
    gangScheduling:
      subGroup:
        forEachInstance: true
        name: ".component.instanceID"
        minMember: ".component.scale.effectiveMinReplicas"
```

LeaderWorkerSet:

```yaml
optimizationInstructions:
  gangScheduling:
    podGroups:
      - name: group
        vars:
          size: "(.root.spec.leaderWorkerTemplate.size // 1) | tonumber"
          startupPolicy: '.root.spec.startupPolicy // "LeaderCreated"'
          workerIndex: '.pod.metadata.labels["leaderworkerset.sigs.k8s.io/worker-index"]'
          groupIndex: '.pod.metadata.labels["leaderworkerset.sigs.k8s.io/group-index"] // "0"'
          minAvailable: >-
            if .vars.startupPolicy == "LeaderReady"
               and .vars.workerIndex == "0"
               and (.pod.spec.nodeName // "") == ""
            then 1
            else .vars.size
            end
        podGroupName: '"\(.root.metadata.name)-group-\(.vars.groupIndex)"'
        minMember: ".vars.minAvailable"
        members:
          - componentName: group
            groupByKeyPaths:
              - '.metadata.labels["leaderworkerset.sigs.k8s.io/name"]'
              - '.metadata.labels["leaderworkerset.sigs.k8s.io/group-index"] // "0"'
```

```yaml
childComponents:
  - name: leader
    gangScheduling:
      subGroup:
        name: '"leader"'
        minMember: "1"
  - name: worker
    gangScheduling:
      subGroup:
        when: ".vars.minAvailable > 1"
        name: '"workers"'
        minMember: ".vars.minAvailable - 1"
```

