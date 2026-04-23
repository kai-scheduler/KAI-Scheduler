// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGlobalConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GlobalConfig Suite")
}

var _ = Describe("SetDefaultWhereNeeded", func() {
	var globalConfig *GlobalConfig

	BeforeEach(func() {
		globalConfig = &GlobalConfig{}
	})

	Context("deprecated ImagePullSecret migration", func() {
		It("should merge deprecated ImagePullSecret into ImagePullSecrets", func() {
			secret := "legacy-secret"
			globalConfig.ImagePullSecret = &secret
			globalConfig.SetDefaultWhereNeeded()
			Expect(globalConfig.ImagePullSecrets).To(ContainElement("legacy-secret"))
			Expect(globalConfig.ImagePullSecret).To(BeNil())
		})

		It("should not duplicate when ImagePullSecret already exists in ImagePullSecrets", func() {
			secret := "secret1"
			globalConfig.ImagePullSecret = &secret
			globalConfig.ImagePullSecrets = []string{"secret1", "secret2"}
			globalConfig.SetDefaultWhereNeeded()
			Expect(globalConfig.ImagePullSecrets).To(Equal([]string{"secret1", "secret2"}))
			Expect(globalConfig.ImagePullSecret).To(BeNil())
		})

		It("should append unique deprecated secret to existing list", func() {
			secret := "secret3"
			globalConfig.ImagePullSecret = &secret
			globalConfig.ImagePullSecrets = []string{"secret1", "secret2"}
			globalConfig.SetDefaultWhereNeeded()
			Expect(globalConfig.ImagePullSecrets).To(Equal([]string{"secret1", "secret2", "secret3"}))
			Expect(globalConfig.ImagePullSecret).To(BeNil())
		})
	})
})
