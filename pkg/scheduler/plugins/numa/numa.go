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
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

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

// zoneReservation records the amount a task was charged on one NUMA zone, in-cycle. Zone is identified by its
// index in nodeTopology.zones.
type zoneReservation struct {
	zoneIndex int
	amount    map[v1.ResourceName]resource.Quantity
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
