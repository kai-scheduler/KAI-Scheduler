# scheduling gates / suspended Job (held, not "no resources")

The workload is **intentionally held**, so the scheduler never considers it - like a Slurm job
with a future begin-time or in `held` state. Not a capacity problem.

**Check** (verified on cluster)
- Pod `STATUS: SchedulingGated` (kubectl shows it directly), or `spec.schedulingGates` non-empty.
  KAI still creates a PodGroup but its `schedulingConditions` stays **empty** - no verdict ever,
  until the gate is removed. (`pod.status.conditions` reason is also `SchedulingGated`.)
- No pods at all + the owner is a `Job` with `spec.suspend: true` -> suspended (e.g. by Kueue);
  nothing to schedule until it is admitted/resumed.

**Fix**
- Remove the gate: `kubectl patch pod <pod> --type=json -p '[{"op":"remove","path":"/spec/schedulingGates"}]'`
  - or, better, fix whatever controller is supposed to remove it (a gate usually waits on a
  precondition: a dependency, a quota object, an admission step).
- Suspended Job -> `kubectl patch job <job> --type=merge -p '{"spec":{"suspend":false}}'`.
