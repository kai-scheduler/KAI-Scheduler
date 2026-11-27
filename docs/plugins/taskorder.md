# Task Order Plugin

## Overview

The `taskorder` plugin lets you choose which pods of the same workload the scheduler places first. You give pods a priority with the `kai.scheduler/task-priority` label, and pods with a higher value are placed before pods with a lower value or no label.

The order applies only between pods (tasks) of one PodGroup. It doesn't change which workload or queue goes first; that is decided by other plugins, such as `priority` and `proportion`.

## Enabling the Plugin

The plugin is **enabled by default** and takes no arguments. To use it, set the label on the pods, usually in the workload's pod template:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: worker-0
  labels:
    kai.scheduler/task-priority: "10"
```

## Configuration

| Label | Value | Meaning |
|---|---|---|
| `kai.scheduler/task-priority` | An integer, for example `"10"` | Higher values are placed first |

There are no plugin arguments.

## Behaviour

When the scheduler compares two pods of the same PodGroup:

1. A pod with the label comes before a pod without it.
2. Between two labeled pods, the higher integer comes first.
3. Equal values, or two pods without the label, are a tie.

Ties are passed on to the next ordering rule. If every rule ties, the scheduler falls back to pod creation time (older first) and then pod UID.

The `kubeflow` and `ray` plugins come before `taskorder` in the default plugin list and also order pods: Kubeflow pods with the `training.kubeflow.org/job-role` label set to `master` or `launcher`, and Ray pods with `ray.io/node-type: head`, go first. `taskorder` only decides between pods those plugins treat as equal.

The order is used by the `allocate`, `reclaim`, `preempt` and `consolidation` actions when they pick which pending pods of a workload to place. It matters most when only some of those pods are being placed, for example an elastic workload that grows one pod at a time above its minimum size.

## Limitations

- **Integers only.** A value that isn't an integer, such as `"high"`, ranks after any valid integer value, so don't rely on it.
- **Within one PodGroup.** Pods of different workloads are never compared by this plugin.
- **Order, not a guarantee.** A higher task priority means the pod is considered earlier. It doesn't reserve resources or guarantee that the pod is placed if it doesn't fit.

## Implementation

Source: `pkg/scheduler/plugins/taskorder/task_order.go`. The plugin registers `TaskOrderFn` with the session in `OnSessionOpen`. The label key is `TaskOrderLabelKey` in `pkg/scheduler/constants/labels/labels.go`. Tests: `task_order_test.go`.
