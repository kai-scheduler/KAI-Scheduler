// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/admission"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
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
	})
})
