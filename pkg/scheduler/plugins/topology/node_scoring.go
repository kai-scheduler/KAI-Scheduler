// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"fmt"
	"math"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

func (t *topologyPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	taskSubGroupInfo, err := t.getTaskSubGroupInfo(task)
	if err != nil {
		return 0, fmt.Errorf("failed to get sub-group info for task %s/%s: %v", task.Namespace, task.Name, err)
	}

	relevantNodeScores := t.getRelevantNodeScores(taskSubGroupInfo)
	if relevantNodeScores == nil {
		// Sub-group or any of its ancestors has not defined any node scores
		return 0, nil
	}

	score, ok := relevantNodeScores[node.Name]
	if !ok {
		return 0, fmt.Errorf("node %s not found in relevant node scores for sub-group %s", node.Name, taskSubGroupInfo.GetName())
	}

	return score, nil
}

func calculateNodeScores(domain *DomainInfo, preferredLevel DomainLevel) map[string]float64 {
	orderedPreferredLevelDomains := getLevelDomains(domain, preferredLevel)

	nodeScores := make(map[string]float64)
	for i, leafDomain := range orderedPreferredLevelDomains {
		for _, node := range leafDomain.Nodes {
			// Score nodes by their domain's order
			score := (float64(i+1) / float64(len(orderedPreferredLevelDomains))) * 10
			// Round down the score to the nearest integer to prevent interference between plugins
			// (ensures topology scores remain higher than other lower-priority plugin scores)
			normalizedScore := math.Floor(score) * scores.Topology
			nodeScores[node.Name] = normalizedScore
		}
	}

	return nodeScores
}

func getLevelDomains(root *DomainInfo, level DomainLevel) []*DomainInfo {
	if root.Level == level {
		return []*DomainInfo{root}
	}
	if len(root.Children) == 0 {
		return []*DomainInfo{}
	}

	levelDomains := []*DomainInfo{}
	for _, childDomain := range root.Children {
		levelDomains = append(levelDomains, getLevelDomains(childDomain, level)...)
	}
	return levelDomains
}

func (t *topologyPlugin) getTaskSubGroupInfo(task *pod_info.PodInfo) (*subgroup_info.SubGroupInfo, error) {
	job := t.session.ClusterInfo.PodGroupInfos[task.Job]
	if job == nil {
		return nil, fmt.Errorf("job %s not found for task %s/%s", task.Job, task.Namespace, task.Name)
	}

	sgName := getTaskSubGroupName(task)
	sg, found := job.GetAllPodSets()[sgName]
	if !found {
		return nil, fmt.Errorf("sub-group %s not found in job %s", sgName, job.Name)
	}

	return &sg.SubGroupInfo, nil
}

func (t *topologyPlugin) getRelevantNodeScores(sgi *subgroup_info.SubGroupInfo) map[string]float64 {
	if scores, ok := t.subGroupNodeScores[sgi.GetName()]; ok {
		return scores
	}

	if sgi.GetParent() == nil {
		return nil
	}

	return t.getRelevantNodeScores(&sgi.GetParent().SubGroupInfo)
}

func getTaskSubGroupName(task *pod_info.PodInfo) string {
	sgName := podgroup_info.DefaultSubGroup
	if task.SubGroupName != "" {
		sgName = task.SubGroupName
	}
	return sgName
}
