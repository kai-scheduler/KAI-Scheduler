// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	kaiDefaultRepository = "ghcr.io/kai-scheduler/kai-scheduler"
	defaultImageTag      = "latest"
)

func TestV1Alpha1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "V1Alpha1 Suite")
}

var _ = Describe("Image", func() {
	It("Set Defaults", func(ctx context.Context) {
		os.Setenv(DefaultRepositoryEnvVarName, kaiDefaultRepository)
		os.Setenv(DefaultTagEnvVarName, defaultImageTag)
		image := &Image{}
		image.SetDefaultsWhereNeeded()
		Expect(*image.Repository).To(Equal(kaiDefaultRepository))
		Expect(*image.PullPolicy).To(Equal(v1.PullIfNotPresent))
		Expect(*image.Tag).To(Equal(defaultImageTag))
	})

	It("Get URL", func(ctx context.Context) {
		image := &Image{
			Repository: ptr.To("ghcr.io/kai-scheduler/kai-scheduler"),
			Name:       ptr.To("example"),
			Tag:        ptr.To("test"),
		}
		Expect(image.Url()).To(Equal("ghcr.io/kai-scheduler/kai-scheduler/example:test"))
	})
})
