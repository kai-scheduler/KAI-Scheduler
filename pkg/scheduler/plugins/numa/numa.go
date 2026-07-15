// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

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
	pluginName              = "numa"
	ignoreListArg           = "ignoreList"
	reconstructAvailableArg = "reconstructAvailable"
)

type numaPlugin struct {
	// ignoreList holds resources reported per-zone but not aligned by the kubelet. Default empty.
	ignoreList sets.Set[v1.ResourceName]
	// reconstructAvailable, when set, ignores the NRT-reported per-zone Available and recomputes it
	// as Allocatable minus the placements of the pods consuming the node (see reconstructNodeAvailable).
	// Defaults true: NRT Available lags across cycles, so reconstruction from the fresh snapshot is the
	// safer default. Set false to trust NRT Available (e.g. when the placement exporter is absent and
	// predicted-only reconstruction is not wanted).
	reconstructAvailable bool
}

func New(arguments framework.PluginArguments) framework.Plugin {
	ignoreList := parseIgnoreList(arguments)
	if ignoreList.Len() > 0 {
		log.InfraLogger.V(4).Infof("numa plugin: ignoring resources in ignoreList: %v", ignoreList)
	}

	reconstructAvailable, err := arguments.GetBool(reconstructAvailableArg, true)
	if err != nil {
		log.InfraLogger.Warningf("numa plugin: invalid %s argument, defaulting to true: %v", reconstructAvailableArg, err)
	}
	if reconstructAvailable {
		log.InfraLogger.V(4).Infof("numa plugin: reconstructing per-zone Available from pod placements (NRT Available ignored)")
	}

	return &numaPlugin{ignoreList: ignoreList, reconstructAvailable: reconstructAvailable}
}

func (pp *numaPlugin) Name() string {
	return pluginName
}

func (pp *numaPlugin) OnSessionOpen(ssn *framework.Session) {
	pp.seedPlacements(ssn)
	if pp.reconstructAvailable {
		pp.reconstructNodeAvailable(ssn)
	}

	ssn.AddPredicateFn(pp.predicate)
	ssn.AddNumaPlacementFn(pp.placement)
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   func(event *framework.Event) { pp.allocate(ssn, event) },
		DeallocateFunc: func(event *framework.Event) { pp.deallocate(ssn, event) },
	})
}

// evaluate is the shared core of predicate and placement: it returns the task's expected NUMA
// placement on the node and whether the kubelet's Topology Manager would admit it. A task the
// plugin does not constrain (wrong policy/QoS, or no NRT) passes through as (nil, true).
func (pp *numaPlugin) evaluate(task *pod_info.PodInfo, node *node_info.NodeInfo) (pod_info.NUMAPlacement, bool) {
	if node == nil || !pp.shouldHandle(task, node.NumaTopology) {
		return nil, true
	}
	topo := node.NumaTopology
	eval := evaluatorFor(topo.Policy)
	concurrent, serial := requestUnits(task, topo.Scope)
	placement, admit := eval.evaluate(topo, pp.ignoreList, concurrent)
	if !admit {
		return nil, false
	}
	// Ordinary init containers run serially before the app containers and free their resources
	// first, so each must be alignable on its own but is not accumulated into the placement.
	for _, unit := range serial {
		if _, ok := eval.evaluate(topo, pp.ignoreList, []v1.ResourceList{unit}); !ok {
			return nil, false
		}
	}
	return placement, true
}

// placement is the session NumaPlacementFn: the task's expected NUMA placement on the node. It's called
// after the predicate, so it's expected to always return a placement - the error log is for safety.
func (pp *numaPlugin) placement(task *pod_info.PodInfo, node *node_info.NodeInfo) pod_info.NUMAPlacement {
	placement, admit := pp.evaluate(task, node)
	if !admit {
		// FittingNode runs the predicate before the allocation path stamps the placement, so a
		// rejection at stamp time is unexpected (the ledger changed between filter and stamp).
		log.InfraLogger.Errorf("numa plugin: task <%s/%s> cannot be NUMA-aligned on node <%s>",
			task.Namespace, task.Name, node.Name)
	}
	return placement
}

func (pp *numaPlugin) predicate(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
	if _, admit := pp.evaluate(task, node); !admit {
		log.InfraLogger.V(6).Infof("numa plugin: task <%s/%s> cannot be NUMA-aligned on node <%s>",
			task.Namespace, task.Name, node.Name)
		return common_info.NewFitError(task.Name, task.Namespace, node.Name, fitErrorMessage)
	}
	return nil
}

// allocate charges the task's per-zone placement against the node's in-cycle ledger. The placement
// is decided before the statement op — stamped by the allocation path via the NumaPlacementFn, or
// restored from the snapshot on eviction undo — so this handler only charges; it never evaluates.
// An empty placement (non-NUMA pod, or unknown) is a no-op.
func (pp *numaPlugin) allocate(ssn *framework.Session, event *framework.Event) {
	task := event.Task
	node := ssn.ClusterInfo.Nodes[task.NodeName]
	if node == nil || node.NumaTopology == nil {
		return
	}
	numaAllocate(node.NumaTopology, task.NUMAPlacement)
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
		subtract(topo.Zones[zone.ZoneIndex].Available, zone.Amount)
	}
}

func numaDeallocate(topo *node_info.NumaTopology, placement pod_info.NUMAPlacement) {
	for _, zone := range placement {
		if zone.ZoneIndex < 0 || zone.ZoneIndex >= len(topo.Zones) {
			log.InfraLogger.Errorf("numa plugin: zone index <%d> out of range", zone.ZoneIndex)
			continue
		}
		add(topo.Zones[zone.ZoneIndex].Available, zone.Amount)
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
