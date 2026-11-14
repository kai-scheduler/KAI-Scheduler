#!/usr/bin/env python3
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

"""Check live 2 -> 1 -> 2 zone updates on a disposable Kind worker."""
import argparse
import json
import re
import subprocess
import time
from pathlib import Path

GPU = "nvidia.com/gpu"
NAMESPACE = "mokka-numa-reconfigure"


def single_zone_profile(profile):
    # Change PCI placement only; preserve GPU UUIDs, BDFs, and the rest of A100.
    match = re.search(r"(?ms)^pcie_topology:\n.*?(?=^[^ \t#\n]|\Z)", profile)
    assert match, "profile has no PCI topology"
    topology = match.group()
    ids = re.findall(r"(?m)^\s+numa_node:\s*(\d+)\s*$", topology)
    assert ids == ["0", "1"], f"expected two A100 roots, got {ids}"
    replacement = re.sub(r"(?m)^(\s+numa_node:\s*)1\s*$", r"\g<1>0", topology)
    return profile[:match.start()] + replacement + profile[match.end():]


def topology_ids(resources):
    return {int(node.get("ID", 0)) for item in resources
            for node in item.get("topology", {}).get("nodes", [])}


def cpu_ids(text):
    result = set()
    for item in text.strip().split(","):
        first, _, last = item.partition("-")
        result.update(range(int(first), int(last or first) + 1))
    return result


class Experiment:
    def __init__(self, cluster, probe, output):
        self.context = "kind-" + cluster
        self.probe = probe
        self.output = Path(output)
        self.output.mkdir(parents=True, exist_ok=True)
        self.node = None
        self.original = None
        self.configmap = None
        self.pod_uid = None
        self.agent_restarts = None

    def run(self, *args, data=None):
        result = subprocess.run(args, input=data, text=True, capture_output=True, timeout=90)
        if result.returncode:
            raise RuntimeError(f"{args}: {result.stderr.strip()} {result.stdout.strip()}")
        return result.stdout

    def kubectl(self, *args, data=None):
        return self.run("kubectl", "--context", self.context, *args, data=data)

    def get(self, *args):
        return json.loads(self.kubectl("get", *args, "-o", "json"))

    def host(self, *args):
        return self.run("docker", "exec", self.node, *args)

    def wait(self, message, check, timeout=240):
        deadline = time.monotonic() + timeout
        last = None
        while time.monotonic() < deadline:
            try:
                result = check()
                print(message, flush=True)
                return result
            except (AssertionError, RuntimeError, KeyError, ValueError) as error:
                last = error
                time.sleep(3)
        raise AssertionError(f"{message}: timed out: {last}")

    def agent(self):
        pods = self.get("pods", "-n", "mokka", "--field-selector", "spec.nodeName=" + self.node)["items"]
        agents = [pod for pod in pods if any(c["name"] == "node-agent" for c in pod["spec"]["containers"])]
        assert len(agents) == 1, f"expected one Mokka pod, got {len(agents)}"
        return agents[0]

    def ready(self):
        node = self.get("node", self.node)
        assert any(c["type"] == "Ready" and c["status"] == "True" for c in node["status"]["conditions"])
        assert int(node["status"]["allocatable"].get(GPU, 0)) == 8
        pod = self.agent()
        assert pod["metadata"]["uid"] == self.pod_uid, "Mokka pod was replaced"
        statuses = pod["status"].get("containerStatuses", [])
        assert statuses and all(c.get("ready") for c in statuses), "Mokka is not Ready"
        status = next(c for c in statuses if c["name"] == "node-agent")
        assert status["restartCount"] == self.agent_restarts, "node agent restarted during the update"

    def snapshot(self):
        return json.loads(self.host("/usr/local/bin/mokka-numa-podresources"))

    def nrt(self, zones):
        report = self.get("noderesourcetopology", self.node)
        local = [zone for zone in report["zones"] if zone["type"] == "Node"]
        assert {z["name"] for z in local} == {f"node-{i}" for i in range(zones)}, report
        counts = [int(r["capacity"]) for zone in local for r in zone.get("resources", []) if r["name"] == GPU]
        assert sorted(counts) == [8 // zones] * zones, counts
        for zone in local:
            resources = {r["name"]: r for r in zone["resources"]}
            assert "cpu" in resources and "memory" in resources, zone
            assert resources["memory"]["capacity"] != "0", zone
        return report

    def restart_operand(self, fragment):
        def matching():
            pods = self.get("pods", "-n", "gpu-operator", "--field-selector", "spec.nodeName=" + self.node)["items"]
            return [pod for pod in pods if any(fragment in owner["name"] and owner["kind"] == "DaemonSet"
                                              for owner in pod["metadata"].get("ownerReferences", []))]
        old = matching()
        assert len(old) == 1, f"expected one {fragment} pod, got {len(old)}"
        uid = old[0]["metadata"]["uid"]
        self.kubectl("delete", "pod", "-n", "gpu-operator", old[0]["metadata"]["name"], "--wait=true", "--timeout=60s")
        def replaced():
            fresh = matching()
            assert len(fresh) == 1 and fresh[0]["metadata"]["uid"] != uid
            assert any(c["type"] == "Ready" and c["status"] == "True" for c in fresh[0]["status"].get("conditions", []))
        self.wait(f"Refreshed {fragment} on {self.node}", replaced)

    def patch(self, profile):
        self.kubectl("patch", "configmap", "-n", "mokka", self.configmap, "--type=merge",
                     "-p", json.dumps({"data": {"config.yaml": profile}}))

    def layout(self, zones, profile=None):
        if profile is not None:
            old_key = self.host("cat", "/run/mokka/numa/.applied").strip()
            self.patch(profile)
            def applied():
                actual = self.host("cat", "/sys/devices/system/node/online").strip()
                assert actual == ("0-1" if zones == 2 else "0"), actual
                key = self.host("cat", "/run/mokka/numa/.applied").strip()
                assert key != old_key and key == self.host("cat", "/var/lib/nvml-mock/numa/desired").strip()
                self.ready()
            self.wait(f"Mokka applied {zones} zone(s) without an agent restart", applied)
            self.restart_operand("device-plugin")
            self.restart_operand("topology-updater")
        self.wait(f"Node and Mokka are Ready with {zones} zone(s)", self.ready)
        report = self.wait(f"NRT reports {zones} zone(s)", lambda: self.nrt(zones))
        snapshot = self.snapshot()
        devices = [d for d in snapshot["allocatable"]["devices"] if d["resourceName"] == GPU]
        assert topology_ids(devices) == set(range(zones)), snapshot
        assert sum(len(d["deviceIds"]) for d in devices) == 8, snapshot
        assert topology_ids(snapshot["allocatable"]["memory"]) == set(range(zones)), snapshot
        self.output.joinpath(f"{self.phase}-nrt.json").write_text(json.dumps(report, indent=2))
        self.output.joinpath(f"{self.phase}-podresources.json").write_text(json.dumps(snapshot, indent=2))

    def workload(self, should_run):
        name = self.phase + "-five-gpus"
        pod = {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": name, "namespace": NAMESPACE},
               "spec": {"nodeName": self.node, "restartPolicy": "Never",
                        "containers": [{"name": "pause", "image": "registry.k8s.io/pause:3.10",
                                        "resources": {side: {"cpu": "1", "memory": "128Mi", GPU: "5"}
                                                      for side in ("requests", "limits")}}]}}
        self.kubectl("create", "-f", "-", data=json.dumps(pod))
        try:
            def admitted():
                fresh = self.get("pod", "-n", NAMESPACE, name)
                if should_run:
                    assert any(c["type"] == "Ready" and c["status"] == "True" for c in fresh["status"].get("conditions", [])), fresh["status"]
                else:
                    assert fresh["status"].get("phase") == "Failed" and fresh["status"].get("reason") == "TopologyAffinityError", fresh["status"]
                return fresh
            fresh = self.wait(f"{name}: " + ("Ready" if should_run else "rejected with TopologyAffinityError"), admitted)
            self.output.joinpath(name + ".json").write_text(json.dumps(fresh, indent=2))
            if should_run:
                def allocated():
                    snapshot = self.snapshot()
                    entries = [p for p in snapshot["pods"]["podResources"] if p["namespace"] == NAMESPACE and p["name"] == name]
                    assert len(entries) == 1, snapshot
                    container = next(c for c in entries[0]["containers"] if c["name"] == "pause")
                    devices = [d for d in container["devices"] if d["resourceName"] == GPU]
                    assert sum(len(d["deviceIds"]) for d in devices) == 5, container
                    assert topology_ids(devices) == {0}, container
                    assert topology_ids(container["memory"]) == {0}, container
                    cpus = {int(cpu) for cpu in container["cpuIds"]}
                    assert len(cpus) == 1 and cpus <= cpu_ids(self.host("cat", "/sys/devices/system/node/node0/cpulist")), container
                    return snapshot
                allocation = self.wait("Real kubelet socket reports aligned CPU, memory, and five GPUs", allocated)
                self.output.joinpath(name + "-allocation.json").write_text(json.dumps(allocation, indent=2))
        finally:
            self.kubectl("delete", "pod", "-n", NAMESPACE, name, "--ignore-not-found", "--wait=true", "--timeout=60s")

    def execute(self):
        nodes = self.get("nodes", "-l", "mokka.nvidia.com/e2e-gpu=true")["items"]
        assert len(nodes) == 1, "this test requires the dedicated one-worker Kind layout"
        self.node = nodes[0]["metadata"]["name"]
        pod = self.agent()
        self.pod_uid = pod["metadata"]["uid"]
        self.agent_restarts = next(c["restartCount"] for c in pod["status"]["containerStatuses"] if c["name"] == "node-agent")
        self.configmap = next(v["configMap"]["name"] for v in pod["spec"]["volumes"] if v["name"] == "mokka-profile")
        self.original = self.get("configmap", "-n", "mokka", self.configmap)["data"]["config.yaml"]
        replacement = single_zone_profile(self.original)
        self.run("docker", "cp", self.probe, self.node + ":/usr/local/bin/mokka-numa-podresources")
        self.kubectl("create", "namespace", NAMESPACE)
        try:
            for phase, zones, profile in [("initial", 2, None), ("one-zone", 1, replacement), ("restored", 2, self.original)]:
                self.phase = phase
                self.layout(zones, profile)
                self.workload(zones == 1)
            self.output.joinpath("result.json").write_text(json.dumps({"result": "passed", "node": self.node, "zones": [2, 1, 2]}))
        finally:
            # Restore the profile even when a check fails; CI still exports the node logs.
            self.patch(self.original)
            self.kubectl("delete", "namespace", NAMESPACE, "--ignore-not-found", "--wait=false")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cluster", required=True)
    parser.add_argument("--probe", required=True)
    parser.add_argument("--output", default="numa-reconfigure-results")
    args = parser.parse_args()
    Experiment(args.cluster, args.probe, args.output).execute()
