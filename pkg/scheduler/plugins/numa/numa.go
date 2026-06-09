// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package numa implements NUMA-aware scheduling against the NodeResourceTopology CRD.
// It predicts the kubelet Topology Manager admission verdict for the policies that
// reject on topology grounds (single-numa-node and restricted) so that Guaranteed pods
// are placed only where the kubelet can NUMA-align their resources.
package numa

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// fitErrorMessage is the predicate rejection reason surfaced to the scheduler.
const fitErrorMessage = "node cannot NUMA-align the pod's resources under its Topology Manager policy"

const (
	pluginName    = "numa"
	ignoreListArg = "ignoreList"
)

type numaPlugin struct {
	// ignoreList holds resources reported per-zone but not aligned by the kubelet. Default empty.
	ignoreList sets.Set[v1.ResourceName]
}

// New builds a numa plugin instance. The only argument is the optional resource ignoreList.
func New(arguments framework.PluginArguments) framework.Plugin {
	ignoreList := parseIgnoreList(arguments)
	if ignoreList.Len() > 0 {
		log.InfraLogger.V(4).Infof("numa plugin: ignoring resources in ignoreList: %v", ignoreList)
	}

	return &numaPlugin{ignoreList: ignoreList}
}

func (pp *numaPlugin) Name() string {
	return pluginName
}

func (pp *numaPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddPredicateFn(pp.predicate)
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   func(event *framework.Event) { pp.allocate(ssn, event) },
		DeallocateFunc: func(event *framework.Event) { pp.deallocate(ssn, event) },
	})
}

func (pp *numaPlugin) predicate(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
	topo := node.NumaTopology
	if !pp.shouldHandle(task, topo) {
		return nil
	}

	if _, admit := evaluatorFor(topo.Policy).evaluate(topo, pp.ignoreList, requestUnits(task, topo.Scope)); !admit {
		log.InfraLogger.V(6).Infof("numa plugin: task <%s/%s> cannot be NUMA-aligned on node <%s>",
			task.Namespace, task.Name, node.Name)
		return common_info.NewFitError(task.Name, task.Namespace, node.Name, fitErrorMessage)
	}
	return nil
}

// allocate evaluates a task's expected NUMA placement and updates the node's numa topology resources.
func (pp *numaPlugin) allocate(ssn *framework.Session, event *framework.Event) {
	task := event.Task
	node := ssn.ClusterInfo.Nodes[task.NodeName]
	if node == nil {
		log.InfraLogger.Errorf("numa plugin: node <%s> not found in session", task.NodeName)
		return
	}

	if !pp.shouldHandle(task, node.NumaTopology) {
		return
	}

	topo := node.NumaTopology

	if len(task.NUMAPlacement) == 0 {
		placement, admit := evaluatorFor(topo.Policy).evaluate(topo, pp.ignoreList, requestUnits(task, topo.Scope))
		if !admit {
			log.InfraLogger.Errorf("numa plugin: task <%s/%s> cannot be NUMA-aligned on node <%s>",
				task.Namespace, task.Name, node.Name)
			return
		}
		task.NUMAPlacement = placement
	}

	numaAllocate(topo, task.NUMAPlacement)
}

// deallocate frees a task's NUMA placement, if it's known, from the node's numa topology resources.
func (pp *numaPlugin) deallocate(ssn *framework.Session, event *framework.Event) {
	task := event.Task
	if len(task.NUMAPlacement) == 0 {
		return
	}
	node := ssn.ClusterInfo.Nodes[task.NodeName]
	if node == nil {
		log.InfraLogger.Errorf("numa plugin: node <%s> not found in session", task.NodeName)
		return
	}

	if node.NumaTopology == nil {
		return
	}

	numaDeallocate(node.NumaTopology, task.NUMAPlacement)
}

func numaAllocate(topo *node_info.NumaTopology, placement pod_info.NUMAPlacement) {
	for _, zone := range placement {
		if zone.ZoneIndex < 0 || zone.ZoneIndex >= len(topo.Zones) {
			log.InfraLogger.Errorf("numa plugin: zone index <%d> out of range", zone.ZoneIndex)
			continue
		}
		subtract(topo.Zones[zone.ZoneIndex].Available, resourceAmounts(zone.Amount))
	}
}

func numaDeallocate(topo *node_info.NumaTopology, placement pod_info.NUMAPlacement) {
	for _, zone := range placement {
		if zone.ZoneIndex < 0 || zone.ZoneIndex >= len(topo.Zones) {
			log.InfraLogger.Errorf("numa plugin: zone index <%d> out of range", zone.ZoneIndex)
			continue
		}
		add(topo.Zones[zone.ZoneIndex].Available, resourceAmounts(zone.Amount))
	}
}

func (pp *numaPlugin) OnSessionClose(_ *framework.Session) {}

func parseIgnoreList(arguments framework.PluginArguments) sets.Set[v1.ResourceName] {
	ignoreList := sets.New[v1.ResourceName]()
	raw := arguments.GetString(ignoreListArg, "")
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ignoreList.Insert(v1.ResourceName(name))
	}
	return ignoreList
}

// shouldHandle engages the plugin for any Guaranteed task on a rejecting-policy node: the
// kubelet aligns every Guaranteed pod (fractional/MIG included, on cpu/memory). The request
// intersection in the evaluator decides which resources actually constrain each task.
func (pp *numaPlugin) shouldHandle(task *pod_info.PodInfo, topo *node_info.NumaTopology) bool {
	if topo == nil || !isModeledPolicy(topo.Policy) {
		return false
	}

	return task.Pod != nil && task.Pod.Status.QOSClass == v1.PodQOSGuaranteed
}

// isModeledPolicy reports whether the plugin engages for a node with this policy.
// Only single-numa-node and restricted are supported at this point.
func isModeledPolicy(policy node_info.TopologyManagerPolicy) bool {
	return policy == node_info.TopologyPolicySingleNUMANode || policy == node_info.TopologyPolicyRestricted
}
