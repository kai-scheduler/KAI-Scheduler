# fair-share: capacity held by others

No typed reason - the verdict stays generic `PodSchedulingErrors` while capacity is held by
others. The reclaim decision surfaces only in the scheduler log - don't recompute fair share,
read the decision: the scheduler retries every `schedule-period` (default 1s) and re-logs it in
full at default verbosity (`-v=3`), so a recent tail is enough.

Per-job lines are keyed by PodGroup name (`$PG` from skill step 2), per-queue lines by queue name.

## 1. Verdict - grep by `$PG`

Grep the scheduler deployment's log for `$PG` and keep the most recent matches.

`Attempting to allocate job:` must repeat across recent timestamps. Absent ->
no verdict, never conclude from an empty grep. Then match:

- `Reclaimed resources for job` / `Successfully preempted for job` -> eviction fired, job is
  pipelined until victims terminate (`Pipelined` event on the pod).
- `Attempting to reclaim for job` + `Didn't find a reclaim strategy` -> queue has enough `fairShare`,
  but no viable victims.
- `Skipping reclaim ... is not easier to reclaim for than: <other>` -> smaller same-queue job
  already failed -> diagnose `<other>`.
- allocate repeats, **no reclaim line at all** -> entry gate: `allocated + request > fairShare`
  -> step 2.
- `Attempting to preempt ... priority: <N>` + `Didn't find a preemption strategy` -> in-queue
  mechanism: no preemptible job strictly below `<N>` in the job's own queue.

Evictions also leave events that survive log rotation: `Evict` on the victim's PodGroup names
the beneficiary.

## 2. Over fair share - explain with the scheduler's numbers

Grep the same log for `Resource division result for queue <$QUEUE>` and for the divided pool -
`Total allocatable resources are` (once per cycle, with node/queue counts); last matches are
current, same `sessionID` = same cycle.

The division line prints deserved / requested / maxAllowed / allocated / historicalUsage /
fairShare per resource.
`fairShare = min(quota, requested) + weighted surplus slice`, capped by maxAllowed (queue `limit`), recomputed top-down each cycle. 
Explain the number from its inputs (e.g. quota 0 + low overQuotaWeight -> thin surplus slice). changing it = Queue spec knobs
(admin) - `docs/queues/README.md`, theory: `docs/scheduling-deep-dive/`.
priorityClass helps only against the own queue's lower-priority preemptible jobs (preempt);
cross-queue reclaim ignores it, and `>= 100` makes the pod non-preemptible (gated to deserved).

Report it as a derived conclusion, not a typed reason.
