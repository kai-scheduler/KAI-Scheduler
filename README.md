[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE) [![Coverage](https://github.com/kai-scheduler/KAI-scheduler/raw/coverage-badge/badges/coverage.svg)](https://github.com/kai-scheduler/KAI-scheduler/blob/main/.github/workflows/update-coverage-badge.yaml)
# KAI Scheduler
KAI Scheduler is a robust, efficient, and scalable [Kubernetes scheduler](https://kubernetes.io/docs/concepts/scheduling-eviction/kube-scheduler/) that optimizes GPU resource allocation for AI and machine learning workloads.

Designed to manage large-scale GPU clusters, including thousands of nodes, and high-throughput of workloads, makes the KAI Scheduler ideal for extensive and demanding environments.
KAI Scheduler allows administrators of Kubernetes clusters to dynamically allocate GPU resources to workloads. 

KAI Scheduler supports the entire AI lifecycle, from small, interactive jobs that require minimal resources to large training and inference, all within the same cluster. 
It ensures optimal resource allocation while maintaining resource fairness between the different consumers.
It can run alongside other schedulers installed on the cluster.

## Key Features
* [Batch Scheduling](docs/batch/README.md): Ensure all pods in a group are scheduled simultaneously or not at all.
* Bin Packing & Spread Scheduling: Optimize node usage either by minimizing fragmentation (bin-packing) or increasing resiliency and load balancing (spread scheduling).
* [Workload Priority](docs/priority/README.md): Prioritize workloads effectively within queues.
* [Hierarchical Queues](docs/queues/README.md): Manage workloads with two-level queue hierarchies for flexible organizational control.
* [Resource distribution](docs/fairness/README.md#resource-division-algorithm): Customize quotas, over-quota weights, limits, and priorities per queue.
* [Fairness Policies](docs/fairness/README.md#reclaim-strategies): Ensure equitable resource distribution using Dominant Resource Fairness (DRF) and resource reclamation across queues.
* Workload Consolidation: Reallocate running workloads intelligently to reduce fragmentation and increase cluster utilization.
* [Elastic Workloads](docs/elastic/README.md): Dynamically scale workloads within defined minimum and maximum pod counts.
* Dynamic Resource Allocation (DRA): Support vendor-specific hardware resources through Kubernetes ResourceClaims (e.g., GPUs from NVIDIA or AMD).
* [GPU Sharing](docs/gpu-sharing/README.md): Allow multiple workloads to efficiently share single or multiple GPUs, maximizing resource utilization.
* Cloud & On-premise Support: Fully compatible with dynamic cloud infrastructures (including auto-scalers like Karpenter) as well as static on-premise deployments.

## Prerequisites
Before installing KAI Scheduler, ensure you have:

- A running Kubernetes cluster
- [Helm](https://helm.sh/docs/intro/install) CLI installed
- [NVIDIA GPU-Operator](https://github.com/NVIDIA/gpu-operator) installed in order to schedule workloads that request GPU resources

## Installation
KAI Scheduler will be installed in `kai-scheduler` namespace.
> ⚠️ When submitting workloads, make sure to use a dedicated namespace. Do not use the `kai-scheduler` namespace for workload submission.

### Installation Methods
KAI Scheduler can be installed:

- **From Production (Recommended)**
- **From Source (Build it Yourself)**

#### Install from Production
Locate the latest release version in [releases](https://github.com/kai-scheduler/KAI-scheduler/releases) page.
Run the following command after replacing `<VERSION>` with the desired release version:
```sh
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler -n kai-scheduler --create-namespace --version <VERSION>
```
#### Build from Source
Follow the instructions [here](docs/developer/building-from-source.md)

## Flavor Specific Instructions
### Openshift
When `gpu-operator` <v25.10.0 is installed, the following flag should be added to the installation command:
```
--set admission.gpuPodRuntimeClassName=null
```

## Support & Breaking changes
Refer to the [Breaking Changes](https://github.com/kai-scheduler/KAI-scheduler/blob/main/docs/migrationguides/README.md) doc for more info

## Quick Start
To start scheduling workloads with KAI Scheduler, please continue to [Quick Start example](docs/quickstart/README.md)

## Roadmap

### High-level overview of the main priorities for 2025
* Refactor the codebase to enhance vendor neutrality
* Support Scheduling Gates https://github.com/kai-scheduler/KAI-scheduler/issues/63
* Research on possible integration with Kueue https://github.com/kai-scheduler/KAI-scheduler/issues/68
* Add Topology Aware Scheduling support of pod-group https://github.com/kai-scheduler/KAI-scheduler/issues/66
* Support Min Run Time per workloads
* Support Max Run Time per workload (with delayed requeue)
* Add more PriorityClasses as part of the default KAI install
* Support JobSet
* Support LWS (LeaderWorkerSet)
* Add metrics for pod and pod-group preemptions
* Decouple Priority and Preemption

### Long term goals
* Support per queue time decay
* Hyper scale improvements
* Support Consolidation of Inference workloads for cluster defragmentation
* Support n-levels of hierarchical queues
* Graceful rollout of Inference workloads (new revision update using queue temporary over-quota)

## Community, Discussion, and Support

We’d love to hear from you! Here are the best ways to connect:

### Contributing
Contributions are encouraged and appreciated! 
Please have a look at KAI-scheduler's [contribution guide](https://github.com/kai-scheduler/KAI-scheduler/blob/main/CONTRIBUTING.md) before submitting PRs.

### Slack
Join the [CNCF Slack](https://communityinviter.com/apps/cloud-native/cncf) first and visit the [#kai-scheduler](https://cloud-native.slack.com/archives/kai-scheduler) channel.

### Bi-weekly Community Call  
**When:** Every other Monday at 17:00 CEST  
[Convert to your time zone](https://dateful.com/time-zone-converter?t=17&tz2=Germany) | [Add to your calendar](https://calendar.google.com/calendar/event?action=TEMPLATE&tmeid=N2Q2bjhoNXAzMGc0cWpnZTQ4OGtpdXFhanFfMjAyNTA2MDlUMTUwMDAwWiAxZjQ2OTZiOWVlM2JiMWE1ZWIzMTAwODBkNDZiZmMwMDZjNTUxYWFiZmU1YTM3ZGM2YTc0NTFhYmNhMmE1ODk0QGc&tmsrc=1f4696b9ee3bb1a5eb310080d46bfc006c551aabfe5a37dc6a7451abca2a5894%40group.calendar.google.com&scp=ALL)  | [Meeting notes & agenda](https://docs.google.com/document/d/13K7NGdPebOstlrsif1YLjGz1x-aJafMXeIgqbO7WghI/edit?usp=sharing)

### Mailing List  
Join the [kai-scheduler mailing list](https://groups.google.com/g/kai-scheduler) to receive updates on biweekly meetings.

### Technical Issues & Feature Requests  
Please open a [GitHub issue](https://github.com/kai-scheduler/KAI-scheduler/issues/new/choose) for bugs, feature suggestions, or technical help. This helps us keep track of requests and respond effectively.