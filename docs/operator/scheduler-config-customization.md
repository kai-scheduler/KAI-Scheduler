# Scheduler Config Customization

Each shard's scheduler is configured with a set of **plugins** (scoring, filtering, and ordering logic) and **actions** (scheduling operations like allocate, preempt, reclaim). You can customize both per-shard using the `plugins` and `actions` fields on the `SchedulingShard` spec.

User overrides are merged with defaults:
- Omitted plugins/actions keep their default settings.
- `enabled` and `priority` are merged individually (only overwritten if specified).
- `arguments` fully replaces the default arguments when specified.
- New plugins/actions added by future KAI upgrades are automatically included.

## Default Plugins

| Plugin | Priority | Description |
|--------|----------|-------------|
| predicates | 1900 | Node filtering |
| proportion | 1800 | Fair-share quota allocation |
| priority | 1700 | Job priority scoring |
| nodeavailability | 1600 | Node availability filtering |
| resourcetype | 1500 | Resource type matching |
| podaffinity | 1400 | Pod affinity/anti-affinity |
| elastic | 1300 | Elastic job support |
| kubeflow | 1200 | Kubeflow job support |
| ray | 1100 | Ray job support |
| subgrouporder | 1000 | Subgroup ordering |
| taskorder | 900 | Task ordering |
| nominatednode | 800 | Nominated node preference |
| dynamicresources | 700 | DRA resource handling |
| minruntime | 600 | Minimum runtime protection |
| topology | 500 | Topology-aware placement |
| snapshot | 400 | Cluster state snapshot |
| gpupack / gpuspread | 300 | GPU device packing or spreading (based on `placementStrategy.gpu`) |
| nodeplacement | 200 | Node-level placement strategy |
| gpusharingorder | 100 | GPU sharing order (binpack only) |

## Default Actions

| Action | Priority | Description |
|--------|----------|-------------|
| allocate | 500 | Schedule pending jobs |
| consolidation | 400 | Consolidate fragmented workloads (disabled when any placement strategy is spread) |
| reclaim | 300 | Reclaim over-quota resources |
| preempt | 200 | Preempt lower-priority jobs |
| stalegangeviction | 100 | Evict stale gang-scheduled pods |

Higher priority values run first.

## Examples

### Disable a built-in plugin

```yaml
spec:
  plugins:
    elastic:
      enabled: false
```

### Change plugin priority (reordering)

Move `predicates` to run after most other plugins:

```yaml
spec:
  plugins:
    predicates:
      priority: 50
```

### Override plugin arguments

```yaml
spec:
  plugins:
    proportion:
      arguments:
        kValue: "2.0"
```

When `arguments` is specified, it fully replaces the default arguments for that plugin.

### Add a custom plugin

```yaml
spec:
  plugins:
    mycustomplugin:
      priority: 1050
      arguments:
        key: value
```

Custom plugins default to `enabled: true` and `priority: 0` if not specified.

### Disable an action

```yaml
spec:
  actions:
    consolidation:
      enabled: false
```

### Independent node-level and device-level placement

Spread across nodes but pack within each node's GPUs:

```yaml
spec:
  placementStrategy:
    gpu: spread  # Node-level: spread across nodes
  plugins:
    gpuspread:
      enabled: false
    gpupack:
      enabled: true  # Device-level: pack within a node
```

The `placementStrategy` controls the `nodeplacement` plugin arguments (node-level), while `gpupack`/`gpuspread` control device-level placement within a node.
