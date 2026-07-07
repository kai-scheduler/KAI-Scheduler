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
	_, _ = io.WriteString(digest, value)
}
