# Karta PodGroup Mapping CRD

## Overview

Karta describes workload structure: root objects, child components, pod selectors, replica selectors, and scale paths. KAI Scheduler uses `PodGroup` resources to gang-schedule related pods.

This design introduces a new KAI-side CRD that connects those two systems. The CRD references a Karta `ResourceInterface` and defines how KAI podgrouper should translate pods from workloads described by that Karta into KAI `PodGroup` and `SubGroup` resources.

This is intentionally separate from Karta `gangScheduling`. That field is expected to be removed from Karta, so the scheduling policy should live in a KAI-owned API rather than inside the Karta API.

## Goals

- Keep Karta focused on workload structure.
- Keep KAI-specific scheduling policy in a KAI-owned CRD.
- Allow KAI podgrouper to support new workload types without writing a custom Go plugin for each type.
- Support simple PodGroups, fixed SubGroups, and dynamic hierarchical SubGroups.
- Reuse Karta component definitions, scale paths, pod selectors, and replica selectors.
- Preserve KAI `PodGroup` semantics for `minMember`, `minSubGroup`, queue, priority, preemptibility, and topology constraints.

## Non-Goals

- Add new scheduling fields to Karta.
- Support multiple generated KAI PodGroups for the same Karta component in the first version.
- Support a mix between a karta-based podgroup and other podgroup plugins.

## Proposed CRD

The CRD name is provisional. This document uses `KartaPodGroupMapping` to make the role explicit.

```yaml
apiVersion: podgrouper.run.ai/v1alpha1
kind: KartaPodGroupMapping
metadata:
  name: example
spec:
  objectMatch:
    kartaRef: example-resource-interface
    labels: (key-value map)
  podGroupSpec:
    minMember: ""
    minSubGroup: ""
    queue: ""
    priorityClassName: ""
    preemptibility: ""
    topologyConstraint:
      topology: ""
      preferredTopologyLevel: ""
      requiredTopologyLevel: ""
    subGroups: []
```

The mapping is cluster-scoped if Karta `ResourceInterface` objects are cluster-scoped. If Karta objects are namespaced, the reference should include `namespace`.

## Field Model

Dynamic fields are object path expressions evaluated by KAI podgrouper when it reconciles a pod.

| Field | Meaning |
|---|---|
| `.root` | The root workload object described by the referenced Karta, such as a PyTorchJob, RayCluster, or LeaderWorkerSet. |
| `.pod` | The pod currently being mapped to a PodGroup or SubGroup. |
| `.karta` | The referenced Karta `ResourceInterface`. |
| `.kartaComponent` | The Karta component currently being evaluated for the pod. |
| `.componentInstance` | The current component instance, when Karta creates multiple instances from one component. |
| `.replica` | The replica identity extracted by the component `podSelector.replicaSelector`, when one exists. |
| `.parent` | The generated parent subgroup context, when evaluating a child subgroup. |

All dynamic values in `podGroupSpec` are object path expressions unless the field is explicitly documented as a literal. Literal strings can still be represented as jq string expressions, for example `'"non-preemptible"'`.

## PodGroup Spec Fields

`podGroupSpec` represents the desired KAI `PodGroup` spec plus the extra mapping fields needed to generate SubGroups.

| Field | Meaning |
|---|---|
| `podGroupName` | Optional jq expression for the generated KAI `PodGroup` name. If omitted, podgrouper uses `pg-<karta-name>-<object-name>-<short-uid>`. |
| `minMember` | jq expression for KAI `PodGroup.spec.minMember`. Mutually exclusive with `minSubGroup`. |
| `minSubGroup` | jq expression for KAI `PodGroup.spec.minSubGroup`. Mutually exclusive with `minMember`. |
| `queue` | jq expression for KAI `PodGroup.spec.queue`. |
| `priorityClassName` | jq expression for KAI `PodGroup.spec.priorityClassName`. |
| `preemptibility` | jq expression returning `preemptible`, `non-preemptible`, or an empty value for default behavior. |
| `topologyConstraint` | Optional KAI PodGroup-level topology constraint. |
| `subGroups` | Optional list of generated KAI SubGroup templates. |

When `subGroups` is empty or omitted, all matching pods attach directly to the generated KAI PodGroup.

## SubGroup Template Fields

```yaml
subGroups:
  - name: ""
    nameBase: ""
    kartaComponentName: ""
    parent: ""
    parentNameBase: ""
    minMember: ""
    minSubGroup: ""
    topologyConstraint:
      topology: ""
      preferredTopologyLevel: ""
      requiredTopologyLevel: ""
```

| Field | Meaning |
|---|---|
| `name` | jq expression for a single fixed SubGroup name. |
| `nameBase` | Literal base name used to generate dynamic SubGroup names. |
| `kartaComponentName` | Karta component whose matching pods attach to this SubGroup. |
| `parent` | jq expression for a fixed concrete parent SubGroup name. |
| `parentNameBase` | Literal parent base name used to attach generated child SubGroups to generated parent SubGroups. |
| `minMember` | jq expression for KAI SubGroup `minMember`. Mutually exclusive with `minSubGroup`. |
| `minSubGroup` | jq expression for KAI SubGroup `minSubGroup`. Mutually exclusive with `minMember`. |
| `topologyConstraint` | Optional SubGroup-level topology constraint. |

`name` and `nameBase` are mutually exclusive. `parent` and `parentNameBase` are mutually exclusive.

## Matching Rules

1. Podgrouper finds the Karta object referenced by `spec.objectMatch.kartaRef`. If `spec.objectMatch.labels` are specified, the object must match the labels as well.
2. Podgrouper checks whether the pod belongs to a workload described by that Karta.
3. Podgrouper resolves the matching Karta component using `podSelector.componentTypeSelector`, `componentInstanceSelector`, and `replicaSelector`.
4. If `podGroupSpec.subGroups` is empty, every pod from the mapped workload attaches to the generated PodGroup.
5. If `subGroups` is defined, a pod attaches to the subgroup whose `kartaComponentName` matches its resolved Karta component.
6. If a subgroup has a parent, the pod must also match the parent component or parent replica context.
7. If a component has `podSelector.replicaSelector`, podgrouper creates one generated subgroup per distinct replica selector value when that component is used with `nameBase`.
8. This version allows only one mapping-generated PodGroup per Karta component.

## Naming Rules

Default generated PodGroup name:

```text
pg-<karta-name>-<object-name>-<short-uid>
```

Fixed subgroup:

```yaml
name: '"workers"'
```

Dynamic subgroup:

```yaml
nameBase: replica
```

If the component replica selector returns `0`, the generated name is:

```text
replica-0
```

Dynamic child subgroup names include the generated parent name:

```text
replica-0-leader
replica-0-workers
```

Generated names must be deterministic, non-empty, and valid DNS labels. Invalid selector values should fail mapping reconciliation with a clear error.

## Example 1: No SubGroups

All pods described by the referenced Karta attach to one KAI PodGroup. The PodGroup is schedulable when `minMember` pods can be scheduled.

```yaml
apiVersion: podgrouper.run.ai/v1alpha1
kind: KartaPodGroupMapping
metadata:
  name: basic-training
spec:
  kartaRef:
    name: ABC
  podGroupSpec:
    minMember: '.karta.spec.structureDefinition.scaleDefinition.replicasPath'
    queue: '.root.metadata.labels["kai.queue"]'
    priorityClassName: '.root.metadata.labels["kai.priority"]'
    preemptibility: '"non-preemptible"'
    topologyConstraint:
      topology: '.karta.spec.structureDefinition.topology.topologyName'
      preferredTopologyLevel: '.karta.spec.structureDefinition.topology.topologyRequired'
      requiredTopologyLevel: '"datacenter"'
```

Generated KAI shape:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  name: pg-abc-my-workload-a1b2c3
spec:
  minMember: 8
  queue: team-a
  priorityClassName: train
  preemptibility: non-preemptible
  topologyConstraint:
    topology: rack
    preferredTopologyLevel: rack
    requiredTopologyLevel: datacenter
```

## Example 2: Fixed Number of SubGroups

This mapping creates one fixed subgroup for the `k1` Karta component. The generated PodGroup is schedulable when at least one direct subgroup is schedulable.

```yaml
apiVersion: podgrouper.run.ai/v1alpha1
kind: KartaPodGroupMapping
metadata:
  name: fixed-subgroup
spec:
  kartaRef:
    name: ABC
  podGroupSpec:
    minSubGroup: "1"
    queue: '.root.metadata.labels["kai.queue"]'
    priorityClassName: '.root.metadata.labels["kai.priority"]'
    preemptibility: '"non-preemptible"'
    subGroups:
      - name: '"k1"'
        kartaComponentName: k1
        minMember: '.kartaComponent.scaleDefinition.replicasPath'
        topologyConstraint:
          topology: '.karta.spec.structureDefinition.topology.topologyName'
          requiredTopologyLevel: '"abc"'
```

Generated KAI shape:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
spec:
  minSubGroup: 1
  subGroups:
    - name: k1
      minMember: 8
      topologyConstraint:
        topology: rack
        requiredTopologyLevel: abc
```

## Example 3: Dynamic Hierarchical SubGroups

This mapping creates one parent subgroup per replica group. Under every generated replica subgroup, it creates `leader` and `workers` child subgroups.

```yaml
apiVersion: podgrouper.run.ai/v1alpha1
kind: KartaPodGroupMapping
metadata:
  name: dynamic-replica-subgroups
spec:
  kartaRef:
    name: ABC
  podGroupSpec:
    minSubGroup: '.karta.spec.structureDefinition.scaleDefinition.replicasPath'
    queue: '.root.metadata.labels["kai.queue"]'
    priorityClassName: '.root.metadata.labels["kai.priority"]'
    preemptibility: '"non-preemptible"'
    subGroups:
      - nameBase: replica
        kartaComponentName: groups
        minSubGroup: "2"
      - nameBase: leader
        kartaComponentName: leaders
        parentNameBase: replica
        minMember: '.kartaComponent.scaleDefinition.replicasPath'
      - nameBase: workers
        kartaComponentName: workers
        parentNameBase: replica
        minMember: '.kartaComponent.scaleDefinition.replicasPath'
```

If the Karta `groups` component has a `podSelector.replicaSelector` and the workload creates replica values `0` and `1`, the generated KAI shape is:

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
spec:
  minSubGroup: 2
  subGroups:
    - name: replica-0
      minSubGroup: 2
    - name: replica-0-leader
      parent: replica-0
      minMember: 1
    - name: replica-0-workers
      parent: replica-0
      minMember: 7
    - name: replica-1
      minSubGroup: 2
    - name: replica-1-leader
      parent: replica-1
      minMember: 1
    - name: replica-1-workers
      parent: replica-1
      minMember: 7
```

## Validation

The CRD admission webhook should reject invalid mappings:

- `spec.kartaRef.name` is required.
- `minMember` and `minSubGroup` cannot both be set on the same PodGroup or SubGroup.
- `name` and `nameBase` cannot both be set on the same SubGroup.
- `parent` and `parentNameBase` cannot both be set on the same SubGroup.
- `kartaComponentName` must reference a component in the target Karta.
- A subgroup with `parentNameBase` must refer to another subgroup template in the same mapping.
- A dynamic subgroup using `nameBase` must reference a Karta component that can produce a concrete component instance or replica identity.
- Generated PodGroup and SubGroup names must be valid DNS labels.
- A Karta component can be owned by only one mapping-generated PodGroup in the first version.

## Podgrouper Flow

KAI podgrouper should add a Karta mapping grouper that:

1. Watches `KartaPodGroupMapping` objects.
2. Watches or reads referenced Karta `ResourceInterface` objects. 
3. For each pod, resolves the top workload object and matching Karta component. 
4. Finds the mapping that references that Karta. A matching Karta definition + KartaPodGroupMapping takes precedence over an existing kai plugin.
5. Evaluates `podGroupSpec` fields into KAI PodGroup metadata.
6. Expands fixed and dynamic subgroup templates.
7. Creates or updates the generated KAI `PodGroup`.
8. Patches the pod with the generated PodGroup name and, when relevant, the generated SubGroup name.

The podgrouper metadata layer must carry both `minMember` and `minSubGroup` intent. The KAI `PodGroup` API supports both fields, but the Karta mapping implementation must ensure the metadata-to-CRD conversion writes `minSubGroup` at both PodGroup and SubGroup levels.

## Notes

- The `KartaPodGroupMapping` doesn't support jq evaluations - only constants and references to fields in other objects. 

## Open Questions
