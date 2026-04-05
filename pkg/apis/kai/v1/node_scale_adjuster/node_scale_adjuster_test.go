// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_scale_adjuster

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNodeScaleAdjuster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeScaleAdjuster type suite")
}

var _ = Describe("NodeScaleAdjuster", func() {
	It("Set Defaults", func(ctx context.Context) {
		adjuster := &NodeScaleAdjuster{}
		adjuster.SetDefaultsWhereNeeded(nil)
		Expect(*adjuster.Service.Enabled).To(Equal(true))
		Expect(*adjuster.Service.Image.Name).To(Equal(imageName))
	})
})
