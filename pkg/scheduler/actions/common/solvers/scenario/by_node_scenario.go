// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"fmt"
	"hash/fnv"
	"sort"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

var _ api.ScenarioInfo = &ByNodeScenario{}

type ByNodeScenario struct {
	*BaseScenario

	potentialVictimsJobsByNode map[string][]common_info.PodGroupID
}

func NewByNodeScenario(
	session *framework.Session, originalJob *podgroup_info.PodGroupInfo, pendingTasks []*pod_info.PodInfo,
	potentialVictimsTasks []*pod_info.PodInfo, recordedVictimsJobs []*podgroup_info.PodGroupInfo,
) *ByNodeScenario {

	simpleScenario := NewBaseScenario(session, originalJob, pendingTasks, potentialVictimsTasks, recordedVictimsJobs)

	bns := &ByNodeScenario{
		BaseScenario:               simpleScenario,
		potentialVictimsJobsByNode: map[string][]common_info.PodGroupID{},
	}

	for _, task := range potentialVictimsTasks {
		bns.addPotentialVictimTask(task)
	}

	return bns
}

func (bns *ByNodeScenario) addPotentialVictimTask(task *pod_info.PodInfo) {
	if !slices.Contains(bns.potentialVictimsJobsByNode[task.NodeName], task.Job) {
		bns.potentialVictimsJobsByNode[task.NodeName] = append(bns.potentialVictimsJobsByNode[task.NodeName], task.Job)
	}
}

func (bns *ByNodeScenario) AddPotentialVictimsTasks(tasks []*pod_info.PodInfo) {
	bns.BaseScenario.AddPotentialVictimsTasks(tasks)

	for _, task := range tasks {
		bns.addPotentialVictimTask(task)
	}
}

func (bns *ByNodeScenario) VictimsTasksFromNodes(nodeNames []string) []*pod_info.PodInfo {
	var tasks []*pod_info.PodInfo
	victimsJobs := bns.potentialVictimsJobsFromNodes(nodeNames)

	for _, jobID := range victimsJobs {
		for _, jobTaskGroup := range bns.victimsJobsTaskGroups[jobID] {
			tasks = append(tasks, maps.Values(jobTaskGroup.GetAllPodsMap())...)
		}
	}

	return tasks
}

// Fingerprint returns a deterministic, order-independent hash of this scenario's victim set.
// Two scenarios with the same preemptor, the same specific victim tasks (and therefore the same
// node assignment), and the same recorded victims produce the same fingerprint regardless of
// insertion order. Scoped to a single job/probe search — not reused across different preemptors.
func (bns *ByNodeScenario) Fingerprint() uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s|", bns.preemptor.UID)

	// Hash victim jobs with their specific task UIDs so scenarios that select different
	// tasks from the same job (i.e. different node assignments) get distinct fingerprints.
	type victimEntry struct {
		jobID    string
		taskUIDs []string
	}
	entries := make([]victimEntry, 0, len(bns.victims))
	for id, v := range bns.victims {
		taskUIDs := make([]string, 0, len(v.Tasks))
		for _, t := range v.Tasks {
			taskUIDs = append(taskUIDs, string(t.UID))
		}
		sort.Strings(taskUIDs)
		entries = append(entries, victimEntry{string(id), taskUIDs})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].jobID < entries[j].jobID })
	for _, e := range entries {
		fmt.Fprintf(h, "%s:", e.jobID)
		for _, uid := range e.taskUIDs {
			fmt.Fprintf(h, "%s,", uid)
		}
		fmt.Fprintf(h, "|")
	}

	recorded := make([]string, 0, len(bns.recordedVictimsTasks))
	for _, t := range bns.recordedVictimsTasks {
		recorded = append(recorded, string(t.UID))
	}
	sort.Strings(recorded)
	for _, uid := range recorded {
		fmt.Fprintf(h, "%s|", uid)
	}

	return h.Sum64()
}

func (bns *ByNodeScenario) potentialVictimsJobsFromNodes(nodeNames []string) []common_info.PodGroupID {
	victimsJobs := map[common_info.PodGroupID]bool{}
	for _, node := range nodeNames {
		for _, jobID := range bns.potentialVictimsJobsByNode[node] {
			victimsJobs[jobID] = true
		}
	}

	return maps.Keys(victimsJobs)
}
