// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// batchOfSize returns a victimBatch carrying n tasks. Only the task count matters for
// disruption accounting, so the task contents are left zero-valued.
func batchOfSize(n int) victimBatch {
	return victimBatch{tasks: make([]*pod_info.PodInfo, n)}
}

func TestNodeCascades_CountsGangsAndPods(t *testing.T) {
	// b0 spans node-0 and node-1 (a 2-node gang of 4 tasks total);
	// b1 lives entirely on node-1 (a single-node gang of 1 task).
	batches := []victimBatch{batchOfSize(4), batchOfSize(1)}
	nodeBatches := map[string][]int{
		"node-0": {0},    // only the wide gang touches node-0
		"node-1": {0, 1}, // wide gang + local gang touch node-1
	}

	cascade := nodeCascades(batches, nodeBatches)

	// Freeing node-0 destroys 1 gang (4 pods).
	require.Equal(t, 1, cascade["node-0"].gangs)
	require.Equal(t, 4, cascade["node-0"].pods)
	// Freeing node-1 destroys 2 gangs (4 + 1 = 5 pods).
	require.Equal(t, 2, cascade["node-1"].gangs)
	require.Equal(t, 5, cascade["node-1"].pods)
}

// TestSortViableCandidates_PrefersLeastDisruptiveNode: wide and narrow nodes both destroy
// 8 gangs and tie on capacity, but the narrow node cascades 8 pods vs 64, so it wins.
func TestSortViableCandidates_PrefersLeastDisruptiveNode(t *testing.T) {
	const wideGangs = 8
	wideNodes := []string{"node-0", "node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7"}
	narrowNode := "node-8"

	batches := make([]victimBatch, 0, wideGangs*2)
	nodeBatches := map[string][]int{}
	nodeCap := map[string]float64{}
	nodeFirstSeenAt := map[string]int{}

	// 8 wide gangs, each an 8-task gang with one task on every wide node.
	for g := 0; g < wideGangs; g++ {
		bi := len(batches)
		batches = append(batches, batchOfSize(len(wideNodes)))
		for _, n := range wideNodes {
			nodeBatches[n] = append(nodeBatches[n], bi)
		}
	}
	// 8 narrow single-task gangs, all on the narrow node.
	for g := 0; g < wideGangs; g++ {
		bi := len(batches)
		batches = append(batches, batchOfSize(1))
		nodeBatches[narrowNode] = append(nodeBatches[narrowNode], bi)
	}

	// Every node frees 8 GPUs locally, so capacity cannot distinguish them.
	for _, n := range append(append([]string{}, wideNodes...), narrowNode) {
		nodeCap[n] = 8
	}
	// Wide nodes are seen first; the narrow node last. A capacity-only sort would
	// therefore rank a wide node ahead of the narrow node.
	for i, n := range wideNodes {
		nodeFirstSeenAt[n] = i
	}
	nodeFirstSeenAt[narrowNode] = len(wideNodes)

	cascade := nodeCascades(batches, nodeBatches)
	// Both destroy 8 gangs; the pod cascade separates them (8 vs 64).
	require.Equal(t, 8, cascade[narrowNode].gangs)
	require.Equal(t, 8, cascade[wideNodes[0]].gangs)
	require.Equal(t, 8, cascade[narrowNode].pods, "narrow node cascades 8 pods")
	require.Equal(t, wideGangs*len(wideNodes), cascade[wideNodes[0]].pods, "wide node cascades 64 pods")

	ordered := sortViableCandidates(nodeBatches, nodeCap, cascade, nodeFirstSeenAt, 1)

	require.Equal(t, narrowNode, ordered[0],
		"least-disruptive node must be tried first despite equal gangs, equal capacity, and later queue position")
}

// TestSortViableCandidates_SingleNodeGangsUnchanged: with only single-node gangs, gangs
// tie, so ordering falls to pod cascade then capacity then queue order (no regression).
func TestSortViableCandidates_SingleNodeGangsUnchanged(t *testing.T) {
	// node-a: one 1-task gang (cap 1); node-b: one 3-task gang (cap 3);
	// node-c: one 3-task gang (cap 3) seen after node-b.
	batches := []victimBatch{batchOfSize(1), batchOfSize(3), batchOfSize(3)}
	nodeBatches := map[string][]int{
		"node-a": {0},
		"node-b": {1},
		"node-c": {2},
	}
	nodeCap := map[string]float64{"node-a": 1, "node-b": 3, "node-c": 3}
	nodeFirstSeenAt := map[string]int{"node-a": 0, "node-b": 1, "node-c": 2}

	cascade := nodeCascades(batches, nodeBatches)
	ordered := sortViableCandidates(nodeBatches, nodeCap, cascade, nodeFirstSeenAt, 1)

	// Fewest pods first, then queue order for the equal-cost pair.
	require.Equal(t, []string{"node-a", "node-b", "node-c"}, ordered)
}
