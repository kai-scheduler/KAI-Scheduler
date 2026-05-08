// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binder

import (
	"context"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

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
		Expect(binder.Plugins).To(HaveLen(3))
		Expect(*binder.Plugins[VolumeBindingPluginName].Priority).To(Equal(defaultPluginPriorities[VolumeBindingPluginName]))
		Expect(*binder.Plugins[DynamicResourcesPluginName].Priority).To(Equal(defaultPluginPriorities[DynamicResourcesPluginName]))
		Expect(*binder.Plugins[GPUSharingPluginName].Priority).To(Equal(defaultPluginPriorities[GPUSharingPluginName]))
		Expect(binder.Plugins[VolumeBindingPluginName].Arguments[BindTimeoutSecondsArgument]).
			To(Equal(strconv.Itoa(DefaultBindTimeoutSeconds)))
		Expect(binder.Plugins[DynamicResourcesPluginName].Arguments[BindTimeoutSecondsArgument]).
			To(Equal(strconv.Itoa(DefaultBindTimeoutSeconds)))
		// CDIEnabled is intentionally not baked into the gpusharing plugin args
		// when Binder.CDIEnabled is unset; the operator resolves it at deploy time.
		_, hasCDIArg := binder.Plugins[GPUSharingPluginName].Arguments[CDIEnabledArgument]
		Expect(hasCDIArg).To(BeFalse())
	})

	It("Set Defaults bakes CDI flag when Binder.CDIEnabled is set", func(ctx context.Context) {
		binder := &Binder{CDIEnabled: ptr.To(true)}
		binder.SetDefaultsWhereNeeded(nil, nil)
		Expect(binder.Plugins[GPUSharingPluginName].Arguments[CDIEnabledArgument]).
			To(Equal(strconv.FormatBool(true)))
	})

	It("Set Defaults With Plugin Overrides", func(ctx context.Context) {
		binder := &Binder{
			VolumeBindingTimeoutSeconds: ptr.To(45),
			Plugins: map[string]PluginConfig{
				GPUSharingPluginName: {
					Enabled: ptr.To(false),
				},
				VolumeBindingPluginName: {
					Arguments: map[string]string{
						BindTimeoutSecondsArgument: "30",
					},
				},
				"custom": {
					Enabled:  ptr.To(true),
					Priority: ptr.To(250),
				},
			},
		}

		binder.SetDefaultsWhereNeeded(nil, nil)

		Expect(*binder.Plugins[GPUSharingPluginName].Enabled).To(BeFalse())
		Expect(*binder.Plugins[VolumeBindingPluginName].Priority).To(Equal(defaultPluginPriorities[VolumeBindingPluginName]))
		Expect(binder.Plugins[VolumeBindingPluginName].Arguments[BindTimeoutSecondsArgument]).To(Equal("30"))
		Expect(binder.Plugins[DynamicResourcesPluginName].Arguments[BindTimeoutSecondsArgument]).To(Equal("45"))
		Expect(*binder.Plugins["custom"].Enabled).To(BeTrue())
		Expect(*binder.Plugins["custom"].Priority).To(Equal(250))
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
