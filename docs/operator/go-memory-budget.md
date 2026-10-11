<!-- Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0 -->

# Go memory budgets

KAI executables derive Go's soft memory limit from their container cgroups before
constructing application clients, caches, or plugins. The scheduler retains its
90% default. All other build targets use 85%, including controllers, operator,
resource reservation, node services, Helm hooks, snapshot tools, and simulators.
This leaves headroom for memory outside the Go runtime.

The budget refreshes every 15 seconds and stops when the process entrypoint
returns or its controller context is canceled. Detection failures and unlimited
cgroups retain the existing budget and log a warning; repeated identical failures
are logged once until detection recovers.

A nonempty `GOMEMLIMIT` disables automatic calculation. `KAI_GOMEMLIMIT_RATIO`
overrides the automatic ratio in the range `(0, 1]`. These are process environment
variables; this change adds no Config or Helm fields. Existing scheduler Config
settings continue to work.

Controller-runtime caches in binder and pod-grouper strip managed fields while
preserving lifecycle, ownership, and grouping data. Other components retain their
existing cache configuration.

The Go budget is soft: it does not bound reachable informer data, native-library
allocations, or total container usage. Monitor GC CPU and reconcile latency when
tuning it. An abrupt container-limit decrease can cause OOM before the next
refresh. This guardrail does not establish that a memory leak has been fixed.
