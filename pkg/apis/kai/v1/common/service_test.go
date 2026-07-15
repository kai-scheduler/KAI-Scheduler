// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("Service", func() {
	Describe("SetDefaultsWhereNeeded", func() {
		It("leaves PodDisruptionBudget disabled by default", func() {
			service := &Service{}
			service.SetDefaultsWhereNeeded("test-service")

			Expect(service.PodDisruptionBudget).NotTo(BeNil())
			Expect(service.PodDisruptionBudget.Enabled).NotTo(BeNil())
			Expect(*service.PodDisruptionBudget.Enabled).To(BeFalse())
		})

		It("preserves explicitly enabled PodDisruptionBudget", func() {
			service := &Service{
				PodDisruptionBudget: &PodDisruptionBudget{
					Enabled: ptr.To(true),
				},
			}
			service.SetDefaultsWhereNeeded("admission")

			Expect(*service.PodDisruptionBudget.Enabled).To(BeTrue())
		})

		It("defaults maxUnavailable to 1 when PodDisruptionBudget is explicitly enabled", func() {
			service := &Service{
				PodDisruptionBudget: &PodDisruptionBudget{
					Enabled: ptr.To(true),
				},
			}
			service.SetDefaultsWhereNeeded("admission")

			Expect(service.PodDisruptionBudget.MaxUnavailable).NotTo(BeNil())
			Expect(*service.PodDisruptionBudget.MaxUnavailable).To(Equal(int32(1)))
		})
	})
})
