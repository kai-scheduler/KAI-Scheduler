// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	rs "github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

var _ = Describe("Quota Policy Check", func() {
	Describe("isAllocatedNonPreemptibleOverQuota", func() {
		var (
			queueAttributes *rs.QueueAttributes
		)
		BeforeEach(func() {
			queueAttributes = &rs.QueueAttributes{
				QueueResourceShare: rs.QueueResourceShare{
					CPU:    rs.EmptyResource(),
					Memory: rs.EmptyResource(),
					GPU:    rs.EmptyResource(),
				},
			}
		})

		Context("isAllocatedNonPreemptibleOverQuota tests", func() {
			tests := map[string]struct {
				deserved                rs.ResourceQuantities
				allocatedNonPreemptible rs.ResourceQuantities
				requestedQuota          rs.ResourceQuantities
				expectedResult          bool
				resource                rs.ResourceName
			}{
				"lower than deserved quota in all resources": {
					deserved: rs.ResourceQuantities{
						rs.CpuResource:    5,
						rs.MemoryResource: 5,
						rs.GpuResource:    5,
					},
					allocatedNonPreemptible: map[rs.ResourceName]float64{
						rs.CpuResource:    1,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    3,
						rs.MemoryResource: 3,
						rs.GpuResource:    3,
					},
					expectedResult: false,
				},
				"lower than deserved in some resources": {
					deserved: rs.ResourceQuantities{
						rs.CpuResource:    5,
						rs.MemoryResource: 5,
						rs.GpuResource:    5,
					},
					allocatedNonPreemptible: rs.ResourceQuantities{
						rs.CpuResource:    1,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    10,
						rs.MemoryResource: 3,
						rs.GpuResource:    3,
					},
					expectedResult: true,
					resource:       rs.CpuResource,
				},
				"higher than deserved in all resources": {
					deserved: rs.ResourceQuantities{
						rs.CpuResource:    5,
						rs.MemoryResource: 5,
						rs.GpuResource:    5,
					},
					allocatedNonPreemptible: rs.ResourceQuantities{
						rs.CpuResource:    1,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    10,
						rs.MemoryResource: 10,
						rs.GpuResource:    10,
					},
					expectedResult: true,
					resource:       rs.CpuResource,
				},
				"higher than deserved quota with job that doesn't request the exceeding resource": {
					deserved: rs.ResourceQuantities{
						rs.CpuResource:    5,
						rs.MemoryResource: 5,
						rs.GpuResource:    5,
					},
					allocatedNonPreemptible: rs.ResourceQuantities{
						rs.CpuResource:    4,
						rs.MemoryResource: 4,
						rs.GpuResource:    6,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    1,
						rs.MemoryResource: 1,
						rs.GpuResource:    0,
					},
					expectedResult: false,
				},
			}
			for name, data := range tests {
				testName := name
				testData := data
				It(testName, func() {
					for _, resource := range rs.AllResources {
						resourceShare := queueAttributes.ResourceShare(resource)
						resourceShare.Deserved = testData.deserved[resource]
						resourceShare.AllocatedNotPreemptible = testData.allocatedNonPreemptible[resource]
					}
					result, resourceName := isAllocatedNonPreemptibleOverQuota(queueAttributes, testData.requestedQuota)
					Expect(result).To(Equal(testData.expectedResult))
					Expect(resourceName).To(Equal(testData.resource))
				})
			}
		})
	})

	Describe("resultsWithNonPreemptibleOverQuota", func() {
		Context("resultsWithNonPreemptibleOverQuota tests", func() {
			tests := map[string]struct {
				queues         map[common_info.QueueID]*rs.QueueAttributes
				job            *podgroup_info.PodGroupInfo
				requestedShare rs.ResourceQuantities
				expectedResult bool
			}{
				"single queue - **preemptible** job - allocated non preemptible below quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"queue1": {
							UID:               "queue1",
							Name:              "queue1",
							ParentQueue:       "",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                3,
									AllocatedNotPreemptible: 1,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "queue1",
						Preemptibility: v2alpha2.Preemptible,
					},
					requestedShare: rs.ResourceQuantities{
						rs.GpuResource: 10,
					},
					expectedResult: true,
				},
				"single queue - non preemptible job - allocated non preemptible below quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"queue1": {
							UID:               "queue1",
							Name:              "queue1",
							ParentQueue:       "",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                3,
									AllocatedNotPreemptible: 1,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:      "job-a",
						Namespace: "team-a",
						Queue:     "queue1",
					},
					requestedShare: rs.ResourceQuantities{
						rs.GpuResource: 1,
					},
					expectedResult: true,
				},
				"single queue - non preemptible job - allocated non preemptible results over quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"queue1": {
							UID:               "queue1",
							Name:              "queue1",
							ParentQueue:       "",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                3,
									AllocatedNotPreemptible: 1,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:      "job-a",
						Namespace: "team-a",
						Queue:     "queue1",
					},
					requestedShare: rs.ResourceQuantities{
						rs.GpuResource: 10,
					},
					expectedResult: false,
				},
				"multiple queues - non preemptible job - allocated non preemptible below quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                3,
									AllocatedNotPreemptible: 1,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                3,
									AllocatedNotPreemptible: 1,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                3,
									AllocatedNotPreemptible: 1,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:      "job-a",
						Namespace: "team-a",
						Queue:     "leaf-queue",
					},
					requestedShare: rs.ResourceQuantities{
						rs.GpuResource: 1,
					},
					expectedResult: true,
				},
				"multiple queues - non preemptible job - allocated non preemptible results over quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                3,
									AllocatedNotPreemptible: 1,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                1,
									AllocatedNotPreemptible: 1,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:                1,
									AllocatedNotPreemptible: 0,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:      "job-a",
						Namespace: "team-a",
						Queue:     "leaf-queue",
					},
					requestedShare: rs.ResourceQuantities{
						rs.GpuResource: 1,
					},
					expectedResult: false,
				},
			}

			for name, data := range tests {
				testName := name
				testData := data
				It(testName, func() {
					capacityPolicy := New(testData.queues)
					result := capacityPolicy.resultsWithNonPreemptibleOverQuota(testData.requestedShare,
						testData.job)
					Expect(result.IsSchedulable).To(Equal(testData.expectedResult))
				})
			}

		})
	})
})
