// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Resource internal logic", func() {
	Context("ResourceFromResourceList", func() {
		It("should skip resources with zero quantity", func() {
			resourceList := v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("5G"),
				GPUResourceName:   resource.MustParse("1"),
				v1.ResourceName("nvidia.com/mig-1g.24gb"): resource.MustParse("0"),
				v1.ResourceName("nvidia.com/mig-2g.48gb"): resource.MustParse("0"),
				v1.ResourceName("rdma/ib0"):               resource.MustParse("0"),
			}

			resource := ResourceFromResourceList(resourceList)

			Expect(resource.milliCpu).To(Equal(float64(1000)))
			Expect(resource.memory).To(Equal(float64(5000000000)))
			Expect(resource.gpus).To(Equal(float64(1)))

			scalarResources := resource.ScalarResources()
			_, hasMig1g := scalarResources[v1.ResourceName("nvidia.com/mig-1g.24gb")]
			_, hasMig2g := scalarResources[v1.ResourceName("nvidia.com/mig-2g.48gb")]
			_, hasRdma := scalarResources[v1.ResourceName("rdma/ib0")]

			Expect(hasMig1g).To(BeFalse())
			Expect(hasMig2g).To(BeFalse())
			Expect(hasRdma).To(BeFalse())
		})
	})
})
