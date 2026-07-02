# gang: can't assemble

Verdict message: `Resources were found for N pods while M are required for gang scheduling`.
Gang is all-or-nothing - KAI places the PodGroup only if `>= minMember` pods fit **at once**; here
each pod fits alone but the cluster can't host `minMember` together.

## Steps

1. Read `M - N` from the message = how many more simultaneous slots are needed.
2. Count nodes with free GPU (`capacity - used`, step 4 detail) >= per-pod request; compare to `minMember`.

## Fix (any of)

- Lower `kai.scheduler/batch-min-member` (or the PodGroup `minMember`).
- Shrink the per-pod request so more pods fit per node.
- Add capacity, or wait for it to free.
