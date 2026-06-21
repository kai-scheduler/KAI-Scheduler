# node-pool / affinity trap

In the dump a node has **free GPU >= request but `SELECTOR no:`** - free capacity exists, the
pod's `nodeSelector`/affinity just excludes it (e.g. pinned to a full pool while another pool is
free). Verdict message includes `didn't match Pod's node affinity/selector`.

Why it surprises: KAI queue quotas are **cluster-wide, not per-pool**, so "the queue has room"
and "the cluster has room" are both true while the pod still can't run.

**Check**
- The `SELECTOR no:` lines name the excluded label (`wants pool=a, has b`) - those are the free
  nodes you're locked out of.

**Fix**
- Relax the selector/affinity (drop it, widen the terms, or make it `preferred`), **or**
- Free / add capacity in the pool the pod targets, **or** re-pin to a pool that has room.
