// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUnscheduledInfo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UnscheduledInfo Suite")
}

var _ = Describe("UnscheduledInfo", func() {
	Context("GetBuildOverCapacityMessageForQueue", func() {

		It("should generate correct message for CPU resource", func() {
			queueName := "cpu-queue"
			resourceName := CpuResource
			deserved := 8000.0 // 8 CPU cores in millicores
			used := 4000.0     // 4 CPU cores in millicores
			requiredResources := &podgroup_info.JobRequirement{
				GPU:      0,
				MilliCPU: 6000.0, // 6 CPU cores in millicores
				Memory:   0,
			}

			message := GetBuildOverCapacityMessageForQueue(queueName, resourceName, deserved, used, requiredResources)

			expectedDetails := "Workload requested 6 CPU cores, but cpu-queue quota is 8 cores, while 4 cores are already allocated for non-preemptible pods."

			Expect(message).To(ContainSubstring(expectedDetails))
		})

		It("should generate correct message for 200G memory resource", func() {
			queueName := "memory-queue"
			resourceName := MemoryResource
			deserved := 100000000000.0 // 100 GB in bytes
			used := 50000000000.0      // 50 GB in bytes
			requiredResources := &podgroup_info.JobRequirement{
				GPU:      0,
				MilliCPU: 0,
				Memory:   200000000000.0, // 200 GB in bytes
			}

			message := GetBuildOverCapacityMessageForQueue(queueName, resourceName, deserved, used, requiredResources)

			expectedDetails := "Workload requested 200 GB memory, but memory-queue quota is 100 GB, while 50 GB are already allocated for non-preemptible pods."

			Expect(message).To(ContainSubstring(expectedDetails))
		})

	})

	Context("GetJobOverMaxAllowedMessageForQueue", func() {
		It("should generate correct message for GPU resource", func() {
			queueName := "gpu-queue"
			resourceName := GpuResource
			maxAllowed := 8.0
			used := 6.0
			requested := 4.0

			message := GetJobOverMaxAllowedMessageForQueue(queueName, resourceName, maxAllowed, used, requested)

			expected := "gpu-queue quota has reached the allowable limit of GPUs. Limit is 8 GPUs, currently 6 GPUs allocated and workload requested 4 GPUs"
			Expect(message).To(Equal(expected))
		})

		It("should generate correct message for CPU resource", func() {
			queueName := "cpu-queue"
			resourceName := CpuResource
			maxAllowed := 16000.0 // 16 CPU cores in millicores
			used := 12000.0       // 12 CPU cores in millicores
			requested := 8000.0   // 8 CPU cores in millicores

			message := GetJobOverMaxAllowedMessageForQueue(queueName, resourceName, maxAllowed, used, requested)

			expected := "cpu-queue quota has reached the allowable limit of CPU cores. Limit is 16 cores, currently 12 cores allocated and workload requested 8 cores"
			Expect(message).To(Equal(expected))
		})

		It("should generate correct message for memory resource", func() {
			queueName := "memory-queue"
			resourceName := MemoryResource
			maxAllowed := 500000000000.0 // 500 GB in bytes
			used := 400000000000.0       // 400 GB in bytes
			requested := 200000000000.0  // 200 GB in bytes

			message := GetJobOverMaxAllowedMessageForQueue(queueName, resourceName, maxAllowed, used, requested)

			expected := "memory-queue quota has reached the allowable limit of memory. Limit is 500 GB, currently 400 GB allocated and workload requested 200 GB"
			Expect(message).To(Equal(expected))
		})
	})

})
