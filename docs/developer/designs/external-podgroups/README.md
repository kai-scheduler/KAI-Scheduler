# Externally-Created PodGroups

## Overview

KAI currently assumes that the podgrouper owns PodGroup creation for KAI-scheduled pods. The podgrouper derives a PodGroup from each pod's owner chain, creates or updates that PodGroup, and patches the pod with `pod-group-name` and, when relevant, `kai.scheduler/subgroup-name`.

This blocks cross-workload gang scheduling. If multiple independent workloads should be scheduled as one atomic unit, an external controller or user needs to create one PodGroup and attach all participating pods to it without podgrouper rewriting the membership.

## Goals

- Support user-created or controller-created PodGroups.
- Allow pods from multiple workloads to join the same PodGroup.
- Prevent podgrouper from creating competing PodGroups for opted-out pods.
- Prevent podgrouper from overwriting external `pod-group-name` and `kai.scheduler/subgroup-name`.
- Detect pods that reference missing subgroups and make the failure visible.
- Preserve current auto-created PodGroup behavior unless users explicitly opt out.

## Non-Goals

- No automatic segmentation or `segmentSize` support.
- No subgroup inference for external PodGroups.
- No cross-namespace PodGroup membership.
- No new lifecycle controller for external PodGroups.
- No event fan-out to all participating workload owners.

## API

Pods still join a PodGroup with the existing annotation:

```yaml
metadata:
  annotations:
    pod-group-name: post-training-pipeline
```

Pods still join a subgroup with the existing label:

```yaml
metadata:
  labels:
    kai.scheduler/subgroup-name: train-workers-rack-0
```

A pod or any object in its owner chain may opt out of automatic podgrouper management with:

```yaml
metadata:
  annotations:
    kai.scheduler/skip-podgrouper: "true"
```

This annotation only tells podgrouper to leave the pod alone. It does not attach the pod to a PodGroup. Users must still set `pod-group-name`, and must set `kai.scheduler/subgroup-name` when using non-default subgroups.

`PodGroup.spec.queue` is authoritative for scheduling. Pod queue labels are not used to select the queue for an external PodGroup.

## Example

```yaml
apiVersion: scheduling.run.ai/v2alpha2
kind: PodGroup
metadata:
  name: post-training-pipeline
  namespace: default
spec:
  minMember: 5
  queue: default-queue
  priorityClassName: normal
  topologyConstraint:
    topology: cluster-topology
    requiredTopologyLevel: topology.kubernetes.io/zone
  subGroups:
    - name: ray-head
      minMember: 1
    - name: ray-workers
      minMember: 2
      topologyConstraint:
        topology: cluster-topology
        requiredTopologyLevel: topology.kubernetes.io/rack
    - name: evaluation
      minMember: 2
```

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: post-training-evaluation
  annotations:
    kai.scheduler/skip-podgrouper: "true"
spec:
  parallelism: 2
  completions: 2
  template:
    metadata:
      annotations:
        pod-group-name: post-training-pipeline
      labels:
        kai.scheduler/subgroup-name: evaluation
    spec:
      schedulerName: kai-scheduler
      containers:
        - name: eval
          image: ubuntu
          command: ["sleep", "infinity"]
      restartPolicy: Never
```

Other workload types, such as RayJob, use the same pattern: set `kai.scheduler/skip-podgrouper: "true"` on the workload or pod template, then set `pod-group-name` and subgroup labels on the generated pod templates.

## Podgrouper Behavior

Podgrouper should evaluate skip ownership before it applies PodGroup metadata or patches the pod:

```text
if pod schedulerName != configured scheduler:
    return

if pod has kai.scheduler/skip-podgrouper: "true":
    return

if pod is ownerless and already has pod-group-name:
    return

topOwner, allOwners = GetPodOwners(pod)

if topOwner or any owner in allOwners has kai.scheduler/skip-podgrouper: "true":
    return

metadata = GetPGMetadata(pod, topOwner, allOwners)
if metadata == nil:
    return

ApplyToCluster(metadata)
assignPodToGroupAndSubGroup(pod, metadata)
```

Only the exact value `"true"` should skip. Missing, `"false"`, or invalid values should preserve existing podgrouper behavior.

The ownerless pod with `pod-group-name` case should keep the current behavior: podgrouper skips it and does not re-create PodGroup metadata.

## Scheduler Behavior

The scheduler already discovers PodGroup membership from `pod-group-name`.

If a pod references a PodGroup that does not exist yet, the scheduler will not schedule it because scheduling happens at the PodGroup level. This may be a normal creation-order race, so the pod should not make a PodGroup fail and should not get a condition for the missing PodGroup.

If a pod has `kai.scheduler/subgroup-name` that does not exist in its referenced PodGroup, only that pod should be ignored for scheduling. The scheduler should not mark the whole PodGroup unschedulable because elastic PodGroups may still be schedulable when some pods are missing or invalid.

The missing-subgroup case should be visible on the offending pod: the scheduler should set a pod condition explaining that the pod references a subgroup that does not exist in the PodGroup.

## Lifecycle And Events

External PodGroups are owned by whoever creates them:

- External controllers should set an owner reference or delete the PodGroup themselves.
- Manually-created PodGroups without owner references require manual cleanup.
- Multiple owner references are risky because Kubernetes may delete the PodGroup when any owner is deleted.

Scheduling status and scheduling events remain on the PodGroup and pods. Users should inspect PodGroup events directly when no single workload owner represents the whole group.

## Implementation Plan

1. Add `kai.scheduler/skip-podgrouper` as a constant.
2. Add pod-level and owner-chain skip checks in podgrouper.
3. Keep `metadata == nil` handling only as plugin compatibility, not as the public API.
4. Keep missing-PodGroup references as ignored pods without setting a pod condition.
5. Keep missing-subgroup handling pod-scoped: ignore the offending pod and set a pod condition explaining the invalid subgroup label.
6. Add tests for pod-level skip, owner-chain skip, normal podgrouper behavior, missing PodGroup behavior, and invalid subgroup handling.

## Test Plan

- Pod annotation skips before owner lookup.
- Top-owner and intermediate-owner annotations skip after owner lookup.
- Owned pods with only `pod-group-name` continue through normal podgrouper reconciliation.
- External PodGroup pods are not rewritten by podgrouper.
- A missing PodGroup reference does not create a pod or PodGroup condition.
- A missing subgroup label causes only the offending pod to be ignored and creates a pod condition.
- A valid cross-workload PodGroup schedules atomically.

## Risks

- Users can opt out without creating or referencing a valid PodGroup.
- External controllers must handle PodGroup lifecycle correctly.
- Owner-chain skip depends on KAI having RBAC to read owner objects.
- Event visibility is weaker when users watch only the individual workload objects.
