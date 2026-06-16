<!-- Copyright 2026 NVIDIA CORPORATION -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# numa-viz — live NUMA placement dashboard

A zero-dependency web dashboard for demoing the KAI **NUMA placement exporter**. It
turns the cluster's live state into a single auto-refreshing page so an audience
can *see* what the scheduler and exporter are doing on a NUMA node.

It is built for the demo assumptions: **one node**, jobs/pods submitted to the
**`default`** namespace, with a topology like
`numa-plugin/examples/numa/nrt.yaml` (2 NUMA zones, 4 GPU + ~96 CPU each,
`RestrictedPodLevel` Topology Manager policy).

## What it shows

For each NUMA zone of the node, in real time:

- **GPU** as a row of slots (one per allocatable GPU), filled by the pods
  actually pinned there.
- **CPU** as a bar, segmented by the pods pinned there.
- **Inter-zone costs** (NUMA distance) and the node's Topology Manager policy.

And below the node:

- **Pending** pods the scheduler could not place, each with its requests and the
  kubelet's rejection reason — the NUMA-alignment message is highlighted.
- **Preempting** pods currently being evicted.

Per-zone occupancy and the colored pod chips come straight from the
`kai.scheduler/numa-placement-observed` annotation the exporter writes back onto
each pod — i.e. the *observed* placement, not a prediction. Zone capacity,
availability and costs come from the node's `NodeResourceTopology`.

> A hatched gray fill means a zone is using a resource that the exporter has not yet
> reported placement for (the brief lag right after a pod starts). It disappears
> on the next exporter poll.

Nodes, zones, and pending/preempting cards are always ordered by name, so nothing
jumps around between refreshes.

## Requirements

- `python3` (3.8+, standard library only — no `pip install`).
- `kubectl` configured for the demo cluster with read access to pods and
  `noderesourcetopologies` (the tool just shells out to `kubectl`).

## Run

```bash
python3 hack/numa-viz/numa-viz.py
# → http://127.0.0.1:8080
```

Then open the URL in a browser. The page refreshes every 1.5 s.

### Options

| Flag | Default | Purpose |
|------|---------|---------|
| `--port` | `8080` | HTTP port |
| `--host` | `127.0.0.1` | bind address (use `0.0.0.0` to share on a LAN) |
| `--namespace`, `-n` | `default` | namespace to watch |
| `--interval` | `1.5` | browser poll interval, seconds |
| `--context` | — | `kubectl --context` to use |
| `--kubeconfig` | — | `kubectl --kubeconfig` path |
| `--once` | — | print the JSON state model once and exit (debugging) |

```bash
python3 hack/numa-viz/numa-viz.py --port 9000 --context my-eks-context
python3 hack/numa-viz/numa-viz.py --once | jq .   # inspect the model
```

## Demo walkthrough

Manifests live in `numa-plugin/examples/numa/`. Start the dashboard, then in
another terminal:

```bash
kubectl apply -f numa-plugin/examples/numa/queues.yaml
```

### 1 — Fragmentation

```bash
kubectl apply -f numa-plugin/examples/numa/01-fragmentation.yaml
```

Watch two pods land — one in **node-0**, one in **node-1** (each 1 GPU + 50 CPU).
The node still has 6 GPU + ~91 CPU free, but a third pod drops into the
**Pending** lane: no single zone has both a free GPU *and* 50 free CPU, and the
`RestrictedPodLevel` policy forbids splitting it across zones. The highlighted
message reads *"cannot NUMA-align the pod's resources under its Topology Manager
policy."*

### 2 — NUMA-aware preemption

```bash
kubectl apply -f numa-plugin/examples/numa/02-preemption-victim.yaml
kubectl wait --for=condition=Ready pod -l app=background --timeout=120s
kubectl apply -f numa-plugin/examples/numa/02-preemption-preemptor.yaml
```

The two background pods fill one zone each. The high-priority pod can't fit
(same fragmentation), so the scheduler preempts a victim — it flashes in the
**Preempting** lane — and the high-priority pod takes over that zone. Its color
chip moves into the freed zone as the exporter re-reports placement.

### Cleanup

```bash
kubectl delete -f numa-plugin/examples/numa/01-fragmentation.yaml
kubectl delete -f numa-plugin/examples/numa/02-preemption-preemptor.yaml \
               -f numa-plugin/examples/numa/02-preemption-victim.yaml
```

## How it works

```
            kubectl get noderesourcetopologies -o json   ─┐
            kubectl get pods -n <ns> -o json             ─┤
                                                          ▼
   numa-viz.py  ──(builds a per-zone attribution model)──►  /api/state (JSON)
        │                                                         ▲
        └── serves a single HTML page that polls ────────────────┘
```

The browser can't run `kubectl`, so the script is a tiny local bridge: a
standard-library HTTP server that runs the two read-only `kubectl` commands on
each `/api/state` request and serves one self-contained page. It is read-only
and never mutates cluster state.

The file is self-contained — copy `numa-viz.py` anywhere (e.g. next to the
example manifests) and run it.
