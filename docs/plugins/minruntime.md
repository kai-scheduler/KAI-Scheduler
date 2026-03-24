# MinRuntime Plugin

## Overview

The MinRuntime plugin for KAI-Scheduler provides runtime protection for jobs by preventing preemption or resource reclamation of jobs until they have run for a specified minimum time duration. This ensures workloads can complete critical initialization or checkpointing before being interrupted.

## Key Features

- **Preemption Protection**: Prevents jobs from being preempted until they have run for a specified minimum duration
- **Reclamation Protection**: Prevents elastic jobs from having resources reclaimed until they have run for a specified minimum duration
- **Queue-based Configuration**: Configure different minimum runtime durations for different queues in your scheduling hierarchy
- **Hierarchical Inheritance**: Minimum runtime settings cascade down from parent queues to leaf queues
- **Flexible Resolution Methods**: Two methods for reclaim minimum runtime resolution:
  - Queue-based: Evaluates the min-runtime protection based on the victim's queue configuration
  - LCA (Lowest Common Ancestor): Uses the common ancestor of reclaimer and victim queues to determine min-runtime protection

## Usage

The minruntime plugin is configured in two ways:

1. **Queue Definition**: Set minimum runtime values in Queue configurations
2. **Plugin Arguments**: Configure default values and resolution methods in the scheduler configuration

### Queue Configuration

Queues can specify the following minimum runtime parameters:

- `preemptMinRuntime`: Minimum runtime before a job in this queue can be preempted
- `reclaimMinRuntime`: Minimum runtime before a job in this queue can have resources reclaimed

Example Queue definition:

```yaml
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata:
  name: production
spec:
  preemptMinRuntime: "20s"
  reclaimMinRuntime: "30s"
```

### Plugin Configuration

In the scheduler configuration (`scheduler-config` ConfigMap), add the minruntime plugin with its arguments:

```yaml
tiers:
- plugins:
  # other plugins...
  - name: minruntime
    arguments:
      defaultPreemptMinRuntime: "10m"
      defaultReclaimMinRuntime: "10m"
      reclaimResolveMethod: "lca"  # or "queue"
```

### Configuration Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `defaultPreemptMinRuntime` | Default minimum runtime before preemption if not specified in queue | "0s" |
| `defaultReclaimMinRuntime` | Default minimum runtime before resource reclamation if not specified in queue | "0s" |
| `reclaimResolveMethod` | Method to resolve reclaim minimum runtime ("lca" or "queue") | "lca" |

0s means workloads are instantly reclaimable/preemptible.

## Resolution Methods

### Preemption Resolution

For preemption, the minimum runtime is determined by starting from the victim's queue and walking up the queue hierarchy until a `preemptMinRuntime` value is found.

### Reclamation Resolution

For reclamation, two methods are supported:

1. **Queue-based Resolution** (`reclaimResolveMethod: "queue"`):
   - Similar to preemption, looks at the victim's queue and walks up the hierarchy until a `reclaimMinRuntime` value is found.
   - If no value is found at all, use the default value for the plugin.

2. **LCA Resolution** (`reclaimResolveMethod: "lca"`):
   - Identifies the Lowest Common Ancestor (LCA) in the queue hierarchy between the preemptor and victim
   - Walks one step down from the LCA towards the victim and uses that queue's `reclaimMinRuntime` value
   - If no value is found, moves up towards the root queue until one is found.
   - If no value is found at all, use the default value for the plugin.

The LCA method is the default method if none is specified. The purpose of the LCA method is to follow how policies might be set for queue hierarchy, allowing users in sub-queues to set min-runtime values that are honored by their siblings, whilst not affecting cousin queues.

## Elastic Jobs Handling

For elastic jobs (where `MinAvailable < number of pods in a job`), the plugin:

1. Allows the job to be considered for preemption/reclamation in the filter phase
2. Validates in the scenario validator phase that the job will maintain its minimum number of pods if min-runtime has not been reached.

## Implementation Details

The plugin implements the following functions:

- `preemptFilterFn`: Filters victims that shouldn't be preempted due to minimum runtime
- `reclaimFilterFn`: Filters victims that shouldn't have resources reclaimed due to minimum runtime
- `preemptScenarioValidatorFn`: Validates preemption scenarios for elastic jobs
- `reclaimScenarioValidatorFn`: Validates reclamation scenarios for elastic jobs

## Example Workflow

1. A job starts running in a queue with `reclaimMinRuntime: "30s"`
2. At 20 seconds of runtime, another job attempts to reclaim resources
3. The minruntime plugin identifies that the minimum runtime has not been reached
4. The victim job is protected from reclamation until it runs for at least 30 seconds

## Caching

The plugin maintains caches to improve performance:
- `preemptMinRuntimeCache`: Caches preemption minimum runtime values by queue
- `reclaimMinRuntimeCache`: Caches reclamation minimum runtime values by queue pair
- `preemptProtectionCache`: Tracks jobs protected from preemption
- `reclaimProtectionCache`: Tracks jobs protected from reclamation

These caches are reset at the beginning of each scheduling session.
