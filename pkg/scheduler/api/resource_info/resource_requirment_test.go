// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("ResourceRequirements Info internal logic", func() {
	Context("RequirementsFromResourceList", func() {
		It("CPU Resources", func() {
			resourceList := v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("5G"),
			}
			resourceInfo := RequirementsFromResourceList(resourceList)
			Expect(resourceInfo.Cpu()).To(Equal(float64(1000)))
			Expect(resourceInfo.Memory()).To(Equal(float64(5000000000)))

			newResourceList := resourceInfo.ToResourceList()
			compareResourceLists(resourceList, newResourceList)
		})
		It("Negative CPU Resources", func() {
			resourceList := v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("-1"),
			}
			resourceInfo := RequirementsFromResourceList(resourceList)
			Expect(resourceInfo.Cpu()).To(Equal(float64(-1000)))

			newResourceList := resourceInfo.ToResourceList()
			compareResourceLists(resourceList, newResourceList)
		})
		It("GPU Resources", func() {
			resourceList := v1.ResourceList{
				GPUResourceName: resource.MustParse("1"),
			}
			resourceInfo := RequirementsFromResourceList(resourceList)
			Expect(resourceInfo.GPUs()).To(Equal(float64(1)))

			newResourceList := resourceInfo.ToResourceList()
			compareResourceLists(resourceList, newResourceList)
		})
		It("GPU Fractional Resources", func() {
			resourceInfo := NewResourceRequirementsWithGpus(0.5)
			Expect(resourceInfo.GPUs()).To(Equal(0.5))

			newResourceList := resourceInfo.ToResourceList()
			gpuResource := newResourceList[GPUResourceName]
			Expect(gpuResource.MilliValue()).To(Equal(int64(500)))
		})
		It("Other Resources", func() {
			resourceList := v1.ResourceList{
				v1.ResourceName("kai.scheduler/test-resource"): resource.MustParse("1"),
			}
			resourceInfo := RequirementsFromResourceList(resourceList)
			Expect(resourceInfo.Get("kai.scheduler/test-resource")).To(Equal(float64(1000)))

			newResourceList := resourceInfo.ToResourceList()
			compareResourceLists(resourceList, newResourceList)
		})
		It("Other Resources - Unsupported resource not count", func() {
			resourceList := v1.ResourceList{
				v1.ResourceName("unsupported"): resource.MustParse("2"),
			}
			resourceInfo := RequirementsFromResourceList(resourceList)
			Expect(resourceInfo.Get("unsupported")).To(Equal(float64(0)))
		})
	})
	Context("Clone", func() {
		It("Cloning all the properties", func() {
			resourceList := v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("5G"),
				v1.ResourceName("kai.scheduler/test-resource"): resource.MustParse("1"),
			}
			resourceInfo := RequirementsFromResourceList(resourceList)
			clone := resourceInfo.Clone()
			Expect(clone.Cpu()).To(Equal(float64(1000)))
			Expect(clone.Memory()).To(Equal(float64(5000000000)))
			Expect(clone.Get("kai.scheduler/test-resource")).To(Equal(float64(1000)))
		})
	})

	Context("SetMaxResource", func() {
		It("Cloning all the properties", func() {
			resourceInfo := RequirementsFromResourceList(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("5G"),
			})
			resourceInfo2 := RequirementsFromResourceList(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("4G"),
			})
			err := resourceInfo.SetMaxResource(resourceInfo2)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourceInfo.Cpu()).To(Equal(float64(2000)))
			Expect(resourceInfo.Memory()).To(Equal(float64(5000000000)))

		})
	})
	Context("IsEmpty", func() {
		It("RealEmpty", func() {
			resourceList := v1.ResourceList{}
			resourceInfo := RequirementsFromResourceList(resourceList)
			Expect(resourceInfo.IsEmpty()).To(BeTrue())

		})
		It("NonEmpty", func() {
			resourceList := v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("1"),
			}
			resourceInfo := RequirementsFromResourceList(resourceList)
			Expect(resourceInfo.IsEmpty()).To(BeFalse())

		})
	})
	Context("Checks", func() {
		Context("LessEqual", func() {
			It("Less in all resources - true", func() {
				resourceInfo1 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("5G"),
				})
				resourceInfo2 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("4G"),
				})
				Expect(resourceInfo2.LessEqual(resourceInfo1)).To(BeTrue())
			})
			It("Less in one resources - false", func() {
				resourceInfo1 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("5G"),
				})
				resourceInfo2 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("3"),
					v1.ResourceMemory: resource.MustParse("4G"),
				})
				Expect(resourceInfo2.LessEqual(resourceInfo1)).To(BeFalse())
			})
			It("Equal in all resources - true", func() {
				resourceInfo1 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("5G"),
				})
				resourceInfo2 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("5G"),
				})
				Expect(resourceInfo2.LessEqual(resourceInfo1)).To(BeTrue())
			})
		})
		Context("LessInAtLeastOneResource", func() {
			It("Less in all resources - true", func() {
				resourceInfo1 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("5G"),
				})
				resourceInfo2 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("4G"),
				})
				Expect(resourceInfo2.LessInAtLeastOneResource(resourceInfo1)).To(BeTrue())
			})
			It("Less in one resources - true", func() {
				resourceInfo1 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("5G"),
				})
				resourceInfo2 := RequirementsFromResourceList(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("3"),
					v1.ResourceMemory: resource.MustParse("4G"),
				})
				Expect(resourceInfo2.LessInAtLeastOneResource(resourceInfo1)).To(BeTrue())
			})
		})
	})
})

func compareResourceLists(r, l v1.ResourceList) {
	for name, quantity := range l {
		_, found := r[name]
		if quantity.IsZero() || !found {
			continue
		}
		Expect(found).To(BeTrue(), "ResourceRequirements %s not found in resource list", name)
	}

	for name, quantity := range r {
		newQuantity, found := l[name]
		if quantity.IsZero() || !found {
			continue
		}
		Expect(found).To(BeTrue(), "ResourceRequirements %s not found in resource list", name)

		cmp := newQuantity.Cmp(quantity)
		Expect(cmp).To(Equal(0), "ResourceRequirements %s quantity is different, expected %s, got %s", name, quantity.String(), newQuantity.String())
	}
}
