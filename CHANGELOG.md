# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed
- Updated Go toolchain and base build images to v1.25.10.

### Fixed
- Fixed plugin server (snapshot and job-order endpoints) listening on all interfaces by binding to localhost only.

## [v0.10.5] - 2025-12-24

## Fixed
- Keep creating service monitors for deprecated prometheus instances [#775](https://github.com/NVIDIA/KAI-Scheduler/pull/775) [itsomri](https://github.com/itsomri)
- Fix retention duration parsing for deprecated prometheus instances [#775](https://github.com/NVIDIA/KAI-Scheduler/pull/775) [itsomri](https://github.com/itsomri)
- Fixed a bug where the scheduler would not consider topology constraints when calculating the scheduling constraints signature [#761](https://github.com/NVIDIA/KAI-Scheduler/pull/766) [gshaibi](https://github.com/gshaibi)
- Fixed Dynamo integration by adding Dynamo GVKs to SkipTopOwner table
- Fixed prometheus instance deprecation - ensure single instance [#780](https://github.com/NVIDIA/KAI-Scheduler/pull/780) [itsomri](https://github.com/itsomri)

## [v0.10.4] - 2025-12-23

## Added 
- Add a "tumbling window" usage configuration - calculate a tumbling window size based on a start timne configuration and a duration config field.
- Added an option to disable prometheus persistency [#765](https://github.com/NVIDIA/KAI-Scheduler/pull/765) [itsomri](https://github.com/itsomri)

### Changed
- If enabled, prometheus storage size is not inferred from cluster objects, but defaults to 50Gi unless explicitly set in KAI config [#765](https://github.com/NVIDIA/KAI-Scheduler/pull/765) [itsomri](https://github.com/itsomri)
- When prometheus is disabled, it will remain in the cluster for a grace period equal to it's retention, unless re-enabled [#765](https://github.com/NVIDIA/KAI-Scheduler/pull/765) [itsomri](https://github.com/itsomri)

## Fixed
- Fixed reclaim/preempt/consolidate actions for topology workloads [#748](https://github.com/NVIDIA/KAI-Scheduler/pull/748)  [itsomri](https://github.com/itsomri)

## [v0.10.3] - 2025-12-09

### Added
- Added support for having the default "preemptibility" per top-owner-type read from the default configs configmap in the pod-grouper

### Fixed
- Fixed confusing resource division log message [#734](https://github.com/NVIDIA/KAI-Scheduler/pull/734) [itsomri](https://github.com/itsomri)
- Fixed a bug where the snapshot tool would not load topology objects [#731](https://github.com/NVIDIA/KAI-Scheduler/pull/731) [itsomri](https://github.com/itsomri)
- Fixed Dynamo integration by adding Dynamo GVKs to SkipTopOwner table

### Changed
- Renamed the previous "tumbling" option for the scheduler usage window type to "cron".

## [v0.10.2] - 2025-11-24

### Fixed
- Removed the requirement to specify container type for init container gpu fractions [#684](https://github.com/NVIDIA/KAI-Scheduler/pull/684) [itsomri](https://github.com/itsomri)
- When a status update for a podGroup in the scheduler is flushed due to update conflict, delete the update payload data as well [#691](https://github.com/NVIDIA/KAI-Scheduler/pull/691) [davidLif](https://github.com/davidLif)

## [v0.10.1] - 2025-11-23

### Fixed
- Fixed scheduler pod group status update conflict [#676](https://github.com/NVIDIA/KAI-Scheduler/pull/676) [davidLif](https://github.com/davidLif) 
- Fixed gpu request validations for pods [#660](https://github.com/NVIDIA/KAI-Scheduler/pull/660) [itsomri](https://github.com/itsomri)

### Changed
- Dependabot configuration to update actions in workflows [#651](https://github.com/NVIDIA/KAI-Scheduler/pull/651) [ScottBrenner](https://github.com/ScottBrenner)
- optimize dependency management by using module cache instead of vendor directory [#645](https://github.com/NVIDIA/KAI-Scheduler/pull/645) [lokielse](https://github.com/lokielse)

## [v0.10.0] - 2025-11-18

### Added
- Added parent reference to SubGroup struct in PodGroup CRD to create a hierarchical SubGroup structure
- Added the option to configure the names of the webhook configuration resources.
- Option to configure reservation pods runtime class.
- Added a tool to run time-aware fairness simulations over multiple cycles (see [Time-Aware Fairness Simulator](cmd/time-aware-simulator/README.md))
- Added enforcement of the `nvidia` runtime class for GPU pods, with the option to enforce a custom runtime class, or disable enforcement entirely.
- Added a preferred podAntiAffinity term by default for all services, can be set to required instead by setting `global.requireDefaultPodAffinityTerm`
- Added support for service-level affinities
- Added [time aware scheduling](docs/timeaware/README.md) capabilities
- Added option to specify container name and type for fraction containers

### Fixed
- (Openshift only) - High CPU usage for the operator pod due to continues reconciles
- Fixed a bug where the scheduler would not re-try updating podgroup status after failure
- Fixed a bug where ray workloads gang scheduling would ignore `minReplicas` if autoscaling was not set
- KAI Config wrong statuses when prometheus operand is enabled
- GPU-Operator v25.10.0 support for CDI enabled environments

## [v0.9.1] - 20250-09-15

### Added
- Added the option of providing the podgrouper app a scheme object to use

## [v0.9.0] - 20250-09-10

### Added
- config.kai.scheduler CRD that will describe the installation of all KAI-scheduler services for the operator
- Initial KAI-operator implementation for managing components
- PodGroup Controller, Queue Controller, Admission and Scale Adjuster operands to operator lifecycle management
- Deployment of operator in Helm chart alongside pod group controller
- Deploy PodGroup Controller, Queue Controller, Admission and Scale Adjuster via operator for streamlined deployment
- schedulingshrards.kai.scheduler CRD that describes partitioning the cluster nodes for different scheduling options.

### Changed
- Moved the CRDs into the helm chart so that they are also installed by helm and not only by the crd-upgrader, but removed the external kueue clone of topology CRD from being automatically installed.
- Updated queue controller image name to align with current deployment standards

### Fixed
- Removed webhook manager component as part of operator-based refactoring

## [v0.8.5] - 20250-09-04

### Added
- Added configurable plugins hub for podgrouper using interface and RegisterPlugins

## [v0.8.4] - 20250-09-02

### Added
- Added a plugin to reflect joborder in scheduler http endpoint - Contributed by Saurabh Kumar Singh <singh1203.ss@gmail.com>

### Fixed
- Fixed a bug where workload with subgroups would not consider additional tasks above minAvailable

## [v0.8.3] - 20250-08-31

### Removed
- Removed unused code that required gpu-operator as a dependency

## [v0.8.2] - 2025-08-25

### Fixed
- Fixed wrong GPU memory unit conversion from node `nvidia.com/gpu.memory` labels
- Fixed incorrect MIG GPU usage calculation leading to wrong scheduling decision

## [v0.8.1] - 2025-08-20

### Added
- Added a new scheduler flag `--update-pod-eviction-condition`. When enabled, a DisruptionTarget condition is set on the pod before deletion

### Fixed
- Fixed scheduler panic in some elastic reclaim scenarios

## [v0.8.0] - 2025-08-18

### Added
- Added leader election configuration in all deployments and added global helm value that controls it during installation

## [v0.7.13] - 2025-08-12

### Added
- Separated admission webhooks from binder service to a separate `kai-admission` service

### Fixed
- crd-upgrader respects global values for nodeSelector, affinity and tolerations 
- kai-scheduler will not ignore pod spec.overhead field

## [v0.7.12] - 2025-08-04

### Fixed
- Fixed container env var overwrite to cover possible cases where env var with Value is replaced with ValueFrom or the other way

## [v0.7.7] - 2025-07-16

### Fixed
- Fixed a scenario where only GPU resources where checked for job and node, causing it to be bound instead of being pipelined

## [v0.7.6] - 2025-07-11

### Added
- Added GPU_PORTION env var for GPU sharing pods

## [v0.7.5] - 2025-07-10

### Fixed
- Fixed a miscalculation where cpu/memory releasing resources were considered idle when requesting GPU fraction/memory

## [v0.7.4] - 2025-07-09

### Changed
- Changed RUNAI-VISIBLE-DEVICES key in GPU sharing configmap to NVIDIA_VISIBLE_DEVICES

## [v0.7.3] - 2025-07-08

### Removed
- Removed GPU sharing configmap name resolution from env vars and volumes

## [v0.7.2] - 2025-07-07
### Added
- Added LeaderWorkerSet support in the podGrouper. Each replica will be given a separate podGroup.

## [v0.7.1] - 2025-07-07

### Added
- Added kueue topology CRD to kai installations

### Fixed
- Fixed cases where reclaim validation operated on outdated info, allowing invalid reclaim scenarios

## [v0.7.0] - 2025-07-02

### Added
- Added optional pod and namespace label selectors to limit the scope of monitored pods
- Added a plugin extension point for scheduler plugins to add annotations to BindRequests
- Added support for Grove

### Changed
- Changed `run.ai/top-owner-metadata` to `kai.scheduler/top-owner-matadata`

## [v0.6.0] - 2025-06-16

### Changed
- Changed `runai-reservation` namespace to `kai-resource-reservation`. For migration guide, refer to this [doc](docs/migrationguides/README.md)
- Changed `runai/queue` label key to `kai.scheduler/queue`. For migration guide, refer to [doc](docs/migrationguides/README.md)

### Fixed
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

### Fixed
- Fixed preempt/reclaim of elastic workloads only taking one pod.
- Scheduler now doesn't label pods' nodepool when nodepool label value is empty
