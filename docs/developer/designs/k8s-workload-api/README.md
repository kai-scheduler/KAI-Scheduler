# Kubernetes Workload API Integration

## Introduction

Kubernetes v1.35 introduces the **Workload API (KEP-4671)**, a new standard for defining group scheduling requirements natively. This design extends KAI Scheduler to natively support this API, implementing a translation layer that maps standard `Workload` definitions to KAI's internal `PodGroup` mechanism. This ensures seamless scheduling for any application using the new Kubernetes standard while preserving KAI's advanced queuing and quota capabilities.

### Kubernetes Workload API Overview

The Workload API introduces a standard way to group pods for scheduling. It consists of two main components:

1.  **Workload Resource (`scheduling.k8s.io/v1alpha1`)**:
    A namespace-scoped resource that defines one or more "PodGroups," each with a specific scheduling policy (e.g., `gang` vs. `basic`).
    ```yaml
    kind: Workload
    spec:
      podGroups:
        - name: training-workers
          policy:
            gang:
              minCount: 4  # Minimum pods required to start
        - name: other-pods
          policy:
            basic: {}
    ```

2.  **Pod Specification (`spec.workloadRef`)**:
    A new field in the Pod spec that explicitly links a Pod to a Workload and a specific group within it.
    ```yaml
    spec:
      workloadRef:
        name: my-workload         # References the Workload resource above
        podGroup: training-workers # References the specific group name
        podGroupReplicaKey: group-a # (Optional) Splits one group definition into multiple instances
    ```

## Design

### 1. Grouping Strategy

Each `Workload.podGroup` + `replicaKey` combination → **separate KAI PodGroup**.

- **Naming**: `{workload}-{podGroup}-{replicaKey?}`
- **Important**: Workload podGroups are independent gangs (no co-scheduling between them), so they must be separate KAI PodGroups, not SubGroups.

**Example**:
```
Workload: my-training
  podGroups:
    - name: driver (gang.minCount: 1)
    - name: workers (gang.minCount: 4)

Pod A: workloadRef={name: my-training, podGroup: driver}
Pod B: workloadRef={name: my-training, podGroup: workers, replicaKey: "0"}
Pod C: workloadRef={name: my-training, podGroup: workers, replicaKey: "1"}

Creates:
  - KAI PodGroup "my-training-driver" (minMember=1) ← Pod A
  - KAI PodGroup "my-training-workers-0" (minMember=4) ← Pod B
  - KAI PodGroup "my-training-workers-1" (minMember=4) ← Pod C
```

### 2. Policy Translation

- **Gang Policy**: `gang.minCount` → KAI `MinMember`
- **Basic Policy**: Map entire Workload podGroup to a single KAI PodGroup with `MinMember: 1` (unified group approach for scalability and centralized quota management)

### 3. Metadata Calculation (Layered Approach)

1. Always run **top owner plugin first** (base metadata)
2. If `workloadRef` exists, **Workload plugin overrides** specific fields

| Field | Source |
|-------|--------|
| **MinAvailable** | Workload (`gang.minCount`, or `1` for basic policy) |
| **Name** | Workload (`{workload}-{podGroup}-{replicaKey}`) |
| **SubGroups** | None (ignored when Workload exists, until the Workload API supports it) |
| **Queue, Priority, Preemptibility** | Workload → Top Owner → Pod (fallback chain) |
| **Topology** | Workload → Top Owner |
| **Labels/Annotations** | Merged (Workload takes precedence) |
| **Owner** | Top Owner |

### 4. Error Handling & Instant Recovery

If a Pod references a Workload or PodGroup that does not exist, strict validation is enforced:

*   **Pending State**: The Pod remains **Pending** and no KAI PodGroup is created. It is never scheduled as a standalone task.
*   **Instant Recovery**: We will implement a **Watcher** on `Workload` resources. As soon as a missing Workload is created, the watcher immediately triggers reconciliation for any pending Pods referencing it, ensuring instant scheduling.

### 5. Opt-Out Mechanism

An opt-out flag is supported to ignore the Workload API and use Top Owner scheduling semantics instead. This allows users to explicitly bypass Workload-based grouping when needed.

*   **Annotation**: `kai.scheduler/ignore-workload-api: "true"` on the Pod or Top Owner resource
*   **Behavior**: When set, the scheduler ignores any `workloadRef` on the Pod and falls back entirely to Top Owner-based grouping

## Key Principles

1. **Workload is authoritative** for scheduling semantics and config overrides
2. **Separate Workload podGroups = separate KAI PodGroups**
3. **Backward compatible** - falls back to top owner if Workload doesn't specify a field or pod has no `workloadRef`
