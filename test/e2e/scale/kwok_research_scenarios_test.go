// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"testing"
)

func TestResearchWorkloadMixScalesWithNodeCount(t *testing.T) {
	previousPods := 0
	previousQueues := 0
	for _, gpuNodes := range []int{100, 500, 1000, 5000} {
		cpuNodes := researchCPUNodeCount(gpuNodes)
		gpuCapacity := float64(gpuNodes * gpusPerNode)
		cpuCapacity := float64(cpuNodes * researchCPUNodeCPUs)

		var totalGPUs, totalCPUs int64
		totalPods := 0
		for _, workload := range researchWorkloadMix(gpuNodes, cpuNodes) {
			gpus, cpus := workload.demand()
			totalGPUs += gpus
			totalCPUs += cpus
			totalPods += workload.pods()

			share := float64(gpus) / gpuCapacity * 100
			if workload.kind == preprocessWorkload {
				share = float64(cpus) / cpuCapacity * 100
			}
			if share < 5 || share > 10 {
				t.Errorf("nodes=%d: %s workload uses %.1f%% of the cluster, want 5-10%%", gpuNodes, workload.kind, share)
			}
		}

		if pct := float64(totalGPUs) / gpuCapacity * 100; pct < 140 || pct > 160 {
			t.Errorf("nodes=%d: GPU demand is %.1f%% of capacity, want ~150%%", gpuNodes, pct)
		}
		if pct := float64(totalCPUs) / cpuCapacity * 100; pct < 140 || pct > 160 {
			t.Errorf("nodes=%d: CPU-pool demand is %.1f%% of capacity, want ~150%%", gpuNodes, pct)
		}
		if totalPods <= previousPods {
			t.Errorf("nodes=%d: %d pods, want more than %d", gpuNodes, totalPods, previousPods)
		}
		if queues := researchQueueCount(gpuNodes); queues < previousQueues {
			t.Errorf("nodes=%d: %d queues, want at least %d", gpuNodes, queues, previousQueues)
		} else {
			previousQueues = queues
		}
		previousPods = totalPods
	}
}

func TestResearchRLWorkloadIsHierarchical(t *testing.T) {
	for _, workload := range researchWorkloadMix(500, researchCPUNodeCount(500)) {
		if workload.kind != rlWorkload {
			continue
		}
		parents := map[string]string{}
		for _, subGroup := range workload.subGroups {
			parents[subGroup.name] = subGroup.parent
		}
		if parents["generator"] != "rollout" || parents["envs"] != "rollout" || parents["trainer"] != "" {
			t.Fatalf("unexpected RL hierarchy: %v", parents)
		}
		return
	}
	t.Fatal("no RL workload in the mix")
}
