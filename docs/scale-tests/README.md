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

- Tests will use dedicated infrastructure and will run every 24 hours
- Test results will be available for viewing through a dashboard

