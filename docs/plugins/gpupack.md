# GpuPack Plugin

## Overview

The `gpupack` plugin decides **which GPU on a node** a fractional (shared) GPU workload should land on. It prefers the GPU that is already the most used and still has room for the workload, so shared workloads fill up one GPU before the next one is touched. This leaves other GPUs free for whole-GPU workloads.

The plugin works at the device level, inside a node. Which *node* is chosen is a separate decision, made by the `nodeplacement` plugin (see [Scheduler Configuration Customization](../operator/scheduler-config-customization.md)). Its counterpart is [`gpuspread`](gpuspread.md), which does the opposite.

## Enabling the Plugin

`gpupack` is part of the default scheduler configuration, and it is enabled when the GPU placement strategy is `binpack` (the default).

To select it explicitly, set the GPU placement strategy on the relevant `SchedulingShard`:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: default
spec:
  placementStrategy:
    gpu: binpack
```

With `gpu: spread`, the operator disables `gpupack` and enables `gpuspread` instead.

You can also toggle the plugins directly. For example, to spread across nodes but pack within each node's GPUs:

```yaml
spec:
  placementStrategy:
    gpu: spread
  plugins:
    gpuspread:
      enabled: false
    gpupack:
      enabled: true
```

When installing via Helm, the same plugin settings go under `scheduler.plugins` in your values file. See `deployments/kai-scheduler/examples/custom-plugins-actions-values.yaml` for the full syntax, and [Scheduler Configuration Customization](../operator/scheduler-config-customization.md) for details.

## Configuration

The plugin takes no arguments. Its only settings are the ones every plugin has, `enabled` and `priority` (default priority: `300`).

## Behaviour

When a fractional GPU workload is scheduled on a node, the scheduler builds a list of candidate GPUs and scores each one. A candidate is either:

- a GPU that already hosts shared workloads, has enough free memory for this workload, and uses the same compute sharing mode (`time-slicing` or `sm-sharing`) as the workload, or
- a free GPU that has no shared workloads yet. All free GPUs appear as a single "whole GPU" entry type, since they are interchangeable.

`gpupack` gives each candidate a score equal to the portion of its memory that is already used by shared workloads:

```
score = used GPU memory / total memory of one GPU on the node
```

GPUs are tried from the highest score to the lowest. Free GPUs get a score of `0`, so they are the last choice.

| GPU state | Score |
|---|---|
| Free GPU (no shared workloads yet) | `0` |
| Shared GPU, half of its memory used | `0.5` |
| Shared GPU, 90% of its memory used | `0.9` |

For example, a node has GPU 0 at 75% used, GPU 1 at 25% used, and one free GPU. A workload asking for 0.25 of a GPU, in the same compute sharing mode, fits on all three. `gpupack` tries GPU 0 first, then GPU 1, and only then the free GPU.

If a node does not report a valid per-GPU memory size, the plugin returns an error for that GPU. The scheduler logs it and skips that GPU for this workload.

To see the scores, raise the scheduler log verbosity to 7. Each estimate is logged as `Estimating Task ... for gpuIdx ... Score: ...`.

## Limitations

- **Fractional requests only.** The score is used when placing shared GPU workloads. It has no effect on the placement of whole-GPU workloads.
- **Memory-based.** The score looks only at the GPU memory used by shared workloads. It does not measure GPU compute usage. The compute sharing mode only decides which GPUs are candidates.
- **One node at a time.** The plugin ranks GPUs on a node that was already chosen. It does not choose nodes.
- **Don't enable it together with `gpuspread`.** Scores from all active GPU-order plugins are added. `gpupack` and `gpuspread` scores add up to `1` for every GPU, so they cancel out and no GPU is preferred.
- **Ties are unordered.** GPUs with the same score are returned in no specified order.

## Implementation

Source: `pkg/scheduler/plugins/gpupack/gpupack.go`. The plugin registers a GPU order function in `OnSessionOpen`. The function reads the used portion from `NodeInfo.GetUsedGpuPortion`.
