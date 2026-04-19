// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_order

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/subgrouporder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/taskorder"
)

type testMetadata struct {
	Name           string
	lqueue         *resource_share.QueueAttributes
	rqueue         *resource_share.QueueAttributes
	lJobInfo       *podgroup_info.PodGroupInfo
	rJobInfo       *podgroup_info.PodGroupInfo
	expectedResult int
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
			lJobInfo:       &podgroup_info.PodGroupInfo{},
			rJobInfo:       &podgroup_info.PodGroupInfo{},
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
			lJobInfo:       &podgroup_info.PodGroupInfo{},
			rJobInfo:       &podgroup_info.PodGroupInfo{},
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
			lJobInfo:       &podgroup_info.PodGroupInfo{},
			rJobInfo:       &podgroup_info.PodGroupInfo{},
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
			lJobInfo:       &podgroup_info.PodGroupInfo{},
			rJobInfo:       &podgroup_info.PodGroupInfo{},
			expectedResult: lQueuePrioritized,
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Logf("Running test %d/%d: %s", i+1, len(tests), test.Name)

			taskOrderFn := func(l, r interface{}) bool {
				if comparison := taskorder.TaskOrderFn(l, r); comparison != 0 {
					return comparison < 0
				}
				return false
			}

			subGroupOrderFn := func(l, r interface{}) bool {
				if comparison := subgrouporder.PodSetOrderFn(l, r); comparison != 0 {
					return comparison < 0
				}
				return false
			}
			result := GetQueueOrderResult(test.lqueue, test.rqueue, test.lJobInfo, test.rJobInfo, nil, nil,
				subGroupOrderFn, taskOrderFn, resource_share.ResourceQuantities{})
			assert.Equal(t, test.expectedResult, result)
		})
	}
}
