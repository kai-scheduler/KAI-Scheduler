// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package strategies

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

func TestReclaimStrategies(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reclaim strategies tests")
}

var _ = Describe("Reclaim strategies", func() {
	Context("Maintain Fair Share Strategy", func() {
		tests := map[string]struct {
			reclaimerQueue *rs.QueueAttributes
			reclaimeeQueue *rs.QueueAttributes
			expected       bool
		}{
			"Reclaimee has less than quota": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   5,
							FairShare:  5,
							Allocated:  0,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   3,
							FairShare:  4,
							Allocated:  1,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},

				expected: false,
			},
			"Reclaimee has less than fair share": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   5,
							FairShare:  5,
							Allocated:  0,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   3,
							FairShare:  4,
							Allocated:  3,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},

				expected: false,
			},
			"Reclaimee has exactly fair share and above quota": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   5,
							FairShare:  5,
							Allocated:  0,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   1,
							FairShare:  3,
							Allocated:  3,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: false,
			},
			"Reclaimee has exactly fair share and equal to quota": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   5,
							FairShare:  5,
							Allocated:  0,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   3,
							FairShare:  3,
							Allocated:  3,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: false,
			},
			"Reclaimee has more than fair share": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   5,
							FairShare:  5,
							Allocated:  0,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   2,
							FairShare:  3,
							Allocated:  4,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: true,
			},
		}
		strategy := &MaintainFairShareStrategy{}
		for testName, testData := range tests {
			testName := testName
			testData := testData
			It(testName, func() {
				remainingResourceShare := testData.reclaimeeQueue.GetAllocatedShare()
				reclaimable := strategy.Reclaimable(nil, testData.reclaimerQueue, testData.reclaimeeQueue,
					remainingResourceShare)
				Expect(testData.expected).To(Equal(reclaimable))
			})
		}
	})

	Context("Maintain Fair Share Strategy - Multi Resource", func() {
		tests := map[string]struct {
			reclaimerQueue         *rs.QueueAttributes
			reclaimeeQueue         *rs.QueueAttributes
			remainingResourceShare rs.ResourceQuantities
			expected               bool
		}{
			"deserved less than fair share. GPU under, CPU over, Memory over": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   100,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   100,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   100 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    80,
					rs.CpuResource:    220,
					rs.MemoryResource: 220 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved less than fair share. GPU under, CPU under, Memory over": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   100,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   100,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   100 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    220,
					rs.CpuResource:    80,
					rs.MemoryResource: 220 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved less than fair share. GPU over, CPU over, Memory under": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   100,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   100,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   100 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    220,
					rs.CpuResource:    220,
					rs.MemoryResource: 80 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved equal to fair share. GPU under, CPU over, Memory over": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    80,
					rs.CpuResource:    220,
					rs.MemoryResource: 220 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved equal to fair share. GPU over, CPU under, Memory over": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    220,
					rs.CpuResource:    80,
					rs.MemoryResource: 220 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved equal to fair share. GPU over, CPU over, Memory under": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    220,
					rs.CpuResource:    220,
					rs.MemoryResource: 80 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved equal to fair share. GPU over, CPU under, Memory under": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    220,
					rs.CpuResource:    120,
					rs.MemoryResource: 120 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved equal to fair share. GPU under, CPU over, Memory under": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    120,
					rs.CpuResource:    220,
					rs.MemoryResource: 120 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved equal to fair share. GPU under, CPU under, Memory over": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    120,
					rs.CpuResource:    120,
					rs.MemoryResource: 220 * resource_info.MinMemory,
				},
				expected: true,
			},
			"deserved equal to fair share. GPU under, CPU under, Memory under": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: commonconstants.UnlimitedResourceQuantity,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    120,
					rs.CpuResource:    120,
					rs.MemoryResource: 120 * resource_info.MinMemory,
				},
				expected: false,
			},
			"Max Allowed is lower than Deserved/FairShare": {
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "reclaimee-1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: 100,
						},
						CPU: rs.ResourceShare{
							Deserved:   200,
							FairShare:  200,
							MaxAllowed: 100,
						},
						Memory: rs.ResourceShare{
							Deserved:   200 * resource_info.MinMemory,
							FairShare:  200 * resource_info.MinMemory,
							MaxAllowed: 100,
						},
					},
				},
				remainingResourceShare: rs.ResourceQuantities{
					rs.GpuResource:    50,
					rs.CpuResource:    120,
					rs.MemoryResource: 30 * resource_info.MinMemory,
				},
				expected: true,
			},
		}

		strategy := &MaintainFairShareStrategy{}
		for testName, testData := range tests {
			testName := testName
			testData := testData
			It(testName, func() {
				reclaimable := strategy.Reclaimable(nil, testData.reclaimerQueue, testData.reclaimeeQueue,
					testData.remainingResourceShare)
				Expect(reclaimable).To(Equal(testData.expected))
			})
		}
	})

	Context("Guarantee Deserved Quota Strategy", func() {
		tests := map[string]struct {
			reclaimerResources *resource_info.Resource
			reclaimerQueue     *rs.QueueAttributes
			reclaimeeQueue     *rs.QueueAttributes
			expected           bool
		}{
			"Reclaimer is above deserved quota and reclaimee above deserved quota": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  1,
							FairShare: 5,
							Allocated: 3,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  1,
							FairShare: 5,
							Allocated: 3,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: false,
			},
			"Reclaimer reaches exactly deserved quota and reclaimee above deserved quota": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  5,
							FairShare: 5,
							Allocated: 3,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  1,
							FairShare: 5,
							Allocated: 4,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: true,
			},
			"Reclaimer gets exactly deserved quota and reclaimee remains with exactly deserved quota": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  5,
							FairShare: 5,
							Allocated: 3,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  1,
							FairShare: 3,
							Allocated: 3,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: true,
			},
			"Reclaimer gets exactly deserved quota and reclaimee goes below deserved quota": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  4,
							FairShare: 5,
							Allocated: 2,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  3,
							FairShare: 5,
							Allocated: 4,
						},
						CPU: rs.ResourceShare{
							Deserved:  1,
							FairShare: 1,
							Allocated: 0,
						},
						Memory: rs.ResourceShare{
							Deserved:  1,
							FairShare: 1,
							Allocated: 0,
						},
					},
				},
				expected: true,
			},
			"Reclaimer is below deserved quota and reclaimee is above deserved quota in *only one* of the resources": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  5,
							FairShare: 2,
							Allocated: 2,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  1,
							FairShare: 3,
							Allocated: 3,
						},
						CPU: rs.ResourceShare{
							Deserved:  100, // cpu is below deserved (0<100) while GPUs are above deserved (3>1)
							FairShare: 100,
						},
						Memory: rs.ResourceShare{},
					},
				},
				expected: true,
			},
			"Reclaimer is below deserved quota and reclaimee has exactly deserved quota": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  5,
							FairShare: 2,
							Allocated: 2,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  3,
							FairShare: 3,
							Allocated: 3,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: false,
			},
			"Reclaimer is below deserved quota and reclaimee is below deserved quota": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  5,
							FairShare: 2,
							Allocated: 2,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  3,
							FairShare: 2,
							Allocated: 2,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: false,
			},
			"Zero quota queue gives up on all resources in favor of a starved queue": {
				reclaimerResources: resource_info.NewResource(0, 0, 2),
				reclaimerQueue: &rs.QueueAttributes{
					Name: "p1",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  4,
							FairShare: 4,
							Allocated: 2,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				reclaimeeQueue: &rs.QueueAttributes{
					Name: "p2",
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							Deserved:  0,
							FairShare: 2,
							Allocated: 2,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				expected: true,
			},
		}

		strategy := &GuaranteeDeservedQuotaStrategy{}
		for testName, testData := range tests {
			testName := testName
			testData := testData
			It(testName, func() {
				reclaimable := strategy.Reclaimable(testData.reclaimerResources, testData.reclaimerQueue,
					testData.reclaimeeQueue, testData.reclaimeeQueue.GetAllocatedShare())
				Expect(testData.expected).To(Equal(reclaimable))
			})
		}
	})
})
