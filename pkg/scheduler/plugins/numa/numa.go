// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"errors"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

var errNotNumaAligned = errors.New("node cannot NUMA-align the pod's resources under its Topology Manager policy")

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

	// ssn is the current session, set in OnSessionOpen and cleared in OnSessionClose, so the deferred
	// callbacks (prePredicate, allocate, deallocate) reach the cluster snapshot without capturing it.
	ssn *framework.Session

	// numaRequestCache caches each task's NUMA request vectors, keyed by pod ID. Rebuilt each session.
	numaRequestCache map[common_info.PodID]*podNumaRequests
	// ignoreIndices is ignoreList projected to shared-map indices (empty in the common case).
	ignoreIndices sets.Set[int]
	// effectiveAwareByNode maps a node name to its aware indices minus ignoreIndices; populated only
	// when ignoreIndices is non-empty (otherwise the topology's AwareIndices are used directly).
	effectiveAwareByNode map[string][]int
	// hasModeledNodes is false when no node carries a modeled-policy topology, letting the
	// PrePredicateFn skip all per-task precompute.
	hasModeledNodes bool
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
	pp.ssn = ssn
	pp.seedPlacements(ssn)
	if pp.reconstructAvailable {
		pp.reconstructNodeAvailable(ssn)
	}
	pp.initCaches(ssn)

	ssn.AddPrePredicateFn(pp.prePredicate)
	ssn.AddPredicateFn(pp.predicate)
	ssn.AddNumaPlacementFn(pp.placement)
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   pp.allocate,
		DeallocateFunc: pp.deallocate,
	})
}

// initCaches (re)builds the per-session predicate fast-path state (see evaluator.go): the per-task
// memo, the ignore indices, the per-node effective-aware indices, and hasModeledNodes.
func (pp *numaPlugin) initCaches(ssn *framework.Session) {
	pp.numaRequestCache = map[common_info.PodID]*podNumaRequests{}
	pp.ignoreIndices = sets.New[int]()
	pp.effectiveAwareByNode = nil
	pp.hasModeledNodes = false

	vectorMap := ssn.ClusterInfo.ResourceVectorMap
	for name := range pp.ignoreList {
		if idx := vectorMap.GetIndex(name); idx >= 0 {
			pp.ignoreIndices.Insert(idx)
		}
	}

	if pp.ignoreIndices.Len() > 0 {
		pp.effectiveAwareByNode = map[string][]int{}
	}
	for _, node := range ssn.ClusterInfo.Nodes {
		topo := node.NumaTopology
		if topo == nil || !isModeledPolicy(topo.Policy) {
			continue
		}
		pp.hasModeledNodes = true
		if pp.effectiveAwareByNode != nil {
			pp.effectiveAwareByNode[node.Name] = filterAware(topo.AwareIndices, pp.ignoreIndices)
		}
	}
}

// filterAware returns the aware indices with the ignored ones removed.
func filterAware(aware []int, ignore sets.Set[int]) []int {
	out := make([]int, 0, len(aware))
	for _, idx := range aware {
		if !ignore.Has(idx) {
			out = append(out, idx)
		}
	}
	return out
}

// prePredicate is the PrePredicateFn: it computes a task's NUMA requests once, before FittingNode runs
// per node. Skipped when no modeled node exists or the task is not Guaranteed.
func (pp *numaPlugin) prePredicate(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo) error {
	vectorMap := pp.ssn.ClusterInfo.ResourceVectorMap
	if !pp.hasModeledNodes || vectorMap == nil || task.Pod == nil || task.Pod.Status.QOSClass != v1.PodQOSGuaranteed {
		return nil // predicate builds the requests lazily against the node's (shared) map
	}
	pp.numaRequestsFor(task, vectorMap)
	return nil
}

// placement is the session NumaPlacementFn: the task's expected NUMA placement on the node. Called
// after the predicate, so it should always admit — the error log guards the unexpected case.
func (pp *numaPlugin) placement(task *pod_info.PodInfo, node *node_info.NodeInfo) pod_info.NUMAPlacement {
	allocation, admit := pp.evaluate(task, node)
	if !admit {
		// FittingNode runs the predicate before the allocation path stamps the placement, so a
		// rejection at stamp time is unexpected (the ledger changed between filter and stamp).
		log.InfraLogger.Errorf("numa plugin: task <%s/%s> cannot be NUMA-aligned on node <%s>",
			task.Namespace, task.Name, node.Name)
	}
	return placementFromAllocation(allocation, node.NumaTopology)
}

func (pp *numaPlugin) predicate(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
	if !pp.allocatable(task, node) {
		// FittingNode already logs this failure at V(6); the shared sentinel keeps the reject path
		// allocation-free (see errNotNumaAligned).
		return errNotNumaAligned
	}
	return nil
}

// allocate charges the task's per-zone placement against the node's in-cycle ledger. The placement
// is decided before the statement op — stamped by the allocation path via the NumaPlacementFn, or
// restored from the snapshot on eviction undo — so this handler only charges; it never evaluates.
// An empty placement (non-NUMA pod, or unknown) is a no-op.
func (pp *numaPlugin) allocate(event *framework.Event) {
	task := event.Task
	node := pp.ssn.ClusterInfo.Nodes[task.NodeName]
	if node == nil || node.NumaTopology == nil {
		return
	}
	numaAllocate(node.NumaTopology, task.NUMAPlacement)
}

// deallocate frees a task's NUMA placement, if it's known, from the node's numa topology resources.
func (pp *numaPlugin) deallocate(event *framework.Event) {
	task := event.Task
	if len(task.NUMAPlacement) == 0 {
		return
	}
	node := pp.ssn.ClusterInfo.Nodes[task.NodeName]
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
		delta := resource_info.NewResourceVectorFromResourceList(zone.Amount, topo.VectorMap)
		topo.Zones[zone.ZoneIndex].Available.Sub(delta)
	}
}

func numaDeallocate(topo *node_info.NumaTopology, placement pod_info.NUMAPlacement) {
	for _, zone := range placement {
		if zone.ZoneIndex < 0 || zone.ZoneIndex >= len(topo.Zones) {
			log.InfraLogger.Errorf("numa plugin: zone index <%d> out of range", zone.ZoneIndex)
			continue
		}
		delta := resource_info.NewResourceVectorFromResourceList(zone.Amount, topo.VectorMap)
		topo.Zones[zone.ZoneIndex].Available.Add(delta)
	}
}

func (pp *numaPlugin) OnSessionClose(_ *framework.Session) {
	pp.ssn = nil
}

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

// shouldHandle engages the plugin on a rejecting-policy node for any task the kubelet would
// NUMA-align. Guaranteed tasks are always handled. A non-Guaranteed task is handled if it requests a
// topology-aware device.
func (pp *numaPlugin) shouldHandle(task *pod_info.PodInfo, topo *node_info.NumaTopology) bool {
	if topo == nil || !isModeledPolicy(topo.Policy) || task.Pod == nil {
		return false
	}
	if isGuaranteed(task) {
		return true
	}
	return pp.requestsAlignedDevice(task, topo)
}

// isGuaranteed reports whether the task's pod is Guaranteed QoS.
func isGuaranteed(task *pod_info.PodInfo) bool {
	return task.Pod != nil && task.Pod.Status.QOSClass == v1.PodQOSGuaranteed
}

// isQoSGatedResource reports whether a resource is NUMA-aligned by the kubelet only for Guaranteed
// pods (cpu via CPU Manager, memory/hugepages via Memory Manager).
func isQoSGatedResource(name v1.ResourceName) bool {
	return name == v1.ResourceCPU || name == v1.ResourceMemory ||
		strings.HasPrefix(string(name), string(v1.ResourceHugePagesPrefix))
}

// requestsAlignedDevice reports whether the task requests a topology-aware device resource the node
// tracks per zone (a non cpu/memory/hugepages aware resource, minus the ignoreList). The kubelet's
// device manager aligns these regardless of QoS, so a non-Guaranteed task that requests one must be
// evaluated. Reads the task's precomputed request vector by shared-map index.
func (pp *numaPlugin) requestsAlignedDevice(task *pod_info.PodInfo, topo *node_info.NumaTopology) bool {
	for _, idx := range topo.AwareIndices {
		name := topo.AwareNames[idx]
		if isQoSGatedResource(name) || pp.ignoreList.Has(name) {
			continue
		}
		if task.ResReqVector.Get(idx) > 0 {
			return true
		}
	}
	return false
}

// isModeledPolicy reports whether the plugin engages for a node with this policy.
// Only single-numa-node and restricted are supported at this point.
func isModeledPolicy(policy node_info.TopologyManagerPolicy) bool {
	return policy == node_info.TopologyPolicySingleNUMANode || policy == node_info.TopologyPolicyRestricted
}
