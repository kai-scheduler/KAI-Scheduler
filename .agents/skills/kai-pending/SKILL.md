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
`.status.schedulingConditions`.

## Facts

- always read `.status.schedulingConditions`. It holds one condition **per node-pool**
  (`type: UnschedulableOnNodePool`, `nodePool` names it); the top-level `reason`/`message` fields
  are deprecated - read the per-condition `reasons[]` array instead.
- The verdict path: `pod.metadata.annotations["pod-group-name"]` -> PodGroup ->
  `.status.schedulingConditions[].reasons[]` with `.reason`, `.message`, `.details.queueDetails`.
  `reasons[]` can repeat the same cause across cycles - dedupe by `.message`.
- `QueueDoesNotExist`, `OverLimit`, `NonPreemptibleOverQuota` are read straight from the verdict
  (the last two carry `queueDetails` numbers). The skill's helper is **not** needed for them.
- `PodSchedulingErrors` is an umbrella: one generic "no nodes with enough resources" message
  covers node-fit, gang, fair-share, fractional GPU and node-pool/affinity, with no `queueDetails`
  - read the message and the pod's annotations to tell them apart.
- No `kai.scheduler/queue` label -> KAI defaults the pod to a queue named `default-queue`; if it
  does not exist the reason is `QueueDoesNotExist` naming `default-queue` (fix: add the label).
- A gated pod (`spec.schedulingGates`) gets a PodGroup but an **empty** verdict and
  `STATUS: SchedulingGated` - never scheduled until the gate is removed.
- Fractional GPU (`gpu-fraction`/`gpu-memory` annotation) is served by a reservation pod in
  `kai-resource-reservation` holding one whole GPU; the workload's own resources carry no GPU.

## 1. Triage - is it KAI's problem?

- Pod is `Running` / `ContainerCreating` / `ImagePullBackOff` / `CrashLoopBackOff` -> not a
  scheduling problem; look at the image/volume/app.
- Pod `STATUS: SchedulingGated` / `spec.schedulingGates` set, or its `Job.spec.suspend: true` ->
  intentionally held, the scheduler won't touch it -> [references/scheduling-gates.md](references/scheduling-gates.md).
- `spec.schedulerName != kai-scheduler` -> KAI never sees the pod (no PodGroup is created). Fix it.
- Unbound PVC, a native `ResourceQuota` (a different object from a KAI `Queue`), cordon -> standard
  k8s, not KAI logic.

## 2. Read the verdict

```bash
PG=$(kubectl get pod <pod> -n <ns> -o jsonpath='{.metadata.annotations.pod-group-name}')
kubectl get podgroup "$PG" -n <ns> -o json \
  | jq '.status.schedulingConditions[] | {nodePool, reasons: (.reasons | unique_by(.message))}'
```

- No `pod-group-name` annotation -> check pod-grouper / admission webhook are up
  (`kubectl -n kai-scheduler get pods`).
- Empty `schedulingConditions` -> no verdict yet; re-check. If it persists: scheduler down, or the
  pod is **gated** (`STATUS SchedulingGated`) - see triage above.
- Otherwise read each entry's `.reason` (typed), `.message` (detail) and `.details.queueDetails`
  (quota numbers). `reasons` repeats the same cause per cycle - `unique_by(.message)` drops the
  duplicates; the richest message (e.g. a `MaxNodePoolResources` subtype) is the verdict.

## 3. Reason -> playbook

| reason | this is a... | playbook |
|--------|-----------|----------|
| `QueueDoesNotExist` | queue-label problem | [references/queue-does-not-exist.md](references/queue-does-not-exist.md) |
| `OverLimit` | queue hard-limit problem | [references/over-limit.md](references/over-limit.md) |
| `NonPreemptibleOverQuota` | quota problem | [references/nonpreemptible-over-quota.md](references/nonpreemptible-over-quota.md) |
| `PodSchedulingErrors` | **node-fit family** - needs the dump below | section 4 + the node-fit playbooks |

The first three are read straight from the verdict - they do **not** need the script.
`PodSchedulingErrors` is a generic "no nodes with enough resources" message that hides several
causes; that is the only branch that needs node math.

## 4. node-fit: dump the cluster, read the table

The script reads JSON you dump with kubectl - it prints each node's free vs total capacity and
whether it matches the pod, but does **not** judge; you do, from the table.

NOTE: `free_capacity.py` takes the **dump directory** as its only positional argument. It does **not**
accept `--ns`/`--namespace` and never calls kubectl - dump the three files first, then pass the
directory:

Run from the repository root:

```bash
mkdir -p /tmp/kai
kubectl get pods -n <ns> --field-selector=status.phase=Pending -o json > /tmp/kai/pods.json
kubectl get nodes -o json > /tmp/kai/nodes.json
# allpods gives FREE (not just total); skip finished pods so it stays small on big clusters:
kubectl get pods -A --field-selector=status.phase!=Succeeded,status.phase!=Failed -o json > /tmp/kai/allpods.json
.agents/skills/kai-pending/scripts/free_capacity.py /tmp/kai --pod <pod>   # only positional arg is the dir
# for a GPU request only GPU nodes are shown; add --all-nodes to include the rest
```

Reading the table (`GPU f/a` = free/allocatable, `SELECTOR` = matches the pod, `TAINTS` =
untolerated):

| what the table shows | cause | playbook |
|---|---|---|
| a node has free GPU >= request, `SELECTOR match`, no taint | **fits** - nodes aren't the blocker; the message likely says `gang scheduling` -> | [references/gang.md](references/gang.md) |
| no node has free room now, but some `match` node's **total** GPU >= request | **contention** - capacity is held by others | [references/fair-share.md](references/fair-share.md) |
| ...and your pod outranks the holders (you expected it to evict them) | **preemption** question | [references/preemption.md](references/preemption.md) |
| a node has free GPU >= request but `SELECTOR no:` | **affinity-trap** - free capacity excluded by the pod's selector | [references/node-pool-affinity.md](references/node-pool-affinity.md) |
| no `match` node's total GPU >= request | **too-big** - request bigger than any node | [references/node-fit.md](references/node-fit.md) |
| request is `gpu-fraction`/`gpu-memory`; no node has free **whole** GPU >= 1 | **fractional** - no GPU for its reservation pod | [references/fractional-gpu.md](references/fractional-gpu.md) |

## RBAC

The verdict is in your own PodGroup. The dump needs cluster-scoped `get nodes` / `get pods -A`
(and `get queues` for fair-share). If you lack them, say so - don't guess.
