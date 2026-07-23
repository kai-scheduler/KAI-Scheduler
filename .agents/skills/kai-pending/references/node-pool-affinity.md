# node-pool / affinity trap

Verdict message includes `didn't match Pod's node affinity/selector`: free capacity exists but the
pod's `nodeSelector`/affinity excludes it (e.g. pinned to a full pool while another is free). 
The quota check and node placement are **separate** - "the queue has room" and
"the cluster has room" are both true while the pod still can't run.

## Steps

1. Read the affinity/selector predicate reason in the step 4 fit detail - it names the label that
   excluded the node; those are the free nodes you're locked out of.

## Fix (any of)

- Relax the selector/affinity (drop it, widen the terms, or make it `preferred`).
- Free / add capacity in the pool the pod targets.
- Re-pin to a pool that has room.
