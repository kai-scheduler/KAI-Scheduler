// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenario

import (
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

// VictimsTasksFromNodes returns the potential victim tasks, on any node, of the jobs that have a
// potential victim on nodeNames. Recorded victims are left out: the scenario already holds them.
func (bns *ByNodeScenario) VictimsTasksFromNodes(nodeNames []string) []*pod_info.PodInfo {
	victimsJobs := bns.potentialVictimsJobsFromNodes(nodeNames)

	var tasks []*pod_info.PodInfo
	for _, task := range bns.PotentialVictimsTasks() {
		if victimsJobs[task.Job] {
			tasks = append(tasks, task)
		}
	}

	return tasks
}

func (bns *ByNodeScenario) potentialVictimsJobsFromNodes(nodeNames []string) map[common_info.PodGroupID]bool {
	victimsJobs := map[common_info.PodGroupID]bool{}
	for _, node := range nodeNames {
		for _, jobID := range bns.potentialVictimsJobsByNode[node] {
			victimsJobs[jobID] = true
		}
	}

	return victimsJobs
}
