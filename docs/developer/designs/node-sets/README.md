# NodeSets

## Overview
This document outlines an enhancement to the scheduling procedure by introducing the concept of NodeSets, enabling new granularity of pod scheduling over cluster of nodes, by hooking a cluster nodes division logic, and performing scheduling attempts over the resulting sets.

Topology aware scheduling is a good example of such requirement - Attempting to schedule a job over a set of nodes under same topological domain. While such feature can be implemented in pre-predicate and predicate hooks, multi domain is not feasible, limiting the topology scheduling capabilities.

In this document we introduce a new concept of `NodeSet` which is a set of nodes the scheduler attempt to schedule a job over. 

## Motivation
Current implementation of topology scheduling is limited to a single domain attempt. This means that when a workload required to be scheduled with topology constraint the plugin searches for a capable domain (without certainty the job is schedulable over) and try to allocate the job over. If the attempt fails, it will not look for a different domain capable to schedule this [workload](https://github.com/kai-scheduler/kai-scheduler/tree/main/docs/developer/designs/topology-awareness#stage-2-simulation-based-evaluation).

The uncertainty comes from the fact that the topology plugin can only have a best effort decision, as it does not aware of all scheduling constraints, such as NodeAffinity, NodeSelector, PVC, etc...

## Goals
- Design a framework hook that allows multi-domain attempt when scheduling with topology scheduling
- Provide abstraction for future division logics
- Define clear behavior in multi plugins environments
- Define user experience in API (Kubernetes events)

## Non-Goals
- Declaring which plugins will implement such hook
- Define the topology plugin internal logic

## Key Concepts

- Scheduling framework - A set of APIs for developers to extend the behavior of KAI by implementing designated logic in multiple decision points during scheduling cycle
- Plugin - A piece of code extending KAI scheduler native behavior by implementing custom logic
- Hooks - Extension points for plugin developers, providing multiple points for affecting scheduling decisions
- Session - A runtime object representing the scheduling cycle, exposing functions to activate registered plugins
- Topology Scheduling Plugin - A plugin implementing topology scheduling logic

## High Level

### NodeSet
Introduce new type of `NodeSet`

```go
type NodeSet []*NodeInfo
```

This type is defined as a zero or more nodes (Single NodeSet equals to multiple nodes grouped together).

### SubsetNodes hook
Introduce the new hook that for a given workload returns a list of NodeSets
```go
type SubsetNodesFn func(PodGroup) []NodeSet
```

Such function accept workload as argument and provides multiple NodeSets to attempt scheduling over.
By implementing such function you can register your code as SubsetNodes plugin.

A node can be in a zero or more NodeSets.

## Defined behavior

### Registration 
Like any other hook, the developer require to register the plugin prior to compilation.
```go
framework.RegisterPluginBuilder("myPlugin", myPlugin.New)
```

In addition the user should add the plugin name into the scheduler configuration.

### Session function
The session object expose API for activating the plugins that implement the hook.

```go
func (ssn *Session) SubsetNodes(PodGroup) []NodeSet
```

Such function is accessible during a scheduling cycle, as long as the session exists, and exposes the logic of all the implementing plugins.

In case of multiple plugins that implement that hook, the expected behavior is multiple layers of SubSetting.
For example if you have 2 plugins `P` and `R`. Dividing the cluster nodes happens in 2 steps:

First plugin, `P`, divide all cluster nodes to subsets:
```
AllNodes -> P1, P2, P3
```
Then plugin R, divide each subset:
```
P1 -> P1R1, P1R2
P2 -> P2R1, P2R2, P2R3
P3 -> P3R1, P3R2
```
So the final results of subsets is:
```
P1R1, P1R2, P2R1, P2R2, P2R3, P3R1, P1R2
```


### During scheduling

During scheduling when a workload is chosen to be scheduled, the scheduler algorithm invokes that hook before attempting to allocate any task, dividing the cluster nodes to NodeSets.

As opposed to traditional behavior, we will now attempt to schedule the workload over each NodeSet, until we find one over its scheduled, and stop the iteration.

```go
for podGroup := range PodGroups {
	nodeSets := ssn.SubsetNodes(podGroup)
	for nodeSet := range nodeSets {
		if attemptToAllocatePodGroup(podGroup, nodeSet) {
			break
        }   
    }
}
```
It means that for each PodGroup we perform the division, and then try each NodeSet, until we find a matching one.


## Low level design

```go
package NodeInfo

type NodeSet []*NodeInfo
```

```go
package api

type SubsetNodesFn func(job *podgroup_info.PodGroupInfo, tasks []*pod_info.PodInfo, nodeSet node_info.NodeSet) (bool, []node_info,NodeSet, error)
```

The `tasks` parameter allows the developer to perform different logic in case of real allocation (For example, topology plugin need to count pending pods only, or also virtually evicted pods during simulation).

The return values are a boolean indicating weather the plugin is relevant for that job (for example, topology requirement is only relevant if the user specify topology constraint), a list of NodeSets, and an error.

## User experience

### Developer logs

On subsetting logic, we should log out (v4) each plugin subsetting result len, and on extended logs (v7) also the node names
```go
func SubsetNodes(PodGroup) {
	for plugin := range registeredPlugins {
		log.v4("Performing {plugin} logic")
		subsets = plugin()
        log.v4("Result of {plugin} logic is {len(subsets)} subsets")
        log.v7("Result of {plugin} logic is {for each subset [{for each node in subset.Nodes}]}")
    }
}
```

For example:
```
V4: Performing topology logic
V4: Result of topology logic is 3 subsets
V7: Result of topology logic is [Node1, Node2], [Node3, Node6], [Node4, Node5, Node7]
```

On failure of running a plugin (`err != nil`) we should log the failure with the name of the plugin and the error message.

It is the developer responsibility to log the internal logic of his plugin.

### Kubernetes events

No changes in user experience, we do not want to expose to the user the concept of NodeSets. In case we did not manage to schedule the job over all the NodeSets, the user should see "unschedulable on cluster".