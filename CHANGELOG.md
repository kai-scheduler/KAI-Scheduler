# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed

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
