---
name: kai-pending
description: Use when a KAI-Scheduler (Run:ai / NVIDIA) pod or PodGroup is stuck Pending and you need to know why - GPU jobs that won't start, queue quota/limit, fair-share, gang scheduling, fractional GPU, node-pool affinity, or scheduling gates. Reads the PodGroup's scheduling verdict; an optional python helper adds per-node free-capacity math.
license: MIT
compatibility: Requires kubectl. The optional helper free_capacity.py needs python3, but most cases are read straight from the scheduler verdict, so the skill works without it.
metadata:
  author: KAI Scheduler maintainers
  version: "1.0"
---

# KAI: why is my job pending?

A KAI "job" is a Pod (single) or PodGroup (gang). Its verdict is on the PodGroup's
`.status.schedulingConditions`, one condition **per node-pool** (`nodePool` names it); read each
condition's `reasons[]` - the top-level `reason`/`message` are deprecated. Walk the steps in order.

## 1. Rule out non-KAI causes - stop if any holds

- `Running` / `ContainerCreating` / `ImagePullBackOff` / `CrashLoopBackOff` -> not scheduling;
  check the image / volume / app.
- `SchedulingGated`, `spec.schedulingGates` set, or `Job.spec.suspend: true` -> held by design ->
  [scheduling-gates](references/scheduling-gates.md).
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

## 3. Act on the reason - read its `.message`, then:

- `QueueDoesNotExist` -> set the `kai.scheduler/queue` label to an existing queue, or create it
  (+ parent). (`default-queue` named = no label at all.)
- `OverLimit` (`allocated + requested > limit`) -> wait, lower the request, or raise the limit.
- `NonPreemptibleOverQuota` (`allocatedNP + requestedNP > deserved`) -> raise `quota`, or use a
  preemptible class (`value < 100`).
- `PodSchedulingErrors` -> umbrella, no `queueDetails` -> step 4.

## 4. PodSchedulingErrors - dump the cluster, read the table

The script prints each node's free vs total capacity and whether it matches the pod; you judge. It
takes the dump dir as its only arg (no `--ns`, never calls kubectl) - dump first, from the repo root:

```bash
mkdir -p /tmp/kai
kubectl get pods -n <ns> --field-selector=status.phase=Pending -o json > /tmp/kai/pods.json
kubectl get nodes -o json > /tmp/kai/nodes.json
# allpods gives FREE (not just total); skip finished pods so it stays small on big clusters:
kubectl get pods -A --field-selector=status.phase!=Succeeded,status.phase!=Failed -o json > /tmp/kai/allpods.json
.agents/skills/kai-pending/scripts/free_capacity.py /tmp/kai --pod <pod>   # only positional arg is the dir
# a GPU request shows only GPU nodes; add --all-nodes to include the rest
```

Columns: `GPU f/a` = free/allocatable, `SELECTOR` = matches the pod, `TAINTS` = untolerated. Match one:

- free GPU >= request, `SELECTOR match`, no taint -> nodes fit; message says `gang scheduling` ->
  [gang](references/gang.md).
- no free room now, but a `match` node's **total** GPU >= request -> contention ->
  [fair-share](references/fair-share.md).
- ...and you outrank the holders (expected eviction) -> [preemption](references/preemption.md).
- free GPU >= request but `SELECTOR no:` -> affinity trap -> [node-pool-affinity](references/node-pool-affinity.md).
- no `match` node's total GPU >= request -> too big for any node -> [node-fit](references/node-fit.md).
- `gpu-fraction`/`gpu-memory` request, no free **whole** GPU >= 1 -> no GPU for its reservation pod
  -> [fractional-gpu](references/fractional-gpu.md).

## 5. Object path silent - which component's logs

When the pod / PodGroup don't answer, read the owning component's logs
(`kubectl -n kai-scheduler logs deploy/<component>`):

- no PodGroup (`pod-group-name` missing) -> **pod-grouper** (unknown owner, webhook reject, panic).
- PodGroup exists, `schedulingConditions` stays empty, no event -> **scheduler** (no verdict produced).
- `PodSchedulingErrors` message too vague -> **scheduler**; per-node detail only at `-v=6` /
  `--detailed-fit-errors=true`, which step 4's script reconstructs without flipping flags.
- scheduled but not Running (`BindRequest` `.status.phase: Failed`) -> **binder** (reservation
  timeout, scale-up, bind error).

`kubectl logs` keeps only recent lines; older logs live in the cluster's store (Loki /
Elasticsearch / etc.). This names *which* component to read, wherever they're kept.

## RBAC

The verdict is in your own PodGroup. The dump needs cluster-scoped `get nodes` / `get pods -A`
(+ `get queues` for fair-share). Lack them -> say so, don't guess.
