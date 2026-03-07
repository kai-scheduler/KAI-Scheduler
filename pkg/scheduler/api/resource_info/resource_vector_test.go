// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

const (
	resourceCPU    = string(v1.ResourceCPU)
	resourceMemory = string(v1.ResourceMemory)

	cpuMedium = 1000
	cpuLarge  = 2000

	oneGiB = 1024 * 1024 * 1024
	twoGiB = 2 * oneGiB

	gpuOne   = 1
	gpuTwo   = 2
	gpuThree = 3

	vecValSmall       = 50
	vecValMedium      = 100
	vecValLarge       = 150
	vecValMemSmall    = 500
	vecValMemMedium   = 1000
	vecValMemLarge    = 1500
	vecValModified    = 999
	vecValDecimal     = 100.5
	vecValMemDecimal  = 1000.25
	vecValGPUDecimal  = 2.5
	vecValSetLarge    = 2000
	vecValSetDecimal  = 4.5
	vecValLessSmaller = 99

	customResourceResA = "resource-a"
	customResourceResB = "resource-b"

	customResourceQtyMedium = 5
	customResourceQtyLarge  = 10
)

func buildTestVectorMap(resourceNames ...string) *ResourceVectorMap {
	rList := v1.ResourceList{}
	for _, name := range resourceNames {
		rList[v1.ResourceName(name)] = resource.MustParse("1")
	}
	return BuildResourceVectorMap([]v1.ResourceList{rList})
}

var _ = Describe("ResourceVector", func() {
	Describe("Add", func() {
		It("should add two vectors of equal length", func() {
			vec1 := ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}
			vec2 := ResourceVector{vecValSmall, vecValMemSmall, gpuOne}

			vec1.Add(vec2)
			Expect(vec1).To(Equal(ResourceVector{vecValLarge, vecValMemLarge, gpuThree}))
		})

		It("should extend shorter vector when adding mismatched lengths", func() {
			vec1 := ResourceVector{vecValMedium, vecValMemMedium}
			vec2 := ResourceVector{vecValSmall, vecValMemSmall, gpuOne}

			vec1.Add(vec2)
			Expect(vec1).To(Equal(ResourceVector{vecValLarge, vecValMemLarge, gpuOne}))

			vec3 := ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}
			vec4 := ResourceVector{vecValSmall, vecValMemSmall}

			vec3.Add(vec4)
			Expect(vec3).To(Equal(ResourceVector{vecValLarge, vecValMemLarge, gpuTwo}))
		})
	})

	Describe("Sub", func() {
		It("should subtract two vectors of equal length", func() {
			vec1 := ResourceVector{vecValLarge, vecValMemLarge, gpuThree}
			vec2 := ResourceVector{vecValSmall, vecValMemSmall, gpuOne}

			vec1.Sub(vec2)
			Expect(vec1).To(Equal(ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}))
		})

		It("should extend shorter vector when subtracting mismatched lengths", func() {
			vec1 := ResourceVector{vecValLarge, vecValMemLarge}
			vec2 := ResourceVector{vecValSmall, vecValMemSmall, gpuOne}

			vec1.Sub(vec2)
			Expect(vec1).To(Equal(ResourceVector{vecValMedium, vecValMemMedium, -gpuOne}))

			vec3 := ResourceVector{vecValLarge, vecValMemLarge, gpuThree}
			vec4 := ResourceVector{vecValSmall, vecValMemSmall}

			vec3.Sub(vec4)
			Expect(vec3).To(Equal(ResourceVector{vecValMedium, vecValMemMedium, gpuThree}))
		})
	})

	Describe("Clone", func() {
		It("should create a deep copy of the vector", func() {
			original := ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}
			cloned := original.Clone()

			// Verify contents are equal
			Expect(cloned[0]).To(Equal(original[0]))
			Expect(cloned[1]).To(Equal(original[1]))
			Expect(cloned[2]).To(Equal(original[2]))

			// Verify they're independent - modify clone
			cloned[0] = vecValModified
			Expect(original[0]).To(Equal(float64(vecValMedium)))
			Expect(cloned[0]).To(Equal(float64(vecValModified)))
		})
	})

	Describe("LessEqual", func() {
		It("should compare vectors of equal length correctly", func() {
			vec1 := ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}
			vec2 := ResourceVector{vecValLarge, vecValMemLarge, gpuThree}
			vec3 := ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}

			Expect(vec1.LessEqual(vec2)).To(BeTrue())
			Expect(vec1.LessEqual(vec3)).To(BeTrue())
			Expect(vec2.LessEqual(vec1)).To(BeFalse())

			vec4 := ResourceVector{vecValLessSmaller, vecValMemMedium, gpuTwo}
			Expect(vec1.LessEqual(vec4)).To(BeFalse())
		})

		It("should handle mismatched lengths with implicit zeros", func() {
			// vec1 shorter - implicit 0s compared against vec2 extras (positive, so true)
			vec1 := ResourceVector{vecValMedium, vecValMemMedium}
			vec2 := ResourceVector{vecValLarge, vecValMemLarge, gpuThree}
			Expect(vec1.LessEqual(vec2)).To(BeTrue())

			// vec1 shorter - implicit 0s compared against vec2 extras (negative, so false)
			vec3 := ResourceVector{vecValMedium, vecValMemMedium}
			vec4 := ResourceVector{vecValLarge, vecValMemLarge, -1}
			Expect(vec3.LessEqual(vec4)).To(BeFalse())

			// vec1 longer with positive extras - compared against implicit 0 (so false)
			vec5 := ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}
			vec6 := ResourceVector{vecValLarge, vecValMemLarge}
			Expect(vec5.LessEqual(vec6)).To(BeFalse())

			// vec1 longer with zero/negative extras - compared against implicit 0 (so true)
			vec7 := ResourceVector{vecValMedium, vecValMemMedium, 0}
			vec8 := ResourceVector{vecValLarge, vecValMemLarge}
			Expect(vec7.LessEqual(vec8)).To(BeTrue())

			vec9 := ResourceVector{vecValMedium, vecValMemMedium, -1}
			vec10 := ResourceVector{vecValLarge, vecValMemLarge}
			Expect(vec9.LessEqual(vec10)).To(BeTrue())
		})
	})

	Describe("Get", func() {
		It("should return the value at the specified index", func() {
			vec := ResourceVector{vecValDecimal, vecValMemDecimal, vecValGPUDecimal}

			Expect(vec.Get(0)).To(Equal(vecValDecimal))
			Expect(vec.Get(1)).To(Equal(vecValMemDecimal))
			Expect(vec.Get(2)).To(Equal(vecValGPUDecimal))
		})
	})

	Describe("Set", func() {
		It("should set the value at the specified index", func() {
			vec := ResourceVector{vecValMedium, vecValMemMedium, gpuTwo}

			vec.Set(0, vecValLarge)
			Expect(vec[0]).To(Equal(float64(vecValLarge)))

			vec.Set(1, vecValSetLarge)
			Expect(vec[1]).To(Equal(float64(vecValSetLarge)))

			vec.Set(2, vecValSetDecimal)
			Expect(vec[2]).To(Equal(vecValSetDecimal))
		})
	})
})

var _ = Describe("ResourceVectorMap", func() {
	Describe("GetIndex", func() {
		It("should return the correct index for known resource names", func() {
			indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)

			Expect(indexMap.GetIndex(resourceCPU)).To(Equal(0))
			Expect(indexMap.GetIndex(resourceMemory)).To(Equal(1))
			Expect(indexMap.GetIndex(commonconstants.GpuResource)).To(Equal(2))
			Expect(indexMap.GetIndex("not-exist")).To(Equal(-1))
			Expect(indexMap.GetIndex("")).To(Equal(-1))
		})
	})
})

var _ = Describe("NewResourceVector", func() {
	It("should create a vector of zeros with the correct length", func() {
		indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)
		vec := NewResourceVector(indexMap)

		Expect(vec).To(HaveLen(4))
		Expect(vec).To(Equal(ResourceVector{0, 0, 0, 0}))
	})
})

var _ = Describe("BuildResourceVectorMap", func() {
	It("should create a map with core resources in correct order", func() {
		nodeResources := []v1.ResourceList{
			{
				v1.ResourceCPU:    *resource.NewMilliQuantity(cpuMedium, resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(oneGiB, resource.DecimalSI),
				v1.ResourceName(commonconstants.NvidiaGpuResource): *resource.NewQuantity(gpuTwo, resource.DecimalSI),
			},
		}

		indexMap := BuildResourceVectorMap(nodeResources)

		Expect(indexMap.Len()).To(Equal(4))
		Expect(indexMap.ResourceAt(0)).To(Equal(resourceCPU))
		Expect(indexMap.ResourceAt(1)).To(Equal(resourceMemory))
		Expect(indexMap.ResourceAt(2)).To(Equal(commonconstants.GpuResource))
		Expect(indexMap.ResourceAt(3)).To(Equal(string(v1.ResourcePods)))
	})

	It("should handle multiple nodes with different resource sets", func() {
		nodeResources := []v1.ResourceList{
			{
				v1.ResourceCPU:                      *resource.NewMilliQuantity(cpuMedium, resource.DecimalSI),
				v1.ResourceMemory:                   *resource.NewQuantity(oneGiB, resource.DecimalSI),
				v1.ResourceName(customResourceResA): *resource.NewQuantity(customResourceQtyLarge, resource.DecimalSI),
			},
			{
				v1.ResourceCPU:    *resource.NewMilliQuantity(cpuLarge, resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(twoGiB, resource.DecimalSI),
				v1.ResourceName(commonconstants.NvidiaGpuResource): *resource.NewQuantity(gpuTwo, resource.DecimalSI),
				v1.ResourceName(customResourceResB):                *resource.NewQuantity(customResourceQtyMedium, resource.DecimalSI),
			},
		}

		indexMap := BuildResourceVectorMap(nodeResources)

		Expect(indexMap.GetIndex(resourceCPU)).To(BeNumerically(">=", 0))
		Expect(indexMap.GetIndex(resourceMemory)).To(BeNumerically(">=", 0))
		Expect(indexMap.GetIndex(commonconstants.GpuResource)).To(BeNumerically(">=", 0))
		Expect(indexMap.GetIndex(customResourceResA)).To(BeNumerically(">=", 0))
		Expect(indexMap.GetIndex(customResourceResB)).To(BeNumerically(">=", 0))
	})

	It("should include GPU resource even when not present in all nodes", func() {
		nodeResources := []v1.ResourceList{
			{
				v1.ResourceCPU:    *resource.NewMilliQuantity(cpuMedium, resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(oneGiB, resource.DecimalSI),
			},
		}

		indexMap := BuildResourceVectorMap(nodeResources)

		Expect(indexMap.GetIndex("gpu-memory")).To(Equal(-1))
		Expect(indexMap.Len()).To(Equal(4))
	})

	It("should normalize non-nvidia GPU resources into generic gpu type", func() {
		const amdGpuResource = "amd.com/gpu"

		nodeResources := []v1.ResourceList{
			{
				v1.ResourceCPU:                  *resource.NewMilliQuantity(cpuMedium, resource.DecimalSI),
				v1.ResourceMemory:               *resource.NewQuantity(oneGiB, resource.DecimalSI),
				v1.ResourceName(amdGpuResource): *resource.NewQuantity(gpuTwo, resource.DecimalSI),
			},
		}

		indexMap := BuildResourceVectorMap(nodeResources)

		Expect(indexMap.GetIndex("gpu-memory")).To(Equal(-1))
		Expect(indexMap.Len()).To(Equal(4))
	})
})

var _ = Describe("Resource conversion", func() {
	Describe("ToVector/FromVector", func() {
		It("should convert Resource to vector and back", func() {
			r := NewResource(cpuMedium, twoGiB, gpuTwo)
			indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)

			vec := r.ToVector(indexMap)

			Expect(vec).To(HaveLen(4))
			Expect(vec.Get(indexMap.GetIndex(resourceCPU))).To(Equal(float64(cpuMedium)))
			Expect(vec.Get(indexMap.GetIndex(resourceMemory))).To(Equal(float64(twoGiB)))
			Expect(vec.Get(indexMap.GetIndex(commonconstants.GpuResource))).To(Equal(float64(gpuTwo)))
		})

		It("should convert vector to Resource", func() {
			vec := ResourceVector{cpuMedium, twoGiB, gpuTwo}
			indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)

			r := EmptyResource()
			r.FromVector(vec, indexMap)

			Expect(r.milliCpu).To(Equal(float64(cpuMedium)))
			Expect(r.memory).To(Equal(float64(twoGiB)))
			Expect(r.gpus).To(Equal(float64(gpuTwo)))
		})

		It("should support round-trip conversion", func() {
			original := NewResource(cpuMedium, twoGiB, gpuTwo)
			indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)

			vec := original.ToVector(indexMap)
			restored := EmptyResource()
			restored.FromVector(vec, indexMap)

			Expect(restored.milliCpu).To(Equal(original.milliCpu))
			Expect(restored.memory).To(Equal(original.memory))
			Expect(restored.gpus).To(Equal(original.gpus))
		})
	})

	Describe("ResourceRequirements", func() {
		It("should convert ResourceRequirements to vector", func() {
			req := NewResourceRequirements(gpuTwo, cpuMedium, twoGiB)
			indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)

			vec := req.ToVector(indexMap)

			Expect(vec).To(HaveLen(4))
			Expect(vec.Get(indexMap.GetIndex(resourceCPU))).To(Equal(float64(cpuMedium)))
			Expect(vec.Get(indexMap.GetIndex(resourceMemory))).To(Equal(float64(twoGiB)))
			Expect(vec.Get(indexMap.GetIndex(commonconstants.GpuResource))).To(Equal(float64(gpuTwo)))
		})

		It("should convert vector to ResourceRequirements", func() {
			vec := ResourceVector{cpuMedium, twoGiB, gpuTwo}
			indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)

			req := EmptyResourceRequirements()
			req.FromVector(vec, indexMap)

			Expect(req.milliCpu).To(Equal(float64(cpuMedium)))
			Expect(req.memory).To(Equal(float64(twoGiB)))
			Expect(req.GPUs()).To(Equal(float64(gpuTwo)))
		})
	})

	Describe("ToResourceQuantities", func() {
		It("should convert vector back to quantities map", func() {
			vec := ResourceVector{cpuMedium, twoGiB, gpuTwo}
			indexMap := buildTestVectorMap(resourceCPU, resourceMemory, commonconstants.NvidiaGpuResource)

			rq := vec.ToResourceQuantities(indexMap)

			Expect(rq[resourceCPU]).To(Equal(float64(cpuMedium)))
			Expect(rq[resourceMemory]).To(Equal(float64(twoGiB)))
			Expect(rq[commonconstants.GpuResource]).To(Equal(float64(gpuTwo)))
		})
	})
})
