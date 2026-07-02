// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package proportion

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

var _ = Describe("Semi-Preemptible Quota Accounting", func() {
	vectorMap := resource_info.NewResourceVectorMap()

	nameOrder := func(l, r interface{}) bool {
		lm := l.(subgroup_info.SubGroupMember)
		rm := r.(subgroup_info.SubGroupMember)
		return lm.GetName() < rm.GetName()
	}
	taskOrder := func(l, r interface{}) bool {
		return l.(*pod_info.PodInfo).UID < r.(*pod_info.PodInfo).UID
	}

	gpuTask := func(name, subGroup string, status pod_status.PodStatus) *pod_info.PodInfo {
		vec := resource_info.NewResourceRequirementsWithGpus(1).ToVector(vectorMap)
		return &pod_info.PodInfo{
			UID:                    common_info.PodID(name),
			Job:                    "job-a",
			Name:                   name,
			Namespace:              "team-a",
			SubGroupName:           subGroup,
			Status:                 status,
			ResReqVector:           vec,
			AcceptedResourceVector: vec,
			VectorMap:              vectorMap,
		}
	}

	newPlugin := func() *proportionPlugin {
		return &proportionPlugin{
			subGroupOrderFn:         nameOrder,
			taskOrderFunc:           taskOrder,
			lastSemiPreemptibleCore: map[common_info.PodGroupID]rs.ResourceQuantities{},
		}
	}

	Describe("coreResourceQuantities and delta", func() {
		It("counts minSubGroup core subgroups once and excludes elastic surplus", func() {
			// 4 fully-gang leaf subgroups (minMember=1), minSubGroup=2 → 2 core subgroups = 2 GPUs.
			root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
			root.SetMinSubGroup(intPtr(2))
			for _, n := range []string{"r0", "r1", "r2", "r3"} {
				ps := subgroup_info.NewPodSet(n, 1, nil)
				ps.AssignTask(gpuTask(n+"-p", n, pod_status.Running))
				root.AddPodSet(ps)
			}
			job := &podgroup_info.PodGroupInfo{
				UID: "job-a", Preemptibility: v2alpha2.SemiPreemptible,
				RootSubGroupSet: root, PodSets: root.GetDescendantPodSets(),
			}

			pp := newPlugin()
			core := pp.coreResourceQuantities(job)
			Expect(core[rs.GpuResource]).To(Equal(float64(2)))
		})

		It("returns the incremental delta as the core grows and flattens once satisfied", func() {
			// Flat job minMember=2. Simulate baseline none, then two allocate events, then an elastic one.
			ps := subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil)
			job := &podgroup_info.PodGroupInfo{
				UID: "job-a", Preemptibility: v2alpha2.SemiPreemptible,
				PodSets: map[string]*subgroup_info.PodSet{podgroup_info.DefaultSubGroup: ps},
			}
			pp := newPlugin()
			pp.lastSemiPreemptibleCore["job-a"] = rs.EmptyResourceQuantities()

			ps.AssignTask(gpuTask("t1", "", pod_status.Running))
			d1 := pp.semiPreemptibleCoreDelta(job)
			Expect(d1[rs.GpuResource]).To(Equal(float64(1)))

			ps.AssignTask(gpuTask("t2", "", pod_status.Running))
			d2 := pp.semiPreemptibleCoreDelta(job)
			Expect(d2[rs.GpuResource]).To(Equal(float64(1)))

			// Third (elastic) task: core stays at 2 → delta 0.
			ps.AssignTask(gpuTask("t3", "", pod_status.Running))
			d3 := pp.semiPreemptibleCoreDelta(job)
			Expect(d3[rs.GpuResource]).To(Equal(float64(0)))
		})
	})

	Describe("splitVictimTasksByCoreSet", func() {
		It("classifies whole elastic subgroups as elastic and protects the core", func() {
			root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
			root.SetMinSubGroup(intPtr(2))
			var allTasks []*pod_info.PodInfo
			for _, n := range []string{"r0", "r1", "r2", "r3"} {
				ps := subgroup_info.NewPodSet(n, 1, nil)
				task := gpuTask(n+"-p", n, pod_status.Running)
				ps.AssignTask(task)
				allTasks = append(allTasks, task)
				root.AddPodSet(ps)
			}
			job := &podgroup_info.PodGroupInfo{
				UID: "job-a", Preemptibility: v2alpha2.SemiPreemptible,
				RootSubGroupSet: root, PodSets: root.GetDescendantPodSets(),
			}

			pp := newPlugin()
			elastic, core := pp.splitVictimTasks(&api.VictimInfo{Job: job, Tasks: allTasks})
			Expect(core).To(HaveLen(2))    // r0, r1
			Expect(elastic).To(HaveLen(2)) // r2, r3
		})
	})
})

func intPtr(v int32) *int32 { return &v }
