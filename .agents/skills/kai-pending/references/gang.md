# gang: can't assemble

Verdict message: `Resources were found for N pods while M are required for gang scheduling`.
Gang is all-or-nothing: KAI places the PodGroup only if >= `minMember` pods fit at once. In the
dump each pod **individually fits** (some node shows free GPU >= per-pod request, `SELECTOR
match`) - the cluster just can't host `minMember` of them together.

**Check**
- `M - N` from the message = how many more simultaneous slots are needed.
- Count `match` nodes with free GPU >= per-pod request vs `minMember`.

**Fix**
- Lower `kai.scheduler/batch-min-member` (or the PodGroup `minMember`), **or**
- Shrink the per-pod request so more pods fit per node, **or** add capacity / wait for it to free.
