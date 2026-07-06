// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestFingerprintScenarioIsOrderIndependent(t *testing.T) {
	ssn, pendingJob, victimTasks := newDedupCacheTestSession(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)

	allAtOnce := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)

	oneByOneReversed := scenario.NewByNodeScenario(ssn, pendingJob, reversedTasks(pendingTasks), nil, nil)
	for index := len(victimTasks) - 1; index >= 0; index-- {
		oneByOneReversed.AddPotentialVictimsTasks([]*pod_info.PodInfo{victimTasks[index]})
	}

	require.Equal(t, fingerprintScenario(allAtOnce), fingerprintScenario(oneByOneReversed))
}

func TestFingerprintScenarioDistinguishesInputs(t *testing.T) {
	ssn, pendingJob, victimTasks := newDedupCacheTestSession(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)
	recordedJob, _ := addGeneratorTestJob(t, ssn, 1, 30, "team-recorded", "node-3")

	base := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)

	differentVictims := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks[:1], nil)
	differentPending := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks[:1], victimTasks, nil)
	differentRecorded := scenario.NewByNodeScenario(
		ssn, pendingJob, pendingTasks, victimTasks, []*podgroup_info.PodGroupInfo{recordedJob},
	)

	baseFingerprint := fingerprintScenario(base)
	require.NotEqual(t, baseFingerprint, fingerprintScenario(differentVictims))
	require.NotEqual(t, baseFingerprint, fingerprintScenario(differentPending))
	require.NotEqual(t, baseFingerprint, fingerprintScenario(differentRecorded))
}

func TestScenarioDedupCacheRecordsAndMatches(t *testing.T) {
	ssn, pendingJob, victimTasks := newDedupCacheTestSession(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)
	fingerprint := fingerprintScenario(scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil))

	cache := newScenarioDedupCache()
	require.False(t, cache.isDuplicate(fingerprint))
	cache.recordFailed(fingerprint)
	require.True(t, cache.isDuplicate(fingerprint))
}

func TestScenarioDedupCacheNilIsSafe(t *testing.T) {
	var cache *scenarioDedupCache

	require.NotPanics(t, func() {
		cache.recordFailed(scenarioFingerprint{})
	})
	require.False(t, cache.isDuplicate(scenarioFingerprint{}))
}

func newDedupCacheTestSession(t *testing.T) (*framework.Session, *podgroup_info.PodGroupInfo, []*pod_info.PodInfo) {
	t.Helper()

	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 1, "node-3": 1})
	pendingJob := addGeneratorTestPendingJob(t, ssn, 2, 10, "team-pending")
	setGeneratorTestMinAvailable(pendingJob, 2)
	_, victimTasks := addGeneratorTestJob(t, ssn, 2, 20, "team-victim", "node-1", "node-2")
	return ssn, pendingJob, victimTasks
}

func dedupCacheTestPendingTasks(ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	return podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
}

func reversedTasks(tasks []*pod_info.PodInfo) []*pod_info.PodInfo {
	reversed := make([]*pod_info.PodInfo, 0, len(tasks))
	for index := len(tasks) - 1; index >= 0; index-- {
		reversed = append(reversed, tasks[index])
	}
	return reversed
}
