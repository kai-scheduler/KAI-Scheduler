# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("numa_reconfigure", Path(__file__).with_name("test-mokka-numa-reconfigure.py"))
experiment = importlib.util.module_from_spec(spec)
spec.loader.exec_module(experiment)


class LiveNUMAProfileTests(unittest.TestCase):
    def test_only_pci_placement_changes(self):
        prefix = "version: '1.0'\ndevices:\n  - uuid: GPU-original\n    numa_node: 1\n"
        topology = "pcie_topology:\n  root_complexes:\n    - id: pci0000:00\n      numa_node: 0\n      devices: [gpu0]\n    - id: pci0000:80\n      numa_node: 1\n      devices: [gpu1]\n"
        suffix = "unrelated:\n  numa_node: 1\n"
        changed = experiment.single_zone_profile(prefix + topology + suffix)
        self.assertEqual(changed, prefix + topology.replace("numa_node: 1", "numa_node: 0") + suffix)

    def test_unexpected_layout_is_rejected_before_patching(self):
        for source in ("devices: []\n", "pcie_topology:\n  roots: []\n", "pcie_topology:\n  root_complexes:\n    - numa_node: 2\n"):
            with self.subTest(source=source), self.assertRaises(AssertionError):
                experiment.single_zone_profile(source)

    def test_zero_and_nonzero_numa_ids(self):
        self.assertEqual(experiment.topology_ids([{"topology": {"nodes": [{"ID": "0"}, {"ID": "1"}]}}]), {0, 1})
        self.assertEqual(experiment.topology_ids([{"topology": {"nodes": [{}]}}]), {0})
        self.assertEqual(experiment.topology_ids([{}]), set())

    def test_cpu_ranges(self):
        self.assertEqual(experiment.cpu_ids("0-2,4,6-7\n"), {0, 1, 2, 4, 6, 7})

    def test_restarted_agent_is_not_a_successful_live_update(self):
        test = experiment.Experiment("test", "probe", ".")
        test.node, test.pod_uid, test.agent_restarts = "worker", "same-pod", 0
        node = {"status": {"conditions": [{"type": "Ready", "status": "True"}], "allocatable": {experiment.GPU: "8"}}}
        pod = {"metadata": {"uid": "same-pod"}, "status": {"containerStatuses": [{"name": "node-agent", "ready": True, "restartCount": 1}]}}
        with patch.object(test, "get", return_value=node), patch.object(test, "agent", return_value=pod):
            with self.assertRaisesRegex(AssertionError, "agent restarted"):
                test.ready()
