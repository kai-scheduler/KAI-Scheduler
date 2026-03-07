// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"strconv"
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestNewTopologyAwareIdleGpusFilter_NoConstraints(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 8,
		"node-2": 8,
	}, nil)

	jobs := []*jobs_fake.TestJobBasic{{
		Name:                "pending-job",
		Namespace:           "default",
		QueueName:           "default",
		RequiredGPUsPerTask: 4,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
		RootSubGroupSet:     nil,
	}}
	jobsInfoMap, _, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
	pendingJob := jobsInfoMap["pending-job"]

	testScenario := scenario.NewByNodeScenario(nil, pendingJob, pendingJob, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)

	if filter != nil {
		t.Errorf("Expected nil filter for scenario without topology constraints, got %v", filter)
	}
}

func TestTopologyAwareIdleGpus_SingleDomainSufficientCapacity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "topologyLevel/rack"
	topologyLabelsPernodes := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
		"node-2": {requiredLevel: "rack-1"},
		"node-3": {requiredLevel: "rack-2"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 8,
		"node-2": 8,
		"node-3": 4,
	}, topologyLabelsPernodes)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup(podgroup_info.DefaultSubGroup, topology, requiredLevel, 2))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(4))},
	}, rootSubGroupSet, vectorMap)

	testScenario := scenario.NewByNodeScenario(nil, pendingJob, pendingJob, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)

	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, true, "rack-1 has 16 GPUs which is enough for 12 GPUs needed")
}

func TestTopologyAwareIdleGpus_NoDomainHasSufficientCapacity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	topologyLabelKey := topology + "/rack"
	topologyLabels := map[string]map[string]string{
		"node-1": {topologyLabelKey: "rack-1"},
		"node-2": {topologyLabelKey: "rack-2"},
		"node-3": {topologyLabelKey: "rack-3"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 4,
		"node-2": 5,
		"node-3": 6,
	}, topologyLabels)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup(podgroup_info.DefaultSubGroup, topology, topologyLabelKey, 2))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
	}, rootSubGroupSet, vectorMap)

	testScenario := scenario.NewByNodeScenario(nil, pendingJob, pendingJob, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)

	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, false, "no rack has 16 GPUs")
}

func TestTopologyAwareIdleGpus_MultiplePodSetsWithSameConstraint(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "topologyLevel/rack"
	topologyLabelsPerNodes := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
		"node-2": {requiredLevel: "rack-2"},
		"node-3": {requiredLevel: "rack-2"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 10,
		"node-2": 2,
		"node-3": 20,
	}, topologyLabelsPerNodes)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset1", topology, requiredLevel, 2))
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset2", topology, requiredLevel, 1))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "podset1", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
		{SubGroupName: "podset1", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
		{SubGroupName: "podset2", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
	}, rootSubGroupSet, vectorMap)

	testScenario := scenario.NewByNodeScenario(nil, pendingJob, pendingJob, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)

	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, true,
		"rack-1 for the second group and rack-2 for the first group have sufficient capacity")
}

func TestTopologyAwareIdleGpus_WithVictimTasks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "topologyLevel2/rack"
	topologyLabelsPerNodes := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
		"node-2": {requiredLevel: "rack-2"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 4,
		"node-2": 12,
	}, topologyLabelsPerNodes)

	victimPod := createRunningPodWithGpus("victim-1", "default", "node-2", 8)
	victimTask := pod_info.NewTaskInfo(victimPod, nil, vectorMap)
	nodes["node-2"].AddTask(victimTask)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup(podgroup_info.DefaultSubGroup, topology, requiredLevel, 1))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
	}, rootSubGroupSet, vectorMap)

	session := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"victim-1-job": podgroup_info.NewPodGroupInfo("victim-1-job"),
			},
		},
	}

	testScenario := scenario.NewByNodeScenario(
		session,
		pendingJob,
		pendingJob,
		[]*pod_info.PodInfo{victimTask},
		nil,
	)

	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, true, "rack-2 will have 12 GPUs after evicting victim")
}

// TestTopologyAwareIdleGpus_VictimNotDoubleCountedAcrossFilterCalls verifies that a victim's
// freed GPUs are counted exactly once even when the same victim appears in the PotentialVictimsTasks
// list of multiple consecutive Filter calls.
//
// Setup: rack-1 has 4 idle GPUs on node-1; victim-1 holds 4 more GPUs on that node.
// After eviction, rack-1 has 8 GPUs total — still short of the 10 GPUs the job needs.
// A second Filter call presenting the same victim must not re-add its 4 GPUs (which would
// artificially inflate rack-1 to 12 GPUs and incorrectly allow the scenario).
func TestTopologyAwareIdleGpus_VictimNotDoubleCountedAcrossFilterCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "rack"
	topologyLabelsPerNodes := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
	}
	// node-1: 8 allocatable GPUs, victim-1 uses 4 → 4 idle at construction time
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 8,
	}, topologyLabelsPerNodes)

	victimPod := createRunningPodWithGpus("victim-1", "default", "node-1", 4)
	victimTask := pod_info.NewTaskInfo(victimPod, nil, vectorMap)
	nodes["node-1"].AddTask(victimTask)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup(podgroup_info.DefaultSubGroup, topology, requiredLevel, 1))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
	}, rootSubGroupSet, vectorMap)

	session := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"victim-1-job": podgroup_info.NewPodGroupInfo("victim-1-job"),
			},
		},
	}

	// Both scenarios present the same victim-1.
	scenario1 := scenario.NewByNodeScenario(session, pendingJob, pendingJob, []*pod_info.PodInfo{victimTask}, nil)
	scenario2 := scenario.NewByNodeScenario(session, pendingJob, pendingJob, []*pod_info.PodInfo{victimTask}, nil)

	filter := NewTopologyAwareIdleGpusFilter(scenario1, nodes)
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}

	// First call: rack-1 = 4 (idle) + 4 (victim-1 freed) = 8 GPUs < 10 needed → invalid.
	runFilterCheck(t, filter, scenario1, false, "rack-1 has 8 GPUs (4 idle + 4 freed), less than 10 needed")

	// Second call with the same victim: rack-1 must remain 8 GPUs — victim-1's freed GPUs
	// must not be added again. Without a victim cache, rack-1 would reach 12 GPUs and the
	// filter would incorrectly allow the scenario.
	runFilterCheck(t, filter, scenario2, false, "victim-1's freed GPUs must not be double-counted on the second Filter call")
}

// TestTopologyAwareIdleGpus_RecordedVictimsCountedTowardCapacity verifies that freed GPUs from
// recorded (already-committed) victims are counted toward domain capacity, not just potential ones.
//
// Setup: rack-1 has 4 idle GPUs on node-1; victim-1 holds 8 more GPUs on that node.
// The pending job needs 10 GPUs.  Without the recorded victim the domain has only 4 GPUs
// (invalid).  Once victim-1 is passed as a recorded victim its 8 freed GPUs are added,
// giving rack-1 12 GPUs total, which satisfies the requirement (valid).
func TestTopologyAwareIdleGpus_RecordedVictimsCountedTowardCapacity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "rack"
	topologyLabelsPerNodes := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
	}
	// node-1: 12 allocatable GPUs; victim-1 uses 8 → 4 idle at construction time.
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 12,
	}, topologyLabelsPerNodes)

	victimPod := createRunningPodWithGpus("victim-1", "default", "node-1", 8)
	victimTask := pod_info.NewTaskInfo(victimPod, nil, vectorMap)
	nodes["node-1"].AddTask(victimTask)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup(podgroup_info.DefaultSubGroup, topology, requiredLevel, 1))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
	}, rootSubGroupSet, vectorMap)

	// Build a PodGroupInfo for the victim job that contains the running task so that
	// RecordedVictimsTasks() can return it.
	victimJob := podgroup_info.NewPodGroupInfo(common_info.PodGroupID("victim-1-job"))
	victimJob.AddTaskInfo(victimTask)

	session := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"victim-1-job": victimJob,
			},
		},
	}

	// Pass victim-1 as a recorded (committed) victim, not a potential one.
	testScenario := scenario.NewByNodeScenario(
		session, pendingJob, pendingJob, nil, []*podgroup_info.PodGroupInfo{victimJob},
	)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, true,
		"rack-1 has 12 GPUs (4 idle + 8 freed by recorded victim), enough for 10 needed")
}

func TestTopologyAwareIdleGpus_MultipleLevels(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	topologyLevels := []string{"topologyPrefix/zone", "topologyPrefix/rack"}
	topologyLabelsPerNodes := map[string]map[string]string{
		"node-1": {topologyLevels[0]: "zone-1", topologyLevels[1]: "rack-1"},
		"node-2": {topologyLevels[0]: "zone-1", topologyLevels[1]: "rack-2"},
		"node-3": {topologyLevels[0]: "zone-2", topologyLevels[1]: "rack-3"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 8,
		"node-2": 8,
		"node-3": 8,
	}, topologyLabelsPerNodes)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset1", topology, topologyLevels[1], 1))
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset2", topology, topologyLevels[0], 2))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "podset1", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(6))},
		{SubGroupName: "podset2", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
		{SubGroupName: "podset2", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
	}, rootSubGroupSet, vectorMap)

	testScenario := scenario.NewByNodeScenario(nil, pendingJob, pendingJob, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)

	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, true,
		"any rack can fit task-1 and zone-1 can fit task-2+task-3")
}

func TestTopologyDomainKey_Equality(t *testing.T) {
	key1 := TopologyDomainKey{topologyName: "topo", level: "rack", value: "rack-1"}
	key2 := TopologyDomainKey{topologyName: "topo", level: "rack", value: "rack-1"}
	key3 := TopologyDomainKey{topologyName: "topo", level: "rack", value: "rack-2"}

	// Test map usage
	m := make(map[TopologyDomainKey]int)
	m[key1] = 1
	m[key2] = 2

	if m[key1] != 2 {
		t.Errorf("Expected key1 and key2 to be equal, got different values")
	}
	if _, exists := m[key3]; exists {
		t.Errorf("Expected key3 to not exist in map")
	}
}

// TestTopologyAwareIdleGpus_FilterDomainMovesTwoStepsLeft verifies that when Filter is called
// with a scenario that includes a victim on the smallest-capacity rack, the domain's capacity
// increase triggers repositionDomainAfterIncrease and the domain moves 2 steps left. Initially
// racks are [rack-1: 10, rack-2: 8, rack-3: 6]. A victim on rack-3 frees 5 GPUs, so rack-3
// becomes 11 and must move to the front for correct greedy matching. The job needs 11 GPUs
// in one rack; only after the reorder does rack-3 satisfy that, so Filter returns true.
func TestTopologyAwareIdleGpus_FilterDomainMovesTwoStepsLeft(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "rack"
	topologyLabels := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
		"node-2": {requiredLevel: "rack-2"},
		"node-3": {requiredLevel: "rack-3"},
	}
	// rack-1: 10 idle, rack-2: 8 idle, rack-3: 11 total with victim using 5 → 6 idle.
	// Initial order [rack-1, rack-2, rack-3]. After victim applied, rack-3 = 11 → moves 2 steps left.
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 10,
		"node-2": 8,
		"node-3": 11,
	}, topologyLabels)

	victimPod := createRunningPodWithGpus("victim-1", "default", "node-3", 5)
	victimTask := pod_info.NewTaskInfo(victimPod, nil, vectorMap)
	nodes["node-3"].AddTask(victimTask)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup(podgroup_info.DefaultSubGroup, topology, requiredLevel, 1))
	pendingJob := buildJob("pending-job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "default", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(11))},
	}, rootSubGroupSet, vectorMap)

	session := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"victim-1-job": podgroup_info.NewPodGroupInfo("victim-1-job"),
			},
		},
	}

	testScenario := scenario.NewByNodeScenario(
		session, pendingJob, pendingJob, []*pod_info.PodInfo{victimTask}, nil,
	)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, true,
		"rack-3 gains 5 GPUs from victim (6+5=11), moves 2 steps left; job needs 11 GPUs in one rack")
}

func TestTopologyAwareIdleGpus_BinPackingPreventsOverAllocation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "rack"
	topologyLabels := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
		"node-2": {requiredLevel: "rack-2"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 15,
		"node-2": 15,
	}, topologyLabels)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset1", topology, requiredLevel, 1))
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset2", topology, requiredLevel, 1))
	job := buildJob("job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "podset1", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
		{SubGroupName: "podset2", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
	}, rootSubGroupSet, vectorMap)

	testScenario := scenario.NewByNodeScenario(nil, job, job, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, true,
		"two PodSets of 10 GPUs each should fit in two racks of 15 GPUs each")
}

func TestTopologyAwareIdleGpus_BinPackingRejectsInsufficientDomains(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "rack"
	topologyLabels := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 15,
	}, topologyLabels)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset1", topology, requiredLevel, 1))
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("podset2", topology, requiredLevel, 1))
	job := buildJob("job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "podset1", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
		{SubGroupName: "podset2", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(10))},
	}, rootSubGroupSet, vectorMap)

	testScenario := scenario.NewByNodeScenario(nil, job, job, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, false, "two PodSets of 10 GPUs each cannot both fit in a single rack of 15 GPUs")
}

// TestTopologyAwareIdleGpus_FragmentedCapacityRejected demonstrates that sorted greedy matching
// detects cases where each subgroup fits in some domain independently, but no joint assignment
// exists due to fragmentation.
//
// Setup: two racks with 10 and 9 GPUs (19 total), three subgroups needing 8, 8, and 3 GPUs
// (19 total).  Each subgroup individually fits in at least one rack (8≤10, 8≤10, 3≤10), so an
// independent per-subgroup check would return valid.  However, the two 8-GPU subgroups must each
// occupy one rack, leaving at most 2 GPUs in rack-1 and 1 GPU in rack-2 — neither can fit the
// 3-GPU subgroup.
func TestTopologyAwareIdleGpus_FragmentedCapacityRejected(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := "cluster-topology"
	requiredLevel := "rack"
	topologyLabels := map[string]map[string]string{
		"node-1": {requiredLevel: "rack-1"},
		"node-2": {requiredLevel: "rack-2"},
	}
	nodes, vectorMap := createTestNodes(ctrl, map[string]int{
		"node-1": 10,
		"node-2": 9,
	}, topologyLabels)

	rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("subA", topology, requiredLevel, 1))
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("subB", topology, requiredLevel, 1))
	rootSubGroupSet.AddSubGroup(newConstrainedSubGroup("subC", topology, requiredLevel, 1))
	job := buildJob("job", []*tasks_fake.TestTaskBasic{
		{SubGroupName: "subA", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
		{SubGroupName: "subB", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(8))},
		{SubGroupName: "subC", State: pod_status.Pending, RequiredGPUs: ptr.To(int64(3))},
	}, rootSubGroupSet, vectorMap)

	testScenario := scenario.NewByNodeScenario(nil, job, job, nil, nil)
	filter := NewTopologyAwareIdleGpusFilter(testScenario, nodes)
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}
	runFilterCheck(t, filter, testScenario, false,
		"total capacity (19) equals total need (19) but fragmentation prevents joint assignment")
}

// Helper functions for creating test data

// newConstrainedSubGroup creates a SubGroupSet with a topology constraint and a single PodSet.
func newConstrainedSubGroup(name, topology, requiredLevel string, podCount int32) *subgroup_info.SubGroupSet {
	constraint := &topology_info.TopologyConstraintInfo{
		Topology:      topology,
		RequiredLevel: requiredLevel,
	}
	subGroup := subgroup_info.NewSubGroupSet(name, constraint)
	subGroup.AddPodSet(subgroup_info.NewPodSet(name, podCount, nil))
	return subGroup
}

// buildJob creates a job with default namespace/queue and returns it from the jobs map.
func buildJob(name string, tasks []*tasks_fake.TestTaskBasic, rootSubGroupSet *subgroup_info.SubGroupSet, vectorMap *resource_info.ResourceVectorMap) *podgroup_info.PodGroupInfo {
	jobs := []*jobs_fake.TestJobBasic{{
		Name:            name,
		Namespace:       "default",
		QueueName:       "default",
		RootSubGroupSet: rootSubGroupSet,
		Tasks:           tasks,
	}}
	jobsInfoMap, _, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
	return jobsInfoMap[common_info.PodGroupID(name)]
}

// runFilterCheck runs the filter and asserts the expected validity.
func runFilterCheck(t *testing.T, filter *TopologyAwareIdleGpus, s *scenario.ByNodeScenario, wantValid bool, reason string) {
	t.Helper()
	valid, err := filter.Filter(s)
	if err != nil {
		t.Fatalf("Filter returned error: %v", err)
	}
	if valid != wantValid {
		t.Errorf("Expected valid=%v: %s", wantValid, reason)
	}
}

func createTestNodes(
	ctrl *gomock.Controller,
	nodeCapacities map[string]int,
	topologyLabels map[string]map[string]string,
) (map[string]*node_info.NodeInfo, *resource_info.ResourceVectorMap) {
	nodes := make(map[string]*node_info.NodeInfo)

	resourceLists := make([]v1.ResourceList, 0, len(nodeCapacities))
	for _, gpus := range nodeCapacities {
		resourceLists = append(resourceLists, v1.ResourceList{
			"nvidia.com/gpu": resource.MustParse(strconv.Itoa(gpus)),
		})
	}
	vectorMap := resource_info.BuildResourceVectorMap(resourceLists)

	for nodeName := range nodeCapacities {
		nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
		nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
		nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

		labels := make(map[string]string)
		if topologyLabels != nil && topologyLabels[nodeName] != nil {
			labels = topologyLabels[nodeName]
		}

		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName,
				Labels: labels,
			},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse(strconv.Itoa(nodeCapacities[nodeName])),
				},
			},
		}

		nodeInfo := node_info.NewNodeInfo(node, nodePodAffinityInfo, vectorMap)
		nodes[nodeName] = nodeInfo
	}

	return nodes, vectorMap
}

func createRunningPodWithGpus(name, namespace, nodeName string, gpus int) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  types.UID(name + "-uid"),
			Name: name, Namespace: namespace,
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: name + "-job",
			},
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			Containers: []v1.Container{{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse(strconv.Itoa(gpus)),
					},
				},
			}},
		},
		Status: v1.PodStatus{Phase: v1.PodRunning},
	}
}
