# MinRuntime Plugin

## Overview

The MinRuntime plugin for KAI-Scheduler provides runtime protection for jobs by preventing preemption, resource reclamation, or consolidation until they have run for a specified minimum time duration. This ensures workloads can complete critical initialization or checkpointing before being interrupted.

## Key Features

- **Preemption Protection**: Prevents jobs from being preempted until they have run for a specified minimum duration
- **Consolidation Protection**: Prevents jobs from being moved while either their preemption or reclamation minimum runtime still applies
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

### Consolidation Resolution

Consolidation honors both existing minimum runtimes. A victim can be moved only after both its resolved preemption and reclamation protection periods expire. Preemption resolution uses the victim's queue hierarchy; reclamation resolution uses the pending job and victim queues with the configured `reclaimResolveMethod`. Defaults and queue overrides apply as above, with no separate consolidation setting.

Protection is measured from the victim's `LastStartTimestamp`, as for preemption and reclamation. Jobs with no start timestamp are unprotected. With both resolved durations set to zero, consolidation behavior is unchanged.

## Elastic Jobs Handling

For elastic jobs (where `MinAvailable < number of pods in a job`), the plugin:

1. Allows the job to be considered for preemption/reclamation in the filter phase
2. Validates in the scenario validator phase that the job will maintain its minimum number of pods if min-runtime has not been reached.

Consolidation filters out protected elastic jobs entirely, because moving a job can interrupt its minimum members as well as its extra pods.

## Implementation Details

The plugin implements the following functions:

- `preemptFilterFn`: Filters victims that shouldn't be preempted due to minimum runtime
- `reclaimFilterFn`: Filters victims that shouldn't have resources reclaimed due to minimum runtime
- `consolidationFilterFn`: Filters victims while either minimum runtime still protects them, including elastic jobs
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
