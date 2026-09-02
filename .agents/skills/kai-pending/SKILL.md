---
name: kai-pending
description: Use when a KAI-Scheduler pod or PodGroup is stuck Pending and you need to know why - GPU jobs that won't start, queue quota/limit, fair-share, gang scheduling, fractional GPU, node-pool affinity, or scheduling gates. Reads the PodGroup's scheduling verdict and the scheduler's own per-node fit errors.
license: MIT
compatibility: Requires kubectl.
metadata:
  author: KAI Scheduler maintainers
  version: "1.0"
---

# KAI: why is my job pending?

A KAI "job" is a Pod (single) or PodGroup (gang). Its verdict is on the PodGroup's
`.status.schedulingConditions`, one condition **per node-pool** (`nodePool` names it); read each
condition's `reasons[]` - the top-level `reason`/`message` are deprecated. Walk the steps in order.
When a branch points to a `references/` file, read that file and follow it before running
anything else - the procedure lives there, not here.

## 1. Rule out non-KAI causes - stop if any holds

- `Running` / `ContainerCreating` / `ImagePullBackOff` / `CrashLoopBackOff` -> not scheduling;
  check the image / volume / app.
- `SchedulingGated`, `spec.schedulingGates` set, or `Job.spec.suspend: true` -> held by design ->
  read [scheduling-gates](references/scheduling-gates.md).
- `spec.schedulerName != kai-scheduler` -> KAI never sees the pod (no PodGroup); set it.
- unbound PVC, native `ResourceQuota` (not a KAI `Queue`), or cordoned node -> plain Kubernetes,
  not KAI.

## 2. Fetch the verdict

```bash
PG=$(kubectl get pod <pod> -n <ns> -o jsonpath='{.metadata.annotations.pod-group-name}')
kubectl get podgroup "$PG" -n <ns> -o json \
  | jq '.status.schedulingConditions[] | {nodePool, reasons: (.reasons | unique_by(.message))}'
```

- no `pod-group-name` annotation -> never grouped -> step 5 (pod-grouper).
- `schedulingConditions` empty -> no verdict yet; re-check. Persists -> scheduler down, or gated (step 1).
- populated -> `reasons[]` repeats per cycle; `unique_by(.message)` dedupes, the richest message
  (e.g. `MaxNodePoolResources`) is the verdict. Take `.reason` -> step 3.

## 3. Explain the reason to the user - read its `.message`, then:

For these reasons, guide the user (do not apply changes yourself):

- `QueueDoesNotExist` -> suggest setting the `kai.scheduler/queue` label to an existing
  queue, or creating it (+ parent). (`default-queue` named = no label at all.)
- `OverLimit` (`allocated + requested > limit`) -> suggest waiting, lowering the request,
  or raising the limit.
- `NonPreemptibleOverQuota` (`allocatedNP + requestedNP > deserved`) -> suggest raising
  `quota`, or making the workload preemptible. If preemptibility is not explicitly set,
  suggest using a priority class with `value < 100`.

For the reason `PodSchedulingErrors` go to step 4.

## 4. PodSchedulingErrors - read the per-node fit detail

The default message is an aggregated histogram, to get per-node numbers
(requested / used / capacity) try to read this:

- per-node lines are in the condition `message`
  (`<node-a>: Insufficient GPUs, requested: 2, used: 6, capacity: 8`).
  (available if installed with `--detailed-fit-errors=true`)
- otherwise the same detail is only in the scheduler log (`Full fit error: ...`) -> step 5. (available if installed with high verbosity `-v=6`)

Match the per-node reason (or, with just the histogram, the short dimension):

- a node's `capacity` >= request but `used` blocks it -> capacity is held by others -> contention /
  preemption -> read [fair-share](references/fair-share.md).
- `capacity` < request on every node -> too big for any single node -> read [node-fit](references/node-fit.md).
- a node-affinity / selector / taint predicate reason (not a resource shortage) -> affinity trap ->
  read [node-pool-affinity](references/node-pool-affinity.md).
- `Resources were found for N pods while M are required for gang scheduling` -> each pod fits but not
  `minMember` at once -> read [gang](references/gang.md).
- `gpu-fraction` / `gpu-memory` request -> fractional fit isn't decidable here yet. Treat whole-GPU fit as context only.

## 5. Object path silent - which component's logs

When the pod / PodGroup don't answer, read the owning component's logs - find its deployment with
`kubectl -n <kai scheduler namespace> get deploy` (names are install-specific):

- no PodGroup (`pod-group-name` missing) -> **pod-grouper** (unknown owner, webhook reject, panic).
- PodGroup exists, `schedulingConditions` stays empty, no event -> **scheduler** (no verdict produced).
- `PodSchedulingErrors` histogram too vague -> **scheduler** at `-v=6` carries the per-node `Full fit
  error` (same data `--detailed-fit-errors=true` puts in the verdict; step 4).
- scheduled but not Running (`BindRequest` `.status.phase: Failed`) -> **binder** (reservation
  timeout, scale-up, bind error).

`kubectl logs` keeps only recent lines.

## RBAC

The verdict and fit errors are in your own PodGroup. fair-share needs cluster-scoped `get queues`;
the scheduler logs need read access in the scheduler's namespace. Lack them -> say so, don't guess.
