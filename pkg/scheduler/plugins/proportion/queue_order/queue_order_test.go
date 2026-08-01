// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_order

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/subgrouporder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/taskorder"
	"k8s.io/utils/ptr"
)

type testMetadata struct {
	Name             string
	lqueue           *resource_share.QueueAttributes
	rqueue           *resource_share.QueueAttributes
	lJobInfo         *podgroup_info.PodGroupInfo
	rJobInfo         *podgroup_info.PodGroupInfo
	expectedResult   int
	totalResources   resource_share.ResourceQuantities
	minNodeGPUMemory *int64
}

var testVectorMap = resource_info.NewResourceVectorMap()

func createGpuMemoryTask(name string, numDevices int64, gpuMemory int64) *pod_info.PodInfo {
	gpuReq := *resource_info.NewGpuResourceRequirementWithMultiFraction(numDevices, 0, gpuMemory)
	task := &pod_info.PodInfo{
		Name:                name,
		Status:              pod_status.Pending,
		SubGroupName:        podgroup_info.DefaultSubGroup,
		ResourceRequestType: pod_info.RequestTypeGpuMemory,
		GpuRequirement:      gpuReq,
		ResReqVector:        resource_info.NewResourceVectorWithValues(0, 0, gpuReq.GPUs(), testVectorMap),
		VectorMap:           testVectorMap,
	}
	return task
}

func createPodGroupWithGpuMemoryTask(name string, numDevices int64, gpuMemory int64) *podgroup_info.PodGroupInfo {
	pg := podgroup_info.NewPodGroupInfoWithVectorMap(common_info.PodGroupID(name), testVectorMap)
	pg.GetAllPodSets()[podgroup_info.DefaultSubGroup].SetMinAvailable(1)
	task := createGpuMemoryTask("task-"+name, numDevices, gpuMemory)
	pg.AddTaskInfo(task)
	return pg
}

func emptyPodGroup(name string) *podgroup_info.PodGroupInfo {
	return podgroup_info.NewPodGroupInfoWithVectorMap(common_info.PodGroupID(name), testVectorMap)
}

func TestPrioritizeUnderUtilizedDoesNotAllocate(t *testing.T) {
	lQueue := &resource_share.QueueAttributes{
		QueueResourceShare: resource_share.QueueResourceShare{
			CPU:    resource_share.ResourceShare{FairShare: 1, Allocated: 1},
			Memory: resource_share.ResourceShare{FairShare: 1, Allocated: 1},
			GPU:    resource_share.ResourceShare{FairShare: 1, Allocated: 1},
		},
	}
	rQueue := &resource_share.QueueAttributes{
		QueueResourceShare: resource_share.QueueResourceShare{
			CPU:    resource_share.ResourceShare{FairShare: 1, Allocated: 2},
			Memory: resource_share.ResourceShare{FairShare: 1, Allocated: 2},
			GPU:    resource_share.ResourceShare{FairShare: 1, Allocated: 2},
		},
	}
	var result int

	allocations := testing.AllocsPerRun(100, func() {
		result = prioritizeUnderUtilized(lQueue, rQueue)
	})

	assert.Equal(t, lQueuePrioritized, result)
	assert.Zero(t, allocations)
}

func TestPrioritizeUnderQuotaWithJobDoesNotAllocate(t *testing.T) {
	lQueue := &resource_share.QueueAttributes{
		QueueResourceShare: resource_share.QueueResourceShare{
			CPU:    resource_share.ResourceShare{Deserved: 1},
			Memory: resource_share.ResourceShare{Deserved: 1},
			GPU:    resource_share.ResourceShare{Deserved: 1},
		},
	}
	rQueue := &resource_share.QueueAttributes{
		QueueResourceShare: resource_share.QueueResourceShare{
			CPU:    resource_share.ResourceShare{Allocated: 2, Deserved: 1},
			Memory: resource_share.ResourceShare{Allocated: 2, Deserved: 1},
			GPU:    resource_share.ResourceShare{Allocated: 2, Deserved: 1},
		},
	}
	lJob := emptyPodGroup("lJob")
	rJob := emptyPodGroup("rJob")
	var result int

	allocations := testing.AllocsPerRun(100, func() {
		result = prioritizeUnderQuotaWithJob(lQueue, rQueue, lJob, rJob, nil, nil, nil)
	})

	assert.Equal(t, lQueuePrioritized, result)
	assert.Zero(t, allocations)
}

func TestGetQueueOrderResult(t *testing.T) {
	tests := []testMetadata{
		{
			Name: "test prioritization based on fair share starvation",
			lqueue: &resource_share.QueueAttributes{
				Name: "lQueue",
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               99,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               20,
						AllocatedNotPreemptible: 0,
						Request:                 99,
					},
				},
			},
			rqueue: &resource_share.QueueAttributes{
				Name: "rQueue",
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               2,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               0,
						AllocatedNotPreemptible: 0,
						Request:                 2,
					},
				},
			},
			lJobInfo:       emptyPodGroup("lJob"),
			rJobInfo:       emptyPodGroup("rJob"),
			expectedResult: rQueuePrioritized,
		},
		{
			Name: "test prioritization based on quota starvation, even if priority is different",
			lqueue: &resource_share.QueueAttributes{
				Name:     "lQueue",
				Priority: 1,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               99,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               20,
						AllocatedNotPreemptible: 0,
						Request:                 99,
					},
				},
			},
			rqueue: &resource_share.QueueAttributes{
				Name:     "rQueue",
				Priority: 0,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               2,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               0,
						AllocatedNotPreemptible: 0,
						Request:                 2,
					},
				},
			},
			lJobInfo:       emptyPodGroup("lJob"),
			rJobInfo:       emptyPodGroup("rJob"),
			expectedResult: rQueuePrioritized,
		},
		{
			Name: "prioritize queue priority if queues are satisfied",
			lqueue: &resource_share.QueueAttributes{
				Name:     "lQueue",
				Priority: 1,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               99,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               20,
						AllocatedNotPreemptible: 0,
						Request:                 99,
					},
				},
			},
			rqueue: &resource_share.QueueAttributes{
				Name:     "rQueue",
				Priority: 0,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               2,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               3,
						AllocatedNotPreemptible: 0,
						Request:                 99,
					},
				},
			},
			lJobInfo:       emptyPodGroup("lJob"),
			rJobInfo:       emptyPodGroup("rJob"),
			expectedResult: lQueuePrioritized,
		},
		{
			Name: "prioritize queue priority if queues are satisfied, quota < rQueue < fairshare",
			lqueue: &resource_share.QueueAttributes{
				Name:     "lQueue",
				Priority: 1,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               99,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               20,
						AllocatedNotPreemptible: 0,
						Request:                 99,
					},
				},
			},
			rqueue: &resource_share.QueueAttributes{
				Name:     "rQueue",
				Priority: 0,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                2,
						FairShare:               80,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               3,
						AllocatedNotPreemptible: 0,
						Request:                 99,
					},
				},
			},
			lJobInfo:       emptyPodGroup("lJob"),
			rJobInfo:       emptyPodGroup("rJob"),
			expectedResult: lQueuePrioritized,
		},
		{
			Name: "GPU memory affects queue order - lower memory request prioritized",
			lqueue: &resource_share.QueueAttributes{
				Name:     "lQueue",
				Priority: 0,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                50,
						FairShare:               50,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               40,
						AllocatedNotPreemptible: 0,
						Request:                 50,
					},
				},
			},
			rqueue: &resource_share.QueueAttributes{
				Name:     "rQueue",
				Priority: 0,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                50,
						FairShare:               50,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               40,
						AllocatedNotPreemptible: 0,
						Request:                 50,
					},
				},
			},
			lJobInfo:         createPodGroupWithGpuMemoryTask("lJob", 2, 5000),
			rJobInfo:         createPodGroupWithGpuMemoryTask("rJob", 2, 10000),
			expectedResult:   lQueuePrioritized,
			totalResources:   resource_share.ResourceQuantities{resource_share.GpuResource: 100},
			minNodeGPUMemory: ptr.To(int64(10000)),
		},
		{
			Name: "GPU memory affects queue order - higher memory request deprioritized",
			lqueue: &resource_share.QueueAttributes{
				Name:     "lQueue",
				Priority: 0,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                50,
						FairShare:               50,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               40,
						AllocatedNotPreemptible: 0,
						Request:                 50,
					},
				},
			},
			rqueue: &resource_share.QueueAttributes{
				Name:     "rQueue",
				Priority: 0,
				QueueResourceShare: resource_share.QueueResourceShare{
					CPU:    resource_share.ResourceShare{},
					Memory: resource_share.ResourceShare{},
					GPU: resource_share.ResourceShare{
						Deserved:                50,
						FairShare:               50,
						MaxAllowed:              -1,
						OverQuotaWeight:         1,
						Allocated:               40,
						AllocatedNotPreemptible: 0,
						Request:                 50,
					},
				},
			},
			lJobInfo:         createPodGroupWithGpuMemoryTask("lJobHigh", 4, 8000),
			rJobInfo:         createPodGroupWithGpuMemoryTask("rJobLow", 4, 2000),
			expectedResult:   rQueuePrioritized,
			totalResources:   resource_share.ResourceQuantities{resource_share.GpuResource: 100},
			minNodeGPUMemory: ptr.To(int64(10000)),
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Logf("Running test %d/%d: %s", i+1, len(tests), test.Name)
			session := &framework.Session{
				SubGroupOrderFns: []common_info.CompareFn{
					subgrouporder.SubGroupOrderFn,
				},
			}

			taskOrderFn := func(l, r interface{}) bool {
				if comparison := taskorder.TaskOrderFn(l, r); comparison != 0 {
					return comparison < 0
				}
				return false
			}
			result := GetQueueOrderResult(test.lqueue, test.rqueue, test.lJobInfo, test.rJobInfo, nil, nil,
				session.SubGroupOrderFn, taskOrderFn, test.totalResources, test.minNodeGPUMemory)
			assert.Equal(t, test.expectedResult, result)
		})
	}
}
