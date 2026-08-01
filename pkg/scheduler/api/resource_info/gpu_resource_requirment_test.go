// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	v1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GpuResourceRequirement mechanism", func() {
	Context("IsEmpty", func() {
		It("Whole number of GPU", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(1, 0)
			Expect(gpuResource.IsEmpty()).To(BeFalse())
		})
		It("Fraction of GPU", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0.5, 0)
			Expect(gpuResource.IsEmpty()).To(BeFalse())
		})
		It("Multi-fraction of GPU", func() {
			gpuResource := NewGpuResourceRequirementWithMultiFraction(3, 0.5, 0)
			Expect(gpuResource.IsEmpty()).To(BeFalse())
		})
		It("Smaller fraction of GPU than minGpu", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0.001, 0)
			Expect(gpuResource.IsEmpty()).To(BeTrue())
		})

		It("GpuMemory is not tested", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0, 100)
			Expect(gpuResource.IsEmpty()).To(BeTrue())
		})

		It("MigDevice", func() {
			gpuResource := NewGpuResourceRequirementWithMig(
				map[v1.ResourceName]int64{
					"nvidia.com/mig-1g.1gb": 1,
				},
			)
			Expect(gpuResource.IsEmpty()).To(BeFalse())
		})
	})

	Context("GpuMemoryAsGpuFraction", func() {
		It("converts memory to an equivalent gpu fraction", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0, 50)
			Expect(gpuResource.GpuMemoryAsGpuFraction(100)).To(Equal(0.5))
		})
		It("scales by the number of devices", func() {
			gpuResource := NewGpuResourceRequirementWithMultiFraction(2, 0, 50)
			Expect(gpuResource.GpuMemoryAsGpuFraction(100)).To(Equal(1.0))
		})
		It("returns 0 for a non-memory request", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0.5, 0)
			Expect(gpuResource.GpuMemoryAsGpuFraction(100)).To(Equal(float64(0)))
		})
		It("returns 0 when the device memory basis is non-positive", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0, 50)
			Expect(gpuResource.GpuMemoryAsGpuFraction(0)).To(Equal(float64(0)))
		})
	})

	Context("SetMaxResource", func() {
		It("Supported Resources", func() {
			gpuResource1 := newGpuResourceRequirementWithValues(5, 0, map[v1.ResourceName]int64{
				"nvidia.com/mig-1g.5gb":  1,
				"nvidia.com/mig-3g.20gb": 1,
			})
			gpuResource2 := newGpuResourceRequirementWithValues(4, 0, map[v1.ResourceName]int64{
				"nvidia.com/mig-1g.5gb":  2,
				"nvidia.com/mig-2g.10gb": 1,
			})
			err := gpuResource1.SetMaxResource(gpuResource2)
			Expect(err).ToNot(HaveOccurred())
			Expect(gpuResource1.GPUs()).To(Equal(float64(5)))
			Expect(gpuResource1.MigResources()["nvidia.com/mig-1g.5gb"]).To(Equal(int64(2)))
			Expect(gpuResource1.MigResources()["nvidia.com/mig-2g.10gb"]).To(Equal(int64(1)))
			Expect(gpuResource1.MigResources()["nvidia.com/mig-3g.20gb"]).To(Equal(int64(1)))
		})

		It("Support multi fractions", func() {
			gpuResource1 := NewGpuResourceRequirementWithMultiFraction(5, 0.2, 0)
			gpuResource2 := NewGpuResourceRequirementWithMultiFraction(6, 0.2, 0)
			err := gpuResource1.SetMaxResource(gpuResource2)
			Expect(err).ToNot(HaveOccurred())
			Expect(gpuResource1.GPUs()).To(Equal(float64(1.2)))
		})

		It("GpuMemory not supported", func() {
			gpuResource1 := NewGpuResourceRequirementWithGpus(0, 500)
			gpuResource2 := NewGpuResourceRequirementWithGpus(0, 600)
			err := gpuResource1.SetMaxResource(gpuResource2)
			Expect(err).ToNot(HaveOccurred())
			Expect(gpuResource1.GpuMemory()).To(Equal(int64(500)))
		})

		It("Different fractions not supported", func() {
			gpuResource1 := NewGpuResourceRequirementWithMultiFraction(1, 0.5, 0)
			gpuResource2 := NewGpuResourceRequirementWithMultiFraction(2, 0.4, 0)
			err := gpuResource1.SetMaxResource(gpuResource2)
			Expect(err).To(HaveOccurred())
			Expect(gpuResource1.GPUs()).To(Equal(float64(0.5)))
		})

		It("With gpus and without gpus can be compared", func() {
			gpuResource1 := NewGpuResourceRequirement()
			gpuResource2 := NewGpuResourceRequirementWithMultiFraction(2, 0.4, 0)
			err := gpuResource1.SetMaxResource(gpuResource2)
			Expect(err).To(Not(HaveOccurred()))
			Expect(gpuResource1.GPUs()).To(Equal(float64(0.8)))
		})
	})

	Context("LessEqual", func() {
		It("Less in GPUs", func() {
			gpuResource1 := NewGpuResourceRequirementWithGpus(1, 0)
			gpuResource2 := NewGpuResourceRequirementWithGpus(4, 0)
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeTrue())
		})
		It("Equal in GPUs", func() {
			gpuResource1 := NewGpuResourceRequirementWithGpus(4, 0)
			gpuResource2 := NewGpuResourceRequirementWithGpus(4, 0)
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeTrue())
		})
		It("Higher in GPUs", func() {
			gpuResource1 := NewGpuResourceRequirementWithGpus(5, 0)
			gpuResource2 := NewGpuResourceRequirementWithGpus(4, 0)
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeFalse())
		})
		It("Higher in GPUs but in less than minGpus", func() {
			gpuResource1 := NewGpuResourceRequirementWithGpus(4.0001, 0)
			gpuResource2 := NewGpuResourceRequirementWithGpus(4, 0)
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeTrue())
		})
		It("Less MIG Device", func() {
			gpuResource1 := NewGpuResourceRequirementWithMig(map[v1.ResourceName]int64{
				"nvidia.com/mig-1g.5gb": 1,
			})
			gpuResource2 := NewGpuResourceRequirementWithMig(map[v1.ResourceName]int64{
				"nvidia.com/mig-1g.5gb": 2,
			})
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeTrue())
		})
		It("Less In One ResourceRequirements", func() {
			gpuResource1 := newGpuResourceRequirementWithValues(1, 0, map[v1.ResourceName]int64{
				"nvidia.com/mig-1g.5gb": 5,
			})
			gpuResource2 := newGpuResourceRequirementWithValues(4, 0, map[v1.ResourceName]int64{
				"nvidia.com/mig-1g.5gb": 1,
			})
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeFalse())
		})
		It("Less in multi-fraction devices", func() {
			gpuResource1 := NewGpuResourceRequirementWithMultiFraction(1, 0.5, 0)
			gpuResource2 := NewGpuResourceRequirementWithMultiFraction(4, 0.5, 0)
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeTrue())
		})
		It("Not equal in multi-gpu portions", func() {
			gpuResource1 := NewGpuResourceRequirementWithMultiFraction(1, 0.6, 0)
			gpuResource2 := NewGpuResourceRequirementWithMultiFraction(4, 0.5, 0)
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeFalse())
		})
		It("Less in multi-gpuMemory devices", func() {
			gpuResource1 := NewGpuResourceRequirementWithMultiFraction(1, 0, 5000)
			gpuResource2 := NewGpuResourceRequirementWithMultiFraction(4, 0, 5000)
			Expect(gpuResource1.LessEqual(gpuResource2)).To(BeTrue())
		})
	})

	Context("GetNumOfGpuDevices", func() {
		It("should return 0 for 0 GPUs", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0, 0)
			Expect(gpuResource.GetNumOfGpuDevices()).To(Equal(int64(0)))
		})

		It("should return 1 for 0.5 GPUs", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(0.5, 0)
			Expect(gpuResource.GetNumOfGpuDevices()).To(Equal(int64(1)))
		})

		It("should return 2 for 2 GPUs", func() {
			gpuResource := NewGpuResourceRequirementWithGpus(2, 0)
			Expect(gpuResource.GetNumOfGpuDevices()).To(Equal(int64(2)))
		})
	})
})

func newGpuResourceRequirementWithValues(
	gpus float64, gpuMemory int64, migResources map[v1.ResourceName]int64) *GpuResourceRequirement {
	gResource := &GpuResourceRequirement{
		count:        fractionDefaultCount,
		portion:      gpus,
		gpuMemory:    gpuMemory,
		migResources: migResources,
	}
	if gpus >= wholeGpuPortion {
		gResource.count = int64(gpus)
		gResource.portion = wholeGpuPortion
	}
	return gResource
}
