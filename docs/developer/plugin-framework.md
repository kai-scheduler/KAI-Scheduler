# Scheduler Plugin Mechanism

## Overview
The scheduler uses a plugin-based architecture that allows extending its functionality through various extension points. The core mechanism is built around `Session` object that maintains the scheduling context and plugin-registered callbacks.

## Core Components

### Plugin Interface
```go
type Plugin interface {
    Name() string
    OnSessionOpen(ssn *Session)
    OnSessionClose(ssn *Session)
}
```

Plugins are registered using a builder pattern:
```go
type PluginBuilder func(map[string]string) Plugin

// Register a new plugin
RegisterPluginBuilder("my-plugin", func(args map[string]string) Plugin {
    return &MyPlugin{}
})
```

## Key Extension Points

### 1. Session Lifecycle Hooks

- **OnSessionOpen**: Called when a scheduling session starts
- **OnSessionClose**: Called when a scheduling session ends

OnSessionOpen is used to initiate state and register to callback functions. OnSessionClose can be used for cleanup or metrics reporting.

### 2. Session Extention Points

The session object provides the plugins with multiple extension points that the plugins can register callbacks for. For example:

#### Scheduling Order
- `AddJobOrderFn`: Define job ordering within a queue - for example, job priority
- `AddTaskOrderFn`: Define task priority within jobs - for example, attempt to schedule leader pod first
- `AddQueueOrderFn`: Define queue priority ordering - for example, fair share or strict priority
- `AddNodeOrderFn`: Score nodes for task placement - for example, binpack, node affinity


#### Predicates

```go
// Pre-predicate functions run before main predicates
type PrePredicateFn func(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo) error

// Main predicate functions determine if a pod can run on a node
type PredicateFn func(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error

// Register predicates
ssn.AddPrePredicateFn(myPrePredicate)
ssn.AddPredicateFn(myPredicate)
```

Example predicate:
```go
func GPUPredicate(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
    if task.RequiresGPU && !node.HasAvailableGPUs() {
        return fmt.Errorf("node %s has no available GPUs", node.Name)
    }
    return nil
}
```

### 3. Scoring Functions

#### Node Scoring
```go
type NodeOrderFn func(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error)

// Example node scoring function
func GPUUtilizationScore(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
    return float64(node.GetGPUUtilization()), nil
}
```

#### GPU Scoring
```go
type GpuOrderFn func(task *pod_info.PodInfo, node *node_info.NodeInfo, gpuIdx string) (float64, error)
```

#### Job/Task Ordering
```go
// CompareFn returns:
// -1 if left should be scheduled before right
//  0 if equal priority
//  1 if right should be scheduled before left
type CompareFn func(left, right interface{}) int
```

### 4. AllocateFunc/DeallocateFunc Callback Functions

Plugins can register callbacks that are triggered during key scheduling events, allowing the plugin to track simulated scheduling decisions as they happen.  

**callbacks:**
- **AllocateFunc**: Called when a pod is virtually allocated to a node
- **DeallocateFunc**: Called when a pod is virtually evicted from a node

These callbacks enable plugins to:
- Update internal state based on scheduling decisions
- Collect metrics about allocations and evictions
- Trigger side effects (e.g., notifications, logging)

Example of how plugins register callbacks:
```go
// NamespaceAllocationTracker is a plugin that tracks the number of allocated pods per namespace.
type NamespaceAllocationTracker struct {
	podsPerNamespace map[string]int
}

func (nat *NamespaceAllocationTracker) OnSessionOpen(ssn *framework.Session) {
	nat.podsPerNamespace = map[string]int{}
	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			if _, found := nat.podsPerNamespace[event.Task.Namespace]; !found {
				nat.podsPerNamespace[event.Task.Namespace] = 0
			}
			nat.podsPerNamespace[event.Task.Namespace]++
		},
		DeallocateFunc: func(event *framework.Event) {
			if _, found := nat.podsPerNamespace[event.Task.Namespace]; !found {
				nat.podsPerNamespace[event.Task.Namespace] = 0
			}
			nat.podsPerNamespace[event.Task.Namespace]--
		},
	})
}

func (nat *NamespaceAllocationTracker) OnSessionClose(ssn *framework.Session) {
	// Log or publish to metrics
}
```

These callbacks can be used by plugins to maintain their state and enforce policies across the entire scheduler lifecycle.
> Note: as of now, these callbacks cannot return values or fail operations - the plugin is expected to track the relevant changes for internal use. Scenarios can be blocked by other functions, such as `ssn.AddReclaimableFn`. Keep the event handlers lean and efficient. Handle errors and heavy calculations in other functions.

## Best Practices

1. Keep scoring functions lightweight and efficient as they're called very frequently during scheduling simulations.
2. Where possible, initiate state and perform pre-calculations in `OnSessionOpen`, as it's only called once per cycle.

## Example Plugin: Spot Instance Management

The following example demonstrates a plugin that manages spot instances by:
1. Preventing non-preemptible pods from being scheduled on spot instances
2. Scoring spot instances lower to increase their chances of being freed for scaling
3. Using node labels to identify spot instances

```go
type SpotInstancePlugin struct {
	// Configuration parameters
	spotLabelKey   string
	spotLabelValue string
	nonSpotScore   float64
}

func NewSpotInstancePlugin(args map[string]string) Plugin {
	return &SpotInstancePlugin{
		spotLabelKey:   "kai.scheduler/instance-type",
		spotLabelValue: "spot",
		nonSpotScore:   1000, // Non-Spot instances get the score which will rank them higher. For reference on the score used by other plugins, check out scheduler/pkg/plugins/scores/scores.go
	}
}

func (sp *SpotInstancePlugin) Name() string {
	return "spot-instance-manager"
}

func (sp *SpotInstancePlugin) OnSessionOpen(ssn *Session) {
	// Register predicate to prevent non-preemptible pods on spot instances
	ssn.AddPredicateFn(func(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
		// Ignore preemptible jobs
        if job.IsPreemptibleJob() {
			return nil
		}

		// Check if node is a spot instance
		isSpot := node.Node.Labels[sp.spotLabelKey] == sp.spotLabelValue
		if !isSpot {
			return nil
		}

		return fmt.Errorf("non-preemptible pod %s cannot be scheduled on spot instance %s",
			task.Name, node.Name)
	})

	// Register scoring function to prefer regular instances over spot instances
	ssn.AddNodeOrderFn(func(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		// Check if node is a spot instance
		isSpot := node.Node.Labels[sp.spotLabelKey] == sp.spotLabelValue
		if isSpot {
			// Return 0 as score, thus not boosting spot nodes
			return 0, nil
		}

        // Return score of 1000 which will boost non-spot nodes
		return sp.nonSpotScore, nil
	})
}


func (sp *SpotInstancePlugin) OnSessionClose(ssn *Session) {
    // No cleanup needed in our case
}
```

### Usage

1. Label spot instances with `kai.scheduler/instance-type=spot`
2. Register the plugin in your scheduler configuration:

```go
RegisterPluginBuilder("spot-instance-manager", NewSpotInstancePlugin)
```

### How It Works

- **Predicate**: Checks if a node is a spot instance and prevents non-preemptible pods from being scheduled on it
- **Scoring**: 
  - Regular instances get a score of 1000 (GpuSharing constant)
  - Spot instances get a score of 0, making them less preferred for scheduling
  - The scheduler sums scores from all plugins, so regular instances will be preferred over spot instances
- **Configuration**: 
  - Uses node labels to identify spot instances (`kai.scheduler/instance-type=spot`)

This plugin helps manage spot instances by:
- Ensuring only preemptible workloads run on spot instances
- Making spot instances less preferred for scheduling by not boosting their score
- Providing flexibility through configuration parameters

This documentation covers the main extension points of the scheduler plugin mechanism. For more detailed information about specific plugin implementations or advanced features, please refer to the codebase examples and tests.
