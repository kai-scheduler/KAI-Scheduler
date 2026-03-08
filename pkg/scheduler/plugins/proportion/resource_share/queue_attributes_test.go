// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

func TestQueueAttributes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "QueueAttributes test")
}

var allUnlimited = ResourceQuantities{
	CpuResource:    commonconstants.UnlimitedResourceQuantity,
	MemoryResource: commonconstants.UnlimitedResourceQuantity,
	GpuResource:    commonconstants.UnlimitedResourceQuantity,
}

var _ = Describe("QueueAttributes", func() {
	var (
		queueAttributes *QueueAttributes
	)
	BeforeEach(func() {
		queueAttributes = &QueueAttributes{
			QueueResourceShare: QueueResourceShare{
				CPU:    EmptyResource(),
				Memory: EmptyResource(),
				GPU:    EmptyResource(),
			},
		}
	})

	Describe("GetRequestedResource", func() {
		tests := map[string]map[string]struct {
			queueAttributes *QueueAttributes
			expected        float64
		}{
			string(GpuResource): {
				"maxAllowed > requested": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							GPU: ResourceShare{
								Request:    5,
								MaxAllowed: 6,
							},
						},
					},
					expected: 5,
				},
				"maxAllowed = requested": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							GPU: ResourceShare{
								Request:    5,
								MaxAllowed: 5,
							},
						},
					},
					expected: 5,
				},
				"maxAllowed < requested and not UnlimitedResourceQuantity": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							GPU: ResourceShare{
								Request:    5,
								MaxAllowed: 3,
							},
						},
					},
					expected: 3,
				},
				"maxAllowed is UnlimitedResourceQuantity": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							GPU: ResourceShare{
								Request:    5,
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
							},
						},
					},
					expected: 5,
				},
			},
			string(CpuResource): {
				"maxAllowed > requested": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							CPU: ResourceShare{
								Request:    5,
								MaxAllowed: 6,
							},
						},
					},
					expected: 5,
				},
				"maxAllowed = requested": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							CPU: ResourceShare{
								Request:    5,
								MaxAllowed: 5,
							},
						},
					},
					expected: 5,
				},
				"maxAllowed < requested and not UnlimitedResourceQuantity": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							CPU: ResourceShare{
								Request:    5,
								MaxAllowed: 3,
							},
						},
					},
					expected: 3,
				},
				"maxAllowed is UnlimitedResourceQuantity": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							CPU: ResourceShare{
								Request:    5,
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
							},
						},
					},
					expected: 5,
				},
			},
			string(MemoryResource): {
				"maxAllowed > requested": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							Memory: ResourceShare{
								Request:    5,
								MaxAllowed: 6,
							},
						},
					},
					expected: 5,
				},
				"maxAllowed = requested": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							Memory: ResourceShare{
								Request:    5,
								MaxAllowed: 5,
							},
						},
					},
					expected: 5,
				},
				"maxAllowed < requested and not UnlimitedResourceQuantity": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							Memory: ResourceShare{
								Request:    5,
								MaxAllowed: 3,
							},
						},
					},
					expected: 3,
				},
				"maxAllowed is UnlimitedResourceQuantity": {
					queueAttributes: &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							Memory: ResourceShare{
								Request:    5,
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
							},
						},
					},
					expected: 5,
				},
			},
		}
		for contextName, contextData := range tests {
			contextName := contextName
			contextData := contextData
			Context(contextName, func() {
				for testName, testData := range contextData {
					resourceName := contextName
					testName := testName
					testData := testData
					It(testName, func() {
						resourceShare := testData.queueAttributes.ResourceShare(ResourceName(resourceName))
						if resourceShare != nil {
							Expect(resourceShare.GetRequestableShare()).To(Equal(testData.expected))
						}
					})
				}
			})
		}
	})

	Describe("DominantResource", func() {
		Context("test", func() {
			tests := map[string]struct {
				deserved      ResourceQuantities
				fairShare     ResourceQuantities
				allocated     ResourceQuantities
				totalCapacity ResourceQuantities
				expected      float64
			}{
				"CPU is most dominant": {
					deserved:  ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					fairShare: ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					allocated: ResourceQuantities{CpuResource: 500, MemoryResource: 100, GpuResource: 200},
					expected:  500.0 / 1000.0,
				},
				"Memory is most dominant": {
					deserved:  ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					fairShare: ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					allocated: ResourceQuantities{CpuResource: 500, MemoryResource: 600, GpuResource: 0},
					expected:  600.0 / 1000.0,
				},
				"GPU is most dominant": {
					deserved:  ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					fairShare: ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					allocated: ResourceQuantities{CpuResource: 500, MemoryResource: 600, GpuResource: 700},
					expected:  700.0 / 1000.0,
				},
				"CPU allocated but not deserved": {
					deserved:  ResourceQuantities{CpuResource: 0, MemoryResource: 1000, GpuResource: 1000},
					fairShare: ResourceQuantities{CpuResource: 0, MemoryResource: 1000, GpuResource: 1000},
					allocated: ResourceQuantities{CpuResource: 500, MemoryResource: 0, GpuResource: 700},
					expected:  500.0 * noFairShareDrfMultiplier,
				},
				"unlimited deserved GPU means the ratio is from total capacity - CPU dominant": {
					deserved:      ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: commonconstants.UnlimitedResourceQuantity},
					fairShare:     ResourceQuantities{CpuResource: 500, MemoryResource: 1000, GpuResource: 1500},
					allocated:     ResourceQuantities{CpuResource: 500, MemoryResource: 0, GpuResource: 700},
					totalCapacity: ResourceQuantities{CpuResource: 10000, MemoryResource: 10000, GpuResource: 2000},
					expected:      500.0 / 1000.0,
				},
				"unlimited deserved GPU means the ratio is from total capacity - GPU dominant": {
					deserved:      ResourceQuantities{CpuResource: 10000, MemoryResource: 1000, GpuResource: commonconstants.UnlimitedResourceQuantity},
					fairShare:     ResourceQuantities{CpuResource: 500, MemoryResource: 1000, GpuResource: 1500},
					allocated:     ResourceQuantities{CpuResource: 500, MemoryResource: 0, GpuResource: 700},
					totalCapacity: ResourceQuantities{CpuResource: 10000, MemoryResource: 10000, GpuResource: 2000},
					expected:      700.0 / 2000.0,
				},
			}

			for testName, testData := range tests {
				testName := testName
				testData := testData
				It(testName, func() {
					queueAttributes := &QueueAttributes{
						QueueResourceShare: QueueResourceShare{
							GPU:    ResourceShare{},
							CPU:    ResourceShare{},
							Memory: ResourceShare{},
						},
					}
					for _, resourceName := range AllResources {
						resourceShare := queueAttributes.ResourceShare(resourceName)
						resourceShare.Deserved = testData.deserved[resourceName]
						resourceShare.FairShare = testData.fairShare[resourceName]
						resourceShare.Allocated = testData.allocated[resourceName]
						resourceShare.MaxAllowed = commonconstants.UnlimitedResourceQuantity
					}
					Expect(queueAttributes.GetDominantResourceShare(testData.totalCapacity)).To(Equal(testData.expected))
				})
			}
		})
	})

	Describe("GetAllocatableShare", func() {
		Context("sanity", func() {
			tests := map[string]struct {
				deserved   ResourceQuantities
				fairShare  ResourceQuantities
				maxAllowed ResourceQuantities
				expected   ResourceQuantities
			}{
				"maxAllowed is limiting all resources": {
					deserved:   ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					fairShare:  ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					maxAllowed: ResourceQuantities{CpuResource: 500, MemoryResource: 100, GpuResource: 200},
					expected:   ResourceQuantities{CpuResource: 500, MemoryResource: 100, GpuResource: 200},
				},
				"maxAllowed is limiting some resources": {
					deserved:   ResourceQuantities{CpuResource: 1000, MemoryResource: 1000, GpuResource: 1000},
					fairShare:  ResourceQuantities{CpuResource: 1500, MemoryResource: 1500, GpuResource: 1500},
					maxAllowed: ResourceQuantities{CpuResource: 2000, MemoryResource: 600, GpuResource: 2000},
					expected:   ResourceQuantities{CpuResource: 1500, MemoryResource: 600, GpuResource: 1500},
				},
				"maxAllowed is limiting some, deserved is dominant in other": {
					deserved:   ResourceQuantities{CpuResource: 2000, MemoryResource: 1000, GpuResource: 1000},
					fairShare:  ResourceQuantities{CpuResource: 1000, MemoryResource: 1500, GpuResource: 1500},
					maxAllowed: ResourceQuantities{CpuResource: 3000, MemoryResource: 600, GpuResource: 3000},
					expected:   ResourceQuantities{CpuResource: 2000, MemoryResource: 600, GpuResource: 1500},
				},
				"maxAllowed is unlimited": {
					deserved:   ResourceQuantities{CpuResource: 2000, MemoryResource: 1000, GpuResource: 1000},
					fairShare:  ResourceQuantities{CpuResource: 1000, MemoryResource: 1500, GpuResource: 1500},
					maxAllowed: allUnlimited,
					expected:   ResourceQuantities{CpuResource: 2000, MemoryResource: 1500, GpuResource: 1500},
				},
				"deserved is unlimited maxAllowed is sometimes not": {
					deserved:   allUnlimited,
					fairShare:  ResourceQuantities{CpuResource: 1000, MemoryResource: 1500, GpuResource: 1500},
					maxAllowed: ResourceQuantities{CpuResource: 2000, MemoryResource: commonconstants.UnlimitedResourceQuantity, GpuResource: 1000},
					expected:   ResourceQuantities{CpuResource: 2000, MemoryResource: commonconstants.UnlimitedResourceQuantity, GpuResource: 1000},
				},
			}

			for testName, testData := range tests {
				testName := testName
				testData := testData
				It(testName, func() {
					for _, resourceName := range AllResources {
						resourceShare := queueAttributes.ResourceShare(resourceName)
						resourceShare.Deserved = testData.deserved[resourceName]
						resourceShare.FairShare = testData.fairShare[resourceName]
						resourceShare.MaxAllowed = testData.maxAllowed[resourceName]
					}
					allocatableShare := queueAttributes.GetAllocatableShare()
					for _, resource := range AllResources {
						Expect(allocatableShare[resource]).To(Equal(testData.expected[resource]))
					}
				})
			}
		})
	})

	Describe("Clone", func() {
		It("clone QueueAttributes struct", func() {
			orig := &QueueAttributes{
				UID:                "123",
				Name:               "some-name",
				ParentQueue:        "p1",
				ChildQueues:        []common_info.QueueID{"c1", "c2", "c3"},
				CreationTimestamp:  metav1.Now(),
				QueueResourceShare: *createQueueResourceShare(),
			}
			clone := orig.Clone()

			Expect(orig.UID).To(Equal(clone.UID))
			Expect(orig.Name).To(Equal(clone.Name))
			Expect(orig.ParentQueue).To(Equal(clone.ParentQueue))
			Expect(orig.ChildQueues).To(Equal(clone.ChildQueues))
			Expect(orig.CreationTimestamp).To(Equal(clone.CreationTimestamp))
			Expect(orig.QueueResourceShare).To(Equal(clone.QueueResourceShare))
		})
	})
})
