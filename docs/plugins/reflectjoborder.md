# Reflect Job Order Plugin

## Overview

The `reflectjoborder` plugin exposes the order in which the scheduler intends to process pending PodGroups (jobs) during the next scheduling cycle. It is the supported way to answer the question *"where does my workload sit in its queue right now?"*.

The plugin is read-only: it observes the same ordering used by the `Allocate` action and serves it over an HTTP endpoint on the scheduler pod. Each scheduling cycle refreshes the data.

## Enabling the Plugin

The plugin is registered in the scheduler binary but **not enabled by default**. Enable it on the relevant `SchedulingShard`:

```yaml
apiVersion: kai.scheduler/v1
kind: SchedulingShard
metadata:
  name: default
spec:
  plugins:
    reflectjoborder:
      enabled: true
```

When installing via Helm, set `scheduler.plugins` in your values file:

```yaml
scheduler:
  plugins:
    reflectjoborder:
      enabled: true
```

See `deployments/kai-scheduler/examples/custom-plugins-actions-values.yaml` for the full plugin-configuration syntax.

## Querying Job Order

The plugin registers an HTTP endpoint `/get-job-order` on the scheduler pod. Port-forward to the scheduler and call it:

```bash
kubectl port-forward -n kai-scheduler deployment/kai-scheduler-default 8081 &
sleep 2
curl -s "localhost:8081/get-job-order" | jq
```

## Response Format

```json
{
  "global_order": [
    { "id": "team-a/training-7d2", "priority": 100 },
    { "id": "team-b/inference-9c1", "priority": 75 }
  ],
  "queue_order": {
    "team-a": [
      { "id": "team-a/training-7d2", "priority": 100 }
    ],
    "team-b": [
      { "id": "team-b/inference-9c1", "priority": 75 }
    ]
  }
}
```

| Field | Description |
|---|---|
| `global_order` | All eligible jobs across all queues, in scheduling order. Index 0 is next to be considered. |
| `queue_order` | Same jobs grouped by queue. Index within a queue is that workload's position in its queue. |
| `id` | PodGroup UID (`<namespace>/<name>`). |
| `priority` | Effective priority used for ordering. |

A workload's queue position is `index in queue_order[<queue>] + 1`.

## Caveats

- **Per-cycle snapshot.** The data is computed at the start of each scheduling cycle; it is not updated continuously. A workload's reported position may be a few seconds stale.
- **Pending and ready jobs only.** The plugin filters with `FilterNonPending: true` and `FilterUnready: true`, so running jobs and jobs not yet ready for scheduling do not appear (`pkg/scheduler/actions/utils/input_jobs.go`).
- **Bounded by `QueueDepthPerAction[Allocate]`.** Only the first *N* jobs per queue are reported, where *N* is the configured allocate queue depth. Jobs beyond that depth are excluded, even if pending.
- **No history.** Only the most recent cycle is exposed; there is no time-series store. To track position over time, scrape the endpoint on an interval.
- **Not authenticated.** The endpoint is served on the scheduler pod's HTTP port. Reach it via `kubectl port-forward` or an in-cluster client; do not expose it publicly.

## Implementation

Source: `pkg/scheduler/plugins/reflectjoborder/reflect_job_order.go`. The plugin populates `ReflectJobOrder` in `OnSessionOpen` by draining `utils.NewJobsOrderByQueues(...)` and registers `serveJobs` on `/get-job-order`.
