// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package nodeassignment lets admins assign nodes to leaf queues with a node label.
// Pods prefer nodes owned by their queue, then unassigned nodes, then nodes owned by other queues.
package nodeassignment

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

const (
	Name            = "nodeassignment"
	DefaultLabelKey = "kai.scheduler/assigned-queue"
	labelKeyArg     = "labelKey"

	// unknownQueue owns nodes whose label is invalid, so no pod is native to them.
	unknownQueue common_info.QueueID = ""
)

type nodeAssignmentPlugin struct {
	labelKey string
	ssn      *framework.Session
	// owners maps an assigned node's name to its owning queue. Unassigned nodes are absent.
	owners map[string]common_info.QueueID
}

func New(arguments framework.PluginArguments) framework.Plugin {
	labelKey := DefaultLabelKey
	if value, found := arguments[labelKeyArg]; found && value != "" {
		labelKey = value
	}
	return &nodeAssignmentPlugin{labelKey: labelKey}
}

func (p *nodeAssignmentPlugin) Name() string {
	return Name
}

func (p *nodeAssignmentPlugin) OnSessionOpen(ssn *framework.Session) {
	p.ssn = ssn
	p.owners = p.buildOwners(ssn)
	ssn.AddNodeOrderFn(p.nodeOrderFn)
}

func (p *nodeAssignmentPlugin) OnSessionClose(_ *framework.Session) {
	p.ssn = nil
	p.owners = nil
}

func (p *nodeAssignmentPlugin) buildOwners(ssn *framework.Session) map[string]common_info.QueueID {
	owners := map[string]common_info.QueueID{}
	var invalid []string
	for name, node := range ssn.ClusterInfo.Nodes {
		if node.Node == nil {
			continue
		}
		value, found := node.Node.Labels[p.labelKey]
		if !found {
			continue
		}
		queueID := common_info.QueueID(value)
		if queue, found := ssn.ClusterInfo.Queues[queueID]; !found || !queue.IsLeafQueue() {
			owners[name] = unknownQueue
			invalid = append(invalid, fmt.Sprintf("%s=%s", name, value))
			continue
		}
		owners[name] = queueID
	}

	if len(invalid) > 0 {
		slices.Sort(invalid)
		log.InfraLogger.V(2).Warnf(
			"%d nodes are assigned to a queue that is not a leaf queue in this shard; no queue is preferred on them: %s",
			len(invalid), strings.Join(invalid, ", "))
	}
	return owners
}

func (p *nodeAssignmentPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	score := p.score(task, node)
	log.InfraLogger.V(7).Do(func() {
		log.InfraLogger.Infof("Estimating Task: <%v/%v> Job: <%v> for node: <%s>. Score: %f",
			task.Namespace, task.Name, task.Job, node.Name, score)
	})
	return score, nil
}

func (p *nodeAssignmentPlugin) score(task *pod_info.PodInfo, node *node_info.NodeInfo) float64 {
	owner, assigned := p.owners[node.Name]
	if !assigned {
		return scores.NodeAssignment
	}
	job, found := p.ssn.ClusterInfo.PodGroupInfos[task.Job]
	if found && owner != unknownQueue && owner == job.Queue {
		return 2 * scores.NodeAssignment
	}
	return 0
}
