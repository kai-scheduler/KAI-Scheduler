// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"crypto/sha256"
	"sort"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

type scenarioFingerprint [sha256.Size]byte

const (
	fingerprintSectionSeparator = "\x1f"
	fingerprintElementSeparator = "\x00"
	fingerprintFieldSeparator   = "\x01"
)

// fingerprintScenario returns a canonical, order-independent identity for the
// simulation input of a ByNodeScenario. Two scenarios with the same fingerprint
// produce the same simulation outcome within a single JobSolver.SolveWithResult
// call: the fingerprint covers every variable simulation input — the preemptor,
// the pending task set (which differs between probes at different k), the
// recorded victims (which also determine the probe's feasible-node additions),
// and the potential victim tasks with their node assignments. The remaining
// simulation inputs (feasible nodes, plugin configuration) are constant across
// one job solve.
func fingerprintScenario(sn *scenario.ByNodeScenario) scenarioFingerprint {
	var builder strings.Builder

	builder.WriteString(string(sn.GetPreemptor().UID))
	builder.WriteString(fingerprintSectionSeparator)
	writeTaskUIDs(&builder, sn.PendingTasks())
	builder.WriteString(fingerprintSectionSeparator)
	writeTaskUIDs(&builder, sn.RecordedVictimsTasks())
	builder.WriteString(fingerprintSectionSeparator)
	writeTaskUIDsWithNodes(&builder, sn.PotentialVictimsTasks())

	return sha256.Sum256([]byte(builder.String()))
}

func writeTaskUIDs(builder *strings.Builder, tasks []*pod_info.PodInfo) {
	elements := make([]string, 0, len(tasks))
	for _, task := range tasks {
		elements = append(elements, string(task.UID))
	}
	writeSorted(builder, elements)
}

func writeTaskUIDsWithNodes(builder *strings.Builder, tasks []*pod_info.PodInfo) {
	elements := make([]string, 0, len(tasks))
	for _, task := range tasks {
		elements = append(elements, string(task.UID)+fingerprintFieldSeparator+task.NodeName)
	}
	writeSorted(builder, elements)
}

func writeSorted(builder *strings.Builder, elements []string) {
	sort.Strings(elements)
	for index, element := range elements {
		if index > 0 {
			builder.WriteString(fingerprintElementSeparator)
		}
		builder.WriteString(element)
	}
}

// scenarioDedupCache skips re-simulation of equivalent scenario candidates
// within one JobSolver.SolveWithResult call, both within a single generator and
// across generators. Only scenarios that were simulated and failed are
// recorded: a solved scenario must remain re-emittable because the final probe
// re-runs the generator to rebuild the winning statement after search probes
// discarded theirs. Skipping repeated failures is sound because simulation is
// deterministic for identical fingerprint inputs, with one known pre-existing
// exception: byPodSolver.solve skips feasibleNodesRollback on
// validator-rejected and error paths, so the probe's feasible-node map can grow
// mid-probe and a previously failed scenario could in theory succeed later.
// That is at worst a missed solution, which the bounded scenario search already
// accepts by design.
type scenarioDedupCache struct {
	seen map[scenarioFingerprint]struct{}
}

func newScenarioDedupCache() *scenarioDedupCache {
	return &scenarioDedupCache{seen: map[scenarioFingerprint]struct{}{}}
}

func (c *scenarioDedupCache) isDuplicate(fingerprint scenarioFingerprint) bool {
	if c == nil {
		return false
	}
	_, found := c.seen[fingerprint]
	return found
}

func (c *scenarioDedupCache) recordFailed(fingerprint scenarioFingerprint) {
	if c == nil {
		return
	}
	c.seen[fingerprint] = struct{}{}
}
