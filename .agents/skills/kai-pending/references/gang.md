# gang: can't assemble

Gang is all-or-nothing - KAI places the PodGroup only when enough pods fit simultaneously.

## Steps

When the verdict says that fewer pods fit than required for gang scheduling, read the embedded
fit error explaining why the next pod could not be placed. Do not infer the missing capacity by
counting nodes or GPUs.

## Fix (any of)

- Lower the `kai.scheduler/batch-min-member` annotation on the workload (or the PodGroup `minMember`).
- Shrink the per-pod request so more pods fit per node.
- Add capacity, or wait for it to free.
