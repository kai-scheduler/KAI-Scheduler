[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE) [![Coverage](https://github.com/kai-scheduler/KAI-scheduler/raw/coverage-badge/badges/coverage.svg)](https://github.com/kai-scheduler/KAI-scheduler/blob/main/.github/workflows/update-coverage-badge.yaml)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/kai-scheduler/KAI-scheduler)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12064/badge)](https://www.bestpractices.dev/projects/12064)

# KAI Scheduler

KAI Scheduler is a robust, efficient, and scalable [Kubernetes scheduler](https://kubernetes.io/docs/concepts/scheduling-eviction/kube-scheduler/) that optimizes GPU resource allocation for AI and machine learning workloads.

Designed to manage large-scale GPU clusters, including thousands of nodes, and high-throughput of workloads, makes the KAI Scheduler ideal for extensive and demanding environments.
KAI Scheduler allows administrators of Kubernetes clusters to dynamically allocate GPU resources to workloads. 

KAI Scheduler supports the entire AI lifecycle, from small, interactive jobs that require minimal resources to large training and inference, all within the same cluster. 
It ensures optimal resource allocation while maintaining resource fairness between the different consumers.
It can run alongside other schedulers installed on the cluster.

## Latest News 🔥

- [2025/11] **KubeCon NA 2025 Talk:** Watch the recording of the presentation "[Lightning Talk: Mind the Topology: Smarter Scheduling for AI Workloads on Kubernetes](https://youtu.be/o5i7pTWZjfo?si=su5iTOAS4r4O1TPa)" to learn how KAI's Topology-Aware Scheduling (TAS) optimizes placement for modern disaggregated serving architectures.
- [2025/11] **Integration with [Grove](https://github.com/ai-dynamo/grove) & Dynamo:** KAI's Topology-Aware and Hierarchical Gang Scheduling capabilities are integrated with Grove to orchestrate complex, multi-component workloads like disaggregated serving and agentic pipelines at scale. Read the [blog post](https://developer.nvidia.com/blog/streamline-complex-ai-inference-on-kubernetes-with-nvidia-grove/) for more details.
- [2025/10] **[v0.10.0 Release:](https://github.com/kai-scheduler/KAI-scheduler/releases/tag/v0.10.0)** Major features released, including [Topology-Aware Scheduling (TAS)](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/topology), [Hierarchical PodGroups](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/developer/designs/hierarchical-podgroup), and [Time-based Fairshare](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/time-based-fairshare).
- [2025/10] **KubeRay Integration:** KAI Scheduler is now natively integrated for [Ray workloads on Kubernetes](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/kai-scheduler.html).
- [2025/08] **Time-Based Fairshare:** [Proposal for Time-based Fairshare](https://github.com/kai-scheduler/KAI-scheduler/blob/main/docs/developer/designs/time-based-fairshare/time-based-fairshare.md) is discussed at batch-wg. [Watch the recording.](https://zoom.us/rec/play/uW5ex5dmQP8_7UqOv5UjOGq8IqZeIa8AhKILqvDUQ6CnBAIdJjPY-BLfUWnoYblvDP-ZIvAp48p7XJNv.Cx5t7x1DwGqJgIYB?eagerLoadZvaPages=&accessLevel=meeting&canPlayFromShare=true&from=share_recording_detail&startTime=1755010542000&componentName=rec-play&originRequestUrl=https%3A%2F%2Fzoom.us%2Frec%2Fshare%2Frd_j_7ZDpC8lXxGNdQwguK2ZunoM3R93HR1Eo4A9rxD7b5lWSbmojDKc8OZ00ZMK.QxgEeMOxMcuiDkIY%3FstartTime%3D1755010542000)
- [2025/04] **Project Introduction:** Recording of the [KAI Scheduler introduction presented at the batch-wg meeting](https://zoom.us/rec/play/E1weaHroJpuTdXx6s9pjMu6oS78BiA53wsnvV9MWe_rIdwmDLFOG8J4XEPNW8-hIp4-HSFNdsbbP7mcv.YstbxFdS7z7tOfKw?eagerLoadZvaPages=&accessLevel=meeting&canPlayFromShare=true&from=share_recording_detail&startTime=1744124229000&componentName=rec-play&originRequestUrl=https%3A%2F%2Fzoom.us%2Frec%2Fshare%2FwP2WH6bqd7Dj8dupZD3YQTMWgG4AP5361_0h5vicI69LNb25JdQB8wn6fkvtLw2f.rLrRcQTSO1OCyRNu%3FstartTime%3D1744124229000).

## Key Features

- [Batch Scheduling](docs/batch/README.md): Ensure all pods in a group are scheduled simultaneously or not at all.
- Bin Packing & Spread Scheduling: Optimize node usage either by minimizing fragmentation (bin-packing) or increasing resiliency and load balancing (spread scheduling).
- [Workload Priority](docs/priority/README.md): Prioritize workloads effectively within queues.
- [Separation of workload priority and preemptibility](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/developer/designs/priority-preemptibility-separation): supports separation of workload priority and workloads preemptibility as two independent policies
- [Hierarchical Queues](docs/queues/README.md): Manage workloads with two-level queue hierarchies for flexible organizational control.
- [Resource distribution](docs/fairness/README.md#resource-division-algorithm): Customize quotas, over-quota weights, limits, and priorities per queue.
- [Fairness Policies](docs/fairness/README.md#reclaim-strategies): Ensure equitable resource distribution using Dominant Resource Fairness (DRF) and resource reclamation across queues.
- [Time-based Fairshare](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/time-based-fairshare): Over-time fair usage of resources, considering historical usage, time decay, and other parameters for fine-tuning.
- [Min-guaranteed-runtime](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/developer/designs/min-runtime): ensures a time period in which the scheduler must not preempt or reclaim a running workload, even if preemptible.
- Workload Consolidation: Reallocate running workloads intelligently to reduce fragmentation and increase cluster utilization.
- [Elastic Workloads](docs/elastic/README.md): Dynamically scale workloads within defined minimum and maximum pod counts.
- Dynamic Resource Allocation (DRA): Support vendor-specific hardware resources through Kubernetes ResourceClaims (e.g., GPUs from NVIDIA or AMD).
- [Topology-Aware Scheduling (TAS)](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/topology): supports optimized placement with [topology aware scheduling](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/developer/designs/topology-awareness) and hierarchical topology aware scheduling for [Hierarchical PodGroups](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/developer/designs/hierarchical-podgroup).
- [Hierarchical PodGroups](https://github.com/kai-scheduler/KAI-scheduler/tree/main/docs/developer/designs/hierarchical-podgroup): supports gang scheduling with optimized topology aware scheduling of multi-level workloads, such as distributed and disaggregated workloads such as Dynamo/Grove.
- DRA support - supporting DRA for NVIDIA ComputeResources (GB200/GB300)
- Workload signatures: KAI Scheduler provides performance optimization for large  multi-pod submissions using workload signatures. 
- Scheduler explainability: based on K8S Events, every major step of the scheduling process is logged.

- [GPU Sharing](docs/gpu-sharing/README.md): Allow multiple workloads to efficiently share single or multiple GPUs, maximizing resource utilization.
- Cloud & On-premise Support: Fully compatible with dynamic cloud infrastructures (including auto-scalers like Karpenter) as well as static on-premise deployments.

> [!NOTE]
> KAI Scheduler is built based on [kube-batch](https://github.com/kubernetes-sigs/kube-batch).

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

### OpenShift

When `gpu-operator` <v25.10.0 is installed, the following flag should be added to the installation command:

```
--set admission.gpuPodRuntimeClassName=null
```
If CDI is enabled also add `--set binder.cdiEnabled=true`.
## Support & Breaking changes

For details on our release lifecycle, LTS versions, and supported releases, see the [Support Policy](SUPPORT.md).

Refer to the [Breaking Changes](https://github.com/kai-scheduler/KAI-scheduler/blob/main/docs/migrationguides/README.md) doc for more info

## Quick Start

To start scheduling workloads with KAI Scheduler, please continue to [Quick Start example](docs/quickstart/README.md)

## Roadmap

You can find the updated KAI Scheduler roadmap (historical, near year and future) [here](roadmap.md).

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

---

<div align="center">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
      <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color/cncf-color.svg">
      <img width="300" alt="Cloud Native Computing Foundation logo" src="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
    </picture>
    <p>KAI Scheduler is <a href="https://cncf.io">Cloud Native Computing Foundation</a> sandbox project.</p>
</div>

Copyright Contributors to KAI Scheduler, established as KAI Scheduler a Series of LF Projects, LLC.
For website terms of use, trademark policy and other project policies please see [lfprojects.org/policies](https://lfprojects.org/policies/).
