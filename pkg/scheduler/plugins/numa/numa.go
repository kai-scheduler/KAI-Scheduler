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
	pluginName  = "numa"
	denylistArg = "denylist"
)

type numaPlugin struct {
	// denylist holds resources reported per-zone but not aligned by the kubelet. Default empty.
	denylist sets.Set[v1.ResourceName]

	nodes map[string]*nodeTopology

	// reserved maps virtually allocated tasks' resources to the expected allocation they'll get on a node. This is used
	// to model in-cycle reservation, to be taken into account when evaluating the next tasks' allocation, and to model evictions.
	reserved map[common_info.PodID][]zoneReservation
}

// zoneReservation records the resources allocated to a task on one NUMA zone during the cycle.
// The zone is identified by its index in nodeTopology.zones.
type zoneReservation struct {
	zoneIndex int
	amount    resourceAmounts
}

// New builds a numa plugin instance. The only argument is the optional resource denylist.
func New(arguments framework.PluginArguments) framework.Plugin {
	denylist := parseDenylist(arguments)
	if denylist.Len() > 0 {
		log.InfraLogger.V(4).Infof("numa plugin: ignoring resources in denylist: %v", denylist)
	}

	return &numaPlugin{
		denylist: denylist,
		nodes:    map[string]*nodeTopology{},
		reserved: map[common_info.PodID][]zoneReservation{},
	}
}

func (pp *numaPlugin) Name() string {
	return pluginName
}

func (pp *numaPlugin) OnSessionOpen(ssn *framework.Session) {
	pp.nodes = map[string]*nodeTopology{}
	pp.reserved = map[common_info.PodID][]zoneReservation{}

	for name, node := range ssn.ClusterInfo.Nodes {
		nt := buildNodeTopology(node.NodeResourceTopology, pp.denylist)
		if nt == nil {
			continue
		}
		pp.nodes[name] = nt
	}

	log.InfraLogger.V(4).Infof("numa plugin: built topology for %d/%d nodes",
		len(pp.nodes), len(ssn.ClusterInfo.Nodes))

	ssn.AddPredicateFn(pp.predicate)
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   pp.allocate,
		DeallocateFunc: pp.deallocate,
	})
}

// predicate rejects a node when the kubelet's Topology Manager could not NUMA-align
// the task there. It is pure: it never mutates the node's working state.
func (pp *numaPlugin) predicate(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
	nt := pp.nodes[node.Name]
	if !pp.shouldHandle(task, nt) {
		return nil
	}

	if _, admit := evaluatorFor(nt.policy).evaluate(nt, requestUnits(task, nt.scope)); !admit {
		log.InfraLogger.V(6).Infof("numa plugin: task <%s/%s> cannot be NUMA-aligned on node <%s>",
			task.Namespace, task.Name, node.Name)
		return common_info.NewFitError(task.Name, task.Namespace, node.Name, fitErrorMessage)
	}
	return nil
}

// allocate charges the task's evaluated per-zone allocation against the node's
// in-cycle headroom, so the next task on the same node sees the reduced zones.
// Fires on commit and on preemption/reclaim redo via the session EventHandler.
func (pp *numaPlugin) allocate(event *framework.Event) {
	task := event.Task
	nt := pp.nodes[task.NodeName]
	if !pp.shouldHandle(task, nt) {
		return
	}

	allocation, admit := evaluatorFor(nt.policy).evaluate(nt, requestUnits(task, nt.scope))
	if !admit {
		return
	}

	reservations := make([]zoneReservation, 0, len(allocation))
	for zoneIndex, amount := range allocation {
		subtract(nt.zones[zoneIndex].available, amount)
		reservations = append(reservations, zoneReservation{zoneIndex: zoneIndex, amount: amount})
	}
	pp.reserved[task.UID] = reservations
}

// deallocate credits back the exact per-zone amounts recorded at allocate time,
// restoring headroom on rollback/eviction. The recorded amounts (not a re-derived
// split) are used because the restricted greedy split depends on headroom at
// allocate time, which changes before deallocate.
func (pp *numaPlugin) deallocate(event *framework.Event) {
	task := event.Task
	reservations, ok := pp.reserved[task.UID]
	if !ok {
		return
	}

	if nt := pp.nodes[task.NodeName]; nt != nil {
		for _, reservation := range reservations {
			add(nt.zones[reservation.zoneIndex].available, reservation.amount)
		}
	}
	delete(pp.reserved, task.UID)
}

func (pp *numaPlugin) OnSessionClose(_ *framework.Session) {
	pp.nodes = nil
	pp.reserved = nil
}

func parseDenylist(arguments framework.PluginArguments) sets.Set[v1.ResourceName] {
	denylist := sets.New[v1.ResourceName]()
	raw := arguments.GetString(denylistArg, "")
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		denylist.Insert(v1.ResourceName(name))
	}
	return denylist
}

// shouldHandle engages the plugin for any Guaranteed task on a rejecting-policy node: the
// kubelet aligns every Guaranteed pod (fractional/MIG included, on cpu/memory). The request
// intersection in the evaluator decides which resources actually constrain each task.
func (pp *numaPlugin) shouldHandle(task *pod_info.PodInfo, nt *nodeTopology) bool {
	if nt == nil || !nt.isModeledPolicy() {
		return false
	}

	return task.Pod != nil && task.Pod.Status.QOSClass == v1.PodQOSGuaranteed
}
