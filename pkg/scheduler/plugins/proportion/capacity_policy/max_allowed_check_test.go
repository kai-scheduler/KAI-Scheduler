// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"reflect"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

var _ = Describe("Max Allowed Policy Check", func() {
	Describe("IsOverMaxAllowed", func() {
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

		Context("IsOverMaxAllowed tests", func() {
			tests := map[string]struct {
				maxAllowed       rs.ResourceQuantities
				allocated        rs.ResourceQuantities
				requestedQuota   rs.ResourceQuantities
				isOverMaxAllowed bool
				resourceName     rs.ResourceName
			}{
				"no maxAllowed": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    commonconstants.UnlimitedResourceQuantity,
						rs.MemoryResource: commonconstants.UnlimitedResourceQuantity,
						rs.GpuResource:    commonconstants.UnlimitedResourceQuantity,
					},
					allocated: map[rs.ResourceName]float64{
						rs.CpuResource:    500,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					isOverMaxAllowed: false,
					resourceName:     "",
				},
				"no allocated equal to max allowed": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					allocated: rs.ResourceQuantities{},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					isOverMaxAllowed: false,
					resourceName:     "",
				},
				"no allocated under max allowed": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					allocated: rs.ResourceQuantities{
						rs.CpuResource:    0,
						rs.MemoryResource: 0,
						rs.GpuResource:    0,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    500,
						rs.MemoryResource: 0.5,
						rs.GpuResource:    0.5,
					},
					isOverMaxAllowed: false,
					resourceName:     "",
				},
				"over max allowed in all recources": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					allocated: rs.ResourceQuantities{
						rs.CpuResource:    500,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					isOverMaxAllowed: true,
					resourceName:     rs.CpuResource,
				},
				"over max allowed only cpu": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					allocated: rs.ResourceQuantities{
						rs.CpuResource:    500,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 0,
						rs.GpuResource:    0,
					},
					isOverMaxAllowed: true,
					resourceName:     rs.CpuResource,
				},
				"over max allowed only Memory": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					allocated: rs.ResourceQuantities{
						rs.CpuResource:    500,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    0,
						rs.MemoryResource: 1,
						rs.GpuResource:    0,
					},
					isOverMaxAllowed: true,
					resourceName:     rs.MemoryResource,
				},
				"over max allowed only gpu": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					allocated: rs.ResourceQuantities{
						rs.CpuResource:    500,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    0,
						rs.MemoryResource: 0,
						rs.GpuResource:    1,
					},
					isOverMaxAllowed: true,
					resourceName:     rs.GpuResource,
				},
				"over max allowed but not requesting exceeding resource": {
					maxAllowed: rs.ResourceQuantities{
						rs.CpuResource:    1000,
						rs.MemoryResource: 1,
						rs.GpuResource:    1,
					},
					allocated: rs.ResourceQuantities{
						rs.CpuResource:    1500,
						rs.MemoryResource: 1,
						rs.GpuResource:    0,
					},
					requestedQuota: rs.ResourceQuantities{
						rs.CpuResource:    0,
						rs.MemoryResource: 0,
						rs.GpuResource:    1,
					},
					isOverMaxAllowed: false,
				},
			}
			for name, data := range tests {
				testName := name
				testData := data
				It(testName, func() {
					for _, resource := range rs.AllResources {
						resourceShare := queueAttributes.ResourceShare(resource)
						resourceShare.MaxAllowed = testData.maxAllowed[resource]
						resourceShare.Allocated = testData.allocated[resource]
					}
					isOverMaxAllowed, resourceName := isOverLimit(queueAttributes, testData.requestedQuota)
					Expect(isOverMaxAllowed).To(Equal(testData.isOverMaxAllowed))
					Expect(resourceName).To(Equal(testData.resourceName))
				})
			}
		})
	})

	Describe("resultsOverLimit", func() {
		Context("resultsOverLimit tests", func() {
			tests := map[string]struct {
				queues          map[common_info.QueueID]*rs.QueueAttributes
				job             *podgroup_info.PodGroupInfo
				requestedShare  rs.ResourceQuantities
				expectedResult  bool
				expectedDetails *v2alpha2.QuotaDetails // optional, only compare if set
			}{
				"single queue - unlimited queue": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"queue1": {
							UID:               "queue1",
							Name:              "queue1",
							ParentQueue:       "",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: commonconstants.UnlimitedResourceQuantity,
									Allocated:  1,
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
					expectedResult: true,
				},
				"single queue - limited queue - results below limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"queue1": {
							UID:               "queue1",
							Name:              "queue1",
							ParentQueue:       "",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  1,
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
						rs.GpuResource: 2,
					},
					expectedResult: true,
				},
				"single queue - limited queue - results above limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"queue1": {
							UID:               "queue1",
							Name:              "queue1",
							ParentQueue:       "",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  1,
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
						rs.GpuResource: 3,
					},
					expectedResult: false,
				},
				"single queue - limited queue - results above limit 0 allocated": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"queue1": {
							UID:               "queue1",
							Name:              "queue1",
							ParentQueue:       "",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  0,
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
						rs.GpuResource: 4,
					},
					expectedResult: false,
					expectedDetails: &v2alpha2.QuotaDetails{
						QueueAllocatedResources: v1.ResourceList{
							v1.ResourceCPU:              *resource.NewMilliQuantity(0, resource.DecimalSI),
							v1.ResourceMemory:           *resource.NewQuantity(0, resource.DecimalSI),
							constants.NvidiaGpuResource: *resource.NewQuantity(0, resource.DecimalSI),
						},
					},
				},
				"multiple queues - limited queues - results below limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  2,
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
									MaxAllowed: 3,
									Allocated:  2,
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
									MaxAllowed: 3,
									Allocated:  2,
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
				"multiple queues - limited queues - result above limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  3,
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
									MaxAllowed: 2,
									Allocated:  1,
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
									MaxAllowed: 1,
									Allocated:  0,
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
					result := capacityPolicy.resultsOverLimit(testData.requestedShare, testData.job)
					Expect(result.IsSchedulable).To(Equal(testData.expectedResult))
					if testData.expectedDetails != nil {
						expectedValues := reflect.ValueOf(*testData.expectedDetails)
						resultValues := reflect.ValueOf(*result.Details.QueueDetails)
						for i := 0; i < expectedValues.NumField(); i++ {
							xField := expectedValues.Field(i).Interface()
							yField := resultValues.Field(i).Interface()
							zero := reflect.Zero(expectedValues.Field(i).Type()).Interface()
							if !reflect.DeepEqual(xField, zero) {
								Expect(xField).To(Equal(yField))
							}
						}
					}
				})
			}

		})
	})
})
