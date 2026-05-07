# KAI Scheduler Adopters

This document lists organizations that have adopted KAI Scheduler — either to run their own workloads on Kubernetes, or to integrate it into the products and services they offer.

If your organization is using KAI Scheduler, we would love to have you listed here! Check out [Add Your Organization](#add-your-organization) below to get started.

## Adoption Phases

- **Evaluation**: Evaluating KAI Scheduler for fit against infrastructure and feature requirements.
- **Staging**: Adopted; rolling out in pre-production or staging environments.
- **Production**: Running KAI Scheduler in production.

## Organization Types

- **End User**: Uses KAI Scheduler to manage internal workloads or services.
- **Platform / Provider**: Integrates KAI Scheduler into a product, managed service, or platform offered to customers.

## Adopters

Listed alphabetically.

| Organization | Contact (GitHub) | Reference | Phase | Type |
| --- | --- | --- | --- | --- |
| [Cantina](https://cantina.com/) | [@sam-huang1223](https://github.com/sam-huang1223) | [KubeCon EU 2026: GPU Reservations](https://www.youtube.com/watch?v=O-OEqmvCkYg) | Production | End User |
| [Lightning AI](https://lightning.ai/) | [@tchaton](https://github.com/tchaton) | [KubeCon EU 2026: GPU Reservations](https://www.youtube.com/watch?v=O-OEqmvCkYg) | — | Platform / Provider |
| [NVIDIA Run:ai](https://www.nvidia.com/en-us/software/run-ai/) | — | KAI was originally developed within the Run:ai platform and continues to be packaged and delivered as part of the [NVIDIA Run:ai platform](https://www.nvidia.com/en-us/software/run-ai/), used in production by enterprises across financial services, education, healthcare, defense and more | Production | Platform / Provider |
| [OSMO](https://github.com/NVIDIA/OSMO/) | — | A platform for scaling Physical AI workloads across heterogeneous compute — unifying training GPUs, simulation clusters, and edge devices. See [KAI Scheduler configuration](https://nvidia.github.io/OSMO/main/deployment_guide/advanced_config/scheduler.html). | Production | End User & Platform / Provider |

## Integrations

Projects that ship a KAI Scheduler integration. Listed alphabetically.

| Project | Link |
| --- | --- |
| [Elotl](https://www.elotl.co/) | [Integration](https://www.elotl.co/blog/building-an-elastic-gpu-cluster-with-the-kai-scheduler-and-luna-autoscaler) |
| [Kubeflow](https://www.kubeflow.org/) | [Integration](https://www.kubeflow.org/docs/components/trainer/legacy-v1/user-guides/job-scheduling/#kai-scheduler) |
| [KubeRay](https://github.com/ray-project/kuberay) | [Integration](https://docs.ray.io/en/latest/cluster/kubernetes/k8s-ecosystem/kai-scheduler.html) |
| [NVIDIA Cloud Functions (NVCF)](https://github.com/NVIDIA/nvcf) | [NVCF integration](https://docs.nvidia.com/nvcf/kai-scheduler) |
| [NVIDIA Dynamo](https://github.com/ai-dynamo/dynamo) | [Dynamo integration](https://docs.nvidia.com/dynamo/latest/kubernetes-deployment/multinode/multinode-deployments#using-grove-default) |
| [vCluster](https://www.vcluster.com/) | [Integration](https://www.vcluster.com/docs/vcluster/third-party-integrations/scheduler/kai-scheduler) |
| [ZenML](https://www.zenml.io/) | [Integration](https://www.zenml.io/blog/nvidia-kai-scheduler-optimize-gpu-usage-in-zenml-pipelines) |

## Add Your Organization

Are you using KAI Scheduler? We'd love to hear from you. Open a Pull Request against this file ([ADOPTERS.md](#)) adding a row with:

- **Organization name**
- **Contact** — GitHub handle of someone we can reach out to
- **Reference** — link to a blog post, talk, integration, or short description of your use case (optional but appreciated)
- **Phase** — Evaluation, Staging, or Production
- **Type** — End User, Platform / Provider, or both

If you'd prefer not to be listed publicly, we're happy to hear from you privately instead — see [MAINTAINERS](#) for contact details.

This list is community-maintained. Entries may be updated or removed if they become stale; if your organization's status changes, please open a PR.