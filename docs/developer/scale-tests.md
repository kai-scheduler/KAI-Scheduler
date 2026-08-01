# KAI Scheduler Scale Tests

## Overview

Scale tests validate KAI scheduler performance and correctness at large cluster sizes (hundreds to thousands of nodes). These tests simulate realistic workloads to ensure the scheduler maintains acceptable performance and correctness under scale.

## What We Test

Scale tests verify:

- **Scheduling performance**: Time to schedule large numbers of pods across many nodes
- **Topology-aware scheduling**: Time to allocate for distributed jobs with topology constraints  
- **Resource allocation**: Proper GPU allocation and queue quota enforcement at scale
- **Reclaim behavior**: Preemption and resource reclamation with background workloads
- **Distributed job scheduling**: Multi-pod job allocation across nodes
- **System stability**: Scheduler behavior under concurrent job creation and high load

## Test Structure

### Test Framework

Tests use **Ginkgo** for test organization and execution. The test suite (`scale_suite_test.go`) defines test contexts and scenarios.

### Node Simulation

Tests use **KWOK** (Kubernetes WithOut Kubelet) to simulate large clusters without requiring real nodes:

- **KWOK nodes**: Virtual nodes created via the [kwok-operator](https://github.com/run-ai/kwok-operator) `NodePool` CRD. Each `NodePool` defines the desired node count and a node template (labels, capacity, allocatable resources). The operator reconciles the pool by creating/deleting KWOK-backed virtual nodes to match the spec. See `test/e2e/scale/base_kwok_managed_nodepool.yaml` for the base pool definition.
- **Default scale**: 500 nodes (configurable via `NODE_COUNT` environment variable)
- **GPU simulation**: Fake GPU operator provides GPU resource reporting
- **Pod lifecycle**: KWOK stages simulate pod completion and status transitions

### Test Organization

Tests are organized into contexts:

- **Topology tests**: Validate topology-aware scheduling with hierarchical constraints
- **Big cluster tests**: Performance tests with large node counts
  - Cluster fill scenarios (scheduler enabled/disabled during job creation)
  - Whole GPU allocation tests
  - Distributed job scheduling
  - Reclaim scenarios


## Environment Setup

Run from the repo root on a cluster with KAI scheduler already installed:

```bash
./hack/setup-scale-test-env.sh
```

This installs:
- **KWOK** + KWOK operator for simulated nodes
- **Fake GPU operator** for GPU resource reporting on KWOK nodes
- **Prometheus + Grafana + Pyroscope** for metrics and profiling
- ServiceMonitors for scheduler and binder metrics
- Tuned scheduler/binder config for scale (consolidation disabled, high binder concurrency)

## Running Tests

```bash
ginkgo -v ./test/e2e/scale/
```

Node count defaults to 500, override with `NODE_COUNT` env var.

## Recommended Architecture

Scale tests should run from a **runner pod inside the target cluster**, not from an external machine. This minimizes API server latency during test execution and metric collection.

The target cluster should be a **real cluster with real GPU nodes** — KWOK simulates node presence but the scheduler, binder, and control plane run on actual hardware.
As these tests are designed to measure Kai-scheduler's performance in real scenarios and not test logic, the tests must run on actual hardware.

Minimal cluster requirements:
- Dedicated control plane nodes (not shared with test workloads)
- KAI scheduler installed via Helm
- `kubectl` access from the runner pod (via ServiceAccount or kubeconfig)

## Test Execution

- Tests run on dedicated infrastructure every 24 hours
- Test results are stored in S3 and displayed on a public dashboard
- Dashboard URL: [KAI Scheduler Scale Tests](https://kai-scheduler.github.io/KAI-Scheduler/scale-tests/)

## Results Dashboard

The scale tests dashboard displays historical test results fetched from S3. The dashboard shows:

- Test execution times and performance metrics
- Pass/fail status for each test
- Complete scale-result details, plus legacy failure messages and logs when available
- Historical trends (30 days)
- Search and filter capabilities

### S3 Bucket Structure

Test results are stored in an S3 bucket configured through the
`SCALE_TESTS_S3_URL` repository variable. The manifest is the authoritative
index of result objects:

```
Public/
  manifest.json                    # Index of all test runs
  <result-object>.json             # Object referenced by manifest.json
```

The `manifest.json` file lists all available test runs:

```json
{
  "runs": [
    {
      "timestamp": "2026-06-29T08:42:33Z",
      "path": "Public/results_main_<timestamp>_<commit>_<run-id>.json",
      "commit": "aa514fc997453d075067e4aceae9319909c92ed6"
    }
  ]
}
```

The object referenced by `path` uses the scale-test result schema. Run metadata
wraps the test-generated result without flattening either object:

```json
{
  "metadata": {
    "kai_scheduler_ref": "main",
    "kai_commit_hash": "aa514fc997453d075067e4aceae9319909c92ed6",
    "kwok_node_count": 500,
    "cluster_node_count": 5,
    "service_resources": {}
  },
  "results": {
    "status": "success",
    "tests": [
      {
        "test_name": "Fill Cluster with single GPU Jobs",
        "status": "success",
        "details": {
          "nodes": 500,
          "jobs": 4000,
          "time": "6m36.737854402s"
        }
      }
    ]
  }
}
```

The dashboard renders every retained metadata and `details` field and uses the
test-specific timing field for historical graphs. Results with the same test
name share a line when their non-timing details and sanitized run metadata are
equal, or when one point only adds fields without changing existing values. The
line adopts the richer point's label. A conflicting field value creates another
line and color. Legends use
a short configuration fingerprint; point tooltips show the complete details and
run metadata. Earlier unwrapped `{ "status": ..., "tests": [...] }` scale
results remain supported.
Empty-string metadata fields and the metadata `timestamp` are omitted. When
`kai_scheduler_ref` is present, `kai_commit_hash` is also omitted because the
manifest already identifies the tested commit. Metadata fields use their
original names in test-point tooltips without a `metadata.` prefix.

Legacy Ginkgo reports remain readable until **2026-07-30**. Their metrics and
the canonical scale results are shown on the same graphs, with a vertical line
marking the first canonical result. After the cutoff, remove the legacy parser
and renderer, legacy test-name aliases and fixture tests, the compatibility
cutoff constant, and references to `report.json` together.

### Dashboard Deployment

The dashboard is automatically deployed to GitHub Pages when changes are pushed to the `docs/scale-tests/` directory. The S3 bucket URL is configured via the `SCALE_TESTS_S3_URL` repository variable
