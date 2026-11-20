// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func TestRemoveTaskInfoMatchesSuffixClone(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	var tasks []*pod_info.PodInfo
	for i := 0; i < 24; i++ {
		phase := []v1.PodPhase{v1.PodRunning, v1.PodPending, v1.PodSucceeded}[i%3]
		nodeName := "node"
		if phase == v1.PodPending {
			nodeName = ""
		}
		tasks = append(tasks, pod_info.NewTaskInfo(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprint("task-", i), UID: types.UID(fmt.Sprint(i))},
			Spec: v1.PodSpec{NodeName: nodeName, Containers: []v1.Container{{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
				v1.ResourceCPU: *resource.NewQuantity(int64(i+1), resource.DecimalSI), resource_info.GPUResourceName: resource.MustParse("1"),
			}}}}}, Status: v1.PodStatus{Phase: phase},
		}, vectorMap))
	}
	source := NewPodGroupInfoWithVectorMap("victim", vectorMap, tasks...)
	remainder := source.Clone()
	for i, task := range tasks {
		_ = remainder.GetAliveTasksRequestedGPUs()
		_ = remainder.GetActiveAllocatedTasksCount()
		_ = remainder.GetAllPodsMap()
		_ = remainder.GetSchedulingConstraintsSignature()
		remainder.RemoveTaskInfo(task)
		remainder.RemoveTaskInfo(task)
		expected := source.CloneWithTasks(tasks[i+1:])
		require.Equal(t, expected.GetSchedulingConstraintsSignature(), remainder.GetSchedulingConstraintsSignature())
		require.Equal(t, expected.GetAllPodsMap(), remainder.GetAllPodsMap())
		require.Equal(t, expected.PodStatusIndex, remainder.PodStatusIndex)
		require.Equal(t, expected.AllocatedVector, remainder.AllocatedVector)
		require.Equal(t, expected.GetActiveAllocatedTasksCount(), remainder.GetActiveAllocatedTasksCount())
		require.Equal(t, expected.GetAliveTasksRequestedGPUs(), remainder.GetAliveTasksRequestedGPUs())
		expectedSet, actualSet := expected.PodSets[DefaultSubGroup], remainder.PodSets[DefaultSubGroup]
		require.Equal(t, expectedSet.GetNumActiveAllocatedTasks(), actualSet.GetNumActiveAllocatedTasks())
		require.Equal(t, expectedSet.GetNumActiveUsedTasks(), actualSet.GetNumActiveUsedTasks())
		require.Equal(t, expectedSet.GetNumAliveTasks(), actualSet.GetNumAliveTasks())
		require.Equal(t, expectedSet.GetNumPendingTasks(), actualSet.GetNumPendingTasks())
		require.Equal(t, expectedSet.IsGangSatisfied(), actualSet.IsGangSatisfied())
	}
	require.Len(t, source.GetAllPodsMap(), 24)
}
