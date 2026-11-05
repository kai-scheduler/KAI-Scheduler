// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"testing"

	kaiv1alpha1 "github.com/kai-scheduler/api/kai/v1alpha1"
	"golang.org/x/exp/maps"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// A required level above the lowest common domain of the job's nodes (a one-node pool, or a pool inside one domain)
// must still be satisfiable: the required level's domains need their free resources computed.
func TestTopologyPlugin_subsetNodesFn_requiredLevelAboveCommonDomain(t *testing.T) {
	node := func(domain, host string) nodes_fake.TestNodeBasic {
		return nodes_fake.TestNodeBasic{GPUs: 1, CPUMillis: 4000, MaxTaskNum: ptr.To(100), Labels: map[string]string{
			"zone": "default", "domain": domain, "kubernetes.io/hostname": host}}
	}
	oneNode := map[string]nodes_fake.TestNodeBasic{"node-1": node("domain-1", "node-1")}
	for _, tc := range []struct {
		name                string
		nodes               map[string]nodes_fake.TestNodeBasic
		required, preferred string
	}{
		{"one node, required domain", oneNode, "domain", ""},
		{"one node, required zone", oneNode, "zone", ""},
		{"one node, required domain, preferred host", oneNode, "domain", "kubernetes.io/hostname"},
		{"two nodes in one domain, required zone", map[string]nodes_fake.TestNodeBasic{
			"a1": node("rack-a", "a1"), "a2": node("rack-a", "a2")}, "zone", ""},
		{"two domains, required domain", map[string]nodes_fake.TestNodeBasic{
			"a1": node("rack-a", "a1"), "b1": node("rack-b", "b1")}, "domain", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := &jobs_fake.TestJobBasic{
				Name: "job", RequiredGPUsPerTask: 1,
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{Topology: "t", RequiredLevel: tc.required, PreferredLevel: tc.preferred}),
				Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			}
			vectorMap := resource_info.NewResourceVectorMap()
			jobs, tasksToNodes, _ := jobs_fake.BuildJobsAndTasksMaps([]*jobs_fake.TestJobBasic{job}, vectorMap)
			nodes := nodes_fake.BuildNodesInfoMap(tc.nodes, tasksToNodes, nil, vectorMap)
			j := jobs["job"]
			plugin := &topologyPlugin{TopologyTrees: map[string]*Info{}, subGroupNodeScores: map[subgroupName]map[string]float64{}}
			plugin.initializeTopologyTree([]*kaiv1alpha1.Topology{{
				ObjectMeta: metav1.ObjectMeta{Name: "t"},
				Spec: kaiv1alpha1.TopologySpec{Levels: []kaiv1alpha1.TopologyLevel{
					{NodeLabel: "zone"}, {NodeLabel: "domain"}, {NodeLabel: "kubernetes.io/hostname"}}},
			}}, nodes)
			subsets, err := plugin.subSetNodesFn(j, &j.RootSubGroupSet.SubGroupInfo, j.RootSubGroupSet.GetDescendantPodSets(),
				podgroup_info.GetTasksToAllocate(j, nil, nil, podgroup_info.RealTaskAllocation), maps.Values(nodes))
			if err != nil {
				t.Fatalf("subSetNodesFn: %v", err)
			}
			offered := 0
			for _, s := range subsets {
				offered += len(s)
			}
			if offered == 0 {
				var msgs []string
				for _, e := range j.JobFitErrors {
					msgs = append(msgs, e.Messages()...)
				}
				t.Errorf("no node offered for a 1-GPU job on idle 1-GPU nodes: %v", msgs)
			}
		})
	}
}
