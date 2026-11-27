# Node Placement Plugin

## Overview

The `nodeplacement` plugin decides which node a pod should prefer once the scheduler knows which nodes can fit it. It scores every fitting node so that pods are either packed onto the fullest nodes (`binpack`) or spread across the emptiest nodes (`spread`).

The strategy is chosen separately for two kinds of pods:

- **GPU pods**: pods that request any kind of GPU (whole GPUs, a GPU fraction or GPU memory, a MIG profile, or GPUs through DRA). They are scored by their free GPUs.
- **CPU-only pods**: every other pod. They are scored by their free CPU.

The plugin works at the node level only. Which GPU devices a pod gets inside the chosen node is decided by the `gpupack` / `gpuspread` plugins.

## Enabling the Plugin

The plugin is **enabled by default**, with both strategies set to `binpack`. You normally don't enable it yourself; you pick the strategies through `placementStrategy` on the `SchedulingShard`:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: default
spec:
  placementStrategy:
    gpu: spread
    cpu: binpack
```

The operator turns `placementStrategy` into the plugin's arguments. See [Scheduler configuration customization](../operator/scheduler-config-customization.md) for how this interacts with `gpupack` / `gpuspread` and with the `consolidation` action.

## Configuration

| Argument | Values | Default | Applies to |
|---|---|---|---|
| `gpu` | `binpack`, `spread` | `binpack` | Pods that request any kind of GPU |
| `cpu` | `binpack`, `spread` | `binpack` | CPU-only pods |

A missing argument falls back to `binpack`.

## Behaviour

For each pod, the plugin adds a score to every node that passed filtering. The node scores from all plugins are summed, and the scheduler prefers nodes with a higher total.

### `binpack`

Before scoring, the plugin looks at the fitting nodes that have the resource (GPU or CPU) and records the smallest and largest amount still free on them. Each node then gets:

```
score = 9 * (1 - (free on this node - smallest free) / (largest free - smallest free))
```

- The node with the least free resource gets the highest score, 9, and the node with the most free resource gets 0. So pods fill up nodes that are already busy and keep other nodes empty for large jobs.
- When every fitting node has the same amount free, all of them get 9, unless none of them has any free, in which case all get 0.
- A node without the resource at all (for example a CPU node for a GPU pod) gets 0 and is left out of the smallest/largest calculation.

### `spread`

Each node gets the fraction of its resource that is still free:

```
score = free on this node / total on this node
```

For GPU pods, the total is the number of GPUs on the node; for CPU-only pods, it is the node's allocatable CPU. An empty node scores 1 and a full node scores 0, so pods go to the least loaded nodes. A node without the resource scores 0.

## Limitations

- **Two values only.** Only `spread` switches a resource to spreading; any other value, including a typo, behaves as `binpack`.
- **One resource per pod.** GPU pods are scored on GPUs only and CPU-only pods on CPU only. Memory and other resources don't affect this plugin's score.
- **A preference, not a rule.** The score is added to the scores of the other node-ordering plugins, such as `topology`, so it can be outweighed. It never makes a node ineligible.
- **Different score ranges.** `binpack` scores range from 0 to 9, while `spread` scores range from 0 to 1, so the same plugin carries less weight in the total when it spreads.

## Implementation

Source: `pkg/scheduler/plugins/nodeplacement/`.
- `nodeplacement.go` reads the `gpu` and `cpu` arguments and picks the scoring functions per pod type in `OnSessionOpen`.
- `pack.go` records the free range in a node pre-order function (`setBinpackPreOrder`) and scores nodes in `nodeResourcePack`.
- `spread.go` scores nodes in `nodeResourceSpread`.
- Tests: `nodepack_test.go` and `nodespread_test.go`.
