// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"crypto/sha256"
	"hash"
	"io"
	"slices"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

type scenarioFingerprint [sha256.Size]byte

const (
	fingerprintSectionSeparator = "\x1f"
	fingerprintElementSeparator = "\x00"
)

// fingerprintScenario returns a canonical, order-independent identity for the
// simulation input of a ByNodeScenario. Two scenarios with the same fingerprint
// produce the same simulation outcome within a single JobSolver.SolveWithResult
// call: the fingerprint covers the pending task set (which differs between
// probes at different k), the recorded victims (which also determine the
// probe's feasible-node additions), and the potential victim tasks. Task UIDs
// stand in for node placements: within one session a task's placement is
// fixed, so the victim UIDs determine which nodes the evictions free. A cache
// that outlives a session, or scenarios that carry hypothetical placements,
// must add node assignments to the key. The remaining simulation inputs
// (feasible nodes, plugin configuration) are constant across one job solve, as
// is the preemptor UID, which is included only as insurance against future
// cache-scope widening. Generators must embed the solve context's recorded
// victims into emitted scenarios for the recorded section to be meaningful;
// all in-tree generators do.
func fingerprintScenario(sn *scenario.ByNodeScenario) scenarioFingerprint {
	digest := sha256.New()

	if preemptor := sn.GetPreemptor(); preemptor != nil {
		writeString(digest, string(preemptor.UID))
	}
	for _, tasks := range [][]*pod_info.PodInfo{
		sn.PendingTasks(),
		sn.RecordedVictimsTasks(),
		sn.PotentialVictimsTasks(),
	} {
		writeString(digest, fingerprintSectionSeparator)
		writeTaskUIDs(digest, tasks)
	}

	var fingerprint scenarioFingerprint
	digest.Sum(fingerprint[:0])
	return fingerprint
}

func writeTaskUIDs(digest hash.Hash, tasks []*pod_info.PodInfo) {
	uids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		uids = append(uids, string(task.UID))
	}
	slices.Sort(uids)
	writeString(digest, strings.Join(uids, fingerprintElementSeparator))
}

func writeString(digest hash.Hash, value string) {
	// hash.Hash writes never return an error.
	_, _ = io.WriteString(digest, value)
}

// scenarioDedupCache skips re-simulation of equivalent scenario candidates
// within one JobSolver.SolveWithResult call, both within a single generator and
// across generators. Only scenarios that were simulated and failed are
// recorded: a solved scenario must remain re-emittable because the final probe
// re-runs the generator to rebuild the winning statement after search probes
// discarded theirs. Skipping repeated failures is sound because a simulation's
// outcome is determined by the fingerprint inputs: session state is restored
// after every failed simulation, and the probe's feasible-node set stays
// derived from the solver's constant node set plus the recorded victims.
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
