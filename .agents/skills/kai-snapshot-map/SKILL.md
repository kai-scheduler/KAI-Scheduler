---
name: kai-snapshot-map
description: Create an interactive cluster map from a KAI Scheduler snapshot ZIP, optionally tracing a named reclaimer's placements, ordered victims, and recursive victim tree. Use for captured cluster topology and reclaim visualization, rather than live-cluster diagnosis.
---

# KAI snapshot map

Turn a captured scheduler snapshot into the interactive map used in this workflow. A snapshot path is required; a reclaimer workload name is optional. Resolve names against the capture, including namespace and top-owner metadata. Do not ask for a reclaimer when none was supplied.

## Choose the mode

**Snapshot only:** generate exactly these three tabs, without replaying or displaying empty reclaim views:

- **Node topology** — all nodes grouped by the captured topology label, with per-node and per-group free GPU counts, readiness and cordon states, and selectable resource/workload detail.
- **Queue hierarchy** — queue parents, GPU quota/limit, captured allocation/demand, priority and pending pods.
- **Workloads & storage** — namespace/pod phases, bound versus unassigned Pending pods, storage and dynamic-resource counts.

**Snapshot plus reclaimer:** include those three tabs and add:

- **Pipeline & victims** — target destinations, victim origins, remaining releasing victims, and new victim reservations.
- **Victim priority order** — per-node and per-group minimum/maximum positions in the actual ordered candidate queue. Rank 1 is first to pop. Keep queue priority and numeric job priority distinct from rank; offer all eligible candidates versus actually evicted pods.
- **Victim tree** — root at the reclaimer; A → B when A was pipelined to a node from which a victim pod of B was evicted. Apply recursively, using each job's placements from this same reclaim. Retain every edge: expand each job once, use shared references for additional parents and cycles, and disclose genuinely disconnected victims separately. Selecting an edge shows the shared node and both jobs' pods.

## Generate

Run the bundled scripts with Python 3. They use only the standard library. Choose an explicitly writable, durable, task-owned output directory; prefer the chat's visualization directory. The snapshot is read in place and never modified. Do not commit capture or replay artifacts.

Snapshot only:

```bash
python3 <skill-dir>/scripts/create_map.py \
  --snapshot /absolute/path/snapshot.zip \
  --output /absolute/task-output/cluster-map.html
```

With a reclaimer, read [references/replay.md](references/replay.md) and supply the KAI checkout to replay:

```bash
python3 <skill-dir>/scripts/create_map.py \
  --snapshot /absolute/path/snapshot.zip \
  --reclaimer namespace/podgroup-or-workload-name \
  --repo /absolute/path/KAI-Scheduler \
  --output /absolute/task-output/cluster-map.html
```

Replay may need a few minutes to build. Preserve the repository's dirty state. Execute within bounded tool calls or launch the process with a log and poll it; do not give a foreground build a tiny timeout. To reuse an **executed and verified** replay, supply `--replay-dir /path/to/replay/run-directory` instead of `--repo`. A run directory contains `replay-data.json` and `order-data.json`.

The replay helper uses native `go build`, fake clients and temporary Go overlays. It adds tracing, not scheduling changes, and does not edit checkout sources or generated files. It currently targets the checkout interfaces documented in the replay reference. If a hook is incompatible, adapt its temporary overlay to that ref; do not silently substitute guessed data or a mismatched binary. If replay is blocked or fails, still provide the three overview tabs and state why the requested reclaim analysis is unavailable.

## Interpret correctly

- Capture data is historical. Derive a timestamp from capture metadata or label the archive filename; never label it as live state.
- Free GPUs are allocatable `nvidia.com/gpu` minus effective requests of nonterminal assigned pods, including assigned Pending pods and init/sidecar requests. They are not measured utilization or a guarantee of schedulability. Keep raw free counts separate from counts on Ready, uncordoned nodes. Do not hide over-requesting by silently converting it into available GPUs.
- The bundled GPU totals cover the standard extended resource. DRA claims and slices are shown separately. If the capture uses DRA-only GPU accounting, MIG or a different fractional-GPU scheme, adapt the normalization and label the basis before calling these values free physical GPUs.
- Use the capture's topology labels, not inferred hostname racks, to group nodes. Hostnames may shorten identifiers; expose full names and exact label values in details/tooltips. Ungrouped GPU nodes are not CPU nodes.
- **Keep one reclaim's committed solution separate from later actions.** Capture pre-target source nodes, target-solution placements at commit, and events naming the exact evictor. Never use end-of-action/end-of-cycle task state to reconstruct that reclaim's reservations. A subsequent reclaim attempt can overwrite a victim's pipeline state and make a valid dependency disappear.
- A re-pipeline is a reservation, not evidence a pod already moved or is running. The same relocation is an outgoing victim at its source and an incoming victim at its destination.
- Do not equate events' `num-evicted-pods` annotation with the number of successfully committed eviction events. Preserve commit failures, missing-queue errors, provenance and evidence rather than claiming exact historical reproduction.

## Present and verify

For inline delivery, use the installed **visualize** skill's output contract and theme utilities; locate/read its current instructions rather than hardcoding a plugin version. The bundled asset is an HTML fragment for that surface, not a standalone web page. Return its absolute path in the visualization reference, using wide mode. Use that skill's export flow only when the user asks for an export.

Validate observable behavior and source accounting:

- The no-reclaimer output has only three tab buttons/panels; the reclaimer output has six. Switching tabs and selecting nodes/jobs changes the corresponding detail.
- Every captured node occurs once per map; group membership covers all nodes. Node and group free counts reconcile, and pod/namespace totals match the capture.
- All target and victim nodes exist in the capture. Eviction evidence names the requested reclaimer; each recorded reservation belongs to its committed solution. Partial/failed commits are not displayed as complete solutions.
- Candidate ranks are unique job positions obtained from the scheduler queue, not asynchronous log/event order. Node/group extrema derive from occupied nodes before this reclaim.
- Each tree edge has at least one shared node and identifies the displaced child pod. Shared/cyclic links terminate as references; do not omit edges or fabricate a parent for a disconnected job.
- Check JavaScript syntax and primary interactions; inspect narrow layout when needed. Keep inline output below 1 MB. The script fails at that limit so you can aggregate details or split views intentionally.

Report replay ref and any material limitations concisely. Keep commands, timings and evidence paths available in the generated analysis or accompanying explanation.
