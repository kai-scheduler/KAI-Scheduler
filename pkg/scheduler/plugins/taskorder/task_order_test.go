// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package taskorder

import (
	"testing"

	"gotest.tools/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func TestTaskOrder(t *testing.T) {
	lPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"kai.scheduler/task-priority": "1",
			},
		},
	}

	rPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"kai.scheduler/task-priority": "2",
			},
		},
	}

	assert.Equal(t, TaskOrderFn(pod_info.NewTaskInfo(lPod, nil, resource_info.NewResourceVectorMap()), pod_info.NewTaskInfo(rPod, nil, resource_info.NewResourceVectorMap())), 1)

	lPod.Labels["kai.scheduler/task-priority"] = "2"
	assert.Equal(t, TaskOrderFn(pod_info.NewTaskInfo(lPod, nil, resource_info.NewResourceVectorMap()), pod_info.NewTaskInfo(rPod, nil, resource_info.NewResourceVectorMap())), 0)

	rPod.Labels["kai.scheduler/task-priority"] = "1"

	assert.Equal(t, TaskOrderFn(pod_info.NewTaskInfo(lPod, nil, resource_info.NewResourceVectorMap()), pod_info.NewTaskInfo(rPod, nil, resource_info.NewResourceVectorMap())), -1)

	lPod.Labels = map[string]string{}
	assert.Equal(t, TaskOrderFn(pod_info.NewTaskInfo(lPod, nil, resource_info.NewResourceVectorMap()), pod_info.NewTaskInfo(rPod, nil, resource_info.NewResourceVectorMap())), 1)

	lPod.Labels = rPod.Labels
	rPod.Labels = map[string]string{}
	assert.Equal(t, TaskOrderFn(pod_info.NewTaskInfo(lPod, nil, resource_info.NewResourceVectorMap()), pod_info.NewTaskInfo(rPod, nil, resource_info.NewResourceVectorMap())), -1)

	lPod.Labels = map[string]string{}
	assert.Equal(t, TaskOrderFn(pod_info.NewTaskInfo(lPod, nil, resource_info.NewResourceVectorMap()), pod_info.NewTaskInfo(rPod, nil, resource_info.NewResourceVectorMap())), 0)

}
