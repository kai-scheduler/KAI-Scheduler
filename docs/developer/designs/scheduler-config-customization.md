# Scheduler Configuration Customization

## Problem

The operator generates a hardcoded scheduler ConfigMap. Users cannot:
- Add or remove actions and plugins
- Modify plugin arguments (only a few are exposed via dedicated fields)

---

## Option 1: Unified Plugin Configuration (Chosen)

Add fields to `SchedulingShardSpec` for plugin and action configuration. Plugin arguments use unstructured `map[string]string` to match the scheduler's internal configuration format.

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: my-shard
spec:
  # Basic fields (unchanged)
  partitionLabelValue: "pool-a"
  placementStrategy:
    gpu: binpack
    cpu: binpack
  kValue: 1.5
  minRuntime:
    preemptMinRuntime: "5m"
    reclaimMinRuntime: "10m"

  # NEW: Plugin configuration (merged with defaults for built-in plugins)
  plugins:
    minruntime:
      enabled: false # Disable this plugin
    proportion:
      arguments: # Override arguments
        kValue: "1.5"
    nodeplacement:
      priority: 500 # Change ordering
      arguments:
        gpu: spread
    mycustomplugin: # Custom plugins go here too
      priority: 250
      arguments:
        key: value

  # NEW: Actions configuration (merged with defaults for built-in actions)
  actions:
    consolidation:
      enabled: false
      priority: 100
    mycustomaction: # Custom actions go here too
      enabled: true
      priority: 50
```

```go
// SchedulingShardSpec defines the desired state of SchedulingShard
type SchedulingShardSpec struct {
	// ... existing fields ...

	// Plugins configures scheduler plugins (both built-in and custom).
	// Built-in plugins use default settings when not specified.
	// Custom plugin names are passed through to the scheduler as-is.
	// +kubebuilder:validation:Optional
	Plugins map[string]PluginConfig `json:"plugins,omitempty"`

	// Actions configures scheduler actions (both built-in and custom).
	// Built-in actions use default settings when not specified.
	// Custom action names are passed through to the scheduler as-is.
	// +kubebuilder:validation:Optional
	Actions map[string]ActionConfig `json:"actions,omitempty"`
}

// PluginConfig defines configuration for a scheduler plugin.
type PluginConfig struct {
	// Enabled controls whether the plugin is active. Defaults to true.
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// Priority determines plugin ordering. Higher values run first.
	// +kubebuilder:validation:Optional
	Priority *int `json:"priority,omitempty"`

	// Arguments contains plugin-specific configuration parameters.
	// Keys and values are passed directly to the plugin.
	// +kubebuilder:validation:Optional
	Arguments map[string]string `json:"arguments,omitempty"`
}

// ActionConfig defines configuration for a scheduler action.
type ActionConfig struct {
	// Enabled controls whether the action is active. Defaults to true.
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// Priority determines action ordering. Higher values run first.
	// +kubebuilder:validation:Optional
	Priority *int `json:"priority,omitempty"`
}
```

**Plugin/Action priorities** (for ordering):

Each action and plugin has a configurable priority for ordering.
The default priorities determine the default order.

**Merge behavior**: Unmentioned plugins/actions use defaults. New plugins from KAI upgrades are auto-included.

**Pros**:
- Explicit state, upgrade-safe
- Simpler code - no per-plugin typed argument structs needed
- Flexible - new plugin arguments can be added without CRD changes
- Matches scheduler's internal representation (`map[string]string`)
- Looser coupling between operator and scheduler plugins

**Cons**:
- Makes our current "internal(?)" scheduler configuration public API that we must maintain
- No CRD-level validation for argument values (validation happens at scheduler startup)
- No IDE autocomplete for plugin arguments
- Users need external documentation to know valid argument keys/values

---

## Option 2: External ConfigMap Reference

Reference a user-managed ConfigMap.

```yaml
spec:
  schedulerConfiguration:
    configMapRef:
      name: my-scheduler-config
    mergeStrategy: overlay # or: replace
```

MergeStrategy `overlay` will use strategic merge patch to merge the user's config with the default config.

**Pros**: Separation of concerns, GitOps-friendly

**Cons**: Two sources of truth, sync issues, harder to validate, limited ordering control, no type safety, user needs to figure out the correct config format, 

---

## Handling Existing Fields

### Fields That Affect Plugins/Actions

| Existing Field | Effect |
|----------------|--------|
| `kValue` | `proportion` plugin arguments |
| `minRuntime` | `minruntime` plugin arguments |
| `placementStrategy.gpu` | `gpupack` vs `gpuspread`, `nodeplacement` args |
| `placementStrategy.cpu` | `nodeplacement` args |
| `placementStrategy` (any spread) | Disables `consolidation` action |

### Recommended Approach: Internal Transformation

1. Existing fields (`placementStrategy`, `kValue`, etc.) are converted to plugin config internally
2. User's `plugins`/`actions` are merged on top (take precedence)

```yaml
spec:
  placementStrategy:
    gpu: spread # Converted internally
  plugins:
    nodeplacement:
      arguments:
        cpu: binpack # User override on top
```

We might want to deprecate the old fields in a backwards compatible way.
