// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func scenarioCheckpointKey(ctx *SolveContext) framework.ScenarioCheckpointKey {
	if ctx == nil || ctx.PartialPendingJob == nil {
		return framework.ScenarioCheckpointKey{}
	}
	return framework.ScenarioCheckpointKey{
		Action: ctx.ActionType,
		JobUID: ctx.PartialPendingJob.UID,
	}
}

func loadScenarioCheckpoint(
	ctx *SolveContext, generatorName string,
) *framework.ScenarioCheckpoint {
	if ctx == nil || ctx.ActionType != framework.Reclaim || ctx.Session == nil || ctx.Session.ScenarioCheckpointStore == nil {
		return nil
	}
	checkpoint, found := ctx.Session.ScenarioCheckpointStore.Load(scenarioCheckpointKey(ctx))
	if !found || checkpoint.GeneratorName != generatorName || checkpoint.ProbeK != ctx.ProbeK {
		return nil
	}
	if checkpoint.InputFingerprint != fingerprintScenarioCheckpointInput(ctx) {
		ctx.Session.ScenarioCheckpointStore.Delete(scenarioCheckpointKey(ctx))
		return nil
	}
	return &checkpoint
}

func checkpointSkipsGenerator(ctx *SolveContext, generatorName string) bool {
	if ctx == nil || ctx.ActionType != framework.Reclaim || ctx.Session == nil || ctx.Session.ScenarioCheckpointStore == nil {
		return false
	}
	checkpoint, found := ctx.Session.ScenarioCheckpointStore.Load(scenarioCheckpointKey(ctx))
	if !found || checkpoint.ProbeK != ctx.ProbeK || checkpoint.GeneratorName == generatorName {
		return false
	}
	if checkpoint.InputFingerprint != fingerprintScenarioCheckpointInput(ctx) {
		ctx.Session.ScenarioCheckpointStore.Delete(scenarioCheckpointKey(ctx))
		return false
	}
	checkpointIndex, generatorIndex := -1, -1
	for index, registration := range ctx.Session.ScenarioGeneratorRegistrations {
		if registration.Name == checkpoint.GeneratorName {
			checkpointIndex = index
		}
		if registration.Name == generatorName {
			generatorIndex = index
		}
	}
	return generatorIndex >= 0 && checkpointIndex > generatorIndex
}

func saveScenarioCheckpoint(
	ctx *SolveContext, generatorName string, cursor scenarioFingerprint, stopReason SearchResultReason,
) {
	if ctx == nil || ctx.ActionType != framework.Reclaim || ctx.Session == nil || ctx.Session.ScenarioCheckpointStore == nil {
		return
	}
	ctx.Session.ScenarioCheckpointStore.Save(scenarioCheckpointKey(ctx), framework.ScenarioCheckpoint{
		InputFingerprint: fingerprintScenarioCheckpointInput(ctx),
		Cursor:           [sha256.Size]byte(cursor),
		GeneratorName:    generatorName,
		ProbeK:           ctx.ProbeK,
		StopReason:       string(stopReason),
	})
}

func deleteScenarioCheckpoint(ctx *SolveContext) {
	if ctx == nil || ctx.ActionType != framework.Reclaim || ctx.Session == nil || ctx.Session.ScenarioCheckpointStore == nil {
		return
	}
	ctx.Session.ScenarioCheckpointStore.Delete(scenarioCheckpointKey(ctx))
}

func deleteScenarioCheckpointForJob(
	ssn *framework.Session, action framework.ActionType, job *podgroup_info.PodGroupInfo,
) {
	if action != framework.Reclaim || ssn == nil || ssn.ScenarioCheckpointStore == nil || job == nil {
		return
	}
	ssn.ScenarioCheckpointStore.Delete(framework.ScenarioCheckpointKey{Action: action, JobUID: job.UID})
}

func fingerprintScenarioCheckpointInput(ctx *SolveContext) [sha256.Size]byte {
	digest := sha256.New()
	if ctx == nil || ctx.Session == nil {
		var fingerprint [sha256.Size]byte
		digest.Sum(fingerprint[:0])
		return fingerprint
	}

	writeCheckpointString(digest, string(ctx.ActionType))
	writeCheckpointString(digest, fmt.Sprint(ctx.ProbeK))
	writeCheckpointPodGroup(digest, ctx.PartialPendingJob)
	writeCheckpointTasks(digest, ctx.RecordedVictimsTasks)
	writeCheckpointFeasibleNodes(digest, ctx)
	writeCheckpointRegistrations(digest, ctx.Session)
	writeCheckpointConfiguration(digest, ctx.Session)
	writeCheckpointClusterState(digest, ctx.Session)

	var fingerprint [sha256.Size]byte
	digest.Sum(fingerprint[:0])
	return fingerprint
}

func writeCheckpointPodGroup(digest hash.Hash, job *podgroup_info.PodGroupInfo) {
	if job == nil {
		writeCheckpointString(digest, "")
		return
	}
	writeCheckpointString(digest, string(job.UID))
	writeCheckpointString(digest, string(job.Queue))
	writeCheckpointString(digest, fmt.Sprint(job.Priority))
	writeCheckpointString(digest, string(job.Preemptibility))
	writeCheckpointTasks(digest, podMapToSlice(job.GetAllPodsMap()))
}

func writeCheckpointTasks(digest hash.Hash, tasks []*pod_info.PodInfo) {
	tasks = append([]*pod_info.PodInfo(nil), tasks...)
	slices.SortFunc(tasks, func(a, b *pod_info.PodInfo) int {
		if a == nil || b == nil {
			if a == b {
				return 0
			}
			if a == nil {
				return -1
			}
			return 1
		}
		return stringCompare(string(a.UID), string(b.UID))
	})
	for _, task := range tasks {
		if task == nil {
			writeCheckpointString(digest, "")
			continue
		}
		writeCheckpointString(digest, string(task.UID))
		writeCheckpointString(digest, task.NodeName)
		writeCheckpointString(digest, fmt.Sprint(task.Status))
		writeCheckpointString(digest, fmt.Sprint(task.ResReqVector))
		writeCheckpointString(digest, fmt.Sprint(task.GpuRequirement))
	}
}

func writeCheckpointFeasibleNodes(digest hash.Hash, ctx *SolveContext) {
	names := make([]string, 0, len(ctx.FeasibleNodes))
	for name := range ctx.FeasibleNodes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		node := ctx.FeasibleNodes[name]
		writeCheckpointString(digest, name)
		if node == nil {
			continue
		}
		writeCheckpointString(digest, fmt.Sprint(node.IdleVector))
		writeCheckpointString(digest, fmt.Sprint(node.ReleasingVector))
		if node.Node != nil {
			writeCheckpointString(digest, node.Node.ResourceVersion)
		}
	}
}

func writeCheckpointRegistrations(digest hash.Hash, ssn *framework.Session) {
	for _, registration := range ssn.ScenarioGeneratorRegistrations {
		writeCheckpointString(digest, registration.Name)
	}
}

func writeCheckpointConfiguration(digest hash.Hash, ssn *framework.Session) {
	if ssn.Config == nil {
		return
	}
	for _, tier := range ssn.Config.Tiers {
		for _, plugin := range tier.Plugins {
			writeCheckpointString(digest, plugin.Name)
			writeCheckpointString(digest, fmt.Sprintf("%t%t%t%t%t%t%t", plugin.JobOrderDisabled,
				plugin.TaskOrderDisabled, plugin.PreemptableDisabled, plugin.ReclaimableDisabled,
				plugin.QueueOrderDisabled, plugin.PredicateDisabled, plugin.NodeOrderDisabled))
			keys := make([]string, 0, len(plugin.Arguments))
			for key := range plugin.Arguments {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				writeCheckpointString(digest, key)
				writeCheckpointString(digest, plugin.Arguments[key])
			}
		}
	}
	if ssn.Config.ScenarioSearchBudgets != nil {
		writeCheckpointString(digest, fmt.Sprint(ssn.Config.ScenarioSearchBudgets))
	}
}

func writeCheckpointClusterState(digest hash.Hash, ssn *framework.Session) {
	jobs := make([]*podgroup_info.PodGroupInfo, 0, len(ssn.ClusterInfo.PodGroupInfos))
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		jobs = append(jobs, job)
	}
	slices.SortFunc(jobs, func(a, b *podgroup_info.PodGroupInfo) int {
		return stringCompare(string(a.UID), string(b.UID))
	})
	for _, job := range jobs {
		writeCheckpointPodGroup(digest, job)
	}
}

func podMapToSlice(tasks pod_info.PodsMap) []*pod_info.PodInfo {
	result := make([]*pod_info.PodInfo, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, task)
	}
	return result
}

func writeCheckpointString(digest hash.Hash, value string) {
	_, _ = fmt.Fprintf(digest, "%d:", len(value))
	_, _ = io.WriteString(digest, value)
}

func stringCompare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
