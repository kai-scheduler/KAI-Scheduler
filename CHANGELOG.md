# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- Update go version to v1.25.6, with appropriate upgrades to the base docker images, linter, and controller generator. [#1284](https://github.com/kai-scheduler/KAI-Scheduler/pull/1284) [davidLif](https://github.com/davidLif)

### Fixed
- Fixed `additionalImagePullSecrets` in Config CR rendering as `map[name:...]` instead of plain strings by extracting `.name` from `global.imagePullSecrets` objects. Also propagated `global.imagePullSecrets` to all Helm hook jobs (`crd-upgrader`, `topology-migration`, `post-delete-cleanup`)
- Added `global.nodeSelector`, `global.tolerations`, `global.affinity`, `global.securityContext` support to the post-delete job hook.
- Fixed Helm template writing `imagesPullSecret` (string) instead of `additionalImagePullSecrets` (array) in Config CR, causing image pull secrets to be silently ignored. Added backward-compatible deprecated `imagesPullSecret` field to CRD schema. [#942](https://github.com/kai-scheduler/KAI-Scheduler/issues/942)
- Fixed `windowSize` field in `SchedulingShard` CR to support Prometheus duration format (e.g. `1w`, `7d`). Previously, using `windowSize: 1w` as shown in the documentation caused the kai-operator to crash-loop with `time: unknown unit "w" in duration "1w"`.
- Race condition where `SyncForGpuGroup` could prematurely delete reservation pods when the informer cache had not yet propagated GPU group labels on recently-bound fraction pods. The binder now checks for active BindRequests referencing the GPU group before deleting a reservation pod.
- Fixed non-preemptible multi-device GPU memory jobs being allowed to exceed their queue's deserved GPU quota. The per-node quota check now correctly accounts for all requested GPU devices. [#1369](https://github.com/kai-scheduler/KAI-Scheduler/issues/1369)
- Added `resourceclaims/binding` RBAC permission to the binder ClusterRole for compatibility with Kubernetes v1.36+, where the `DRAResourceClaimGranularStatusAuthorization` feature gate requires explicit permission on the `resourceclaims/binding` subresource to modify `status.allocation` and `status.reservedFor` on ResourceClaims. [#1372](https://github.com/kai-scheduler/KAI-Scheduler/pull/1372) [praveen0raj](https://github.com/praveen0raj)
- Allow users to override minMember for k8s batch Jobs and JobSets using the `kai.scheduler/batch-min-member` annotation [#1308](https://github.com/kai-scheduler/KAI-Scheduler/pull/1308) [itsomri](https://github.com/itsomri)
- Fixed a bug where nil minMember caused subgroups creation to fail in scheduler [#1407](https://github.com/kai-scheduler/KAI-Scheduler/pull/1407) [itsomri](https://github.com/itsomri)
- Improved performance by evaluating SetNode once per session instead of on each predicate evaluation  [#1421](https://github.com/kai-scheduler/KAI-Scheduler/pull/1421) [itsomri](https://github.com/itsomri)
- Added persistent volumes to cluster snapshot [#1424](https://github.com/kai-scheduler/KAI-Scheduler/pull/1424) [itsomri](https://github.com/itsomri)
- Improved scheduling performance for preempt/reclaim/consolidate actions on jobs with many tasks by replacing per-task linear probing with exponential+binary search in the job solver, reducing the number of scenario simulations from O(n) to O(log n) [#1435](https://github.com/kai-scheduler/KAI-Scheduler/pull/1435) [itsomri](https://github.com/itsomri)
- Avoid expensive solver-backed reclaim/preempt/consolidation work for jobs already blocked by victim-invariant pre-solver failures such as missing PVCs, missing required ConfigMaps, or requests larger than the maximum node size. [#1502](https://github.com/kai-scheduler/KAI-Scheduler/issues/1502)
- Fixed `skipTopOwnerGrouper` not propagating per-type defaults (priority class and preemptibility) for skipped owners (e.g. `DynamoGraphDeployment`), causing PodGroup spec to retain stale values after defaults ConfigMap updates.
- Fixed binder DRA detection on clusters where the upstream `DynamicResourceAllocation` feature gate does not reflect server-side DRA availability. The binder now probes the API server during init (matching the scheduler) so the DRA plugin is gated on the same authoritative decision. [#1481](https://github.com/kai-scheduler/KAI-Scheduler/issues/1481)
- Suppressed noisy `Reconciler error` logs and `PodGrouperWarning` events on transient PodGroup update conflicts. The podgrouper now treats `IsConflict` errors as expected and silently requeues the reconcile instead of surfacing the apiserver's "object has been modified" message.

- Updated resource enumeration logic to exclude resources with count of 0. [#1120](https://github.com/NVIDIA/KAI-Scheduler/issues/1120)
- Fixed plugin server (snapshot and job-order endpoints) listening on all interfaces by binding to localhost only.

## [v0.4.18] - 2026-01-25

## [v0.4.17] - 2026-01-07

### Fixed
- Fixed a bug where the scheduler would not re-try updating podgroup status after failure
- GPU Memory pods are not reclaimed or consolidated correctly
- Fixed GPU memory pods Fair Share and Queue Order calculations

## [v0.4.13-16] - ???

### Fixed
- kai-scheduler will not ignore pod spec.overhead field
- Fixed wrong GPU memory unit conversion from node `nvidia.com/gpu.memory` labels
- Fixed incorrect MIG GPU usage calculation leading to wrong scheduling decision

## [v0.4.12] - 2025-07-18

### Fixed
- Fixed a scenario where only GPU resources where checked for job and node, causing it to be bound instead of being pipelined

## [v0.4.11] - 2025-07-13

### Fixed
- Fixed a miscalculation where cpu/memory releasing resources were considered idle when requesting GPU fraction/memory

## [v0.4.10] - 2025-06-09

### Fixed
- Fix scheduler pod group status synchronization between incoming update and in-cluster data

## [v0.4.9] - 2025-05-27

### Fixed
- Fixed pod status scheduled race condition between the scheduler and the pod binding
- Scheduler now doesn't label pods' nodepool when nodepool label value is empty

## [v0.4.8]

### Fixed
- Queue order function now takes into account potential victims, resulting in better reclaim scenarios.

### CHANGED
- Cached GetDeservedShare and GetFairShare function in the scheduler PodGroupInfo to improve performance.
- Added cache to the binder resource reservation client.
- More Caching and improvements to PodGroupInfo class.
- Update pod labels after scheduling decision concurrently in the background.

## [v0.4.7]
