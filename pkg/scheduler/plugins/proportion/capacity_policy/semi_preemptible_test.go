// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

// semiPreemptibleQuotaVectorMap and helpers build segmented semi-preemptible jobs to exercise the
// tree-aware (phase-based) core quota: a gang-phase batch is wholly core; an elastic burst is not.
var semiPreemptibleQuotaVectorMap = resource_info.NewResourceVectorMap()

func segmentPodSet(name string, minAvailable int32, allocated, pending int) (*subgroup_info.PodSet, []*pod_info.PodInfo) {
	ps := subgroup_info.NewPodSet(name, minAvailable, nil)
	var pendingTasks []*pod_info.PodInfo
	for i := 0; i < allocated; i++ {
		ps.AssignTask(newQuotaTask(fmt.Sprintf("%s-a%d", name, i), name, pod_status.Running))
	}
	for i := 0; i < pending; i++ {
		t := newQuotaTask(fmt.Sprintf("%s-p%d", name, i), name, pod_status.Pending)
		ps.AssignTask(t)
		pendingTasks = append(pendingTasks, t)
	}
	return ps, pendingTasks
}

func newQuotaTask(uid, subGroup string, status pod_status.PodStatus) *pod_info.PodInfo {
	t := &pod_info.PodInfo{
		UID:          common_info.PodID(uid),
		SubGroupName: subGroup,
		Status:       status,
		ResReqVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(semiPreemptibleQuotaVectorMap),
		VectorMap:    semiPreemptibleQuotaVectorMap,
	}
	return t
}

func TestGetCoreRequiredQuota_TreeAware(t *testing.T) {
	cp := New(nil, nil)

	t.Run("GangPhase_AllBatchTasksAreCore", func(t *testing.T) {
		// minSubGroup=2 over 2 pending segments, nothing allocated yet → gang phase.
		seg0, p0 := segmentPodSet("segment-0", 2, 0, 2)
		seg1, p1 := segmentPodSet("segment-1", 2, 0, 2)
		root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		root.SetMinSubGroup(ptr.To(int32(2)))
		root.AddPodSet(seg0)
		root.AddPodSet(seg1)
		job := &podgroup_info.PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}

		batch := append(append([]*pod_info.PodInfo{}, p0...), p1...)
		quota := cp.getCoreRequiredQuota(batch, job)
		assert.Equal(t, 4000.0, quota[rs.CpuResource], "whole gang batch (4 pods) counts as core")
	})

	t.Run("ElasticPhase_BurstIsNotCore", func(t *testing.T) {
		// minSubGroup=1 with one segment already satisfied → min met; the burst segment is elastic.
		seg0, _ := segmentPodSet("segment-0", 2, 2, 0) // satisfied (core)
		seg1, burst := segmentPodSet("segment-1", 2, 0, 2)
		root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		root.SetMinSubGroup(ptr.To(int32(1)))
		root.AddPodSet(seg0)
		root.AddPodSet(seg1)
		job := &podgroup_info.PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}

		quota := cp.getCoreRequiredQuota(burst, job)
		assert.Equal(t, 0.0, quota[rs.CpuResource], "elastic burst does not count against non-preemptible quota")
	})
}
