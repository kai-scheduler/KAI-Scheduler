#!/usr/bin/env python3
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

"""
numa-viz: a live demo dashboard for the KAI NUMA placement agent.

Bridges `kubectl` to a browser. Polls two sources and renders them as a single,
auto-refreshing page:

  - NodeResourceTopology (NRT): per-NUMA-zone allocatable/available/capacity for
    each resource, inter-zone costs, and the kubelet Topology Manager policy.
  - Pods in a namespace: phase, queue, requests, and the placement the NUMA agent
    observed and wrote back to the pod via the
    `kai.scheduler/numa-placement-observed` annotation.

The page shows, for the (assumed single) node: each NUMA zone with its GPU slots
and CPU bar filled by the pods actually pinned there, the pending pods that the
scheduler could not NUMA-align (with the kubelet's rejection reason), and any pods
currently being preempted.

No third-party dependencies — Python 3 standard library + a working `kubectl`.

    python3 numa-viz.py                 # serve on http://localhost:8080
    python3 numa-viz.py --port 9000 --namespace default
    python3 numa-viz.py --context my-ctx --kubeconfig ~/.kube/config
    python3 numa-viz.py --once          # print the JSON model and exit (debug)
"""

import argparse
import json
import subprocess
import sys
import time
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

PLACEMENT_ANNOTATION = "kai.scheduler/numa-placement-observed"
QUEUE_LABEL = "kai.scheduler/queue"
GPU_RESOURCE = "nvidia.com/gpu"

# Distinct, projection-friendly hues. Pods are assigned colors deterministically
# (sorted by name) so the same pod keeps its color across refreshes and across the
# zone bars, slots and chips.
PALETTE = [
    "#5b9cff", "#ffb454", "#36c5a8", "#e06c9f", "#a3be8c", "#b48ead",
    "#d08770", "#88c0d0", "#ebcb8b", "#bf9bdb", "#81a1c1", "#f2a6c2",
    "#9ccfd8", "#f6c177", "#7aa2f7", "#bb9af7",
]


# ── quantity helpers ────────────────────────────────────────────────────────

def to_num(value):
    """Parse a Kubernetes count/CPU quantity to a float in natural units.

    Handles plain integers ("4"), CPU millicores ("500m" -> 0.5). Anything
    unparseable yields 0.
    """
    if value is None:
        return 0.0
    s = str(value).strip()
    if s.endswith("m") and s[:-1].lstrip("-").isdigit():
        return float(s[:-1]) / 1000.0
    try:
        return float(s)
    except ValueError:
        return 0.0


_MEM_UNITS = [
    ("Ki", 1024), ("Mi", 1024 ** 2), ("Gi", 1024 ** 3), ("Ti", 1024 ** 4), ("Pi", 1024 ** 5),
    ("k", 1e3), ("M", 1e6), ("G", 1e9), ("T", 1e12), ("P", 1e15),
]


def mem_bytes(value):
    if value is None:
        return 0.0
    s = str(value).strip()
    for suffix, mult in _MEM_UNITS:
        if s.endswith(suffix):
            try:
                return float(s[: -len(suffix)]) * mult
            except ValueError:
                return 0.0
    try:
        return float(s)
    except ValueError:
        return 0.0


def human_mem(b):
    for suffix, div in (("Ti", 1024 ** 4), ("Gi", 1024 ** 3), ("Mi", 1024 ** 2), ("Ki", 1024)):
        if b >= div:
            x = b / div
            return f"{int(x)}{suffix}" if abs(x - round(x)) < 1e-9 else f"{x:.1f}{suffix}"
    return f"{int(b)}B"


def fmt_num(x):
    return str(int(round(x))) if abs(x - round(x)) < 1e-9 else f"{x:.1f}"


# ── kubectl ───────────────────────────────────────────────────────────────────

def run_kubectl(global_args, args, timeout=15):
    cmd = ["kubectl", *global_args, *args]
    proc = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    if proc.returncode != 0:
        raise RuntimeError(f"`{' '.join(cmd)}` failed: {proc.stderr.strip() or proc.stdout.strip()}")
    return json.loads(proc.stdout)


# ── state model ────────────────────────────────────────────────────────────────

def parse_placement(annotation):
    """Decode the agent's placement annotation into {zone: {resource: amount}}."""
    out = {}
    if not annotation:
        return out
    try:
        entries = json.loads(annotation)
    except (ValueError, TypeError):
        return out
    for entry in entries or []:
        zone = entry.get("zone")
        amount = entry.get("amount", {})
        if not zone:
            continue
        out.setdefault(zone, {})
        for res, qty in amount.items():
            out[zone][res] = out[zone].get(res, 0.0) + to_num(qty)
    return out


def container_requests(pod):
    gpu = cpu = 0.0
    mem = 0.0
    for c in pod.get("spec", {}).get("containers", []):
        req = (c.get("resources", {}) or {}).get("requests", {}) or {}
        gpu += to_num(req.get(GPU_RESOURCE, 0))
        cpu += to_num(req.get("cpu", 0))
        mem += mem_bytes(req.get("memory", 0))
    return gpu, cpu, mem


def scheduling_block(pod):
    """Returns (reason, message) from the PodScheduled=False condition, else ('', '')."""
    for cond in pod.get("status", {}).get("conditions", []):
        if cond.get("type") == "PodScheduled" and cond.get("status") != "True":
            return cond.get("reason", "") or "", cond.get("message", "") or ""
    return "", ""


def classify(pod):
    meta = pod.get("metadata", {})
    phase = pod.get("status", {}).get("phase", "")
    terminating = bool(meta.get("deletionTimestamp"))
    if terminating and phase in ("Running", "Pending"):
        return "evicting"
    if phase == "Running":
        return "running"
    if phase == "Pending":
        return "pending"
    if phase in ("Succeeded", "Failed"):
        return "finished"
    return "pending"


def build_state(namespace, global_args):
    nrt = run_kubectl(global_args, ["get", "noderesourcetopologies", "-o", "json"])
    pods_raw = run_kubectl(global_args, ["get", "pods", "-n", namespace, "-o", "json"])

    pods = []
    for p in pods_raw.get("items", []) or []:
        meta = p.get("metadata", {})
        labels = meta.get("labels", {}) or {}
        annotations = meta.get("annotations", {}) or {}
        gpu, cpu, mem = container_requests(p)
        reason, message = scheduling_block(p)
        pods.append({
            "name": meta.get("name", ""),
            "kind": classify(p),
            "phase": p.get("status", {}).get("phase", ""),
            "queue": labels.get(QUEUE_LABEL, ""),
            "app": labels.get("app", ""),
            "node": p.get("spec", {}).get("nodeName", ""),
            "priorityClass": p.get("spec", {}).get("priorityClassName", "") or "",
            "priority": p.get("spec", {}).get("priority", 0) or 0,
            "reqGpu": gpu,
            "reqCpu": cpu,
            "reqMem": mem,
            "placement": parse_placement(annotations.get(PLACEMENT_ANNOTATION)),
            "reason": reason,
            "message": message,
        })

    # Deterministic color per active pod (running/evicting/pending), keyed by name.
    active = sorted({p["name"] for p in pods if p["kind"] in ("running", "evicting", "pending")})
    color_of = {name: PALETTE[i % len(PALETTE)] for i, name in enumerate(active)}
    for p in pods:
        p["color"] = color_of.get(p["name"], "#64748b")

    # Pods that currently hold resources on the node, for zone attribution.
    occupying = [p for p in pods if p["kind"] in ("running", "evicting")]

    nodes = []
    for item in nrt.get("items", []) or []:
        nmeta = item.get("metadata", {})
        attrs = {a.get("name"): a.get("value") for a in item.get("attributes", []) or []}
        node_name = nmeta.get("name", "")

        zones = []
        for z in item.get("zones", []) or []:
            zname = z.get("name", "")
            costs = {c.get("name"): c.get("value") for c in z.get("costs", []) or []}
            resources = {r.get("name"): r for r in z.get("resources", []) or []}

            def attributed(resource_name):
                segs = []
                for p in occupying:
                    amt = p["placement"].get(zname, {}).get(resource_name, 0.0)
                    if amt > 0:
                        segs.append({"pod": p["name"], "color": p["color"], "amount": amt,
                                     "evicting": p["kind"] == "evicting"})
                segs.sort(key=lambda s: s["pod"])
                return segs

            def axis(resource_name):
                r = resources.get(resource_name)
                if not r:
                    return None
                alloc = to_num(r.get("allocatable", r.get("capacity", 0)))
                avail = to_num(r.get("available", 0))
                cap = to_num(r.get("capacity", alloc))
                segs = attributed(resource_name)
                attr_sum = sum(s["amount"] for s in segs)
                used = max(alloc - avail, 0.0)
                unattributed = max(used - attr_sum, 0.0)
                return {
                    "allocatable": alloc, "available": avail, "capacity": cap,
                    "used": max(used, attr_sum), "segments": segs, "unattributed": unattributed,
                }

            zones.append({
                "name": zname,
                "self": zname,
                "costs": costs,
                "gpu": axis(GPU_RESOURCE),
                "cpu": axis("cpu"),
            })

        zones.sort(key=lambda z: z["name"])
        nodes.append({
            "name": node_name,
            "policy": attrs.get("topologyManagerPolicy", ""),
            "scope": attrs.get("topologyManagerScope", ""),
            "topologyPolicy": (item.get("topologyPolicies") or [""])[0],
            "zones": zones,
        })

    nodes.sort(key=lambda n: n["name"])

    finished = {"Succeeded": 0, "Failed": 0}
    for p in pods:
        if p["kind"] == "finished":
            finished[p["phase"]] = finished.get(p["phase"], 0) + 1

    def view(p):
        zones = sorted(p["placement"].keys())
        return {
            "name": p["name"], "queue": p["queue"], "app": p["app"], "node": p["node"],
            "priorityClass": p["priorityClass"], "priority": p["priority"],
            "reqGpu": p["reqGpu"], "reqCpu": p["reqCpu"], "reqMem": human_mem(p["reqMem"]),
            "color": p["color"], "reason": p["reason"], "message": p["message"],
            "zones": zones,
        }

    return {
        "ok": True,
        "ns": namespace,
        "fetchedAt": datetime.now(timezone.utc).isoformat(),
        "nodes": nodes,
        "pods": {
            "pending": sorted((view(p) for p in pods if p["kind"] == "pending"), key=lambda v: v["name"]),
            "evicting": sorted((view(p) for p in pods if p["kind"] == "evicting"), key=lambda v: v["name"]),
            "finished": finished,
        },
    }


# ── HTML / CSS / JS (single page, vanilla, no build step) ───────────────────────

HTML_PAGE = r"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>KAI · NUMA placement</title>
<style>
  :root{
    --bg:#0b1020; --bg2:#0e1530; --panel:#141c38; --panel2:#1a2444;
    --line:#27325a; --ink:#e8ecfb; --muted:#9aa7d4;
    --muted2:#6b78a8; --ok:#36c5a8; --warn:#ffb454; --bad:#ff6b81;
    --free:#222c4e; --grayfill:#48527a;
  }
  *{box-sizing:border-box}
  html,body{margin:0;height:100%}
  body{
    background:radial-gradient(1200px 700px at 75% -10%, #16204a 0%, var(--bg) 60%) fixed;
    color:var(--ink);
    font:14px/1.45 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial;
    -webkit-font-smoothing:antialiased; padding:18px 22px 40px;
  }
  h1,h2,h3{margin:0;font-weight:650}
  .wrap{max-width:1280px;margin:0 auto}

  header{display:flex;align-items:center;gap:14px;flex-wrap:wrap;margin-bottom:16px}
  .brand{display:flex;align-items:baseline;gap:10px}
  .brand h1{font-size:19px;letter-spacing:.2px}
  .brand .sub{color:var(--muted);font-size:12.5px}
  header .spacer{flex:1}
  .live{display:flex;align-items:center;gap:8px;color:var(--muted);font-size:12.5px;
        background:var(--panel);border:1px solid var(--line);border-radius:999px;padding:6px 12px}
  .dot{width:9px;height:9px;border-radius:50%;background:var(--ok);box-shadow:0 0 0 0 rgba(54,197,168,.6);
       animation:pulse 1.8s infinite}
  .dot.stale{background:var(--bad);animation:none}
  @keyframes pulse{0%{box-shadow:0 0 0 0 rgba(54,197,168,.55)}70%{box-shadow:0 0 0 7px rgba(54,197,168,0)}100%{box-shadow:0 0 0 0 rgba(54,197,168,0)}}

  .badge{display:inline-flex;align-items:center;gap:6px;font-size:11.5px;font-weight:600;
         padding:3px 9px;border-radius:999px;border:1px solid var(--line);background:var(--panel2);color:var(--muted)}
  .badge.policy{color:#ffd9a0;border-color:#5a4422;background:#2a1f10}
  .badge.scope{color:#bcd;}

  .banner{display:none;margin:0 0 14px;padding:10px 14px;border-radius:10px;
          background:#33121a;border:1px solid #74303d;color:#ffd2d8;font-size:13px;white-space:pre-wrap}
  .banner.show{display:block}

  .nodes{display:flex;flex-direction:column;gap:16px}
  .node{background:linear-gradient(180deg,var(--panel) 0%,var(--bg2) 100%);
        border:1px solid var(--line);border-radius:16px;padding:16px 18px;
        box-shadow:0 14px 40px -22px rgba(0,0,0,.8)}
  .node-head{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:14px}
  .node-head .nname{font-size:15px;font-weight:650}
  .node-head .ico{opacity:.8}

  .zones{display:grid;grid-template-columns:repeat(auto-fit,minmax(330px,1fr));gap:14px}
  .zone{background:var(--panel2);border:1px solid var(--line);border-radius:13px;padding:14px 15px;
        position:relative;overflow:hidden}
  .zone::before{content:"";position:absolute;inset:0 0 auto 0;height:3px;
                background:linear-gradient(90deg,#5b9cff,#36c5a8)}
  .zone-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:12px}
  .zone-name{font-size:14.5px;font-weight:650;letter-spacing:.2px}
  .zone-costs{display:flex;gap:6px;flex-wrap:wrap;justify-content:flex-end}
  .cost{font-size:10.5px;color:var(--muted);background:#0f1733;border:1px solid var(--line);
        border-radius:6px;padding:2px 7px}
  .cost.self{color:var(--ok);border-color:#1f4a42}

  .res{margin:10px 0 4px}
  .res-label{display:flex;justify-content:space-between;align-items:baseline;margin-bottom:6px}
  .res-label .name{font-size:11.5px;font-weight:650;color:var(--muted);text-transform:uppercase;letter-spacing:.6px}
  .res-label .nums{font-size:12.5px;color:var(--ink);font-variant-numeric:tabular-nums}
  .res-label .nums b{color:#fff}
  .res-label .nums .of{color:var(--muted2)}

  .slots{display:flex;gap:6px;flex-wrap:wrap}
  .slot{width:34px;height:34px;border-radius:8px;border:1.5px solid var(--line);
        background:var(--free);transition:background .5s ease,border-color .5s ease,transform .2s ease;
        position:relative;display:flex;align-items:center;justify-content:center}
  .slot.free{border-style:dashed;border-color:#374272;background:transparent}
  .slot.used{border-color:rgba(255,255,255,.25)}
  .slot.unattr{background:repeating-linear-gradient(45deg,var(--grayfill),var(--grayfill) 5px,#3c456b 5px,#3c456b 10px)}
  .slot .g{font-size:10px;font-weight:700;color:rgba(0,0,0,.55)}

  .track{height:18px;border-radius:7px;background:var(--free);border:1px solid var(--line);
         display:flex;overflow:hidden}
  .seg{height:100%;transition:width .55s cubic-bezier(.2,.7,.2,1);min-width:0}
  .seg.unattr{background:repeating-linear-gradient(45deg,var(--grayfill),var(--grayfill) 6px,#3c456b 6px,#3c456b 12px)}
  .seg.evicting{opacity:.6;outline:1px dashed rgba(255,107,129,.8);outline-offset:-2px}

  .chips{display:flex;gap:7px;flex-wrap:wrap;margin-top:12px;min-height:4px}
  .chip{display:inline-flex;align-items:center;gap:7px;font-size:11.5px;color:var(--ink);
        background:#0f1733;border:1px solid var(--line);border-radius:999px;padding:4px 10px 4px 7px;
        transition:opacity .3s ease,transform .3s ease}
  .chip .sw{width:10px;height:10px;border-radius:3px;flex:none}
  .chip .pn{max-width:160px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .chip .amt{color:var(--muted);font-variant-numeric:tabular-nums}
  .chip.evicting{border-color:#74303d;color:#ffd2d8}

  .lanes{display:grid;grid-template-columns:1fr;gap:14px;margin-top:16px}
  section.lane{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:14px 16px}
  .lane h2{font-size:13px;text-transform:uppercase;letter-spacing:.7px;color:var(--muted);
           display:flex;align-items:center;gap:9px;margin-bottom:12px}
  .lane h2 .count{font-size:11.5px;color:var(--ink);background:var(--panel2);border:1px solid var(--line);
                  border-radius:999px;padding:1px 9px}
  .lane.empty{display:none}
  .lane.pend h2{color:var(--warn)}
  .lane.evict h2{color:var(--bad)}

  .cards{display:grid;grid-template-columns:repeat(auto-fill,minmax(340px,1fr));gap:12px}
  .pod{border:1px solid var(--line);border-radius:11px;padding:12px 13px;background:var(--panel2);
       transition:opacity .3s ease,transform .3s ease}
  .pod.enter{opacity:0;transform:translateY(6px)}
  .pod.leave{opacity:0;transform:scale(.97)}
  .pod.pending{border-left:3px solid var(--warn)}
  .pod.evicting{border-left:3px solid var(--bad)}
  .pod-top{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:9px}
  .pod-top .sw{width:11px;height:11px;border-radius:3px;flex:none}
  .pod-top .pname{font-weight:650;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:200px}
  .reqs{display:flex;gap:7px;flex-wrap:wrap;margin-bottom:8px}
  .req{font-size:11.5px;color:var(--ink);background:#0f1733;border:1px solid var(--line);
       border-radius:7px;padding:2px 8px;font-variant-numeric:tabular-nums}
  .req .k{color:var(--muted)}
  .msg{font-size:12px;color:var(--muted);background:#11183300;border-radius:8px;line-height:1.4}
  .msg.numa{color:#ffe1bd;background:#2a1f10;border:1px solid #5a4422;padding:8px 10px}
  .msg .tag{display:inline-block;font-weight:700;color:var(--warn);margin-right:6px}

  .legend{display:flex;gap:18px;flex-wrap:wrap;align-items:center;margin-top:18px;
          color:var(--muted);font-size:12px}
  .legend .item{display:flex;align-items:center;gap:7px}
  .legend .box{width:16px;height:16px;border-radius:5px;border:1px solid var(--line)}
  .legend .box.free{border-style:dashed;background:transparent}
  .legend .box.unattr{background:repeating-linear-gradient(45deg,var(--grayfill),var(--grayfill) 4px,#3c456b 4px,#3c456b 8px)}
  .legend .box.used{background:linear-gradient(90deg,#5b9cff,#36c5a8)}
  .legend .spacer{flex:1}
  .foot{margin-top:10px;color:var(--muted2);font-size:11.5px}
  a{color:#8fb4ff}
</style>
</head>
<body>
<div class="wrap">
  <header>
    <div class="brand">
      <h1>NUMA placement</h1>
      <span class="sub">KAI scheduler · live demo</span>
    </div>
    <div class="spacer"></div>
    <span id="badges"></span>
    <div class="live"><span class="dot" id="dot"></span><span id="liveText">connecting…</span></div>
  </header>

  <div class="banner" id="banner"></div>

  <div class="nodes" id="nodes"></div>

  <div class="lanes">
    <section class="lane pend empty" id="pendLane">
      <h2>⏳ Pending — could not be placed <span class="count" id="pendCount">0</span></h2>
      <div class="cards" id="pending"></div>
    </section>
    <section class="lane evict empty" id="evictLane">
      <h2>⚡ Preempting <span class="count" id="evictCount">0</span></h2>
      <div class="cards" id="evicting"></div>
    </section>
  </div>

  <div class="legend">
    <span class="item"><span class="box used"></span> allocated to a pod</span>
    <span class="item"><span class="box unattr"></span> used, agent not yet reported</span>
    <span class="item"><span class="box free"></span> free</span>
    <span class="spacer"></span>
    <span class="item" id="finished"></span>
  </div>
  <div class="foot" id="foot"></div>
</div>

<script>
const CFG = "__CONFIG__";
const $ = (id) => document.getElementById(id);
let lastUpdate = 0, lastError = false;

// ── tiny keyed reconciler: add / update / remove children with enter+leave ──
function reconcile(container, items, keyOf, create, update){
  const existing = new Map();
  for(const c of [...container.children]) existing.set(c.dataset.key, c);
  const seen = new Set();
  items.forEach((it, idx) => {
    const k = String(keyOf(it)); seen.add(k);
    let el = existing.get(k);
    if(!el){
      el = create(it); el.dataset.key = k; el.classList.add('enter');
      container.appendChild(el);
      requestAnimationFrame(() => el.classList.remove('enter'));
    }
    update(el, it, idx);
    container.appendChild(el); // keep DOM order == items order
  });
  existing.forEach((el, k) => {
    if(!seen.has(k)){ el.classList.add('leave'); setTimeout(() => el.remove(), 320); }
  });
}

const el = (tag, cls, txt) => { const e = document.createElement(tag); if(cls) e.className = cls; if(txt!=null) e.textContent = txt; return e; };

// ── GPU slots ───────────────────────────────────────────────────────────────
function slotDescriptors(axis){
  const out = [];
  for(const s of axis.segments){
    const n = Math.round(s.amount);
    for(let i=0;i<n;i++) out.push({kind:'used', color:s.color, pod:s.pod, evicting:s.evicting});
  }
  for(let i=0;i<Math.round(axis.unattributed);i++) out.push({kind:'unattr'});
  while(out.length < Math.round(axis.allocatable)) out.push({kind:'free'});
  return out;
}
function renderSlots(host, axis){
  reconcile(host, slotDescriptors(axis), (_d,i)=>i, () => el('div','slot'),
    (e,d) => {
      e.className = 'slot ' + (d.kind==='used'?'used':d.kind==='unattr'?'unattr':'free') + (d.evicting?' evicting':'');
      e.style.background = d.kind==='used' ? d.color : '';
      e.title = d.kind==='used' ? d.pod : (d.kind==='unattr' ? 'used — placement not yet reported by agent' : 'free');
    });
}

// ── CPU bar ───────────────────────────────────────────────────────────────────
function renderTrack(host, axis){
  const alloc = axis.allocatable || 1;
  const segs = axis.segments.map(s => ({key:s.pod, color:s.color, w:s.amount/alloc*100, evicting:s.evicting}));
  if(axis.unattributed > 0.001) segs.push({key:'__unattr__', color:null, w:axis.unattributed/alloc*100, evicting:false});
  reconcile(host, segs, s=>s.key, () => el('div','seg'),
    (e,s) => {
      e.className = 'seg' + (s.color?'':' unattr') + (s.evicting?' evicting':'');
      e.style.background = s.color || '';
      e.style.width = Math.max(0, s.w) + '%';
      e.title = s.color ? s.key : 'used — placement not yet reported by agent';
    });
}

// ── chips: one per pod present on the zone (union of gpu+cpu segments) ─────────
function zoneChips(zone){
  const m = new Map();
  for(const ax of ['gpu','cpu']){
    const axis = zone[ax]; if(!axis) continue;
    for(const s of axis.segments){
      const e = m.get(s.pod) || {pod:s.pod, color:s.color, gpu:0, cpu:0, evicting:s.evicting};
      if(ax==='gpu') e.gpu += s.amount; else e.cpu += s.amount;
      m.set(s.pod, e);
    }
  }
  return [...m.values()].sort((a,b)=>a.pod.localeCompare(b.pod));
}
function renderChips(host, zone){
  reconcile(host, zoneChips(zone), c=>c.pod,
    () => { const c = el('div','chip'); c.append(el('span','sw'), el('span','pn'), el('span','amt')); return c; },
    (e,c) => {
      e.className = 'chip' + (c.evicting?' evicting':'');
      e.children[0].style.background = c.color;
      e.children[1].textContent = c.pod;
      e.children[1].title = c.pod;
      const parts = [];
      if(c.gpu>0) parts.push(fmt(c.gpu)+' GPU');
      if(c.cpu>0) parts.push(fmt(c.cpu)+' CPU');
      e.children[2].textContent = parts.join(' · ');
    });
}

const fmt = (x) => Math.abs(x-Math.round(x))<1e-9 ? String(Math.round(x)) : x.toFixed(1);

// ── zones & nodes ──────────────────────────────────────────────────────────────
function createZone(){
  const z = el('div','zone');
  z.innerHTML = `
    <div class="zone-head"><span class="zone-name"></span><span class="zone-costs"></span></div>
    <div class="res"><div class="res-label"><span class="name">GPU</span><span class="nums gpu-nums"></span></div><div class="slots gpu-slots"></div></div>
    <div class="res"><div class="res-label"><span class="name">CPU cores</span><span class="nums cpu-nums"></span></div><div class="track cpu-track"></div></div>
    <div class="chips"></div>`;
  return z;
}
function numsHTML(host, axis){
  host.innerHTML = `<b>${fmt(axis.used)}</b> <span class="of">/ ${fmt(axis.allocatable)}</span>`;
}
function updateZone(z, zone){
  z.querySelector('.zone-name').textContent = 'NUMA ' + zone.name;
  const costs = z.querySelector('.zone-costs'); costs.innerHTML = '';
  const names = Object.keys(zone.costs).sort();
  for(const n of names){
    const isSelf = n === zone.self;
    const c = el('span', 'cost' + (isSelf?' self':''), (isSelf?'self ':'↔ '+n+' ') + zone.costs[n]);
    c.title = isSelf ? 'local NUMA access cost' : 'distance to ' + n;
    costs.appendChild(c);
  }
  if(zone.gpu){ numsHTML(z.querySelector('.gpu-nums'), zone.gpu); renderSlots(z.querySelector('.gpu-slots'), zone.gpu); }
  if(zone.cpu){ numsHTML(z.querySelector('.cpu-nums'), zone.cpu); renderTrack(z.querySelector('.cpu-track'), zone.cpu); }
  renderChips(z.querySelector('.chips'), zone);
}
function createNode(){
  const n = el('div','node');
  n.innerHTML = `<div class="node-head"><span class="ico">🖥️</span><span class="nname"></span></div><div class="zones"></div>`;
  return n;
}
function updateNode(n, node){
  n.querySelector('.nname').textContent = node.name;
  reconcile(n.querySelector('.zones'), node.zones, z=>z.name, createZone, updateZone);
}

// ── pending / evicting cards ────────────────────────────────────────────────────
function createPod(){
  const p = el('div','pod');
  p.innerHTML = `<div class="pod-top"><span class="sw"></span><span class="pname"></span></div><div class="reqs"></div><div class="msg"></div>`;
  return p;
}
function reqPill(k,v){ const r = el('span','req'); r.innerHTML = `<span class="k">${k}</span> ${v}`; return r; }
function updatePod(kind){
  return (e, p) => {
    e.className = 'pod ' + kind;
    e.children[0].children[0].style.background = p.color;
    e.children[0].children[1].textContent = p.name;
    e.children[0].children[1].title = p.name;
    const top = e.children[0];
    [...top.querySelectorAll('.badge')].forEach(b=>b.remove());
    if(p.queue) top.appendChild(badge('badge', p.queue));
    if(p.priorityClass) top.appendChild(badge('badge policy', p.priorityClass));
    const reqs = e.children[1]; reqs.innerHTML = '';
    if(p.reqGpu>0) reqs.appendChild(reqPill('GPU', fmt(p.reqGpu)));
    if(p.reqCpu>0) reqs.appendChild(reqPill('CPU', fmt(p.reqCpu)));
    if(p.reqMem) reqs.appendChild(reqPill('MEM', p.reqMem));
    const msg = e.children[2];
    if(kind === 'pending' && (p.message || p.reason)){
      const numa = /NUMA|Topology Manager|align/i.test(p.message);
      msg.className = 'msg' + (numa?' numa':'');
      msg.innerHTML = (numa?'<span class="tag">NUMA</span>':'') + escapeHtml(p.message || p.reason);
    } else if(kind === 'evicting'){
      msg.className = 'msg';
      msg.textContent = p.zones.length ? ('was placed on ' + p.zones.join(', ')) : 'being preempted…';
    } else { msg.className='msg'; msg.textContent=''; }
  };
}
function badge(cls, txt){ const b = el('span', cls, txt); return b; }
function escapeHtml(s){ const d = el('div'); d.textContent = s||''; return d.innerHTML; }

// ── top-level render ─────────────────────────────────────────────────────────────
function render(s){
  reconcile($('nodes'), s.nodes, n=>n.name, createNode, updateNode);

  const n0 = s.nodes[0];
  const badges = $('badges'); badges.innerHTML = '';
  if(n0){
    if(n0.topologyPolicy) badges.appendChild(badge('badge policy', n0.topologyPolicy));
    else if(n0.policy) badges.appendChild(badge('badge policy', n0.policy));
    if(n0.scope) badges.appendChild(badge('badge scope', 'scope: ' + n0.scope));
    badges.appendChild(badge('badge', 'ns: ' + s.ns));
  }

  const pend = s.pods.pending || [];
  $('pendCount').textContent = pend.length;
  $('pendLane').classList.toggle('empty', pend.length === 0);
  reconcile($('pending'), pend, p=>p.name, createPod, updatePod('pending'));

  const evict = s.pods.evicting || [];
  $('evictCount').textContent = evict.length;
  $('evictLane').classList.toggle('empty', evict.length === 0);
  reconcile($('evicting'), evict, p=>p.name, createPod, updatePod('evicting'));

  const f = s.pods.finished || {};
  const fin = [];
  if(f.Succeeded) fin.push(f.Succeeded + ' completed');
  if(f.Failed) fin.push(f.Failed + ' failed/terminated');
  $('finished').textContent = fin.length ? ('finished: ' + fin.join(' · ')) : '';

  lastUpdate = Date.now(); lastError = false;
}

function setLive(){
  const dot = $('dot'), t = $('liveText');
  if(lastError){ dot.classList.add('stale'); return; }
  if(!lastUpdate){ t.textContent = 'connecting…'; return; }
  const age = Math.round((Date.now() - lastUpdate)/1000);
  dot.classList.toggle('stale', age > Math.max(6, CFG.pollMs/1000*4));
  t.textContent = age <= 1 ? 'updated just now' : 'updated ' + age + 's ago';
}

async function poll(){
  try{
    const r = await fetch('/api/state', {cache:'no-store'});
    const s = await r.json();
    if(!s.ok){ showBanner(s.error || 'kubectl error'); lastError = true; }
    else { hideBanner(); render(s); }
  }catch(e){ showBanner(String(e)); lastError = true; }
  finally{ setLive(); setTimeout(poll, CFG.pollMs); }
}
function showBanner(msg){ const b = $('banner'); b.textContent = '⚠ ' + msg; b.classList.add('show'); }
function hideBanner(){ $('banner').classList.remove('show'); }

setInterval(setLive, 1000);
poll();
</script>
</body>
</html>
"""


def html_with_config(cfg):
    return HTML_PAGE.replace('"__CONFIG__"', json.dumps(cfg))


# ── server ──────────────────────────────────────────────────────────────────────

class Server(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, addr, handler, namespace, global_args, poll_ms):
        super().__init__(addr, handler)
        self.namespace = namespace
        self.global_args = global_args
        self.poll_ms = poll_ms


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass  # quiet; this is a foreground demo tool

    def _send(self, code, content_type, body, no_cache=False):
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        if no_cache:
            self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = urlparse(self.path).path
        if path in ("/", "/index.html"):
            cfg = {"pollMs": self.server.poll_ms, "ns": self.server.namespace}
            self._send(200, "text/html; charset=utf-8", html_with_config(cfg).encode())
            return
        if path == "/api/state":
            try:
                state = build_state(self.server.namespace, self.server.global_args)
            except subprocess.TimeoutExpired:
                state = {"ok": False, "error": "kubectl timed out"}
            except Exception as exc:  # surface to the UI banner rather than 500
                state = {"ok": False, "error": str(exc)}
            self._send(200, "application/json", json.dumps(state).encode(), no_cache=True)
            return
        self._send(404, "text/plain; charset=utf-8", b"not found")


def main():
    ap = argparse.ArgumentParser(description="Live NUMA placement dashboard for the KAI scheduler demo.")
    ap.add_argument("--port", type=int, default=8080)
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--namespace", "-n", default="default")
    ap.add_argument("--interval", type=float, default=1.5, help="browser poll interval in seconds")
    ap.add_argument("--context", default="", help="kubectl --context to use")
    ap.add_argument("--kubeconfig", default="", help="path passed to kubectl --kubeconfig")
    ap.add_argument("--once", action="store_true", help="print the JSON state model once and exit")
    args = ap.parse_args()

    global_args = []
    if args.context:
        global_args += ["--context", args.context]
    if args.kubeconfig:
        global_args += ["--kubeconfig", args.kubeconfig]

    if args.once:
        print(json.dumps(build_state(args.namespace, global_args), indent=2))
        return

    server = Server((args.host, args.port), Handler, args.namespace, global_args,
                    int(args.interval * 1000))
    url = f"http://{args.host}:{args.port}"
    print(f"numa-viz → {url}  (namespace={args.namespace}, refresh every {args.interval}s)")
    print("Ctrl-C to stop.")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nbye.")


if __name__ == "__main__":
    main()
