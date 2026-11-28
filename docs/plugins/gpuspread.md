# GpuSpread Plugin

## Overview

The `gpuspread` plugin decides **which GPU on a node** a fractional (shared) GPU workload should land on. It prefers the GPU that is the least used and still has room for the workload, so shared workloads are spread across the GPUs of a node. Each workload then competes with fewer neighbours on its GPU.

The plugin works at the device level, inside a node. Which *node* is chosen is a separate decision, made by the `nodeplacement` plugin (see [Scheduler Configuration Customization](../operator/scheduler-config-customization.md)). Its counterpart is [`gpupack`](gpupack.md), which does the opposite.

## Enabling the Plugin

`gpuspread` is enabled when the GPU placement strategy is `spread`. Set it on the relevant `SchedulingShard`:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: default
spec:
  placementStrategy:
    gpu: spread
```

With `gpu: spread`, the operator enables `gpuspread` and disables `gpupack`. With the default `binpack`, `gpupack` is used instead.

Setting the GPU (or CPU) placement strategy to `spread` also turns off the `consolidation` action by default.

You can also toggle the plugins directly. For example, to pack across nodes but spread within each node's GPUs:

```yaml
spec:
  placementStrategy:
    gpu: binpack
  plugins:
    gpupack:
      enabled: false
    gpuspread:
      enabled: true
```

When installing via Helm, the same plugin settings go under `scheduler.plugins` in your values file. See `deployments/kai-scheduler/examples/custom-plugins-actions-values.yaml` for the full syntax, and [Scheduler Configuration Customization](../operator/scheduler-config-customization.md) for details.

## Configuration

The plugin takes no arguments. Its only settings are the ones every plugin has, `enabled` and `priority` (default priority: `300`).

## Behaviour

When a fractional GPU workload is scheduled on a node, the scheduler builds a list of candidate GPUs and scores each one. A candidate is either:

- a GPU that already hosts shared workloads, has enough free memory for this workload, and uses the same compute sharing mode (`time-slicing` or `sm-sharing`) as the workload, or
- a free GPU that has no shared workloads yet. All free GPUs appear as a single "whole GPU" entry type, since they are interchangeable.

`gpuspread` gives each candidate a score of one minus the portion of its memory that is already used by shared workloads:

```
score = 1 - (used GPU memory / total memory of one GPU on the node)
```

GPUs are tried from the highest score to the lowest. Free GPUs get a score of `1`, the highest possible, so they are tried first.

| GPU state | Score |
|---|---|
| Free GPU (no shared workloads yet) | `1` |
| Shared GPU, half of its memory used | `0.5` |
| Shared GPU, 90% of its memory used | `0.1` |

For example, a node has GPU 0 at 75% used, GPU 1 at 25% used, and one free GPU. A workload asking for 0.25 of a GPU, in the same compute sharing mode, fits on all three. `gpuspread` tries the free GPU first, then GPU 1, and only then GPU 0.

If a node does not report a valid per-GPU memory size, the plugin returns an error for that GPU. The scheduler logs it and skips that GPU for this workload.

To see the scores, raise the scheduler log verbosity to 7. Each estimate is logged as `Estimating Task ... for gpuIdx ... Score: ...`.

## Limitations

- **Fractional requests only.** The score is used when placing shared GPU workloads. It has no effect on the placement of whole-GPU workloads.
- **Memory-based.** The score looks only at the GPU memory used by shared workloads. It does not measure GPU compute usage. The compute sharing mode only decides which GPUs are candidates.
- **One node at a time.** The plugin ranks GPUs on a node that was already chosen. It does not choose nodes.
- **Don't enable it together with `gpupack`.** Scores from all active GPU-order plugins are added. `gpupack` and `gpuspread` scores add up to `1` for every GPU, so they cancel out and no GPU is preferred.
- **Ties are unordered.** GPUs with the same score are returned in no specified order.

## Implementation

Source: `pkg/scheduler/plugins/gpuspread/gpuspread.go`. The plugin registers a GPU order function in `OnSessionOpen`. The function reads the used portion from `NodeInfo.GetUsedGpuPortion`.
