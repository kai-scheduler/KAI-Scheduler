// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

func TestImagePullSecrets(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Image Pull Secrets Suite")
}

var _ = Describe("test secrets generation", Ordered, func() {
	var kaiConfig = &kaiv1.Config{}
	BeforeEach(func() {
		kaiConfig = &kaiv1.Config{
			Spec: kaiv1.ConfigSpec{
				Global: &kaiv1.GlobalConfig{},
			},
		}
	})
	Context("Generate secrets", func() {

		It("Generate secrets when Image Pull Secrets are set ", func() {
			kaiConfig.Spec.Global.ImagePullSecrets = []string{"secret1", "secret2"}
			res := GetGlobalImagePullSecrets(kaiConfig.Spec.Global)
			expected := []v1.LocalObjectReference{
				{Name: "secret1"},
				{Name: "secret2"},
			}
			Expect(len(res)).To(Equal(2))
			Expect(res).To(Equal(expected))
		})
		It("Empty secrets", func() {
			res := GetGlobalImagePullSecrets(kaiConfig.Spec.Global)
			Expect(len(res)).To(Equal(0))
		})
	})
})
