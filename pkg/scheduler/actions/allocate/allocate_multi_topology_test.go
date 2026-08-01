// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	. "go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleTopologyAllocation_MultiTopologiesAcrossSubgroups(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(buildMultiTopologySubgroupsTest(), controller)
	allocate.New().Execute(ssn)

	job, found := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending_job0")]
	if !found {
		t.Fatalf("pending_job0 was not found in session")
	}

	rackDomainCounts := map[string]int{}
	zoneDomainCounts := map[string]int{}
	rackNodes := map[string]bool{}
	zoneNodes := map[string]bool{}

	for _, task := range job.GetAllPodsMap() {
		if task.Status != pod_status.Binding {
			t.Fatalf("task %s has status %s, expected %s", task.Name, task.Status, pod_status.Binding)
		}

		node, found := ssn.ClusterInfo.Nodes[task.NodeName]
		if !found {
			t.Fatalf("task %s was allocated to unknown node %s", task.Name, task.NodeName)
		}

		switch task.SubGroupName {
		case "rack-workers":
			rackDomain := node.Node.Labels["topology.test/rack-domain"]
			rackDomainCounts[rackDomain]++
			rackNodes[node.Name] = true
		case "zone-workers":
			zoneDomain := node.Node.Labels["topology.test/zone-domain"]
			zoneDomainCounts[zoneDomain]++
			zoneNodes[node.Name] = true
		default:
			t.Fatalf("task %s belongs to unexpected subgroup %s", task.Name, task.SubGroupName)
		}
	}

	assertDomainCount(t, rackDomainCounts, "rack-a", 3, "rack-workers")
	assertDomainCount(t, zoneDomainCounts, "zone-x", 3, "zone-workers")
	assertNodeSet(t, rackNodes, []string{"node0", "node1"}, "rack-workers")
	assertNodeSet(t, zoneNodes, []string{"node2", "node4"}, "zone-workers")
}

func TestHandleTopologyAllocation_HierarchicalMultiTopologiesAcrossSubgroups(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(buildHierarchicalMultiTopologySubgroupsTest(), controller)
	allocate.New().Execute(ssn)

	job, found := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending_job1")]
	if !found {
		t.Fatalf("pending_job1 was not found in session")
	}

	childZoneDomains := map[string]map[string]int{
		"child-a": {},
		"child-b": {},
	}
	childRackDomains := map[string]map[string]int{
		"child-a": {},
		"child-b": {},
	}

	for _, task := range job.GetAllPodsMap() {
		if task.Status != pod_status.Binding {
			t.Fatalf("task %s has status %s, expected %s", task.Name, task.Status, pod_status.Binding)
		}

		node, found := ssn.ClusterInfo.Nodes[task.NodeName]
		if !found {
			t.Fatalf("task %s was allocated to unknown node %s", task.Name, task.NodeName)
		}

		childZones, found := childZoneDomains[task.SubGroupName]
		if !found {
			t.Fatalf("task %s belongs to unexpected subgroup %s", task.Name, task.SubGroupName)
		}
		childRacks := childRackDomains[task.SubGroupName]
		childZones[node.Node.Labels["topology.test/zone-domain"]]++
		childRacks[node.Node.Labels["topology.test/rack-domain"]]++
	}

	assertDomainCount(t, childZoneDomains["child-a"], "zone-x", 3, "child-a zone")
	assertDomainCount(t, childZoneDomains["child-b"], "zone-x", 3, "child-b zone")

	childARack := assertSingleDomainName(t, childRackDomains["child-a"], "child-a rack")
	childBRack := assertSingleDomainName(t, childRackDomains["child-b"], "child-b rack")
	if childARack == childBRack {
		t.Fatalf("child-a and child-b were allocated to the same rack domain: %s", childARack)
	}
	assertDomainSet(t, []string{childARack, childBRack}, []string{"rack-a", "rack-b"}, "child rack domains")
}

func buildMultiTopologySubgroupsTest() test_utils.TestTopologyBasic {
	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	minSubGroup := int32(2)
	root.SetMinSubGroup(&minSubGroup)
	root.AddPodSet(subgroup_info.NewPodSet("rack-workers", 3, &topology_info.TopologyConstraintInfo{
		Topology:      "rack-topology",
		RequiredLevel: "topology.test/rack-domain",
	}))
	root.AddPodSet(subgroup_info.NewPodSet("zone-workers", 3, &topology_info.TopologyConstraintInfo{
		Topology:      "zone-topology",
		RequiredLevel: "topology.test/zone-domain",
	}))

	return test_utils.TestTopologyBasic{
		Name: "Allocate one job with two subgroups using different topology CRs",
		Topologies: []*kaiv1alpha1.Topology{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "rack-topology"},
				Spec: kaiv1alpha1.TopologySpec{
					Levels: []kaiv1alpha1.TopologyLevel{
						{NodeLabel: "topology.test/rack-domain"},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-topology"},
				Spec: kaiv1alpha1.TopologySpec{
					Levels: []kaiv1alpha1.TopologyLevel{
						{NodeLabel: "topology.test/zone-domain"},
					},
				},
			},
		},
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "seed_rack_a",
				RequiredGPUsPerTask: 4,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:    pod_status.Running,
						NodeName: "node0",
					},
				},
			},
			{
				Name:                "seed_zone_x",
				RequiredGPUsPerTask: 4,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:    pod_status.Running,
						NodeName: "node2",
					},
				},
			},
			{
				Name:                "pending_job0",
				RequiredGPUsPerTask: 4,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				RootSubGroupSet:     root,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending, SubGroupName: "rack-workers"},
					{State: pod_status.Pending, SubGroupName: "rack-workers"},
					{State: pod_status.Pending, SubGroupName: "rack-workers"},
					{State: pod_status.Pending, SubGroupName: "zone-workers"},
					{State: pod_status.Pending, SubGroupName: "zone-workers"},
					{State: pod_status.Pending, SubGroupName: "zone-workers"},
				},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-a", "topology.test/zone-domain": "zone-x"}},
			"node1": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-a", "topology.test/zone-domain": "zone-y"}},
			"node2": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-b", "topology.test/zone-domain": "zone-x"}},
			"node3": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-b", "topology.test/zone-domain": "zone-y"}},
			"node4": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-c", "topology.test/zone-domain": "zone-x"}},
			"node5": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-c", "topology.test/zone-domain": "zone-y"}},
		},
		Queues: []test_utils.TestQueueBasic{
			{
				Name:               "queue0",
				ParentQueue:        "department-a",
				DeservedGPUs:       64,
				GPUOverQuotaWeight: 1,
				MaxAllowedGPUs:     64,
			},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{
				Name:         "department-a",
				DeservedGPUs: 64,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: 6,
			},
		},
	}
}

func buildHierarchicalMultiTopologySubgroupsTest() test_utils.TestTopologyBasic {
	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	parent := subgroup_info.NewSubGroupSet("parent", &topology_info.TopologyConstraintInfo{
		Topology:      "zone-topology",
		RequiredLevel: "topology.test/zone-domain",
	})
	parentMinSubGroup := int32(2)
	parent.SetMinSubGroup(&parentMinSubGroup)
	parent.AddPodSet(subgroup_info.NewPodSet("child-a", 3, &topology_info.TopologyConstraintInfo{
		Topology:      "rack-topology",
		RequiredLevel: "topology.test/rack-domain",
	}))
	parent.AddPodSet(subgroup_info.NewPodSet("child-b", 3, &topology_info.TopologyConstraintInfo{
		Topology:      "rack-topology",
		RequiredLevel: "topology.test/rack-domain",
	}))
	root.AddSubGroup(parent)

	return test_utils.TestTopologyBasic{
		Name: "Allocate one hierarchical job with parent and children using different topology CRs",
		Topologies: []*kaiv1alpha1.Topology{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "rack-topology"},
				Spec: kaiv1alpha1.TopologySpec{
					Levels: []kaiv1alpha1.TopologyLevel{
						{NodeLabel: "topology.test/rack-domain"},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-topology"},
				Spec: kaiv1alpha1.TopologySpec{
					Levels: []kaiv1alpha1.TopologyLevel{
						{NodeLabel: "topology.test/zone-domain"},
					},
				},
			},
		},
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "seed_parent_zone_x_rack_a",
				RequiredGPUsPerTask: 4,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:    pod_status.Running,
						NodeName: "node0",
					},
				},
			},
			{
				Name:                "seed_parent_zone_x_rack_b",
				RequiredGPUsPerTask: 4,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:    pod_status.Running,
						NodeName: "node2",
					},
				},
			},
			{
				Name:                "pending_job1",
				RequiredGPUsPerTask: 4,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				RootSubGroupSet:     root,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending, SubGroupName: "child-a"},
					{State: pod_status.Pending, SubGroupName: "child-a"},
					{State: pod_status.Pending, SubGroupName: "child-a"},
					{State: pod_status.Pending, SubGroupName: "child-b"},
					{State: pod_status.Pending, SubGroupName: "child-b"},
					{State: pod_status.Pending, SubGroupName: "child-b"},
				},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-a", "topology.test/zone-domain": "zone-x"}},
			"node1":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-a", "topology.test/zone-domain": "zone-x"}},
			"node2":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-b", "topology.test/zone-domain": "zone-x"}},
			"node3":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-b", "topology.test/zone-domain": "zone-x"}},
			"node4":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-c", "topology.test/zone-domain": "zone-x"}},
			"node5":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-c", "topology.test/zone-domain": "zone-x"}},
			"node6":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-a", "topology.test/zone-domain": "zone-y"}},
			"node7":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-a", "topology.test/zone-domain": "zone-y"}},
			"node8":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-b", "topology.test/zone-domain": "zone-y"}},
			"node9":  {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-b", "topology.test/zone-domain": "zone-y"}},
			"node10": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-c", "topology.test/zone-domain": "zone-y"}},
			"node11": {GPUs: 8, Labels: map[string]string{"topology.test/rack-domain": "rack-c", "topology.test/zone-domain": "zone-y"}},
		},
		Queues: []test_utils.TestQueueBasic{
			{
				Name:               "queue0",
				ParentQueue:        "department-a",
				DeservedGPUs:       128,
				GPUOverQuotaWeight: 1,
				MaxAllowedGPUs:     128,
			},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{
				Name:         "department-a",
				DeservedGPUs: 128,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: 6,
			},
		},
	}
}

func assertDomainCount(t *testing.T, counts map[string]int, expectedDomain string, expectedTasks int, subgroup string) {
	t.Helper()

	if len(counts) != 1 {
		t.Fatalf("%s allocated across multiple domains: %#v", subgroup, counts)
	}
	if counts[expectedDomain] != expectedTasks {
		t.Fatalf("%s allocated to unexpected domain counts: %#v, expected %s=%d", subgroup, counts, expectedDomain, expectedTasks)
	}
}

func assertSingleDomainName(t *testing.T, counts map[string]int, subgroup string) string {
	t.Helper()

	if len(counts) != 1 {
		t.Fatalf("%s allocated across multiple domains: %#v", subgroup, counts)
	}
	for domain := range counts {
		return domain
	}
	t.Fatalf("%s allocated to no domain", subgroup)
	return ""
}

func assertDomainSet(t *testing.T, actual []string, expected []string, name string) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Fatalf("%s mismatch: actual=%v expected=%v", name, actual, expected)
	}
	actualSet := map[string]bool{}
	for _, value := range actual {
		actualSet[value] = true
	}
	for _, value := range expected {
		if !actualSet[value] {
			t.Fatalf("%s mismatch: actual=%v expected=%v", name, actual, expected)
		}
	}
}

func assertNodeSet(t *testing.T, actual map[string]bool, expected []string, subgroup string) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Fatalf("%s allocated to unexpected nodes: %#v, expected %v", subgroup, actual, expected)
	}
	for _, nodeName := range expected {
		if !actual[nodeName] {
			t.Fatalf("%s allocated to unexpected nodes: %#v, expected %v", subgroup, actual, expected)
		}
	}
}
