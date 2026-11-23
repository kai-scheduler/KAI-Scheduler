// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package nodeassignment lets admins assign nodes to leaf queues with a node label.
// Pods prefer nodes owned by their queue, then unassigned nodes, then nodes owned by other queues.
// Non-preemptible pods of other queues are not allowed on owned nodes.
package nodeassignment

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

const (
	Name            = "nodeassignment"
	DefaultLabelKey = "kai.scheduler/assigned-queue"
	labelKeyArg     = "labelKey"

	foreignNonPreemptibleReason = "node(s) are assigned to another queue; non-preemptible pods of other queues are not allowed"

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
	ssn.AddPredicateFn(p.predicateFn)
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
	if job, found := p.ssn.ClusterInfo.PodGroupInfos[task.Job]; found && isNative(owner, job) {
		return 2 * scores.NodeAssignment
	}
	return 0
}

// predicateFn keeps owned nodes free of pods that reclaim can't evict. Semi-preemptible jobs count as
// non-preemptible, since their protected core can change.
func (p *nodeAssignmentPlugin) predicateFn(
	task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo,
) error {
	owner, assigned := p.owners[node.Name]
	if !assigned || job == nil || job.IsPreemptibleJob() || isNative(owner, job) {
		return nil
	}
	return common_info.NewFitErrorWithDetailedMessage(task.Name, task.Namespace, node.Name,
		[]string{foreignNonPreemptibleReason},
		fmt.Sprintf("node is assigned to queue %s; non-preemptible pods of other queues are not allowed",
			node.Node.Labels[p.labelKey]))
}

func isNative(owner common_info.QueueID, job *podgroup_info.PodGroupInfo) bool {
	return owner != unknownQueue && owner == job.Queue
}
