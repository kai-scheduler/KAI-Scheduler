// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	schedapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/numa"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func cpuZone(id, allocatable, available string) *node_info.NumaZone {
	return &node_info.NumaZone{
		ID:          id,
		Allocatable: map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse(allocatable)},
		Available:   map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse(available)},
	}
}

func singleTask(job *podgroup_info.PodGroupInfo) *pod_info.PodInfo {
	for _, task := range job.GetAllPodsMap() {
		return task
	}
	return nil
}

// setObservedPlacement stamps the pod's observed NUMA placement record, from which OnSessionOpen
// seeds the task's NUMAPlacement and reconstructs the zone's Available.
func setObservedPlacement(task *pod_info.PodInfo, zoneID, cpu string) {
	record := []schedulingv1alpha2.NUMAZonePlacement{{Zone: zoneID, Amount: v1.ResourceList{"cpu": resource.MustParse(cpu)}}}
	raw, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	if task.Pod.Annotations == nil {
		task.Pod.Annotations = map[string]string{}
	}
	task.Pod.Annotations[commonconstants.NumaPlacementObserved] = string(raw)
}

// TestReclaimRepipelineDoesNotGoNegative reproduces the preemption bookkeeping bug through the real
// statement flow (Evict → Pipeline → dedup/Unevict), so candidate fixes are judged against the
// actual code path rather than a hand-simulated one.
//
// One node, two NUMA zones (4 cpu allocatable each), single-numa-node. Two running victims use 3
// cpu each — one per zone — so the in-cycle availability is 1 cpu per zone, and each victim's
// observed placement is seeded. A preemptor needs 3 cpu; it fits neither zone (1 < 3), so reclaim
// evicts the victims to free a zone.
//
// The solver evicts BOTH victims (crediting both zones to 4), pipelines the preemptor onto the
// freed zone 0 (charging it to 1), then re-pipelines a victim. The re-pipelined victim still
// carries its stale zone-0 placement, and because the plugin evaluates lazily inside AllocateFunc
// (after the dedup), the numaPlacementChanged gate sees stale-vs-stale and lets the dedup fire:
// Unevict restores the zone-0 placement and re-charges zone 0, driving its available cpu negative.
func TestReclaimRepipelineDoesNotGoNegative(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()

	jobs := []*jobs_fake.TestJobBasic{
		{Name: "victim0", QueueName: "q", RequiredCPUsPerTask: 3,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0"}}},
		{Name: "victim1", QueueName: "q", RequiredCPUsPerTask: 3,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0"}}},
		{Name: "preemptor", QueueName: "q", RequiredCPUsPerTask: 3,
			Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}}},
	}
	jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
	nodesInfoMap := nodes_fake.BuildNodesInfoMap(
		map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 16000, CPUMemory: 16000}},
		tasksToNodeMap, nil, vectorMap)
	node := nodesInfoMap["node0"]

	node.NumaTopology = &node_info.NumaTopology{
		Policy:    node_info.TopologyPolicySingleNUMANode,
		Scope:     node_info.TopologyScopePod,
		Zones:     []*node_info.NumaZone{cpuZone("node-0", "4", "1"), cpuZone("node-1", "4", "1")},
		Resources: sets.New[v1.ResourceName]("cpu"),
	}

	victim0 := singleTask(jobsInfoMap["victim0"])
	victim1 := singleTask(jobsInfoMap["victim1"])
	preemptor := singleTask(jobsInfoMap["preemptor"])

	// The plugin handles Guaranteed pods; the fakes build Burstable pods, so mark QoS explicitly.
	for _, task := range []*pod_info.PodInfo{victim0, victim1, preemptor} {
		task.Pod.Status.QOSClass = v1.PodQOSGuaranteed
	}
	// Victims carry observed placement records: OnSessionOpen seeds each task's NUMAPlacement from
	// them and reconstructs zone Available (4 allocatable - 3 observed = 1 per zone).
	setObservedPlacement(victim0, "node-0", "3")
	setObservedPlacement(victim1, "node-1", "3")

	ssn := &framework.Session{
		ClusterInfo: &schedapi.ClusterInfo{PodGroupInfos: jobsInfoMap, Nodes: nodesInfoMap},
	}
	numa.New(framework.PluginArguments{}).OnSessionOpen(ssn) // registers the charge/credit EventHandler

	zone := func(i int) int64 { q := node.NumaTopology.Zones[i].Available["cpu"]; return q.Value() }
	stmt := ssn.Statement()

	// pipeline mirrors allocateTaskToNode: the allocation path stamps the task's NUMA placement for
	// the chosen node (via the NumaPlacementFn) before the statement op, then pipelines.
	pipeline := func(task *pod_info.PodInfo) error {
		task.NUMAPlacement = ssn.GetNumaPlacement(task, node)
		return stmt.Pipeline(task, "node0", false)
	}

	// Evict both victims → credits both zones back to full.
	require.NoError(t, stmt.Evict(victim0, "reclaim", eviction_info.EvictionMetadata{}))
	require.NoError(t, stmt.Evict(victim1, "reclaim", eviction_info.EvictionMetadata{}))
	assert.Equal(t, int64(4), zone(0), "evicting the victims frees zone 0")

	// Pipeline the preemptor → fresh evaluate picks the freed zone 0 and charges it.
	require.NoError(t, pipeline(preemptor))
	assert.Equal(t, int64(1), zone(0), "preemptor placed on zone 0")

	// Re-pipeline a victim (the solver re-allocates evicted victims to see who can stay). Its old
	// placement was zone 0, but zone 0 is now the preemptor's; the fresh placement re-homes it to
	// zone 1, so the dedup does not fire and zone 0 is not re-charged.
	require.NoError(t, pipeline(victim0))

	assert.GreaterOrEqual(t, zone(0), int64(0),
		"node NUMA cpu went negative — re-pipelined victim re-charged its stale zone-0 placement")
	assert.Equal(t, []int{1}, victim0.NUMAPlacement.ZoneIndices(), "victim re-homed to the free zone 1")
	assert.Equal(t, int64(1), zone(1), "victim charged on zone 1")
	assert.Equal(t, int64(1), zone(0), "zone 0 unchanged (preemptor only)")
}
