# BindRequest Annotations Mutation Plugin Point

## Summary

This design document outlines a new plugin point in the KAI-Scheduler that allows scheduler plugins to modify BindRequest annotations before they are created. This enhancement addresses synchronization issues between the scheduler and binder components.

## Motivation

The current architecture of KAI-Scheduler separates the scheduling and binding processes into distinct components, which improves scalability and error resilience. However, the communication between these components is currently limited to the fixed fields defined in the BindRequest specification.

We need a more open and flexible API that allows plugins in the scheduler to communicate additional information to plugins in the binder without requiring changes to the BindRequest CRD for each new use case. This will enable more sophisticated scheduling and binding behaviors while maintaining a clean separation between components.

By allowing scheduler plugins to add annotations to BindRequests that can be interpreted by corresponding binder plugins, we create an extensible communication channel that can evolve without API changes. This approach preserves backward compatibility while enabling new functionality through plugins.

## Usage Stories

### Node State Synchronization

A plugin in the scheduler detects that a node is suitable for certain GPU workloads and adds annotations to the BindRequest indicating which plugin should handle the bind on the binder. If that plugin can't detect the same environments it can try to refresh the status of the node in the binder cache or reject the bind request.

### Topology Information Transfer

For topology-aware scheduling, a plugin can inject the exact topology location information into the BindRequest annotations. This allows each pod to receive information about its position in the topology (relevant for block implementation) without adding fields to the BindRequest specification.

### Device Management Plugin Communication

Device management plugins can transfer parameters between the scheduler and binder without adding custom fields to the BindRequest, maintaining a clean API while enabling rich functionality.

## Goals

- Enable scheduler plugins to modify BindRequest annotations before creation
- Create a flexible interface for transferring information from scheduler plugins to binder plugins
- Maintain backward compatibility with existing scheduler and binder behavior

## Design Details

### Extension Point Definition

Following the scheduler's plugin extension conventions, we introduce a function type for mutating BindRequest annotations:

```go
// In pkg/scheduler/api/types.go
// BindRequestMutateFn allows plugins to mutate annotations before BindRequest creation.
type BindRequestMutateFn func(pod *pod_info.PodInfo, nodeName string) map[string]string
```

A slice of these functions is added to the `Session` struct, and a registration method is provided:

```go
// In pkg/scheduler/framework/session.go and session_plugins.go
// In Session struct:
BindRequestMutateFns []api.BindRequestMutateFn

// Registration method:
func (ssn *Session) AddBindRequestMutateFn(fn api.BindRequestMutateFn) {
    ssn.BindRequestMutateFns = append(ssn.BindRequestMutateFns, fn)
}
```

### Plugin Registration

Plugins register their mutate function during `OnSessionOpen`:

```go
func (p *MyPlugin) OnSessionOpen(ssn *framework.Session) {
    ssn.AddBindRequestMutateFn(p.MyBindRequestMutateFn)
}

func (p *MyPlugin) MyBindRequestMutateFn(pod *pod_info.PodInfo, nodeName string) map[string]string {
    bindRequestAnnotations := map[string]string{}
    bindRequestAnnotations["my-plugin.kai.scheduler/some-key"] = "some-value"
    return bindRequestAnnotations
}
```

### Annotation Merging and Session Responsibility

The session is responsible for collecting and merging all plugin-provided annotations before passing them to the cache for BindRequest creation. This is done by calling all registered `BindRequestMutateFn` functions and merging their results into a single map named `bindRequestAnnotations`.

If multiple plugins provide the same annotation key, the value from the last plugin registered will overwrite previous values for that key.

### Usage in the Scheduler

When creating a BindRequest, the scheduler will call all registered mutate functions and merge their results:

```go
// In session_plugins.go:
bindRequestAnnotations := map[string]string{}
for _, fn := range ssn.BindRequestMutateFns {
    for k, v := range fn(pod, nodeName) {
        bindRequestAnnotations[k] = v
    }
}
// ... pass bindRequestAnnotations to the cache for BindRequest creation
```

### Binder Plugin Access

Binder plugins already have access to the BindRequest object during the PreBind and PostBind phases, so they can read the annotations added by scheduler plugins:

```go
func (p *MyBinderPlugin) PreBind(ctx context.Context, pod *v1.Pod, node *v1.Node, 
                                bindRequest *v1alpha2.BindRequest, state *state.BindingState) error {
    // Read annotations added by scheduler plugins
    if value, exists := bindRequest.Annotations["my-plugin.kai.scheduler/some-key"]; exists {
        // Use the annotation value to modify binding behavior
    }
    return nil
}
```

### Annotation Naming Convention

To avoid conflicts between different plugins, we recommend using a namespaced approach for annotation keys:

```
<plugin-name>.kai.scheduler/<key>
```

For example:
```
topology-plugin.kai.scheduler/topology-level: "rack"
gpu-plugin.kai.scheduler/requires-env-vars: "true"
```
