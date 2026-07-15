[License](LICENSE) [Coverage](https://github.com/kai-scheduler/KAI-scheduler/blob/main/.github/workflows/update-coverage-badge.yaml)
[Ask DeepWiki](https://deepwiki.com/kai-scheduler/KAI-scheduler)

# Roadmap

## High-level overview of the main priorities for 2026

- Support DRA for NVidia whole GPUs allocation
- Support automatic sub-grouping of Ray, Pytorch and LWS
- Block topology aware scheduling
- Support scheduling placement strategy at the GPU level (currently supported at the node level)
- Support K8S workload/pod-group API
- Support Max Run Time per workload (with delayed requeue)
- Max run time per queue (with delayed requeue)
- Add metrics for pod and pod-group preemptions
- User-level fairness
- Support DRA for MIG devices 
- Support GPU compute sharing constraints
- Support DRA for fractional GPU devices
- Semi-preemptible workloads
- Per queue multiple GPU types resource management


## High-level overview of the main priorities for 2025

- Refactor the codebase to enhance vendor neutrality [https://github.com/kai-scheduler/KAI-scheduler/issues/134](https://github.com/kai-scheduler/KAI-scheduler/issues/134)
- Support Scheduling Gates [https://github.com/kai-scheduler/KAI-scheduler/issues/63](https://github.com/kai-scheduler/KAI-scheduler/issues/63)
- Research on possible integration with Kueue [https://github.com/kai-scheduler/KAI-scheduler/issues/68](https://github.com/kai-scheduler/KAI-scheduler/issues/68)
- Add Topology Aware Scheduling support of pod-group [https://github.com/kai-scheduler/KAI-scheduler/issues/66](https://github.com/kai-scheduler/KAI-scheduler/issues/66)
- Support Min Run Time per workloads [https://github.com/kai-scheduler/KAI-scheduler/issues/136](https://github.com/kai-scheduler/KAI-scheduler/issues/136)
- Add more PriorityClasses as part of the default KAI install
- Support JobSet [https://github.com/kai-scheduler/KAI-scheduler/issues/763](https://github.com/kai-scheduler/KAI-scheduler/issues/763)
- Support LWS (LeaderWorkerSet) [https://github.com/kai-scheduler/KAI-scheduler/issues/124](https://github.com/kai-scheduler/KAI-scheduler/issues/124)
- Decouple Priority and Preemption
- Specify fraction container name [#654](https://github.com/kai-scheduler/KAI-scheduler/pull/654)
- Support n-levels of hierarchical queues ​​[#858](https://github.com/kai-scheduler/KAI-scheduler/pull/858)
- Add Time-based Fairshare [#494](https://github.com/kai-scheduler/KAI-scheduler/pull/494)


## Long term goals

- Add support for multi-cluster scheduling
- Hyper scale improvements
- Support Consolidation of Inference workloads for cluster defragmentation
- Graceful rollout of Inference workloads (new revision update using queue temporary over-quota)
- Support Hero Job
- Support resource reservation and resources backfill
- Support global priority scheme

