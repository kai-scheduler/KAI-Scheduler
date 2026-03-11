# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed

- Updated resource enumeration logic to exclude resources with count of 0. [#1120](https://github.com/NVIDIA/KAI-Scheduler/issues/1120)
- Fixed plugin server (snapshot and job-order endpoints) listening on all interfaces by binding to localhost only.

## [v0.6.16] - 2026-01-07

### Fixed
- Fixed a bug where the scheduler would not re-try updating podgroup status after failure
- GPU Memory pods are not reclaimed or consolidated correctly
- Fixed GPU memory pods Fair Share and Queue Order calculations

## [v0.6.14] - 2025-08-26

### Removed
- Removed unused code that required gpu-operator as a dependency

### Fixed
- Fixed scheduler panic in some elastic reclaim scenarios
- Fixed wrong GPU memory unit conversion from node `nvidia.com/gpu.memory` labels
- Fixed incorrect MIG GPU usage calculation leading to wrong scheduling decision

## [v0.6.11] - 2025-08-18

### Changed
- Scheduler now allows reclaim scenarios when both queues are above fair share, if starvation ratio will improve

### Fixed
- kai-scheduler will not ignore pod spec.overhead field

## [v0.6.9] - 2025-07-18

### Fixed
- Fixed a scenario where only GPU resources where checked for job and node, causing it to be bound instead of being pipelined

## [v0.6.8] - 2025-07-13

### Fixed
- Fixed a miscalculation where cpu/memory releasing resources were considered idle when requesting GPU fraction/memory

## [v0.6.7] - 2025-07-07

### Added
- Added LeaderWorkerSet support in the podGrouper. Each replica will be given a separate podGroup.

## [v0.6.6] - 2025-07-06

### Fixes
- Fixed cases where reclaim validation operated on outdated info, allowing invalid reclaim scenarios

## [v0.6.4] - 2025-06-27

### Fixes
- Fix: pod group controller fails on missing priority class

## [v0.6.0] - 2025-06-16

### Changed
- Changed `runai-reservation` namespace to `kai-resource-reservation`. For migration guide, refer to this [doc](docs/migrationguides/README.md)
- Changed `runai/queue` label key to `kai.scheduler/queue`. For migration guide, refer to [doc](docs/migrationguides/README.md)

### Fixes
- Fixed pod status scheduled race condition between the scheduler and the pod binding
- Removed redundant `replicas` key for binder from `values.yaml` as it is not used and not supported

### Removed
- Removed `runai-job-id` and `runai/job-id` annotations from pods and podgroups

### Added
- Added [minruntime](docs/plugins/minruntime.md) plugin, allowing PodGroups to run for a configurable amount of time without being reclaimed/preempted.
- PodGroup Controller that will update podgroups statuses with allocation data.
- Queue Controller that will update queues statuses with allocation data.


## [v0.5.1] - 2025-05-20

### Added
- Added support for [k8s pod scheduling gates](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/)
- nodeSelector, affinity and tolerations configurable with global value definitions
- Added `PreemptMinRuntime` and `ReclaimMinRuntime` properties to queue CRD
- Scheduler now adds a "LastStartTimestamp" to podgroup on allocation

### Changed
- Queue order function now takes into account potential victims, resulting in better reclaim scenarios.

### Fixes
- Fixed preempt/reclaim of elastic workloads only taking one pod.
- Scheduler now doesn't label pods' nodepool when nodepool label value is empty
