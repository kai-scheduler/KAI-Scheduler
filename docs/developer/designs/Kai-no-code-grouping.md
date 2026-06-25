# Kai generic grouping via Karta integration

This document describes a generic grouping path for KAI that lets workload
owners define pod-grouping behavior without writing a dedicated pod-grouper
plugin. The integration with [Karta](https://github.com/run-ai/karta) provides
the no-code definition model: Karta describes the workload structure, and KAI
translates that structure into standard PodGroup, SubGroup, and topology
constraints.

The goal is to make KAI extensible for workload types that do not have a native
plugin. Native KAI plugins keep precedence for workload types with specialized
logic, while Karta supplies a declarative generic path for the broader set of
workloads that can be grouped from metadata and component definitions.

## Karta Plugin Flow

KAI selects the pod-grouper in the following order:

1. If a native KAI plugin matches the workload GVK, KAI uses the native plugin.
2. Otherwise, KAI looks for a matching Karta definition.
3. If the Karta definition is valid and contains pod-grouping instructions, KAI
   uses the Karta plugin.
4. Otherwise, KAI falls back to the default pod-grouper.

The Karta plugin translates the matched Karta definition into regular KAI
PodGroup fields. KAI scheduling continues to operate on the generated PodGroup
and does not need Karta-specific scheduling primitives.

## API Shape

The Karta gang-scheduling instruction keeps the alpha `podGroups` format for
compatibility and adds a clearer `podGroup` format for the KAI integration.
When both fields are present, `podGroup` takes precedence.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package v1alpha1

type GangSchedulingInstruction struct {
	// PodGroups defines the alpha grouping format that KAI still supports.
	// +listType=map
	// +listMapKey=name
	PodGroups []PodGroupDefinition `json:"podGroups,omitempty"`

	// PodGroup defines the grouping, subgroup, and topology behavior used by
	// the KAI-native Karta integration.
	// +kubebuilder:validation:Optional
	PodGroup *PodGroupDefinitionV2 `json:"podGroup,omitempty"`
}

// PodGroupDefinition defines the alpha grouping format that KAI still supports.
type PodGroupDefinition struct {
	// Name is the unique identifier for this pod group.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Members defines which components belong to this pod group.
	// +listType=map
	// +listMapKey=componentName
	Members []PodGroupMemberDefinition `json:"members"`
}

// PodGroupMemberDefinition selects and filters components in the alpha format.
type PodGroupMemberDefinition struct {
	// ComponentName references a component defined in the Karta structure.
	// +kubebuilder:validation:Required
	ComponentName string `json:"componentName"`

	// GroupByKeyPaths are JQ paths to values used for grouping.
	// Each path must return a single, non-empty value.
	// +kubebuilder:validation:Optional
	// +listType=set
	GroupByKeyPaths []string `json:"groupByKeyPaths,omitempty" jq:"validate"`

	// Filters are JQ expressions used to select matching pods.
	// Expressions are evaluated against individual pod objects.
	// +kubebuilder:validation:Optional
	// +listType=set
	Filters []string `json:"filters,omitempty" jq:"validate"`
}

// PodGroupDefinitionV2 defines the KAI-native grouping format.
type PodGroupDefinitionV2 struct {
	// Name is the unique identifier for this pod group.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// SubGroups defines which Karta components should become KAI SubGroups.
	// +kubebuilder:validation:Optional
	// +listType=map
	// +listMapKey=componentName
	SubGroups []SubGroupDefinition `json:"subGroups,omitempty"`

	// Topology defines the topology constraint for all workload pods.
	// +kubebuilder:validation:Optional
	Topology *TopologyConstraint `json:"topology,omitempty"`
}

type SubGroupDefinition struct {
	// ComponentName references a component defined in the Karta structure.
	// +kubebuilder:validation:Required
	ComponentName string `json:"componentName"`

	// Topology defines the topology constraint for this component's pods.
	// +kubebuilder:validation:Optional
	Topology *TopologyConstraint `json:"topology,omitempty"`
}

type TopologyConstraint struct {
	// TopologyName is the topology resource used by the constraint.
	// +kubebuilder:validation:Required
	TopologyName string `json:"topologyName"`

	// PreferredTopologyLevel is the preferred locality level.
	// +kubebuilder:validation:Required
	PreferredTopologyLevel string `json:"preferredTopologyLevel"`

	// RequiredTopologyLevel is the maximal level that all matching pods must
	// fit within.
	// +kubebuilder:validation:Required
	RequiredTopologyLevel string `json:"requiredTopologyLevel"`
}
```

## Field Translation

| Karta field | KAI PodGroup field | Notes |
| --- | --- | --- |
| Workload owner GVK and Karta GVK labels | PodGroup owner and grouping identity | Selects the matching Karta definition for the workload. |
| `podGroup.name` | PodGroup name suffix or grouping identity | The exact name still follows KAI pod-grouper naming rules. |
| `podGroup.topology` | PodGroup topology constraint | Applies to all pods in the generated PodGroup. |
| `podGroup.subGroups[].componentName` | SubGroup name and pod membership | One direct SubGroup is generated per listed component. |
| Component `scaleDefinition.replicasPath` | SubGroup `minMember` | The component replica count defines the required members for that SubGroup. |
| Number of items in the list `podGroup.subGroups[]` | PodGroup `minSubGroup` | Set only when SubGroups are generated. |
| `podGroup.subGroups[].topology` | SubGroup topology constraint | Applies only to pods that belong to the matching component. |

KAI also preserves the standard metadata handled by the pod-grouper, including
namespace, owner references, queue, priority, labels, annotations, and
preemptibility.

## Topology Behavior

The KAI-native format supports topology constraints at two levels:

- PodGroup topology applies to the whole workload.
- SubGroup topology applies to pods from a specific Karta component.

Workload owners may override the PodGroup-level topology through the supported
KAI labels or annotations. Component-level topology comes from the Karta
definition and is not overridden by workload labels, because it describes the
component layout expected by the workload type.

## SubGroup Generation

When `podGroup.subGroups` is set, the Karta plugin generates a fixed, single subgroups level 
KAI PodGroup:

- Each listed Karta component becomes one direct SubGroup.
- The SubGroup name is the component name.
- The SubGroup `minMember` is derived from the component replica scale
  definition.
- The PodGroup `minSubGroup` is set to the number of generated direct
  SubGroups.

This gives KAI enough structure to schedule multi-component workloads with
clear gang and topology semantics while keeping the generated PodGroup easy to
reason about.

## Alpha Instruction Compatibility

KAI must continue to support the [alpha](https://github.com/run-ai/karta/blob/main/pkg/api/runai/v1alpha1/instructions.go) `podGroups` instruction format. That path
keeps its existing behavior:

- Multiple alpha PodGroups do not schedule together as one unit.
- Filters must be written so each pod belongs to only one generated PodGroup.
- SubGroups and topology constraints are available only through the newer
  `podGroup` format.

The alpha path remains important for existing Karta workloads, but new Karta
definitions should use `podGroup` so KAI can generate richer PodGroup,
SubGroup, and topology output.

## Constraints

The first KAI-native implementation intentionally keeps the generated structure
simple:

- No elastic PodGroups.
- No elastic SubGroups.
- No nested SubGroups.
- No generated `minSubGroup` inside a SubGroup.
- Only one SubGroup is created per Karta component.
- All matching pods for a component attach to that component's SubGroup,
  regardless of grouping key value.

Components that rely on `componentInstanceSelector` or `replicaSelector` are
therefore poor candidates for SubGroup generation. They can still participate in
the PodGroup, but converting them into a single SubGroup may hide meaningful
instance-level structure.

## Validation

The Karta plugin should reject or ignore incomplete instructions before creating
an invalid PodGroup:

- `podGroup.subGroups[].componentName` must reference a component that exists in
  the Karta structure.
- A component used as a SubGroup must expose a replica scale definition.
- Topology constraints must include topology name. It should also include at least one constraint parameter (preferred/required).
- `podGroup` takes precedence over `podGroups` when both are set.
