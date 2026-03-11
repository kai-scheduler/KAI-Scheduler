// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_updater

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/common"
)

func TestUpdateQueue_PodGroupsOnly(t *testing.T) {
	objects := []client.Object{
		&v2alpha2.PodGroup{
			ObjectMeta: v12.ObjectMeta{
				Name:      "podgroup1",
				Namespace: "proj-1",
			},
			Spec: v2alpha2.PodGroupSpec{
				Queue: "queue-name",
			},
			Status: v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
						"cpu":            resource.MustParse("100m"),
						"memory":         resource.MustParse("2Gi"),
					},
					AllocatedNonPreemptible: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
						"cpu":            resource.MustParse("100m"),
						"memory":         resource.MustParse("2Gi"),
					},
					Requested: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
						"cpu":            resource.MustParse("100m"),
						"memory":         resource.MustParse("2Gi"),
					},
				},
			},
		},
		&v2alpha2.PodGroup{
			ObjectMeta: v12.ObjectMeta{
				Name:      "podgroup2",
				Namespace: "proj-1",
			},
			Spec: v2alpha2.PodGroupSpec{
				Queue: "queue-name",
			},
			Status: v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: v1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("2Gi"),
					},
					AllocatedNonPreemptible: v1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("2Gi"),
					},
					Requested: v1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("2Gi"),
					},
				},
			},
		},
		&v2alpha2.PodGroup{
			ObjectMeta: v12.ObjectMeta{
				Name:      "podgroup3",
				Namespace: "proj-1",
			},
			Spec: v2alpha2.PodGroupSpec{
				Queue: "not-queue-name",
			},
			Status: v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
						"cpu":            resource.MustParse("100m"),
						"memory":         resource.MustParse("2Gi"),
					},
					AllocatedNonPreemptible: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
						"cpu":            resource.MustParse("100m"),
						"memory":         resource.MustParse("2Gi"),
					},
					Requested: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
						"cpu":            resource.MustParse("100m"),
						"memory":         resource.MustParse("2Gi"),
					},
				},
			},
		},
	}

	queue := v2.Queue{
		ObjectMeta: v12.ObjectMeta{
			Name: "queue-name",
		},
	}

	updater := ResourceUpdater{
		Client: newFakeClientBuilder(t).WithObjects(objects...).Build(),
	}

	err := updater.UpdateQueue(context.Background(), &queue)
	assert.Nil(t, err)

	expectedGPU := resource.MustParse("1")
	assert.True(t, expectedGPU.Equal(queue.Status.Allocated["nvidia.com/gpu"]))
	assert.True(t, expectedGPU.Equal(queue.Status.AllocatedNonPreemptible["nvidia.com/gpu"]))
	assert.True(t, expectedGPU.Equal(queue.Status.Requested["nvidia.com/gpu"]))

	expectedCpu := resource.MustParse("200m")
	assert.True(t, expectedCpu.Equal(queue.Status.Allocated["cpu"]))
	assert.True(t, expectedCpu.Equal(queue.Status.AllocatedNonPreemptible["cpu"]))
	assert.True(t, expectedCpu.Equal(queue.Status.Requested["cpu"]))

	expectedMemory := resource.MustParse("4Gi")
	assert.True(t, expectedMemory.Equal(queue.Status.Allocated["memory"]))
	assert.True(t, expectedMemory.Equal(queue.Status.AllocatedNonPreemptible["memory"]))
	assert.True(t, expectedMemory.Equal(queue.Status.Requested["memory"]))
}

func TestUpdateQueue_PodGroupWithoutQueueLabel(t *testing.T) {
	objects := []client.Object{
		&v2alpha2.PodGroup{
			ObjectMeta: v12.ObjectMeta{
				Name:      "podgroup-with-label",
				Namespace: "proj-1",
				Labels: map[string]string{
					"kai.scheduler/queue": "queue-name",
				},
			},
			Spec: v2alpha2.PodGroupSpec{
				Queue: "queue-name",
			},
			Status: v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
					Requested: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
				},
			},
		},
		&v2alpha2.PodGroup{
			ObjectMeta: v12.ObjectMeta{
				Name:      "podgroup-without-label",
				Namespace: "proj-1",
			},
			Spec: v2alpha2.PodGroupSpec{
				Queue: "queue-name",
			},
			Status: v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
					Requested: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
				},
			},
		},
	}

	queue := v2.Queue{
		ObjectMeta: v12.ObjectMeta{
			Name: "queue-name",
		},
	}

	updater := ResourceUpdater{
		Client: newFakeClientBuilder(t).WithObjects(objects...).Build(),
	}

	err := updater.UpdateQueue(context.Background(), &queue)
	assert.Nil(t, err)

	expectedGPU := resource.MustParse("2")
	assert.True(t, expectedGPU.Equal(queue.Status.Allocated["nvidia.com/gpu"]),
		"expected 2 GPUs allocated (both PodGroups), got %v", queue.Status.Allocated["nvidia.com/gpu"])
	assert.True(t, expectedGPU.Equal(queue.Status.Requested["nvidia.com/gpu"]),
		"expected 2 GPUs requested (both PodGroups), got %v", queue.Status.Requested["nvidia.com/gpu"])
}

func TestUpdateQueue_QueuesOnly(t *testing.T) {
	objects := []client.Object{
		&v2.Queue{
			ObjectMeta: v12.ObjectMeta{
				Name: "child-queue1",
			},
			Spec: v2.QueueSpec{
				ParentQueue: "parent",
			},
			Status: v2.QueueStatus{
				Requested: v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
					"cpu":            resource.MustParse("100m"),
					"memory":         resource.MustParse("2Gi"),
				},
				AllocatedNonPreemptible: v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
					"cpu":            resource.MustParse("100m"),
					"memory":         resource.MustParse("2Gi"),
				},
				Allocated: v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
					"cpu":            resource.MustParse("100m"),
					"memory":         resource.MustParse("2Gi"),
				},
			},
		},
		&v2.Queue{
			ObjectMeta: v12.ObjectMeta{
				Name: "child-queue2",
			},
			Spec: v2.QueueSpec{
				ParentQueue: "parent",
			},
			Status: v2.QueueStatus{
				Requested: v1.ResourceList{
					"cpu":    resource.MustParse("100m"),
					"memory": resource.MustParse("2Gi"),
				},
				AllocatedNonPreemptible: v1.ResourceList{
					"cpu":    resource.MustParse("100m"),
					"memory": resource.MustParse("2Gi"),
				},
				Allocated: v1.ResourceList{
					"cpu":    resource.MustParse("100m"),
					"memory": resource.MustParse("2Gi"),
				},
			},
		},
		&v2.Queue{
			ObjectMeta: v12.ObjectMeta{
				Name: "non-child-queue",
			},
			Spec: v2.QueueSpec{},
			Status: v2.QueueStatus{
				Requested: v1.ResourceList{
					"cpu":    resource.MustParse("100m"),
					"memory": resource.MustParse("2Gi"),
				},
				AllocatedNonPreemptible: v1.ResourceList{
					"cpu":    resource.MustParse("100m"),
					"memory": resource.MustParse("2Gi"),
				},
				Allocated: v1.ResourceList{
					"cpu":    resource.MustParse("100m"),
					"memory": resource.MustParse("2Gi"),
				},
			},
		},
	}

	queue := v2.Queue{
		ObjectMeta: v12.ObjectMeta{
			Name: "parent",
		},
	}

	updater := ResourceUpdater{
		Client: newFakeClientBuilder(t).WithObjects(objects...).Build(),
	}

	err := updater.UpdateQueue(context.Background(), &queue)
	assert.Nil(t, err)

	expectedGPU := resource.MustParse("1")
	assert.True(t, expectedGPU.Equal(queue.Status.Allocated["nvidia.com/gpu"]))
	assert.True(t, expectedGPU.Equal(queue.Status.AllocatedNonPreemptible["nvidia.com/gpu"]))
	assert.True(t, expectedGPU.Equal(queue.Status.Requested["nvidia.com/gpu"]))

	expectedCpu := resource.MustParse("200m")
	assert.True(t, expectedCpu.Equal(queue.Status.Allocated["cpu"]))
	assert.True(t, expectedCpu.Equal(queue.Status.AllocatedNonPreemptible["cpu"]))
	assert.True(t, expectedCpu.Equal(queue.Status.Requested["cpu"]))

	expectedMemory := resource.MustParse("4Gi")
	assert.True(t, expectedMemory.Equal(queue.Status.Allocated["memory"]))
	assert.True(t, expectedMemory.Equal(queue.Status.AllocatedNonPreemptible["memory"]))
	assert.True(t, expectedMemory.Equal(queue.Status.Requested["memory"]))
}

func newFakeClientBuilder(t *testing.T) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithIndex(&v2.Queue{}, common.ParentQueueIndexName, parentQueueIndexer).
		WithIndex(&v2alpha2.PodGroup{}, common.PodGroupQueueIndexName, podGroupQueueIndexer)
}

func podGroupQueueIndexer(object client.Object) []string {
	pg := object.(*v2alpha2.PodGroup)
	if pg.Spec.Queue == "" {
		return []string{}
	}
	return []string{pg.Spec.Queue}
}

func parentQueueIndexer(object client.Object) []string {
	queue := object.(*v2.Queue)
	if queue.Spec.ParentQueue == "" {
		return []string{}
	}
	return []string{queue.Spec.ParentQueue}
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	err := v2alpha2.AddToScheme(scheme)
	assert.Nil(t, err)
	err = v2.AddToScheme(scheme)
	assert.Nil(t, err)
	return scheme
}
