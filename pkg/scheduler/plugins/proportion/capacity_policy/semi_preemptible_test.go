// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

var _ = Describe("Semi-Preemptible Capacity Policy", func() {
	vectorMap := resource_info.NewResourceVectorMap()

	oneGpuTask := func(name string, status pod_status.PodStatus) *pod_info.PodInfo {
		return &pod_info.PodInfo{
			UID:            common_info.PodID(name),
			Job:            "job-a",
			Name:           name,
			Namespace:      "team-a",
			Status:         status,
			GpuRequirement: *resource_info.NewGpuResourceRequirementWithGpus(1, 0),
			ResReqVector:   resource_info.NewResourceRequirementsWithGpus(1).ToVector(vectorMap),
			VectorMap:      vectorMap,
		}
	}

	// Leaf queue with deserved(quota)=2 GPUs, generous limit; allocated non-preemptible starts at 0.
	newQueue := func(allocatedNonPreemptible float64) map[common_info.QueueID]*rs.QueueAttributes {
		return map[common_info.QueueID]*rs.QueueAttributes{
			"leaf-queue": {
				UID:         "leaf-queue",
				Name:        "leaf-queue",
				ParentQueue: "",
				QueueResourceShare: rs.QueueResourceShare{
					GPU: rs.ResourceShare{
						Deserved:                2,
						MaxAllowed:              100,
						AllocatedNotPreemptible: allocatedNonPreemptible,
					},
					CPU:    rs.ResourceShare{Deserved: commonUnlimited(), MaxAllowed: commonUnlimited()},
					Memory: rs.ResourceShare{Deserved: commonUnlimited(), MaxAllowed: commonUnlimited()},
				},
			},
		}
	}

	// semiPreemptibleJob builds a flat semi-preemptible job with a single PodSet, mirroring how
	// setSubGroups derives PodSets from RootSubGroupSet. GetTasksToAllocate walks RootSubGroupSet
	// only, so a job built from the flat PodSets map alone would look empty to it.
	semiPreemptibleJob := func(minAvailable int32, tasks map[common_info.PodID]*pod_info.PodInfo) *podgroup_info.PodGroupInfo {
		root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		root.AddPodSet(subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, minAvailable, nil).WithPodInfos(tasks))
		return &podgroup_info.PodGroupInfo{
			Name: "job-a", Namespace: "team-a", Queue: "leaf-queue",
			Preemptibility:  v2alpha2.SemiPreemptible,
			JobFitErrors:    make([]common_info.JobFitError, 0),
			RootSubGroupSet: root,
			PodSets:         root.GetDescendantPodSets(),
		}
	}

	It("charges the whole batch to non-preemptible quota during gang phase", func() {
		// Flat semi-preemptible job, minMember=2, all pending → min not satisfied → core = whole batch (2 GPUs).
		// Queue deserved=2, allocated non-preemptible=0 → exactly fits (2 <= 2) → schedulable.
		job := semiPreemptibleJob(2, map[common_info.PodID]*pod_info.PodInfo{
			"t1": oneGpuTask("t1", pod_status.Pending),
			"t2": oneGpuTask("t2", pod_status.Pending),
			"t3": oneGpuTask("t3", pod_status.Pending),
		})
		cp := New(newQueue(0), ptr.To[int64](node_info.DefaultGpuMemory))
		tasks := podgroup_info.GetTasksToAllocate(job, dummyTasksLessThen, dummyTasksLessThen, podgroup_info.PartialTaskAllocation)
		Expect(cp.IsNonPreemptibleJobOverQuota(job, tasks).IsSchedulable).To(BeTrue())
	})

	It("charges nothing to non-preemptible quota once the minimum is satisfied (elastic burst)", func() {
		// minMember=2, two running (min satisfied) + one pending elastic. Queue deserved=2, already
		// allocated non-preemptible=2. The elastic burst must charge 0 → schedulable even at full quota.
		job := semiPreemptibleJob(2, map[common_info.PodID]*pod_info.PodInfo{
			"t1": oneGpuTask("t1", pod_status.Running),
			"t2": oneGpuTask("t2", pod_status.Running),
			"t3": oneGpuTask("t3", pod_status.Pending),
		})
		cp := New(newQueue(2), ptr.To[int64](node_info.DefaultGpuMemory))
		tasks := podgroup_info.GetTasksToAllocate(job, dummyTasksLessThen, dummyTasksLessThen, podgroup_info.PartialTaskAllocation)
		Expect(cp.IsNonPreemptibleJobOverQuota(job, tasks).IsSchedulable).To(BeTrue())
	})

	It("rejects a gang-phase batch that exceeds the non-preemptible quota", func() {
		// minMember=3, all pending → core = 3 GPUs, queue deserved=2 → over quota.
		job := semiPreemptibleJob(3, map[common_info.PodID]*pod_info.PodInfo{
			"t1": oneGpuTask("t1", pod_status.Pending),
			"t2": oneGpuTask("t2", pod_status.Pending),
			"t3": oneGpuTask("t3", pod_status.Pending),
		})
		cp := New(newQueue(0), ptr.To[int64](node_info.DefaultGpuMemory))
		tasks := podgroup_info.GetTasksToAllocate(job, dummyTasksLessThen, dummyTasksLessThen, podgroup_info.PartialTaskAllocation)
		Expect(cp.IsNonPreemptibleJobOverQuota(job, tasks).IsSchedulable).To(BeFalse())
	})
})

func commonUnlimited() float64 {
	return -1
}
