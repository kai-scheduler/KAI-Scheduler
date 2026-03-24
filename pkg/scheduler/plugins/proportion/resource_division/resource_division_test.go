// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_division

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

func TestProportion(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proportion test")
}

var _ = Describe("Proportion", func() {
	Describe("setResourceShare - GPU", func() {
		Context("single queue within quota (sanity)", func() {
			var (
				queues map[common_info.QueueID]*rs.QueueAttributes
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								Deserved:        3,
								FairShare:       0,
								OverQuotaWeight: 0,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         2,
							},
							CPU:    rs.EmptyResource(),
							Memory: rs.EmptyResource(),
						},
					},
				}
			})
			It("gives requested GPUs - no remaining", func() {
				remaining := setResourceShare(2, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(float64(0)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(2)))
			})
			It("gives requested GPUs with remaining", func() {
				remaining := setResourceShare(3, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(float64(1)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(2)))
			})
			It("doesn't give more GPUs than allowed", func() {
				queues["1"].GPU.MaxAllowed = 2
				remaining := setResourceShare(3, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(float64(1)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(2)))
			})
			It("gives up to deserved when over subscribed", func() {
				remaining := setResourceShare(1, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(float64(0)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(2)))
			})
			It("gives up to full quota", func() {
				queues["1"].GPU.Request = float64(5)
				remaining := setResourceShare(7, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(float64(4)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(3)))
			})
			It("gives with fraction, if stated in deserved", func() {
				queues["1"].GPU.Deserved = 1.5
				remaining := setResourceShare(2, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(0.5))
				Expect(queues["1"].GPU.FairShare).To(Equal(1.5))
			})
			It("gives with fraction, if stated in request", func() {
				queues["1"].GPU.Request = 1.5
				remaining := setResourceShare(2, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(0.5))
				Expect(queues["1"].GPU.FairShare).To(Equal(1.5))
			})
			It("doesn't give if deserved is zero", func() {
				queues["1"].GPU.Deserved = float64(0)
				remaining := setResourceShare(2, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(float64(2)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(0)))
			})
		})

		Context("single queue over quota (sanity)", func() {
			var (
				queues map[common_info.QueueID]*rs.QueueAttributes
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								Deserved:        3,
								FairShare:       3,
								OverQuotaWeight: 1,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5,
							},
							CPU:    rs.EmptyResource(),
							Memory: rs.EmptyResource(),
						},
					},
				}
			})
			It("gives requested GPUs no remaining", func() {
				remaining := divideOverQuotaResource(2, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(0)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(5)))
			})
			It("gives requested GPUs with remaining", func() {
				remaining := divideOverQuotaResource(3, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(1)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(5)))
			})
			It("doesn't give more GPUs than allowed", func() {
				queues["1"].GPU.MaxAllowed = 4
				remaining := divideOverQuotaResource(2, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(1)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(4)))
			})
			It("doesn't give when over quota weight is zero", func() {
				queues["1"].GPU.OverQuotaWeight = 0
				remaining := divideOverQuotaResource(2, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(2)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(3)))
			})
			It("gives with fraction if requested", func() {
				queues["1"].GPU.Request = 4.5
				remaining := divideOverQuotaResource(2, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(0.5))
				Expect(queues["1"].GPU.FairShare).To(Equal(4.5))
			})
			It("gives remainder fraction", func() {
				remaining := divideOverQuotaResource(0.5, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(0)))
				Expect(queues["1"].GPU.FairShare).To(Equal(3.5))
			})
			It("gives when deserved is zero", func() {
				queues["1"].GPU.Deserved = float64(0)
				queues["1"].GPU.FairShare = float64(0)
				remaining := divideOverQuotaResource(6, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(1)))
				Expect(queues["1"].GPU.FairShare).To(Equal(float64(5)))
			})
			It("doesn't break when there are zero resources to divide", func() {
				origFairShare := queues["1"].GPU.FairShare
				remaining := divideOverQuotaResource(0, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(0)))
				Expect(queues["1"].GPU.FairShare).To(Equal(origFairShare))
			})
			It("doesn't give when over quota weight is zero", func() {
				origFairShare := queues["1"].GPU.FairShare
				queues["1"].GPU.OverQuotaWeight = float64(0)
				remaining := divideOverQuotaResource(10, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(10)))
				Expect(queues["1"].GPU.FairShare).To(Equal(origFairShare))
			})
		})

		Context("two queues over quota (sanity)", func() {
			var (
				queues map[common_info.QueueID]*rs.QueueAttributes
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								Deserved:        3,
								FairShare:       3,
								OverQuotaWeight: 1,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5,
							},
							CPU:    rs.EmptyResource(),
							Memory: rs.EmptyResource(),
						},
					},
					"2": {
						UID:  "2",
						Name: "queue-2",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								Deserved:        3,
								FairShare:       3,
								OverQuotaWeight: 1,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         3,
							},
							CPU:    rs.EmptyResource(),
							Memory: rs.EmptyResource(),
						},
					},
				}
			})
			It("doesn't give when over quota weight is zero", func() {
				origFairShare := queues["1"].GPU.FairShare
				queues["1"].GPU.OverQuotaWeight = float64(0)
				remaining := divideOverQuotaResource(10, 0, queues, rs.GpuResource)
				Expect(remaining).To(Equal(float64(10)))
				Expect(queues["1"].GPU.FairShare).To(Equal(origFairShare))
			})
		})

		Context("two queues", func() {
			type testMetadata struct {
				totalGPUs           float64
				request             map[common_info.QueueID]float64
				maxAllowed          map[common_info.QueueID]float64
				gpuOverQuotaWeights map[common_info.QueueID]float64
				overQuotaPriority   map[common_info.QueueID]int
				expectedRemaining   float64
				expectedShare       map[common_info.QueueID]float64
			}

			getQueues := func() map[common_info.QueueID]*rs.QueueAttributes {
				return map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:               "1",
						Name:              "queue-1",
						CreationTimestamp: v1.Now(),
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								Deserved:        2,
								FairShare:       0,
								OverQuotaWeight: 2,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         6,
							},
							CPU:    rs.EmptyResource(),
							Memory: rs.EmptyResource(),
						},
					},
					"2": {
						UID:               "2",
						Name:              "queue-2",
						CreationTimestamp: v1.Now(),
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								Deserved:        2,
								FairShare:       0,
								OverQuotaWeight: 2,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         6,
							},
							CPU:    rs.EmptyResource(),
							Memory: rs.EmptyResource(),
						},
					},
				}
			}
			DescribeTable("two queues", func(testData testMetadata) {
				queues := getQueues()
				for id, newMaxAllowed := range testData.maxAllowed {
					queues[id].GPU.MaxAllowed = newMaxAllowed
				}
				for id, newGPUOverQuotaWeight := range testData.gpuOverQuotaWeights {
					queues[id].GPU.OverQuotaWeight = newGPUOverQuotaWeight
				}
				for id, request := range testData.request {
					queues[id].GPU.Request = request
				}
				for id, priority := range testData.overQuotaPriority {
					queues[id].Priority = priority
				}

				remaining := setResourceShare(testData.totalGPUs, 0, rs.GpuResource, queues)
				Expect(remaining).To(Equal(testData.expectedRemaining), "Expected remaining to equal %.2f, but got %.2f", testData.expectedRemaining, remaining)
				for uuid, expectedShare := range testData.expectedShare {
					Expect(queues[uuid].GPU.FairShare).To(Equal(expectedShare), "Expected share for queue %s to equal %.2f, but got %.2f", uuid, expectedShare, queues[uuid].GPU.FairShare)
				}
			},
				Entry("allocates many available gpus", testMetadata{
					totalGPUs:         15,
					expectedRemaining: 3,
					expectedShare: map[common_info.QueueID]float64{
						"1": 6,
						"2": 6,
					},
				}),
				Entry("allocates exact requested amount gpus", testMetadata{
					totalGPUs:         12,
					expectedRemaining: 0,
					expectedShare: map[common_info.QueueID]float64{
						"1": 6,
						"2": 6,
					},
				}),
				Entry("allocates GPUs proportionally", testMetadata{
					totalGPUs:         8,
					expectedRemaining: 0,
					gpuOverQuotaWeights: map[common_info.QueueID]float64{
						"1": 1,
						"2": 3,
					},
					expectedShare: map[common_info.QueueID]float64{
						"1": 3, // 2 deserved + 1 over quota
						"2": 5, // 2 deserved + 3 over quota
					},
				}),
				Entry("doesn't allocate more than allowed", testMetadata{
					totalGPUs: 12,
					maxAllowed: map[common_info.QueueID]float64{
						"1": 5,
					},
					expectedRemaining: 1,
					expectedShare: map[common_info.QueueID]float64{
						"1": 5, // 2 deserved + 3 over quota
						"2": 6, // 2 deserved + 4 over quota
					},
				}),
				Entry("distributes remainder between queues based on largest remaining", testMetadata{
					totalGPUs: 11,
					gpuOverQuotaWeights: map[common_info.QueueID]float64{
						"1": 1,
						"2": 4,
					},
					request: map[common_info.QueueID]float64{
						"1": 10,
						"2": 10,
					},
					expectedRemaining: 0,
					expectedShare: map[common_info.QueueID]float64{
						"1": 3, // 2 deserved + 7*1/5 over quota = 2 + [1.4] = 2 + 1 = 3 (remaining 0.4)
						"2": 8, // 2 deserved + 7*4/5 over quota = 2 + [5.6] = 2 + 5 = 7 (remaining 0.6) + 1 (has largest remaining)
					},
				}),
				Entry("distributes remainder between queues based on creation time", testMetadata{
					totalGPUs:         11,
					expectedRemaining: 0,
					expectedShare: map[common_info.QueueID]float64{
						"1": 6, // 2 deserved + 4 over quota
						"2": 5, // 2 deserved + 3 over quota
					},
				}),
				Entry("priority doesn't affect deserved", testMetadata{
					totalGPUs:         4,
					expectedRemaining: 0,
					overQuotaPriority: map[common_info.QueueID]int{
						"1": 1,
						"2": 2,
					},
					expectedShare: map[common_info.QueueID]float64{
						"1": 2, // 2 deserved + 0 over quota
						"2": 2, // 2 deserved + 0 over quota
					},
				}),
				Entry("priority does affect over-quota", testMetadata{
					totalGPUs:         6,
					expectedRemaining: 0,
					overQuotaPriority: map[common_info.QueueID]int{
						"1": 1,
						"2": 2,
					},
					expectedShare: map[common_info.QueueID]float64{
						"1": 2, // 2 deserved + 0 over quota
						"2": 4, // 2 deserved + 2 over quota
					},
				}),
				Entry("priority does affect over-quota, even when overQuotaWeight is set", testMetadata{
					totalGPUs:         6,
					expectedRemaining: 0,
					gpuOverQuotaWeights: map[common_info.QueueID]float64{
						"1": 100,
						"2": 1,
					},
					overQuotaPriority: map[common_info.QueueID]int{
						"1": 1,
						"2": 2,
					},
					expectedShare: map[common_info.QueueID]float64{
						"1": 2, // 2 deserved + 0 over quota
						"2": 4, // 2 deserved + 2 over quota
					},
				}),
			)
		})

		It("divides the remainder even when using priorities", func() {
			queues := map[common_info.QueueID]*rs.QueueAttributes{
				"1": {
					UID:               "1",
					Name:              "queue-1",
					CreationTimestamp: v1.Now(),
					Priority:          2,
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:        2,
							FairShare:       0,
							OverQuotaWeight: 2,
							MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
							Allocated:       0,
							Request:         5,
						},
						CPU:    rs.EmptyResource(),
						Memory: rs.EmptyResource(),
					},
				},
				"2": {
					UID:               "2",
					Name:              "queue-2",
					CreationTimestamp: v1.Now(),
					Priority:          2,
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:        2,
							FairShare:       0,
							OverQuotaWeight: 2,
							MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
							Allocated:       0,
							Request:         5,
						},
						CPU:    rs.EmptyResource(),
						Memory: rs.EmptyResource(),
					},
				},
				"3": {
					UID:               "3",
					Name:              "queue-3",
					CreationTimestamp: v1.Now(),
					Priority:          1,
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:        1,
							FairShare:       0,
							OverQuotaWeight: 2,
							MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
							Allocated:       0,
							Request:         0,
						},
						CPU:    rs.EmptyResource(),
						Memory: rs.EmptyResource(),
					},
				},
			}

			expectedShare := map[common_info.QueueID]float64{
				"1": 3,
				"2": 2,
				"3": 0,
			}

			remaining := setResourceShare(5, 0, rs.GpuResource, queues)
			Expect(remaining).To(Equal(0.0), "Expected remaining to equal %.2f, but got %.2f", 0, remaining)
			for uuid, expectedShare := range expectedShare {
				Expect(queues[uuid].GPU.FairShare).To(Equal(expectedShare), "Expected share for queue %s to equal %.2f, but got %.2f", uuid, expectedShare, queues[uuid].GPU.FairShare)
			}
		})
	})

	Describe("setResourceShare - CPU", func() {
		Context("one queue", func() {
			var (
				queues map[common_info.QueueID]*rs.QueueAttributes
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.ResourceShare{
								Deserved:        3000,
								FairShare:       0,
								OverQuotaWeight: 3000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         2000,
							},
							Memory: rs.EmptyResource(),
						},
					},
				}
			})
			It("gives requested CPUs to queue", func() {
				remaining := setResourceShare(3000, 0, rs.CpuResource, queues)
				Expect(remaining).To(Equal(float64(1000)))
				Expect(queues["1"].CPU.FairShare).To(Equal(float64(2000)))
			})
			It("gives deserved in over subscription", func() {
				remaining := setResourceShare(1000, 0, rs.CpuResource, queues)
				Expect(remaining).To(Equal(float64(0)))
				Expect(queues["1"].CPU.FairShare).To(Equal(float64(2000)))
			})
		})
		Context("two queues", func() {
			var (
				queues            map[common_info.QueueID]*rs.QueueAttributes
				totalCPUResources float64 = 10000
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         10000,
							},
							Memory: rs.EmptyResource(),
						},
					},
					"2": {
						UID:  "2",
						Name: "queue-2",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.ResourceShare{
								Deserved:        0,
								FairShare:       0,
								OverQuotaWeight: 0,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       5000,
								Request:         5000,
							},
							Memory: rs.EmptyResource(),
						},
					},
				}
			})
			When("queue 2 has no deserved CPU", func() {
				It("reclaims all resources of queue 2", func() {
					remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
					Expect(remaining).To(Equal(float64(0)))
					Expect(queues["2"].CPU.FairShare).To(Equal(float64(0)))
					Expect(queues["1"].CPU.FairShare).To(Equal(totalCPUResources))
				})
			})
		})
		Context("three queues", func() {
			var (
				queues            map[common_info.QueueID]*rs.QueueAttributes
				totalCPUResources float64 = 10000
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5000,
							},
							Memory: rs.EmptyResource(),
						},
					},
					"2": {
						UID:  "2",
						Name: "queue-2",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5000,
							},
							Memory: rs.EmptyResource(),
						},
					},
					"3": {
						UID:  "3",
						Name: "queue-3",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5000,
							},
							Memory: rs.EmptyResource(),
						},
					},
				}
			})
			Context("no pre-allocated resources", func() {
				When("all queues deserve the same fairShare of CPU", func() {
					When("all queues request more than they deserve", func() {
						It("all queues get their deserved fairShare - over subscription", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(5000)))
						})
					})
					When("all queues request more than they deserve", func() {
						BeforeEach(func() {
							queues["1"].CPU.Request = 6000
							queues["2"].CPU.Request = 6000
							queues["3"].CPU.Request = 6000
						})
						It("all queues get their deserved fairShare - over quota", func() {
							remaining := setResourceShare(18000, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(6000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(6000)))
						})
					})
					When("one queue request more than it deserves", func() {
						BeforeEach(func() {
							queues["1"].CPU.Request = 7000
							queues["2"].CPU.Request = 2000
							queues["3"].CPU.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(2000)))
						})

						It("reclaims from both allocated queues to fulfill the last unallocated queue", func() {
							queues["2"].CPU.Request = 5000
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000))) // 5000 deserved (requested 7000)
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(5000))) // 5000 deserved and requested
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(2000))) // 2000 as requested
						})
					})
					When("one queue request more than it deserves and allowed", func() {
						BeforeEach(func() {
							queues["1"].CPU.Request = 7000
							queues["1"].CPU.MaxAllowed = 7000
							queues["2"].CPU.Request = 2000
							queues["3"].CPU.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(6000))) // 5000 deserved + 100 over quota
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(2000)))
						})
					})
				})
				When("q2 has no deserved CPU but is allowed OQ", func() {
					BeforeEach(func() {
						queues["2"].CPU.Deserved = 0
						queues["2"].CPU.MaxAllowed = commonconstants.UnlimitedResourceQuantity
						queues["2"].GPU.Deserved = 0
						queues["2"].GPU.Deserved = commonconstants.UnlimitedResourceQuantity
					})

					When("q1 uses the max allowed CPU and q2 use all the rest as OQ", func() {
						It("reclaims all resources of queue 2 and allocates allowed resource in queue 3", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(0)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(5000)))
						})
					})
				})
			})
			Context("all resources allocated between 2 queues", func() {
				BeforeEach(func() {
					queues["1"].CPU.Allocated = 5000
					queues["2"].CPU.Allocated = 5000
				})
				When("all queues deserve the same fairShare of CPU", func() {
					When("all queues request more than they deserve", func() {
						It("they get deserved and compete on the available resources", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(5000)))
						})
					})
					When("one queue request more than it deserves but not allowed", func() {
						BeforeEach(func() {
							queues["1"].CPU.Request = 7000
							queues["2"].CPU.Request = 2000
							queues["3"].CPU.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(2000)))
						})
						It("reclaims from both allocated queues to fulfill the last unallocated queue", func() {
							queues["2"].CPU.Request = 5000
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(2000)))
						})
					})
					When("one queue request more than it deserves and allowed", func() {
						BeforeEach(func() {
							queues["1"].CPU.Request = 7000
							queues["1"].CPU.MaxAllowed = 7000
							queues["2"].CPU.Request = 2000
							queues["3"].CPU.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(2000)))
						})
						It("reclaims from both allocated queues to fulfill the last unallocated queue", func() {
							queues["2"].CPU.Request = 5000
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(2000)))
						})
					})
				})
				When("q2 has no deserved CPU but is allowed OQ", func() {
					BeforeEach(func() {
						queues["2"].CPU.Deserved = 0
						queues["2"].CPU.MaxAllowed = commonconstants.UnlimitedResourceQuantity
						queues["2"].GPU.Deserved = 0
						queues["2"].GPU.MaxAllowed = commonconstants.UnlimitedResourceQuantity
					})

					When("q1 uses the max allowed CPU and q2 use all the rest as OQ and q1,q3 request more than they deserve", func() {
						BeforeEach(func() {
							queues["1"].CPU.Request = 10000
							queues["3"].CPU.Request = 10000
						})
						It("reclaims all resources of queue 2 and allocates allowed resource in queue 3", func() {
							remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].CPU.FairShare).To(Equal(float64(5000)))
						})
					})
					It("reclaims all resources of queue 2 and allocates allowed resource in queue 3", func() {
						remaining := setResourceShare(totalCPUResources, 0, rs.CpuResource, queues)
						Expect(remaining).To(Equal(float64(0)))
						Expect(queues["1"].CPU.FairShare).To(Equal(float64(5000)))
						Expect(queues["3"].CPU.FairShare).To(Equal(float64(5000)))
					})
				})
			})
		})
	})

	Describe("setResourceShare - memory", func() {
		Context("one queue", func() {
			var (
				queues map[common_info.QueueID]*rs.QueueAttributes
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.EmptyResource(),
							Memory: rs.ResourceShare{
								Deserved:        3000,
								FairShare:       0,
								OverQuotaWeight: 3000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         2000,
							},
						},
					},
				}
			})
			It("gives requested memory to queue", func() {
				remaining := setResourceShare(3000, 0, rs.MemoryResource, queues)
				Expect(remaining).To(Equal(float64(1000)))
				Expect(queues["1"].Memory.FairShare).To(Equal(float64(2000)))
			})
			It("gives deserved in over subscription", func() {
				remaining := setResourceShare(1000, 0, rs.MemoryResource, queues)
				Expect(remaining).To(Equal(float64(0)))
				Expect(queues["1"].Memory.FairShare).To(Equal(float64(2000)))
			})
		})
		Context("two queues", func() {
			var (
				queues               map[common_info.QueueID]*rs.QueueAttributes
				totalMemoryResources float64 = 10000
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.EmptyResource(),
							Memory: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         10000,
							},
						},
					},
					"2": {
						UID:  "2",
						Name: "queue-2",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.EmptyResource(),
							Memory: rs.ResourceShare{
								Deserved:        0,
								FairShare:       0,
								OverQuotaWeight: 0,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       5000,
								Request:         5000,
							},
						},
					},
				}
			})
			When("queue 2 has no deserved memory", func() {
				It("reclaims all resources of queue 2", func() {
					remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
					Expect(remaining).To(Equal(float64(0)))
					Expect(queues["2"].Memory.FairShare).To(Equal(float64(0)))
					Expect(queues["1"].Memory.FairShare).To(Equal(totalMemoryResources))
				})
			})
		})
		Context("three queues", func() {
			var (
				queues               map[common_info.QueueID]*rs.QueueAttributes
				totalMemoryResources float64 = 10000
			)
			BeforeEach(func() {
				queues = map[common_info.QueueID]*rs.QueueAttributes{
					"1": {
						UID:  "1",
						Name: "queue-1",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.EmptyResource(),
							Memory: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5000,
							},
						},
					},
					"2": {
						UID:  "2",
						Name: "queue-2",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.EmptyResource(),
							Memory: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5000,
							},
						},
					},
					"3": {
						UID:  "3",
						Name: "queue-3",
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.EmptyResource(),
							CPU: rs.EmptyResource(),
							Memory: rs.ResourceShare{
								Deserved:        5000,
								FairShare:       0,
								OverQuotaWeight: 5000,
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Allocated:       0,
								Request:         5000,
							},
						},
					},
				}
			})
			Context("no pre-allocated resources", func() {
				When("all queues deserve the same fairShare of memory", func() {
					When("all queues request more than they deserve", func() {
						It("all queues get their deserved fairShare - over subscription", func() {
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(5000)))
						})
					})
					When("all queues request more than they deserve", func() {
						BeforeEach(func() {
							queues["1"].Memory.Request = 6000
							queues["2"].Memory.Request = 6000
							queues["3"].Memory.Request = 6000
						})
						It("all queues get their deserved fairShare - over quota", func() {
							remaining := setResourceShare(18000, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(6000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(6000)))
						})
					})
					When("one queue request more than it deserves", func() {
						BeforeEach(func() {
							queues["1"].Memory.Request = 7000
							queues["2"].Memory.Request = 2000
							queues["3"].Memory.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(2000)))
						})

						It("reclaims from both allocated queues to fulfill the last unallocated queue", func() {
							queues["2"].Memory.Request = 5000
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000))) // 5000 deserved (requested 7000)
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(5000))) // 5000 deserved and requested
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(2000))) // 2000 as requested
						})
					})
					When("one queue request more than it deserves and allowed", func() {
						BeforeEach(func() {
							queues["1"].Memory.Request = 7000
							queues["1"].Memory.MaxAllowed = 7000
							queues["2"].Memory.Request = 2000
							queues["3"].Memory.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(6000))) // 5000 deserved + 100 over quota
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(2000)))
						})
					})
				})
				When("q2 has no deserved memory but is allowed OQ", func() {
					BeforeEach(func() {
						queues["2"].GPU.Deserved = 0
						queues["2"].GPU.MaxAllowed = commonconstants.UnlimitedResourceQuantity
						queues["2"].Memory.Deserved = 0
						queues["2"].Memory.MaxAllowed = commonconstants.UnlimitedResourceQuantity
					})

					When("q1 uses the max allowed memory and q2 use all the rest as OQ", func() {
						It("reclaims all resources of queue 2 and allocates allowed resource in queue 3", func() {
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(0)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(5000)))
						})
					})
				})
			})
			Context("all resources allocated between 2 queues", func() {
				BeforeEach(func() {
					queues["1"].Memory.Allocated = 5000
					queues["2"].Memory.Allocated = 5000
				})
				When("all queues deserve the same fairShare of memory", func() {
					When("all queues request more than they deserve", func() {
						It("they get deserved and compete on the available resources", func() {
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(5000)))
						})
					})
					When("one queue request more than it deserves but not allowed", func() {
						BeforeEach(func() {
							queues["1"].Memory.Request = 7000
							queues["2"].Memory.Request = 2000
							queues["3"].Memory.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(2000)))
						})
						It("reclaims from both allocated queues to fulfill the last unallocated queue", func() {
							queues["2"].Memory.Request = 5000

							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(2000)))
						})
					})
					When("one queue request more than it deserves and allowed", func() {
						BeforeEach(func() {
							queues["1"].Memory.Request = 7000
							queues["1"].Memory.MaxAllowed = 7000
							queues["2"].Memory.Request = 2000
							queues["3"].Memory.Request = 2000
						})
						It("reclaims unneeded resources from an allocated queue to fulfill the unallocated queue", func() {
							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(6000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(2000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(2000)))
						})
						It("reclaims from both allocated queues to fulfill the last unallocated queue", func() {
							queues["2"].Memory.Request = 5000

							remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
							Expect(remaining).To(Equal(float64(0)))
							Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["2"].Memory.FairShare).To(Equal(float64(5000)))
							Expect(queues["3"].Memory.FairShare).To(Equal(float64(2000)))
						})
					})
				})
				When("q2 has no deserved memory but is allowed OQ", func() {
					BeforeEach(func() {
						queues["2"].GPU.Deserved = 0
						queues["2"].GPU.MaxAllowed = commonconstants.UnlimitedResourceQuantity
						queues["2"].Memory.Deserved = 0
						queues["2"].Memory.MaxAllowed = commonconstants.UnlimitedResourceQuantity
					})
				})

				When("q1 uses the max allowed memory and q2 use all the rest as OQ and q1,q3 request more than they deserve", func() {
					BeforeEach(func() {
						queues["1"].Memory.Request = 10000
						queues["3"].Memory.Request = 10000
					})
					It("reclaims all resources of queue 2 and allocates allowed resource in queue 3", func() {
						remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
						Expect(remaining).To(Equal(float64(0)))
						Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000)))
						Expect(queues["3"].Memory.FairShare).To(Equal(float64(5000)))
					})
				})
				It("reclaims all resources of queue 2 and allocates allowed resource in queue 3", func() {
					remaining := setResourceShare(totalMemoryResources, 0, rs.MemoryResource, queues)
					Expect(remaining).To(Equal(float64(0)))
					Expect(queues["1"].Memory.FairShare).To(Equal(float64(5000)))
					Expect(queues["3"].Memory.FairShare).To(Equal(float64(5000)))
				})
			})
		})
	})

	Describe("SetResourcesShare", func() {
		queueCreationTime := v1.Now()
		tests := map[string]map[string]struct {
			queues         map[common_info.QueueID]*rs.QueueAttributes
			totalResources rs.ResourceQuantities
			expectedShare  map[common_info.QueueID]*rs.QueueAttributes
		}{
			"one queue": {
				"Enough resources - should gets all request": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         1,
								},
								CPU: rs.ResourceShare{
									Deserved:        200,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         500,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         500,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.GpuResource:    10,
						rs.CpuResource:    2500,
						rs.MemoryResource: 1000,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 1,
								},
								CPU: rs.ResourceShare{
									FairShare: 500,
								},
								Memory: rs.ResourceShare{
									FairShare: 500,
								},
							},
						},
					},
				},
				"Not enough resources - should gets all cluster": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        2,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         3,
								},
								CPU: rs.ResourceShare{
									Deserved:        20000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         2500,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.GpuResource:    1,
						rs.CpuResource:    1000,
						rs.MemoryResource: 3000,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							// In over subscription (the GPU scenario) the fair share is the deserved of each,
							// we get a first-come first-serve policy
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 2,
								},
								CPU: rs.ResourceShare{
									FairShare: 2500,
								},
								Memory: rs.ResourceShare{
									FairShare: 3000,
								},
							},
						},
					},
				},
			},
			"two queues": {
				"Enough resources - should gets all request": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         1,
								},
								CPU: rs.ResourceShare{
									Deserved:        200,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         500,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         500,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         1,
								},
								CPU: rs.ResourceShare{
									Deserved:        200,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         500,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         500,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.MemoryResource: 1000,
						rs.GpuResource:    10,
						rs.CpuResource:    2500,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 1,
								},
								CPU: rs.ResourceShare{
									FairShare: 500,
								},
								Memory: rs.ResourceShare{
									FairShare: 500,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 1,
								},
								CPU: rs.ResourceShare{
									FairShare: 500,
								},
								Memory: rs.ResourceShare{
									FairShare: 500,
								},
							},
						},
					},
				},
				"Request more than total with same Over Quota and deserved - divide evenly": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         8,
								},
								CPU: rs.ResourceShare{
									Deserved:        2000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         3000,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         8,
								},
								CPU: rs.ResourceShare{
									Deserved:        2000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         3000,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.MemoryResource: 1000,
						rs.GpuResource:    10,
						rs.CpuResource:    3000,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						// The memory and CPU will have a first-come first-serve policy
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 5,
								},
								CPU: rs.ResourceShare{
									FairShare: 2000,
								},
								Memory: rs.ResourceShare{
									FairShare: 1000,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 5,
								},
								CPU: rs.ResourceShare{
									FairShare: 2000,
								},
								Memory: rs.ResourceShare{
									FairShare: 1000,
								},
							},
						},
					},
				},
				"Request more than total with different Over Quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        250,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
								CPU: rs.ResourceShare{
									Deserved:        250,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
								Memory: rs.ResourceShare{
									Deserved:        250,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        150,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
								CPU: rs.ResourceShare{
									Deserved:        150,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
								Memory: rs.ResourceShare{
									Deserved:        150,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.MemoryResource: 1000,
						rs.GpuResource:    1000,
						rs.CpuResource:    1000,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 450,
								},
								CPU: rs.ResourceShare{
									FairShare: 450,
								},
								Memory: rs.ResourceShare{
									FairShare: 450,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 550,
								},
								CPU: rs.ResourceShare{
									FairShare: 550,
								},
								Memory: rs.ResourceShare{
									FairShare: 550,
								},
							},
						},
					},
				},
				"Request more than total with different Over Quota and max allowed": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        250,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
								CPU: rs.ResourceShare{
									Deserved:        250,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
								Memory: rs.ResourceShare{
									Deserved:        250,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         5000,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        150,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      400,
									Allocated:       0,
									Request:         5000,
								},
								CPU: rs.ResourceShare{
									Deserved:        150,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      400,
									Allocated:       0,
									Request:         5000,
								},
								Memory: rs.ResourceShare{
									Deserved:        150,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      400,
									Allocated:       0,
									Request:         5000,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.MemoryResource: 1000,
						rs.GpuResource:    1000,
						rs.CpuResource:    1000,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 600,
								},
								CPU: rs.ResourceShare{
									FairShare: 600,
								},
								Memory: rs.ResourceShare{
									FairShare: 600,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 400,
								},
								CPU: rs.ResourceShare{
									FairShare: 400,
								},
								Memory: rs.ResourceShare{
									FairShare: 400,
								},
							},
						},
					},
				},
			},
			"three queues": {
				"divide resource evenly queues": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								CPU: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								CPU: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
							},
						},
						"3": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								CPU: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								Memory: rs.ResourceShare{
									Deserved:        1000,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.MemoryResource: 9999,
						rs.GpuResource:    9999,
						rs.CpuResource:    9999,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 3333,
								},
								CPU: rs.ResourceShare{
									FairShare: 3333,
								},
								Memory: rs.ResourceShare{
									FairShare: 3333,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 3333,
								},
								CPU: rs.ResourceShare{
									FairShare: 3333,
								},
								Memory: rs.ResourceShare{
									FairShare: 3333,
								},
							},
						},
						"3": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 3333,
								},
								CPU: rs.ResourceShare{
									FairShare: 3333,
								},
								Memory: rs.ResourceShare{
									FairShare: 3333,
								},
							},
						},
					},
				},
				"divide resource between queues with different over quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 6,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								CPU: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 6,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								Memory: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 6,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								CPU: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								Memory: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
							},
						},
						"3": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								CPU: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
								Memory: rs.ResourceShare{
									Deserved:        0,
									FairShare:       0,
									OverQuotaWeight: 1,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         10000,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.MemoryResource: 9999,
						rs.GpuResource:    9999,
						rs.CpuResource:    9999,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 6666,
								},
								CPU: rs.ResourceShare{
									FairShare: 6666,
								},
								Memory: rs.ResourceShare{
									FairShare: 6666,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 2222,
								},
								CPU: rs.ResourceShare{
									FairShare: 2222,
								},
								Memory: rs.ResourceShare{
									FairShare: 2222,
								},
							},
						},
						"3": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 1111,
								},
								CPU: rs.ResourceShare{
									FairShare: 1111,
								},
								Memory: rs.ResourceShare{
									FairShare: 1111,
								},
							},
						},
					},
				},
			},
			"advanced scenarios": {
				"fractional fair share": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        12,
									FairShare:       0,
									OverQuotaWeight: 12,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         32.54999999999996,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        7,
									FairShare:       0,
									OverQuotaWeight: 7,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         9.5,
								},
							},
						},
						"3": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        2,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      2,
									Allocated:       0,
									Request:         1.65,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.GpuResource: 45,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 32.54999999999996,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 9.5,
								},
							},
						},
						"3": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 1.65,
								},
							},
						},
					},
				},
				"divide last remaining GPU based to similar queues": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							CreationTimestamp: queueCreationTime,
							UID:               "queue-1",
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        2,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         3,
								},
							},
						},
						"2": {
							CreationTimestamp: queueCreationTime,
							UID:               "queue-2",
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									Deserved:        2,
									FairShare:       0,
									OverQuotaWeight: 2,
									MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
									Allocated:       0,
									Request:         3,
								},
							},
						},
					},
					totalResources: rs.ResourceQuantities{
						rs.GpuResource: 5,
					},
					expectedShare: map[common_info.QueueID]*rs.QueueAttributes{
						"1": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 3,
								},
							},
						},
						"2": {
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									FairShare: 2,
								},
							},
						},
					},
				},
			},
		}
		for contextName, contextData := range tests {
			cName := contextName
			cData := contextData
			Context(cName, func() {
				for testName, testData := range cData {
					testName := testName
					testData := testData
					It(testName, func() {
						for queueName, queue := range testData.queues {
							if len(queue.UID) == 0 {
								queue.UID = common_info.QueueID(queueName)
							}
						}
						SetResourcesShare(testData.totalResources, 0, testData.queues)
						for queueName, queue := range testData.expectedShare {
							fairShare := queue.GetFairShare()
							expectedFairShare := testData.queues[queueName].GetFairShare()
							for _, resource := range rs.AllResources {
								Expect(fairShare[resource]).To(Equal(expectedFairShare[resource]), "Expected queue %s %ss fair share to be %.2f, but got %.2f", queueName, resource, expectedFairShare[resource], fairShare[resource])
							}
							testData.queues[queueName].CPU.FairShare = 0 // Fix issue with re-running tests
							testData.queues[queueName].Memory.FairShare = 0
							testData.queues[queueName].GPU.FairShare = 0
						}
					})
				}
			})
		}
	})
})

var _ = Describe("getQueuesByPriority", func() {
	It("should return queues by priority", func() {
		queues := map[common_info.QueueID]*rs.QueueAttributes{
			"1": {
				Priority: 1,
			},
			"2": {
				Priority: 6,
			},
			"3": {
				Priority: 2,
			},
			"4": {
				Priority: 3,
			},
			"6": {
				Priority: 2,
			},
		}
		expectedResult := map[int]map[common_info.QueueID]*rs.QueueAttributes{
			6: {
				"2": {
					Priority: 6,
				},
			},
			3: {
				"4": {
					Priority: 3,
				},
			},
			2: {
				"3": {
					Priority: 2,
				},
				"6": {
					Priority: 2,
				},
			},
			1: {
				"1": {
					Priority: 1,
				},
			},
		}
		expectedPriorities := []int{6, 3, 2, 1}

		result, priorities := getQueuesByPriority(queues)
		Expect(result).To(Equal(expectedResult), "Expected queues to be sorted by priority")
		Expect(priorities).To(Equal(expectedPriorities), "Expected priorities to be sorted in descending order")
	})
})
