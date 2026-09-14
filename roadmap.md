[License](LICENSE) [Coverage](https://github.com/kai-scheduler/KAI-scheduler/blob/main/.github/workflows/update-coverage-badge.yaml)
[Ask DeepWiki](https://deepwiki.com/kai-scheduler/KAI-scheduler)

# Roadmap

Status reviewed on **2026-09-14** against `main`, the [changelog](CHANGELOG.md), and linked issues and pull requests. **Available** means implemented in `main`; consult the changelog for release availability. **Partial** identifies delivered functionality with remaining work. **Remaining** carries forward an existing goal without a delivery commitment. A merged design or closed issue alone does not establish feature availability.

## 2026 priorities

| Priority | Status | Current state and remaining work |
| --- | --- | --- |
| DRA for NVIDIA whole-GPU allocation | Available | GPU ResourceClaim accounting and quota/fair-share integration landed in [#900](https://github.com/kai-scheduler/KAI-Scheduler/pull/900). DRA `firstAvailable` support remains tracked separately in [#1855](https://github.com/kai-scheduler/KAI-Scheduler/issues/1855). |
| Automatic sub-grouping of Ray, PyTorch and LWS | Partial | PyTorch and LWS support [segment-based subgroup creation](docs/topology/segments.md); Ray supports subgroup topology placement, but automatic Ray segmentation remains open in [#1419](https://github.com/kai-scheduler/KAI-Scheduler/issues/1419). |
| Block topology-aware scheduling | Partial | [Indexed block/segment placement](docs/topology/segments.md) is available for PyTorch and LWS. Non-indexed automatic grouping remains open in [#2077](https://github.com/kai-scheduler/KAI-Scheduler/issues/2077). |
| GPU-level scheduling placement strategy | Available for fractional GPUs | Independent node-level and GPU-device packing/spreading is supported through the `gpupack` and `gpuspread` plugins; see [scheduler configuration](docs/operator/scheduler-config-customization.md#independent-node-level-and-device-level-placement). |
| Kubernetes Workload/PodGroup API | Remaining | The [integration design](docs/developer/designs/k8s-workload-api/README.md) is merged; implementation remains tracked in [#1317](https://github.com/kai-scheduler/KAI-Scheduler/issues/1317) and [#1343](https://github.com/kai-scheduler/KAI-Scheduler/issues/1343). |
| Maximum runtime per workload, with delayed requeue | Remaining | Separate from the available [minimum-runtime protection](docs/plugins/minruntime.md) and [preemption delay](docs/preemption-delay/README.md). |
| Maximum runtime per queue, with delayed requeue | Remaining | Retained as a roadmap goal. |
| Pod and PodGroup preemption metrics | Partial | Pod eviction and scheduling-action counters exist. Workload-level preemption counts, failure reasons and queue attribution remain tracked in [#2053](https://github.com/kai-scheduler/KAI-Scheduler/issues/2053). |
| User-level fairness | Remaining | Existing [time-based fair share](docs/time-based-fairshare/README.md) operates at queue level. |
| DRA for MIG devices | Remaining | Whole-GPU DRA accounting does not establish MIG-aware DRA support. |
| GPU compute-sharing constraints | Remaining | [GPU sharing](docs/gpu-sharing/README.md), [MPS](docs/gpu-sharing/mps/README.md) and [HAMi memory isolation](docs/gpu-sharing/hami/README.md) are available. The broader sharing proposal remains open in [#1316](https://github.com/kai-scheduler/KAI-Scheduler/issues/1316); the compute-mode/policy tasks [#1522](https://github.com/kai-scheduler/KAI-Scheduler/issues/1522) and [#1523](https://github.com/kai-scheduler/KAI-Scheduler/issues/1523) were closed as not planned, so this goal needs scope confirmation. |
| DRA for fractional GPU devices | Remaining | Existing annotation-based GPU sharing does not establish fractional DRA support. |
| Semi-preemptible workloads | Remaining | The [design](docs/developer/designs/semi-preemptible/README.md) is merged; implementation remains open in [#1585](https://github.com/kai-scheduler/KAI-Scheduler/issues/1585), [#2139](https://github.com/kai-scheduler/KAI-Scheduler/issues/2139) and [#2140](https://github.com/kai-scheduler/KAI-Scheduler/issues/2140). |
| Per-queue resource management for multiple GPU types | Remaining | Per-GPU-type queue quotas remain requested in [#1087](https://github.com/kai-scheduler/KAI-Scheduler/issues/1087). |

### Other capabilities delivered since the original roadmap

- [NUMA-aware scheduling](docs/numa/README.md): opt-in topology feasibility filtering and node scoring.
- [Preemption delay](docs/preemption-delay/README.md): give autoscalers time to provision capacity before a pending workload triggers evictions.
- [Elastic hierarchical PodGroups](docs/elastic/README.md): minimum pod and subgroup counts throughout the hierarchy.
- Declarative grouping for additional workload kinds through the Karta fallback plugin, recorded in the [v0.17.0 changelog](CHANGELOG.md#v0170---2026-08-03).

## 2025 priorities — historical status

These are the original 2025 goals, with their current status; this section does not imply that every completed item shipped in 2025.

| Priority | Status | Reference |
| --- | --- | --- |
| Refactor for vendor neutrality | Remaining | [#134](https://github.com/kai-scheduler/KAI-Scheduler/issues/134) is reopened; naming cleanup is not complete. |
| Scheduling gates | Available | [#63](https://github.com/kai-scheduler/KAI-Scheduler/issues/63), [changelog](CHANGELOG.md). |
| Research possible Kueue integration | Research issue closed | [#68](https://github.com/kai-scheduler/KAI-Scheduler/issues/68) records integration discussion; its closure does not establish native Kueue queue integration. |
| PodGroup topology-aware scheduling | Available | [Topology guide](docs/topology/README.md), [#66](https://github.com/kai-scheduler/KAI-Scheduler/issues/66). |
| Minimum runtime per workload | Available | [Minimum-runtime plugin](docs/plugins/minruntime.md), [#136](https://github.com/kai-scheduler/KAI-Scheduler/issues/136). |
| More default PriorityClasses | Available | [Default priority classes](docs/priority/README.md#priority-classes). |
| JobSet | Available | [JobSet grouping](docs/developer/pod-grouper.md#jobset-grouping), [#763](https://github.com/kai-scheduler/KAI-Scheduler/issues/763). |
| LeaderWorkerSet | Available | [#124](https://github.com/kai-scheduler/KAI-Scheduler/issues/124); subgroup and segment support is covered in the 2026 section. |
| Decouple priority and preemption | Available | Explicit workload preemptibility is independent of priority; see [priority/preemptibility separation](docs/developer/designs/priority-preemptibility-separation/README.md). |
| Specify GPU-fraction container name | Available | [GPU-fraction container selection](docs/gpu-sharing/README.md#gpu-fraction-with-non-default-container), [#654](https://github.com/kai-scheduler/KAI-Scheduler/pull/654). |
| N-level hierarchical queues | Available | [Queues guide](docs/queues/README.md), [#858](https://github.com/kai-scheduler/KAI-Scheduler/pull/858). |
| Time-based fair share | Available | [Time-based fair share](docs/time-based-fairshare/README.md), [#494](https://github.com/kai-scheduler/KAI-Scheduler/pull/494). |

## Long-term goals

- Multi-cluster scheduling.
- Hyperscale improvements; scale testing and scheduler performance work are ongoing.
- Consolidation of inference workloads for cluster defragmentation; [design tracking](https://github.com/kai-scheduler/KAI-Scheduler/issues/1500).
- Graceful rollout of inference workloads using temporary queue over-quota capacity for revision updates.
- Hero Job support.
- Resource reservation and backfill for large gang workloads; [#1969](https://github.com/kai-scheduler/KAI-Scheduler/issues/1969), [design tracking #1987](https://github.com/kai-scheduler/KAI-Scheduler/issues/1987).
- Global priority scheme.
