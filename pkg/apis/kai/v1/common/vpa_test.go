// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("VPASpec", func() {
	Describe("SetDefaultsWhereNeeded", func() {
		It("should set Enabled to false and UpdatePolicy to InPlaceOrRecreate when all fields are nil", func() {
			vpa := &VPASpec{}
			vpa.SetDefaultsWhereNeeded()

			Expect(vpa.Enabled).To(Equal(ptr.To(false)))
			expectedMode := vpav1.UpdateModeInPlaceOrRecreate
			Expect(vpa.UpdatePolicy).To(Equal(&vpav1.PodUpdatePolicy{
				UpdateMode: &expectedMode,
			}))
		})

		It("should not override Enabled when already set", func() {
			vpa := &VPASpec{Enabled: ptr.To(true)}
			vpa.SetDefaultsWhereNeeded()

			Expect(*vpa.Enabled).To(BeTrue())
		})

		It("should not override UpdatePolicy when already set", func() {
			mode := vpav1.UpdateModeOff
			vpa := &VPASpec{
				UpdatePolicy: &vpav1.PodUpdatePolicy{UpdateMode: &mode},
			}
			vpa.SetDefaultsWhereNeeded()

			Expect(*vpa.UpdatePolicy.UpdateMode).To(Equal(vpav1.UpdateModeOff))
		})

		It("should set UpdateMode to InPlaceOrRecreate when UpdatePolicy is set but UpdateMode is nil", func() {
			vpa := &VPASpec{
				UpdatePolicy: &vpav1.PodUpdatePolicy{},
			}
			vpa.SetDefaultsWhereNeeded()

			Expect(vpa.UpdatePolicy.UpdateMode).NotTo(BeNil())
			Expect(*vpa.UpdatePolicy.UpdateMode).To(Equal(vpav1.UpdateModeInPlaceOrRecreate))
		})

		It("should not set ResourcePolicy", func() {
			vpa := &VPASpec{}
			vpa.SetDefaultsWhereNeeded()

			Expect(vpa.ResourcePolicy).To(BeNil())
		})
	})
})
