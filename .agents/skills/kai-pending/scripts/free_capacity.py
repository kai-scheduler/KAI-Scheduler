#!/usr/bin/env python3
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
"""Free GPU/CPU/mem per node, from one pending pod's point of view.

Prints every node's free vs total capacity and whether the pod may land there
(nodeSelector/affinity, taints). "Free" = allocatable minus what scheduled pods *request* - the
scheduler's own accounting, so it won't be fooled by idle-but-reserved GPUs the way a utilization
graph would, and it is scoped to one pod's eligibility (unlike a cluster-wide dashboard). KAI's
verdict already names most cases; use this when the message is the generic "no nodes with enough
resources" and you need the actual free numbers (and where any free space is).

Reads JSON dumped with kubectl (it does not call kubectl); pass the dump DIRECTORY.
See `--help` for the exact dump commands.

Example output (`--pod waiter` - an 8-GPU gang; default view drops non-GPU nodes):

    POD  waiter   queue=scn08-lo   schedulerName=kai-scheduler
      requests: gpu=8  cpu=0m  mem=0.0G
      nodeSelector: {}

      NODE                              GPU f/a         CPU f/a       MEM f/a  SELECTOR  TAINTS
      e2e-kai-scheduler-worker              0/8     3650m/4000m     6.8G/7.6G  match     -
      e2e-kai-scheduler-worker2             0/8     3450m/4000m     6.7G/7.6G  match     -
      e2e-kai-scheduler-worker3             0/8     3700m/4000m     7.0G/7.6G  match     -
      e2e-kai-scheduler-worker4             0/8     3600m/4000m     7.2G/7.6G  match     -
      (2 non-GPU node(s) hidden - pass --all-nodes to show them)

Columns: for a GPU request only GPU nodes are shown (clusters can have thousands of non-GPU
nodes); pass `--all-nodes` to see them. `GPU f/a` = free/allocatable, where free = allocatable -
sum of requests of the pods already on the node; `SELECTOR` = `match`, or why the pod's
nodeSelector/affinity rules the node out; `TAINTS` = node taints the pod does not tolerate. Above,
every GPU node shows 0 free of 8: capacity exists but is held by others (contention / fair-share),
not a node-fit shortage.
"""
from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass
from pathlib import Path

USAGE = """\
free_capacity.py reads JSON you dump with kubectl first - it does NOT take a namespace and does
NOT call kubectl itself.

  1. dump (run these, pick any dir):
       mkdir -p /tmp/kai
       kubectl get pods -n <ns> --field-selector=status.phase=Pending -o json > /tmp/kai/pods.json
       kubectl get nodes -o json > /tmp/kai/nodes.json
       # allpods gives FREE (not just total); skip finished pods to stay small on big clusters:
       kubectl get pods -A --field-selector=status.phase!=Succeeded,status.phase!=Failed -o json > /tmp/kai/allpods.json
  2. run, passing the DIRECTORY:
       free_capacity.py /tmp/kai [--pod <name>]
"""


# --- Kubernetes quantity parsing -------------------------------------------------------------
# CPU quantities are "100m" (millicores) or "2" (whole cores). Memory is "<n><suffix>" with a
# binary suffix (Ki/Mi/Gi/...) or plain bytes. nvidia.com/gpu is always an integer count.

_MEM_SUFFIX = {"Ki": 2**10, "Mi": 2**20, "Gi": 2**30, "Ti": 2**40, "Pi": 2**50, "Ei": 2**60}


def parse_cpu(value: str | None) -> int:
    """CPU quantity -> millicores."""
    if not value:
        return 0
    value = str(value)
    if value.endswith("m"):
        return int(float(value[:-1]))
    return int(float(value) * 1000)


def parse_mem(value: str | None) -> int:
    """Memory quantity -> bytes."""
    if not value:
        return 0
    value = str(value)
    for suffix, factor in _MEM_SUFFIX.items():
        if value.endswith(suffix):
            return int(float(value[: -len(suffix)]) * factor)
    return int(float(value))


def parse_gpu(value: str | None) -> int:
    return int(float(value)) if value else 0


def fmt_cpu(millicores: int) -> str:
    return f"{millicores}m"


def fmt_mem(num_bytes: int) -> str:
    return f"{num_bytes / 2**30:.1f}G"


# --- Pod request -----------------------------------------------------------------------------

@dataclass
class Request:
    gpu: int = 0          # whole GPUs
    cpu: int = 0          # millicores
    mem: int = 0          # bytes
    fraction: str = ""    # GPU-sharing pods put the ask in an annotation, not in resources


def _amount(resources: dict, key: str) -> str | None:
    """A container's request for `key`, falling back to its limit (k8s default when unset)."""
    requests = resources.get("requests") or {}
    limits = resources.get("limits") or {}
    return requests.get(key, limits.get(key))


def pod_request(pod: dict) -> Request:
    # Fractional-GPU pods carry the ask in an annotation and have no nvidia.com/gpu in resources;
    # they need a whole free GPU for a reservation pod (see the fractional-gpu playbook).
    annotations = (pod.get("metadata") or {}).get("annotations") or {}
    if "gpu-fraction" in annotations:
        return Request(fraction=f"gpu-fraction={annotations['gpu-fraction']}")
    if "gpu-memory" in annotations:
        return Request(fraction=f"gpu-memory={annotations['gpu-memory']}")

    req = Request()
    for container in pod["spec"].get("containers", []):
        resources = container.get("resources") or {}
        req.gpu += parse_gpu(_amount(resources, "nvidia.com/gpu"))
        req.cpu += parse_cpu(_amount(resources, "cpu"))
        req.mem += parse_mem(_amount(resources, "memory"))
    return req


# --- Nodes -----------------------------------------------------------------------------------

@dataclass
class Node:
    name: str
    gpu: int
    cpu: int               # millicores
    mem: int               # bytes
    labels: dict
    taints: list           # NoSchedule / NoExecute taints (each a dict)
    used_gpu: int = 0
    used_cpu: int = 0
    used_mem: int = 0

    @property
    def free_gpu(self) -> int:
        return self.gpu - self.used_gpu

    @property
    def free_cpu(self) -> int:
        return self.cpu - self.used_cpu

    @property
    def free_mem(self) -> int:
        return self.mem - self.used_mem


def build_nodes(node_items: list, all_pods: list | None) -> list[Node]:
    """One Node per cluster node, with usage summed from the pods placed on it (if available)."""
    nodes: dict[str, Node] = {}
    for item in node_items:
        allocatable = item["status"].get("allocatable", {})
        taints = [t for t in (item["spec"].get("taints") or [])
                  if t.get("effect") in ("NoSchedule", "NoExecute")]
        nodes[item["metadata"]["name"]] = Node(
            name=item["metadata"]["name"],
            gpu=parse_gpu(allocatable.get("nvidia.com/gpu")),
            cpu=parse_cpu(allocatable.get("cpu")),
            mem=parse_mem(allocatable.get("memory")),
            labels=item["metadata"].get("labels") or {},
            taints=taints,
        )

    for pod in all_pods or []:
        node_name = (pod.get("spec") or {}).get("nodeName")
        phase = (pod.get("status") or {}).get("phase")
        if not node_name or node_name not in nodes or phase in ("Succeeded", "Failed"):
            continue
        req = pod_request(pod)
        node = nodes[node_name]
        node.used_gpu += req.gpu
        node.used_cpu += req.cpu
        node.used_mem += req.mem
    return list(nodes.values())


# --- Per-(pod, node) checks ------------------------------------------------------------------

def selector_status(pod: dict, node_labels: dict) -> str:
    """'match', or 'no: ...' naming the first nodeSelector/affinity requirement the node fails."""
    for key, want in (pod["spec"].get("nodeSelector") or {}).items():
        if node_labels.get(key) != want:
            return f"no: wants {key}={want}, has {node_labels.get(key, '<none>')}"
    affinity = (pod["spec"].get("affinity") or {}).get("nodeAffinity") or {}
    required = affinity.get("requiredDuringSchedulingIgnoredDuringExecution")
    if required:
        terms = required.get("nodeSelectorTerms") or []
        if terms and not any(_affinity_term_ok(term, node_labels) for term in terms):
            return "no: nodeAffinity"
    return "match"


def _affinity_term_ok(term: dict, labels: dict) -> bool:
    """A required-nodeAffinity term holds when all of its matchExpressions hold."""
    for expr in term.get("matchExpressions") or []:
        key, operator, values = expr.get("key"), expr.get("operator"), expr.get("values") or []
        present = key in labels
        value = labels.get(key)
        if operator == "In" and not (present and value in values):
            return False
        if operator == "NotIn" and present and value in values:
            return False
        if operator == "Exists" and not present:
            return False
        if operator == "DoesNotExist" and present:
            return False
    return True


def untolerated_taints(pod: dict, taints: list) -> list[str]:
    """Taints on the node that the pod does NOT tolerate (these exclude the node)."""
    tolerations = pod["spec"].get("tolerations") or []
    blocking = []
    for taint in taints:
        if not any(_tolerates(tol, taint) for tol in tolerations):
            blocking.append(f"{taint.get('key')}:{taint.get('effect')}")
    return blocking


def _tolerates(toleration: dict, taint: dict) -> bool:
    if toleration.get("effect") and toleration["effect"] != taint.get("effect"):
        return False
    if toleration.get("operator", "Equal") == "Exists":
        return not toleration.get("key") or toleration["key"] == taint.get("key")
    return (toleration.get("key") == taint.get("key")
            and toleration.get("value", "") == taint.get("value", ""))


# --- Output ----------------------------------------------------------------------------------

def visible_nodes(req: Request, nodes: list[Node], all_nodes: bool) -> list[Node]:
    """For a GPU request only GPU nodes can host it; big clusters are mostly non-GPU, so by default
    drop the rest (a request without GPU keeps all nodes; --all-nodes keeps them regardless)."""
    if all_nodes or not (req.gpu > 0 or req.fraction):
        return nodes
    return [n for n in nodes if n.gpu > 0]


def print_pod(pod: dict, nodes: list[Node], live: bool, hidden: int = 0) -> None:
    req = pod_request(pod)
    labels = pod["metadata"].get("labels") or {}
    print(f"POD  {pod['metadata']['name']}   queue={labels.get('kai.scheduler/queue', '<none>')}"
          f"   schedulerName={pod['spec'].get('schedulerName', '<none>')}")
    if req.fraction:
        print(f"  requests: {req.fraction}  (fractional GPU -> needs a node with a free WHOLE gpu)")
    else:
        print(f"  requests: gpu={req.gpu}  cpu={fmt_cpu(req.cpu)}  mem={fmt_mem(req.mem)}")
    print(f"  nodeSelector: {pod['spec'].get('nodeSelector') or '{}'}")
    print()
    print(f"  {'NODE':32} {'GPU f/a':>8} {'CPU f/a':>15} {'MEM f/a':>13}  {'SELECTOR':16} TAINTS")
    for node in nodes:
        gpu = f"{node.free_gpu}/{node.gpu}" if live else f"?/{node.gpu}"
        cpu = f"{fmt_cpu(node.free_cpu)}/{fmt_cpu(node.cpu)}" if live else f"?/{fmt_cpu(node.cpu)}"
        mem = f"{fmt_mem(node.free_mem)}/{fmt_mem(node.mem)}" if live else f"?/{fmt_mem(node.mem)}"
        print(f"  {node.name:32} {gpu:>8} {cpu:>15} {mem:>13}  "
              f"{selector_status(pod, node.labels):16} {','.join(untolerated_taints(pod, node.taints)) or '-'}")
    if hidden:
        print(f"  ({hidden} non-GPU node(s) hidden - pass --all-nodes to show them)")
    print()


def load_dir(path: Path) -> tuple[list, list, list | None]:
    pods = json.loads((path / "pods.json").read_text())["items"]
    nodes = json.loads((path / "nodes.json").read_text())["items"]
    all_pods_file = path / "allpods.json"
    all_pods = json.loads(all_pods_file.read_text())["items"] if all_pods_file.exists() else None
    return pods, nodes, all_pods


def main() -> int:
    parser = argparse.ArgumentParser(
        prog="free_capacity.py",
        description="Print each node's free vs total capacity against a pending pod's request "
                    "(reads JSON you dumped with kubectl; does not call kubectl).",
        epilog=USAGE, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("dump_dir", metavar="DUMP_DIR",
                        help="directory containing pods.json, nodes.json and (optional) allpods.json")
    parser.add_argument("--pod", metavar="NAME", help="only this pod (default: every Pending pod)")
    parser.add_argument("--all-nodes", action="store_true",
                        help="show non-GPU nodes too (default: for a GPU request, only GPU nodes)")
    args = parser.parse_args()

    try:
        pods, node_items, all_pods = load_dir(Path(args.dump_dir))
    except FileNotFoundError as exc:
        print(f"error: {exc.filename} not found - dump it first.\n", file=sys.stderr)
        print(USAGE, file=sys.stderr)
        return 2
    live = all_pods is not None
    if not live:
        print("note: no allpods.json - free capacity unknown, showing '?/total' (dump `kubectl get pods -A` for free).\n")
    nodes = build_nodes(node_items, all_pods)

    targets = [p for p in pods
               if p.get("status", {}).get("phase") == "Pending"
               and (args.pod is None or p["metadata"]["name"] == args.pod)]
    if not targets:
        print("no Pending pods matched.")
        return 0
    for pod in targets:
        shown = visible_nodes(pod_request(pod), nodes, args.all_nodes)
        print_pod(pod, shown, live, hidden=len(nodes) - len(shown))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
