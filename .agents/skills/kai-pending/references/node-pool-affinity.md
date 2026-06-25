# node-pool / affinity trap

Verdict message includes `didn't match Pod's node affinity/selector`: free capacity exists but the
pod's `nodeSelector`/affinity excludes it (e.g. pinned to a full pool while another is free). It
surprises because KAI queue quotas are **cluster-wide, not per-pool** - "the queue has room" and
"the cluster has room" are both true while the pod still can't run.

## Steps

1. Read the `SELECTOR no:` lines - they name the excluded label (`wants pool=a, has b`); those are
   the free nodes you're locked out of.

## Fix (any of)

- Relax the selector/affinity (drop it, widen the terms, or make it `preferred`).
- Free / add capacity in the pool the pod targets.
- Re-pin to a pool that has room.
