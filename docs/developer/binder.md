# Binder

## Overview
The Binder is a controller responsible for handling the pod binding process in Kubernetes. The binding process involves actually placing a pod on its selected node, as well as it's dependencies - volumes, resource claims, etc.

### Why a Separate Binder?

Traditional Kubernetes schedulers handle both node selection and binding within the same component. However, this approach has several limitations:

1. **Error Resilience**: The binding process can fail for various reasons (node state changes, resource contention, API server issues). When this happens in a monolithic scheduler, it might affect the scheduling of other pods.

2. **Performance**: Binding operations involve multiple API calls and can be slow, especially when handling Dynamic Resource Allocation (DRA) or other dependencies, such as volumes. Having the scheduler wait for these operations to complete reduces its throughput.

3. **Retry Management**: Failed bindings often need sophisticated retry mechanisms with exponential backoff, which adds complexity to the scheduler.

By separating the binding logic into its own controller, the scheduler can quickly move on to schedule other pods while the binder handles the potentially slow or error-prone binding process asynchronously.

### Communication via BindRequest API

The scheduler and binder communicate through a custom resource called `BindRequest`. When the scheduler decides where a pod should run, it creates a BindRequest object that contains:

- The pod to be scheduled
- The selected node
- Information about resource allocations (including GPU resources)
- DRA (Dynamic Resource Allocation) binding information
- Retry settings

The BindRequest API serves as a clear contract between the scheduler and binder, allowing them to operate independently.

### Binding Process

1. The scheduler creates a BindRequest for each pod that needs to be bound
2. The binder controller watches for BindRequest objects
3. When a new BindRequest is detected, the binder:
   - Attempts to bind the pod to the specified node
   - Handles any DRA or Persistent Volume allocations
   - Updates the BindRequest status to reflect success or failure
   - Retries failed bindings according to the backoff policy
4. Until the pod is bound, the scheduler considers the bind request status as the expected scheduling result for this pod and it's dependencies.

### Error Handling

Binding can fail for various reasons:
- The node may no longer have sufficient resources
- API server connectivity issues
- Intermittent issues with dependencies

The binder tracks failed attempts and can retry up to a configurable limit (BackoffLimit). If binding ultimately fails, the BindRequest is marked as failed, allowing the scheduler to potentially reschedule the pod.

## Extending the binder

### Binder Plugins

The binder uses a plugin-based architecture that allows for extending its functionality without modifying core binding logic. Plugins can participate in different stages of the binding process and implement specialized handling for various resource types or pod requirements.

#### Plugin Interface

All binder plugins must implement the following interface:

```go
type Plugin interface {
    // Name returns the name of the plugin
    Name() string
    
    // Validate checks if the pod configuration is valid for this plugin
    Validate(*v1.Pod) error
    
    // Mutate allows the plugin to modify the pod before scheduling
    Mutate(*v1.Pod) error
    
    // PreBind is called before the pod is bound to a node and can perform
    // additional setup operations required for successful binding
    PreBind(ctx context.Context, pod *v1.Pod, node *v1.Node, 
            bindRequest *v1alpha2.BindRequest, state *state.BindingState) error
    
    // PostBind is called after the pod is successfully bound to a node
    // and can perform cleanup or logging operations
    PostBind(ctx context.Context, pod *v1.Pod, node *v1.Node, 
             bindRequest *v1alpha2.BindRequest, state *state.BindingState)
}
```

Each method serves a specific purpose in the binding lifecycle:

- **Name**: Returns the unique identifier of the plugin.
- **Validate**: Verifies that pod configuration is valid for this plugin's concerns. For example, the GPU plugin validates that GPU resource requests are properly specified.
- **Mutate**: Allows the plugin to modify the pod spec before binding, such as injecting environment variables or container settings.
- **PreBind**: Executes before binding occurs and can perform prerequisite operations like volume or resource claim allocation.
- **PostBind**: Runs after successful binding for cleanup or logging purposes.

### Configuration

Binder plugins can be configured through `spec.binder.plugins` in the KAI `Config` CR or through Helm values under `binder.plugins`. The operator serializes the resolved configuration into the binder process `--plugins` argument. Binder plugin configuration is not stored in a ConfigMap.

Each plugin entry has the following fields:

```yaml
enabled: true
priority: 300
arguments:
  key: value
```

- `enabled`: Whether the plugin should run. Defaults to `true`.
- `priority`: Higher priority plugins run first. Plugins with equal priority are ordered by plugin name.
- `arguments`: String key-value arguments passed to the plugin builder.

The binder defaulting logic starts with the built-in defaults and merges user overrides:

- Omitted built-in plugins keep their default settings.
- `enabled` and `priority` are merged independently.
- If `arguments` is specified for a plugin, it replaces that plugin's default arguments.
- A configured plugin that is not part of the built-in default set defaults to `enabled: true` and priority `0`. The binder binary must register a matching plugin builder for that plugin name.

### Default Plugins

The current default binder plugins are:

| Plugin | Priority | Arguments | Purpose |
| --- | ---: | --- | --- |
| `volumebinding` | 300 | `bindTimeoutSeconds: "120"` | Handles Kubernetes persistent volume binding before pod bind. |
| `dynamicresources` | 200 | `bindTimeoutSeconds: "120"` | Handles Kubernetes Dynamic Resource Allocation claim binding before pod bind. |
| `gpusharing` | 100 | `cdiEnabled: "false"`, `hamiCoreEnabled: "false"` | Handles fractional GPU pod mutation needed for GPU sharing. |

For operator-managed deployments, the operator sets the `gpusharing` `cdiEnabled` argument from `spec.binder.cdiEnabled`. If `spec.binder.cdiEnabled` is unset, the operator attempts to auto-detect CDI from the NVIDIA GPU Operator `ClusterPolicy`. Set `hamiCoreEnabled: "true"` to opt into HAMI-core GPU memory limits.

### Config Examples

Disable the GPU sharing plugin:

```yaml
apiVersion: kai.scheduler/v1
kind: Config
spec:
  binder:
    plugins:
      gpusharing:
        enabled: false
```

Change the volume binding timeout:

```yaml
apiVersion: kai.scheduler/v1
kind: Config
spec:
  binder:
    plugins:
      volumebinding:
        arguments:
          bindTimeoutSeconds: "60"
```

Change plugin ordering:

```yaml
apiVersion: kai.scheduler/v1
kind: Config
spec:
  binder:
    plugins:
      dynamicresources:
        priority: 400
      volumebinding:
        priority: 300
```

Equivalent Helm values:

```yaml
binder:
  plugins:
    gpusharing:
      enabled: false
    volumebinding:
      arguments:
        bindTimeoutSeconds: "60"
```

The binder binary also accepts plugin configuration directly as JSON:

```bash
--plugins='{"gpusharing":{"enabled":false}}'
```

### Built-In Plugins

#### Volume Binding Plugin

The volume binding plugin handles Kubernetes persistent volume binding work before the final pod bind. It uses the `bindTimeoutSeconds` argument to control how long it waits for binding operations.

#### Dynamic Resources Plugin

The dynamic resources plugin handles Kubernetes Dynamic Resource Allocation resources. It processes resource claim allocations from the `BindRequest` and updates the relevant resource claims before the pod is bound. It also uses the `bindTimeoutSeconds` argument.

#### GPU Sharing Plugin

The GPU sharing plugin handles fractional GPU assignments. For shared GPU allocations it creates the required GPU sharing ConfigMaps and sets the NVIDIA visible devices and GPU portion information on the target container.

When `hamiCoreEnabled` is `true`, the plugin also writes `CUDA_DEVICE_MEMORY_LIMIT` to the GPU sharing ConfigMap based on node label `nvidia.com/gpu.memory` and `BindRequest.spec.receivedGPU.portion`. This is disabled by default; enable it to opt into HAMI-core memory isolation.

### Creating Custom Plugins

To create a custom binder plugin:

1. Implement the `Plugin` interface.
2. Register a plugin builder with the binder plugin registry using the same name that users will configure.
3. Define and validate the plugin's expected string arguments.
4. Ensure the plugin handles errors gracefully and provides clear error messages.

Custom plugins can address specialized use cases such as:
- Network configuration and policy enforcement
- Custom resource binding and setup
- Integration with external systems
- Advanced validation and mutation based on organizational policies