# Replay tracing and temporal boundaries

Use this reference only when a reclaimer is supplied.

The archive must contain `snapshot.json`. The known capture schema has `rawObjects`, `config`, `schedulerParams` and optional discovery data. Useful raw objects include nodes, pods, queues, PodGroups, topologies and DRA objects. Do not copy arbitrary container environments, annotations, mounted data or secrets into the map.

## Reproduce on an identified source

Prefer the reported scheduler ref when available. If the image/version is absent from a sanitized snapshot, say so and record the checkout used. Check `git status` and `git rev-parse HEAD`; keep unrelated edits intact. A prebuilt binary's filename does not establish its provenance or host architecture. Inspect `go version -m` and `file` if considering reuse.

The bundled helper builds the current checkout for the host. To use another ref, first create an isolated checkout/worktree using the repository's normal workflow. Do not switch a dirty working tree for this task.

## What the temporary overlay records

`scripts/replay_snapshot.py` verifies source anchors and builds the original snapshot-tool entrypoint with these additions:

1. On entering `attemptToReclaimForSpecificJob`, save all tasks' names, job membership, node and status as `before-target.json` for the exact namespace/PodGroup.
2. Wrap `getOrderedVictimsQueue`: create an independent queue from the same cached candidates, drain it with `PopNextJob`, and save one-based ranks plus active assigned pod memberships. Return the untouched solver queue. This preserves hierarchy, queue fairness, numeric priority, and tie-break ordering.
3. Save `target-solution.json` immediately before the target's `Statement.Commit`. These are the placements that belong to this reclaim, not to a later job.
4. Record target commit errors explicitly. Wait for cache workers and allow fake-client events to flush, then export events and the normal run summary.

The files modified by the Go overlay are temporary copies of `pkg/scheduler/actions/reclaim/reclaim.go`; the entrypoint is a temporary copy of `cmd/snapshot-tool/main.go`. Snapshot objects and scheduler algorithms are unchanged. Hook names are intentionally checked: source changes must lead to an explicit adaptation, never a silent stale replay.

The extractor pairs each successful Evict event's evictor annotations with the target, resolves the victim's source from the pre-target capture, and obtains its re-pipeline destination from the target solution. A Pipelined event must corroborate that destination. If a commit failed, the helper refuses to present a complete plan. Raw event details and logs remain in the run directory for investigation.

## Why end-of-action state is insufficient

A single reclaim action iterates over multiple pending jobs. The sequence may be:

- Reclaimer A pipelines victim B onto C's former node and commits C's eviction.
- A later reclaimer attempts to evict B, changing its task status—even during an unsuccessful commit.
- End-of-action state now lacks B's earlier reservation.

Building A's tree from that final state incorrectly makes C appear disconnected. Use A's commit-scoped solution. Later allocation/reclaim activity can be displayed separately, but must not rewrite A's explanation.

Likewise, a source node for a victim may come from an ordinary allocation earlier in the replay cycle. Use the state immediately before the target's reclaim rather than requiring every victim to have been assigned in the original capture.

## Tree representation

The underlying relation is a graph. For each parent job, group its target-solution incoming reservations by node; children are victim jobs with source pods on those nodes. Preserve all matching pods as edge evidence. A breadth-first spanning tree gives one canonical occurrence per reachable job; all remaining edges are selectable references. This handles multiple parents and cycles without exponential expansion.

A disconnected victim is not proof of a needless eviction. First validate statement scope, source assignments, suppressed/failed event writes, whole-job/gang eviction and accounting. Investigate the solver only after establishing that the dependency really is absent.

## Known interface limits

The overlay currently expects `SchedulerCache.WaitForWorkers`, fake core-v1 events, the `attemptToReclaimForSpecificJob` and `reclaimForPendingTasks` call structure, and the current `JobsOrderByQueues` interfaces. Earlier releases may need different hooks or imports. The renderer is independent of the Go ref: an adapted tracer can emit the same normalized `replay-data.json` and `order-data.json` schemas.

Stop a bounded run that times out; retain the partial log and report the limitation. Do not call live Kubernetes APIs or change scheduler configuration to make a fake replay appear successful.
