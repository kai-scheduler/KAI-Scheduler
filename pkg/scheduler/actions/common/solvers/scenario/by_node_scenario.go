// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenario

import (
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
	session *framework.Session, originalJob, pendingTasksAsJob *podgroup_info.PodGroupInfo,
	potentialVictimsTasks []*pod_info.PodInfo, recordedVictimsJobs []*podgroup_info.PodGroupInfo,
) *ByNodeScenario {

	simpleScenario := NewBaseScenario(session, originalJob, pendingTasksAsJob, potentialVictimsTasks, recordedVictimsJobs)

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

func (bns *ByNodeScenario) potentialVictimsJobsFromNodes(nodeNames []string) []common_info.PodGroupID {
	victimsJobs := map[common_info.PodGroupID]bool{}
	for _, node := range nodeNames {
		for _, jobID := range bns.potentialVictimsJobsByNode[node] {
			victimsJobs[jobID] = true
		}
	}

	return maps.Keys(victimsJobs)
}
