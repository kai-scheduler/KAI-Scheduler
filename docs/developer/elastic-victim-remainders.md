# Elastic victim remainders

The baseline for this experiment is upstream `main` at
`f348ba244ce9` (fetched November 18, 2026), after #2400 and #2420.
Both requeue calls to `CloneWithTasks` were still present in
`PodAccumulatedScenarioBuilder.addNextPotentialVictims`.

## Source trace and ownership

`JobsOrderByQueues.PopNextJob` selects a victim through the queue hierarchy,
retains its allocation for future queue comparisons, and removes it from its
leaf heap. `GetTasksToEvict` then reevaluates subgroup floors, task ordering,
stale members, and semi-preemptible core protection against the current job.
The baseline rebuilt the job's entire remaining task suffix before each push,
including pod clones, subgroup state, status indexes, and resource aggregates.
With one-pod elastic batches, that copies a quadratic number of task entries.

The builder now owns one private remainder per eligible partially consumed job. The
first requeue clones only the unconsumed tasks. Reuse requires nonnegative
integer request-vector entries and allocated sums no greater than 2^53, so
subtraction is exact. Fractional or oversized vectors retain suffix cloning
to preserve rounding-sensitive queue-order ties. Later eligible requeues remove consumed
tasks from this private job while it is outside the heap, then push it again.
There is no precomputed eviction cursor: selection and ordering callbacks still
run on each pop against the updated remainder.

`PodGroupInfo.RemoveTaskInfo` updates the leaf's pod/status maps and counters,
the job's status index and active count, allocated resources, and cached task
queries. It preserves the already-updated all-pod map to avoid rebuilding it
on every deletion. Parent subgroup counts derive from the leaf counts.
Removal never changes pod fields. Consumed pods can therefore escape to the
outer scenario and simulation; they are no longer indexed by the requeued
remainder. Scenario representatives still use their existing independent
`CloneWithTasks` path. Source jobs and sibling builders have separate indexes.

Queue history requires special care: it formerly retained every popped
`PodGroupInfo`, including each successive suffix. In-place removal would have
changed past queue comparisons. The in-tree historical victim reader is
proportion's `calculateDominantResourceShareWithJob`, which reads only
`AllocatedVector` and `VectorMap`. History now stores a vector copy per pop,
including repeated pops of the same job. The next heap entry contributes its
current vector. No deduplication or accounting correction is included.

`api.CompareQueueFn` and `Session.QueueOrderFn` now take
`[]api.QueueVictimResources` for victim arguments. This is an internal Go plugin
API change: custom queue-order callbacks must adopt the resource-snapshot type.
There is no Kubernetes API or scheduler configuration change.

The heap's live job still supplies all remainder-sensitive inputs, including
GPU job ordering's `GetAliveTasksRequestedGPUs`. Recorded-victim batches remove
all recorded members before requeueing, as before. Empty batches are not
requeued, preserving the semi-preemptible core termination guard.

## Measurement

Identical `BenchmarkElasticVictimRequeue` scaffolding was compiled against the
pinned baseline and candidate. Each operation exhausts one GPU elastic victim
with a one-pod floor and asserts every ordered victim UID and batch boundary.
Scenario representative construction remains included. Fixtures are outside
the timed loop; queue creation and all requeue work are timed.

Final measurements on November 19 used six alternating unprofiled
one-operation samples per revision, Go 1.26.10,
linux/amd64, Intel Core Ultra 7 165H, `GOMAXPROCS=4`:

| Tasks | Metric | Baseline median | Candidate median | Change |
| --- | --- | ---: | ---: | ---: |
| 1,024 | Time/op | 1.187 s | 112.9 ms | -90.49% |
| 1,024 | Bytes/op | 586.64 MB | 27.49 MB | -95.31% |
| 1,024 | Allocs/op | 3,809,211 | 61,471 | -98.39% |
| 2,464 | Time/op | 6.907 s | 611.6 ms | -91.14% |
| 2,464 | Bytes/op | 3,446.72 MB | 158.26 MB | -95.41% |
| 2,464 | Allocs/op | 21,657,670 | 151,732 | -99.30% |

All six comparisons have benchstat `p=0.002`, `n=6`. MB denotes decimal MB.
These are synthetic path measurements, not a claim about production cycle
latency, retained heap, or GC CPU. Eviction selection still rebuilds task
priority queues and scans remaining tasks on each pop, so the complete path
still contains quadratic work.

Separate one-operation CPU and allocation profiles of the 2,464-task case
attribute about 2,993 MiB of sampled baseline allocations to `CloneWithTasks`,
versus about 6.5 MiB in the candidate (including scenario representatives).
The baseline CPU profile attributes 5.71 s cumulatively to `CloneWithTasks`.
The candidate instead spends 690 ms cumulatively in `GetTasksToEvict`, 68% of
its 1.01 s of CPU samples; task-priority-queue insertion accounts for about
159 MiB, 85% of its sampled allocations. These whole-process profiles include
fixture setup and package initialization and are not the timed B/op figures.
They locate the next cost in eviction selection, rather than remainder cloning.

Two action controls used six alternating samples per revision, three complete
cycles per sample, with session construction and outcome assertions timed:

| Workload | Time/op, baseline → candidate | Bytes/op | Allocs/op |
| --- | --- | --- | --- |
| 32-pod elastic victim, 16 committed evictions | 116.9 → 114.7 ms; -1.87%, p=0.015 | -20.17%, p=0.002 | -16.92%, p=0.002 |
| Existing many-single-GPU full cycle, 10 nodes | 153.4 → 146.6 ms; inconclusive, p=0.310 | inconclusive, p=0.394 | +0.08%, p=0.002 |

The small allocation increase in the control is the cost of keeping immutable
resource snapshots for popped jobs. The final committed-cycle fixture shows a small timing improvement, while
an earlier pass was inconclusive. Neither establishes a production latency
improvement. The historical #2400 parent/child
CI slowdown comparison was not part of this experiment.

## Correctness checks

The regression oracle retains the upstream suffix-cloning algorithm and
compares ordered victim batches and complete remainder accounting with the
candidate. Cases cover recorded victims, gang batches, nested subgroup floors,
stale members, semi-preemptible cores, empty batches, queue exhaustion,
competing jobs, queue hierarchies, fractional/oversized resource fallback, and
every queue-comparison resource trace.
Separate checks cover repeated task removal, mixed task statuses, source-job
and sibling-builder isolation, and escaped scenario representatives. Rejected post-allocation simulations are
also checked across successive batches: source task states, node resources,
pending tasks, and the private remainder must all survive rollback.

A full-cycle fixture restricts the portfolio to MultiNodeGang, forcing the
modified builder path. It consumes sixteen one-pod batches from a 32-pod
elastic GPU job, commits exactly sixteen evictions, and pipelines sixteen
pending pods onto the expected nodes. Baseline and candidate use the identical
fixture and compare canonical job/task statuses and node assignments.

## Reproduction

Run on each revision with the same benchmark and fixture files:

```sh
GOMAXPROCS=4 go test ./pkg/scheduler/actions/common/solvers \
  -run '^$' -bench BenchmarkElasticVictimRequeue -benchtime=1x -count=1
GOMAXPROCS=4 go test ./pkg/scheduler/actions/integration_tests/reclaim \
  -run '^$' -bench 'Benchmark(ElasticRemainderCommittedCycle|ReclaimManySingleGPUJobsFullCycle_10Node)$' \
  -benchtime=3x -count=1
```

Alternate revisions between samples. Collect CPU and allocation profiles in
separate runs; do not mix profiled results into timing samples. The fixture
`TestElasticRemainderCommittedCycle` emits a sorted canonical outcome with
`go test -v` for cross-revision comparison.

## Validation

`make validate` checks generated files, licenses, formatting, repository-wide
vetting, and the configured golangci-lint checks. The affected solver, queue,
PodGroup/subgroup, framework, proportion, GPU-order, and reclaim integration
suites were also exercised. No captured-cluster replay or historical #2400
parent/child benchmark was used to support these results.
