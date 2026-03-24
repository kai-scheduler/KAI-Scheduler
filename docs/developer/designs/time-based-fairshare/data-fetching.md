# Usage data fetching – High-Level Design

> Note: *Goals, non-goals, and detailed motivation are captured in* `time-based-fairshare.md`. The focus of this document is the **system architecture** and **integration points** required to implement the feature.

## Rationale

In large-scale environments the scheduler runs a tight loop that shouldn't depend on external calls per cycle. Executing raw PromQL (or other TSDB) queries every few milliseconds would:
* Introduce unpredictable latency spikes when the database is under load.
* Create pressure on the DB as cluster size and metric cardinality grow.
* Couple scheduler availability to the health of an external datastore.

A **background Usage-Query routine embedded inside the Scheduler pod** acts as a *caching proxy*:
1. Every *Δt* (e.g., 60 s) it executes PromQL queries against the TSDB, pre-computes the decayed, windowed usage vectors and stores them in memory.
2. The main scheduling loop reads the in-memory vector with zero I/O.
3. If the TSDB becomes slow or unreachable, the routine continues to serve *stale but bounded* data for up to `maxStaleness`, so overall scheduler latency remains stable.

## 1. Components & Responsibilities

| Component | Responsibility | Notes |
|-----------|---------------|-------|
| **Metrics Exporters** | Emit raw usage metrics for PodGroups and Queues | Implemented inside the Queue & PodGroup controllers.
| **Usage DB (TSDB)** | Store aggregated resource-hours per `<queue, resource>` | Pluggable backend. Prometheus (via PromQL) is the reference implementation; additional adapters can target any metrics store (e.g., Thanos, VictoriaMetrics, proprietary TSDB). |
| **Scheduler (incl. Usage-Query)** | • Background routine pulls & caches usage<br/>• TA scheduling logic consumes cached vector<br/>• Exposes historical usage as metrics | Runs everything in one pod; no network hop between query routine and scheduler core.

## 2. High-Level Architecture
```
+--------------------+
| Metrics Exporters  |  (Queue & PG controllers)
+---------+----------+
          | scrape
          v
+------------------+
|   Prometheus     |
+---------+--------+
          | Pull (PromQL)
          v
+-------------------------------------------+
|            Scheduler Pod                  |
|  +------------------------------+         |
|  | Background Usage-Query Loop  |         |
|  +------------------------------+         |
|  |  TA Scheduling Loop          |         |
|  |  Metrics Endpoint            |         |
+-------------------------------------------+
```

### Interaction Flow
1. **Collection** – Metric exporters in the Queue & PodGroup controllers publish resource-usage metrics.
2. **Polling & Aggregation** – The scheduler’s background routine runs PromQL every *Δt* to compute `sum by (queue,resource)(usage_seconds)`.
    2.1. **Decay & Windowing** – For each poll:
       * Apply exponential decay `u ← u·e^(−λΔt)`.
       * Add the new sample.
       * Drop data older than *windowSize* (for a sliding window) or reset when crossing the next tumbling boundary.
3. **Scheduling Cycle** – The main loop reads the cached vector, orders queues, and places pods.
4. **Metrics Exposure** – Scheduler exports gauges such as `kai_queue_historical_usage{queue="q1",resource="gpu"}=0.73`.

## 3. Configuration Parameters
| Field | Example | Description |
|-------|---------|-------------|
| `decayHalfLife` | `24h` | Exponential decay half-life. 0 will disable the decay and negative values are not allowed. |
| `windowSize` | `2w` | Window duration. |
| `windowType` | `sliding` | `sliding` (moving window) or `tumbling` (fixed windows). |
| `tumblingWindowAnchor` | `2024-01-01T00:00:00Z` | RFC-3339 timestamp that anchors tumbling windows; ignored for sliding windows. |
| `backend.type` | `prometheus` | Backend adapter name; e.g. `prometheus`, `victoriametrics`, `influx`, `memory` (tests). |
| `backend.prometheus.url` | `http://prometheus.monitoring.svc:9090` | Base URL for PromQL API. |
| `pollInterval` | `60s` | How often the background routine polls the DB. |
| `maxStaleness` | `5m` | Max age of cached data before scheduler marks itself degraded. |

## 4. Failure Modes & Fallbacks
* **TSDB unavailable** – Scheduler keeps using cached vector up to `maxStaleness`; Then the scheduler will treat the usage history as unavailable.
* **Exporter gaps** – Missing metrics reduce calculated usage; system self-corrects as soon as exporters resume.

---
