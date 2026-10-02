// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/admission"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/binder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/numa_placement_exporter"
)

var _ = Describe("ConfigSpec", func() {
	Describe("SetDefaultsWhereNeeded", func() {
		It("leaves PodDisruptionBudget disabled for operands without explicit opt-in", func() {
			spec := &ConfigSpec{}
			spec.SetDefaultsWhereNeeded()

			Expect(spec.Binder.Service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*spec.Binder.Service.PodDisruptionBudget.Enabled).To(BeFalse())
			Expect(spec.PodGrouper.Service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*spec.PodGrouper.Service.PodDisruptionBudget.Enabled).To(BeFalse())
			Expect(spec.Scheduler.Service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*spec.Scheduler.Service.PodDisruptionBudget.Enabled).To(BeFalse())
			Expect(spec.QueueController.Service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*spec.QueueController.Service.PodDisruptionBudget.Enabled).To(BeFalse())
			Expect(spec.PodGroupController.Service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*spec.PodGroupController.Service.PodDisruptionBudget.Enabled).To(BeFalse())
			Expect(spec.NodeScaleAdjuster.Service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*spec.NodeScaleAdjuster.Service.PodDisruptionBudget.Enabled).To(BeFalse())
			Expect(spec.Admission.Service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*spec.Admission.Service.PodDisruptionBudget.Enabled).To(BeFalse())
		})

		It("preserves explicitly enabled PodDisruptionBudget on admission", func() {
			spec := &ConfigSpec{
				Admission: &admission.Admission{
					Service: &common.Service{
						PodDisruptionBudget: &common.PodDisruptionBudget{
							Enabled: ptr.To(true),
						},
					},
				},
			}
			spec.SetDefaultsWhereNeeded()

			Expect(*spec.Admission.Service.PodDisruptionBudget.Enabled).To(BeTrue())
		})

		It("defaults NumaPlacementExporter to NUMA memory nodes", func() {
			spec := &ConfigSpec{}
			spec.SetDefaultsWhereNeeded()

			Expect(spec.NumaPlacementExporter.NodeSelector).To(Equal(map[string]string{
				"feature.node.kubernetes.io/memory-numa": "true",
			}))
		})

		It("defaults an empty NumaPlacementExporter node selector to NUMA memory nodes", func() {
			spec := &ConfigSpec{
				NumaPlacementExporter: &numa_placement_exporter.NumaPlacementExporter{
					NodeSelector: map[string]string{},
				},
			}
			spec.SetDefaultsWhereNeeded()

			Expect(spec.NumaPlacementExporter.NodeSelector).To(Equal(map[string]string{
				"feature.node.kubernetes.io/memory-numa": "true",
			}))
		})

		It("preserves an explicit NumaPlacementExporter node selector", func() {
			selector := map[string]string{"node-role.kubernetes.io/worker": "true"}
			spec := &ConfigSpec{
				NumaPlacementExporter: &numa_placement_exporter.NumaPlacementExporter{
					NodeSelector: selector,
				},
			}
			spec.SetDefaultsWhereNeeded()

			Expect(spec.NumaPlacementExporter.NodeSelector).To(Equal(selector))
		})
	})

	Describe("GpuSharingMode resolution", func() {
		It("defaults to NonMemoryEnforced for a fresh config", func() {
			spec := &ConfigSpec{}
			spec.SetDefaultsWhereNeeded()

			Expect(*spec.Global.GpuSharingMode).To(Equal(common.GpuSharingModeNonMemoryEnforced))
			Expect(*spec.Admission.GPUSharing).To(BeTrue())
			Expect(*spec.Binder.Plugins[binder.GPUSharingPluginName].Enabled).To(BeTrue())
			Expect(*spec.Binder.Plugins[binder.HamiCorePluginName].Enabled).To(BeFalse())
			Expect(*spec.Binder.Plugins[binder.NvFractionsPluginName].Enabled).To(BeFalse())
		})

		It("honors an explicit mode", func() {
			spec := &ConfigSpec{
				Global: &GlobalConfig{GpuSharingMode: ptr.To(common.GpuSharingModeNvFractions)},
			}
			spec.SetDefaultsWhereNeeded()

			Expect(*spec.Global.GpuSharingMode).To(Equal(common.GpuSharingModeNvFractions))
			Expect(*spec.Admission.GPUSharing).To(BeFalse())
			Expect(*spec.Binder.Plugins[binder.NvFractionsPluginName].Enabled).To(BeTrue())
			Expect(*spec.Binder.Plugins[binder.GPUSharingPluginName].Enabled).To(BeFalse())
		})

		It("derives Disabled from legacy admission.gpuSharing=false on upgrade", func() {
			spec := &ConfigSpec{
				Admission: &admission.Admission{GPUSharing: ptr.To(false)},
			}
			spec.SetDefaultsWhereNeeded()

			Expect(*spec.Global.GpuSharingMode).To(Equal(common.GpuSharingModeDisabled))
			Expect(*spec.Admission.GPUSharing).To(BeFalse())
			Expect(*spec.Binder.Plugins[binder.GPUSharingPluginName].Enabled).To(BeFalse())
		})

		It("derives NonMemoryEnforced from legacy admission.gpuSharing=true on upgrade", func() {
			spec := &ConfigSpec{
				Admission: &admission.Admission{GPUSharing: ptr.To(true)},
			}
			spec.SetDefaultsWhereNeeded()

			Expect(*spec.Global.GpuSharingMode).To(Equal(common.GpuSharingModeNonMemoryEnforced))
			Expect(*spec.Binder.Plugins[binder.GPUSharingPluginName].Enabled).To(BeTrue())
		})

		It("derives HamiCore from legacy hamicore plugin enabled on upgrade", func() {
			spec := &ConfigSpec{
				Admission: &admission.Admission{GPUSharing: ptr.To(true)},
				Binder: &binder.Binder{
					Plugins: map[string]binder.PluginConfig{
						binder.HamiCorePluginName: {Enabled: ptr.To(true)},
					},
				},
			}
			spec.SetDefaultsWhereNeeded()

			Expect(*spec.Global.GpuSharingMode).To(Equal(common.GpuSharingModeHamiCore))
			Expect(*spec.Binder.Plugins[binder.HamiCorePluginName].Enabled).To(BeTrue())
			Expect(*spec.Binder.Plugins[binder.GPUSharingPluginName].Enabled).To(BeTrue())
		})
	})
})
