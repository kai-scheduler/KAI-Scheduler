# PodGroup scheduling status ownership

The scheduler applies only its node pool's `status.schedulingConditions` entry and
`status.schedulingState` to the PodGroup status subresource. The stable field manager
is `kai-scheduler-status/<node-pool>`, including the default node pool. This matches
the existing node pool partition selector and survives scheduler restarts. Replicas
of one partition must use the same leader election scope; overlapping active
schedulers for the same partition are not independent status writers.

## Schema and rollout

The companion change in `kai-scheduler/api` makes `schedulingConditions` a map list
keyed by required `nodePool`. Each condition and `schedulingState` are atomic
objects, so replacement removes obsolete explanations and core membership together.
The generated PodGroup CRD is also included in this repository's chart and embedded
CRDs. No Go field types change. After the API change is released, update the API
module dependency to that release before merging the scheduler change.

Upgrade the PodGroup CRD before enabling the new scheduler writer. Stop the old
scheduler status writers during the transition; ordinary updates do not respect
SSA ownership. The new writer checks the map list schema using a status apply
dry-run before its first real apply. An old atomic schema is rejected rather than
allowing an apply to replace another pool's result. CRDs on an existing Helm
installation must be upgraded explicitly using the repository's CRD upgrade path.

### Consumer compatibility

This writer does not preserve compatibility with older pod-group-assigners that
treat the last list element as the latest condition. Local deduplication and append
do not guarantee that SSA places the updated entry last on the server. Consumers
must select the latest condition by the highest numeric `TransitionID`, as the
current scheduler's `GetLastSchedulingCondition` does.

Upgrade and verify all such consumers before enabling the SSA writer. The rollout
order is: upgrade consumers, upgrade the PodGroup CRD, stop old scheduler status
writers, then start the SSA writer. A minimum compatible assigner release number
has not been verified; list-position readers must not be assumed compatible based
on the scheduler version alone. Deployments that must retain those readers must
defer enabling this writer; this implementation has no legacy-write mode.

On the first affected write for an existing PodGroup, the scheduler:

1. Normalizes historical missing node pool names to `default` and keeps the highest
   transition ID for duplicate keys, preferring the last entry on ties.
2. Converts old atomic list ownership into keyed ownership for other pools and
   releases the current pool's entry from legacy Update managers. Resource status
   ownership and other fields remain intact.
3. Adopts a historical current-pool condition before omitting it if the first
   requested operation is deletion.
4. Applies the current pool's complete desired condition and scheduling state.

Managed fields migration uses a version-guarded metadata patch. Normalization uses
a version-guarded status merge patch only for legacy objects. There is no full
`UpdateStatus` fallback. Other pools' historical entries remain owned by their
legacy manager until those pools migrate them.

## Concurrency and shard handoff

Every apply carries the PodGroup UID and the resource version from a fresh read.
SSA ownership alone cannot fence changes to the spec or routing label. On a
resource version conflict, the writer re-reads and retries with bounded backoff
while the UID, spec and labels still match the scheduling snapshot.
An input change discards the verdict for the next scheduling cycle. Unrelated
resource statistics updates can cause internal retries but do not invalidate the
verdict or trigger a whole-status update.

When a PodGroup moves to another pool, the new pool recomputes `schedulingState`
and takes ownership of that atomic field with the same version guard. The old
pool's condition remains intact. The previous pool's queued status write cannot
land after the routing change. A migration does not clear the published core set
before publishing its replacement.

Workers serialize updates for the same PodGroup. Completion and retries only
remove or replace the exact pending update that was processed, preserving newer
queued work. The local snapshot overlay merges only the current pool's entry and
scheduling state; it preserves other pools' entries and resource statistics. List
position is not used to decide whether a condition needs another write.

## Validation

`TestPodGroupStatusApply` in `pkg/scheduler/cache/status_updater` runs against envtest.
It covers an atomic-to-map CRD upgrade, legacy ownership and duplicate migration,
first-write deletion, independent pool applies, concurrent resource updates, core
set handoff, stale inputs, and same-name PodGroup recreation. Set
`KUBEBUILDER_ASSETS` to run this suite; the queue and snapshot unit tests also run
without envtest assets.
