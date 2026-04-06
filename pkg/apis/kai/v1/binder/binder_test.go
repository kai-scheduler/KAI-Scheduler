// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binder

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

func TestBinder(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Binder type suite")
}

var _ = Describe("Binder", func() {
	It("Set Defaults", func(ctx context.Context) {
		binder := &Binder{}
		binder.SetDefaultsWhereNeeded(nil, nil)
		Expect(*binder.Service.Enabled).To(Equal(true))
		Expect(*binder.Service.Image.Name).To(Equal("binder"))
		Expect(binder.Service.Resources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("50m")))
		Expect(binder.Service.Resources.Requests[v1.ResourceMemory]).To(Equal(resource.MustParse("200Mi")))
		Expect(binder.Service.Resources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("100m")))
		Expect(binder.Service.Resources.Limits[v1.ResourceMemory]).To(Equal(resource.MustParse("200Mi")))
	})
	It("Set Defaults With Replica Count", func(ctx context.Context) {
		binder := &Binder{}
		var replicaCount int32
		replicaCount = 3
		binder.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*binder.Replicas).To(Equal(int32(3)))
	})

	Context("ResourceReservation PodResources configuration", func() {
		It("should not set default PodResources when not configured", func(ctx context.Context) {
			binder := &Binder{}
			binder.SetDefaultsWhereNeeded(nil, nil)

			// PodResources should be nil when not configured
			Expect(binder.ResourceReservation.PodResources).To(BeNil())
		})

		It("should preserve configured PodResources values", func(ctx context.Context) {
			podResources := &common.Resources{
				Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2m"),
					v1.ResourceMemory: resource.MustParse("20Mi"),
				},
				Limits: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("100m"),
					v1.ResourceMemory: resource.MustParse("200Mi"),
				},
			}
			binder := &Binder{
				ResourceReservation: &ResourceReservation{
					PodResources: podResources,
				},
			}
			binder.SetDefaultsWhereNeeded(nil, nil)

			// Configured values should be preserved
			Expect(binder.ResourceReservation.PodResources).NotTo(BeNil())
			Expect(binder.ResourceReservation.PodResources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("2m")))
			Expect(binder.ResourceReservation.PodResources.Requests[v1.ResourceMemory]).To(Equal(resource.MustParse("20Mi")))
			Expect(binder.ResourceReservation.PodResources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("100m")))
			Expect(binder.ResourceReservation.PodResources.Limits[v1.ResourceMemory]).To(Equal(resource.MustParse("200Mi")))
		})

		It("should allow partial configuration (only CPU)", func(ctx context.Context) {
			podResources := &common.Resources{
				Requests: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse("5m"),
				},
				Limits: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse("50m"),
				},
			}
			binder := &Binder{
				ResourceReservation: &ResourceReservation{
					PodResources: podResources,
				},
			}
			binder.SetDefaultsWhereNeeded(nil, nil)

			// Only CPU should be set
			Expect(binder.ResourceReservation.PodResources).NotTo(BeNil())
			Expect(binder.ResourceReservation.PodResources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("5m")))
			Expect(binder.ResourceReservation.PodResources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("50m")))

			// Memory should not be set
			_, hasMemoryRequest := binder.ResourceReservation.PodResources.Requests[v1.ResourceMemory]
			_, hasMemoryLimit := binder.ResourceReservation.PodResources.Limits[v1.ResourceMemory]
			Expect(hasMemoryRequest).To(BeFalse())
			Expect(hasMemoryLimit).To(BeFalse())
		})
	})
})
