# Priority and Preemptibility Separation (P0)

*Status: Draft*

## Table of Contents
- [Background](#background)
- [Problem Statement](#problem-statement)
- [Goals / Non-Goals](#goals-non-goals)
   * [Goals](#goals)
   * [Non-Goals](#non-goals)
- [Proposal](#proposal)
   * [API Changes](#api-changes)
      + [1. PodGroup Spec Field](#1-podgroup-spec-field)
      + [2. Pod Label](#2-pod-label)
   * [Scheduler Logic Changes](#scheduler-logic-changes)
      + [1. Preemptibility Determination](#1-preemptibility-determination)
   * [Backward Compatibility](#backward-compatibility)
      + [1. Default Behavior Support](#1-default-behavior-support)
      + [2. Configuration Validation](#2-configuration-validation)
- [Examples](#examples)
   * [Example 1: High-Priority Preemptible PodGroup](#example-1-high-priority-preemptible-podgroup)
   * [Example 2: Low-Priority Non-Preemptible PodGroup](#example-2-low-priority-non-preemptible-podgroup)
   * [Example 3: External Workload with Explicit Preemptibility](#example-3-external-workload-with-explicit-preemptibility)
   * [Example 4: Default Behavior PodGroup (Backward Compatible)](#example-4-default-behavior-podgroup-backward-compatible)


---

## Background

Currently, KAI-scheduler users can submit workloads with associated priority classes that apply to all subordinate pods. The scheduler implicitly assumes that all workloads using priority classes lower than 100 are preemptible, while workloads using priority classes higher than or equal to 100 are non-preemptible.

## Problem Statement

This coupling between priority and preemptibility limits usage flexibility for several important use cases:

1. **High-priority preemptible workloads**: Users may want to submit high-priority workloads that are still preemptible (e.g., high-priority training workloads)
2. **Low-priority non-preemptible workloads**: Users may want to submit low-priority workloads (e.g., data processing) that run to completion when granted resources
3. **Semi-preemptible workloads**: Users may want to submit composite workloads with min-members count where min-members are non-preemptible but additional pods above min-members are preemptible

This document will handle cases 1 and 2. Case 3 will be handled in a separate document.

The current implementation creates artificial constraints that prevent users from expressing their true scheduling requirements:

- **Priority** should control the order in which workloads are considered for scheduling in a queue
- **Preemptibility** should control whether a workload can be interrupted once running
- These are orthogonal concerns that should be independently configurable

The current priority-based preemptibility determination (priority >= 100 = non-preemptible) is too simplistic and doesn't accommodate the diverse scheduling requirements of modern AI/ML workloads.

## Goals / Non-Goals

### Goals
- **Separate priority from preemptibility**: Allow independent configuration of workload priority and preemptibility
- **Maintain backward compatibility**: Existing workloads without explicit preemptibility configuration should continue to work using the default priority-based determination
- **Support two preemptibility modes**: Preemptible, Non-Preemptible

### Non-Goals
- **P1 features**: Semi-preemptible workloads (addressed separately)


## Proposal

Add a new `preemptibility` parameter at the pod/podgroup level with the following possible values:
- `preemptible`: PodGroup can be preempted by higher-priority workloads
- `non-preemptible`: PodGroup runs to completion once scheduled
- `semi-preemptible`: PodGroup has both preemptible and non-preemptible components (P1 feature)

When `preemptibility` is not explicitly set, the system defaults to priority-based preemptibility determination.

### API Changes

#### 1. PodGroup Spec Field
Add a preemptibility field to the PodGroup spec:

```yaml
spec:
  preemptibility: "preemptible"  # or "non-preemptible"
  priorityClassName: "train"     # existing field, now independent of preemptibility
```

```go
type PodGroupSpec struct {
    // ... existing fields ...
    
    // Preemptibility defines whether this PodGroup can be preempted
    // Defaults to priority-based preemptibility determination (preemptible if priority < 100)
    // +kubebuilder:validation:Enum=preemptible;non-preemptible;semi-preemptible
    // +optional
    Preemptibility string `json:"preemptibility,omitempty"`
}
```

#### 2. Pod Label
Pods can also specify preemptibility via the `kai.scheduler/preemptibility` label, which is useful for external workloads or cases where PodGroup spec modification is not feasible (same as with priority):

```yaml
metadata:
  labels:
    kai.scheduler/preemptibility: "non-preemptible"
```

Creating a Pod with an invalid preemptibility value will result in a fallback to the default priority-based determination.

### Scheduler Logic Changes

#### 1. Preemptibility Determination
The scheduler will determine preemptibility using the following precedence:

1. **Explicit preemptibility spec field** on PodGroup
2. **Explicit preemptibility label** on pod's top owner (for external workloads)
3. **Explicit preemptibility label** on pod (for external workloads)
4. **Legacy priority-based determination** (preemptible if priority < 100) for backward compatibility

The same logic will be used in the PodGroupController to publish the preemptibility status on the PodGroup (for backward compatibility).

### Backward Compatibility

#### 1. Default Behavior Support
Workloads without explicit preemptibility configuration will continue to use the priority-based determination as the default behavior:
- Priority < 100 → Preemptible
- Priority >= 100 → Non-Preemptible

#### 2. Configuration Validation
The preemptibility values will be validated and fall back to the default priority-based determination for invalid values.

## Examples

### Example 1: High-Priority Preemptible PodGroup
```yaml
apiVersion: scheduling.kai.nvidia.com/v2alpha2
kind: PodGroup
metadata:
  name: high-priority-training
spec:
  preemptibility: "preemptible"
  priorityClassName: "inference"  # High priority (125)
  # ... rest of podgroup spec
```

### Example 2: Low-Priority Non-Preemptible PodGroup
```yaml
apiVersion: scheduling.kai.nvidia.com/v2alpha2
kind: PodGroup
metadata:
  name: data-processing
spec:
  preemptibility: "non-preemptible"
  priorityClassName: "train"  # Low priority (50)
  # ... rest of podgroup spec
```

### Example 3: External Workload with Explicit Preemptibility
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-workload
spec:
  template:
    metadata:
      labels:
        kai.scheduler/preemptibility: "non-preemptible"
        priorityClassName: "train"
    spec:
      # ... pod spec
```

### Example 4: Default Behavior PodGroup (Backward Compatible)
```yaml
apiVersion: scheduling.kai.nvidia.com/v2alpha2
kind: PodGroup
metadata:
  name: default-behavior-workload
spec:
  priorityClassName: "build"  # Priority 100 → Non-Preemptible (default behavior)
  # No preemptibility field → uses default priority-based determination
  # ... rest of podgroup spec
```
