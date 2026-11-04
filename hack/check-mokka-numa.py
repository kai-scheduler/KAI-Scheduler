#!/usr/bin/env python3
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

"""Require real NFD reports for each simulated GPU node before running tests."""
import json
import subprocess
import time


def get_objects(resource, *args):
    return json.loads(subprocess.check_output(
        ["kubectl", "get", resource, *args, "-o", "json"], text=True))


def check():
    nodes = get_objects("nodes", "-l", "mokka.nvidia.com/e2e-gpu=true")["items"]
    reports = {nrt["metadata"]["name"]: nrt for nrt in
               get_objects("noderesourcetopologies")["items"]}
    assert len(nodes) == 4, f"expected four GPU nodes, got {len(nodes)}"
    policies = []
    for node in nodes:
        name = node["metadata"]["name"]
        policy = node["metadata"]["labels"]["mokka.nvidia.com/numa-policy"]
        report = reports.get(name)
        assert report is not None, f"waiting for NRT on {name}"
        attrs = {attr["name"]: attr["value"] for attr in report.get("attributes", [])}
        assert attrs.get("topologyManagerPolicy") == policy, (name, attrs, policy)
        gpu_zones = sorted(int(resource["capacity"]) for zone in report["zones"]
                           if zone["type"] == "Node" for resource in zone.get("resources", [])
                           if resource["name"] == "nvidia.com/gpu")
        assert gpu_zones == [4, 4], (name, gpu_zones)
        cpu_zones = [int(resource["allocatable"]) for zone in report["zones"]
                     if zone["type"] == "Node" for resource in zone.get("resources", [])
                     if resource["name"] == "cpu"]
        assert len(cpu_zones) == 2 and min(cpu_zones) >= 1, (name, cpu_zones)
        memory_zones = [resource["allocatable"] for zone in report["zones"]
                        if zone["type"] == "Node" for resource in zone.get("resources", [])
                        if resource["name"] == "memory"]
        assert len(memory_zones) == 2 and all(v != "0" for v in memory_zones), (name, memory_zones)
        policies.append(policy)
        print(f"{name}: {policy}, GPUs per zone: {gpu_zones}, CPUs: {cpu_zones}, memory: {memory_zones}", flush=True)
    assert sorted(policies) == ["best-effort", "restricted", "single-numa-node", "single-numa-node"]


if __name__ == "__main__":
    for attempt in range(60):
        try:
            check()
            break
        except (AssertionError, subprocess.CalledProcessError) as error:
            if attempt == 59:
                raise
            print(f"Waiting for NFD: {error}", flush=True)
            time.sleep(5)
