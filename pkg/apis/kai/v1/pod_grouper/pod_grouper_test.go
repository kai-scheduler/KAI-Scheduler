// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_grouper

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPodGrouper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PodGrouper type suite")
}

var _ = Describe("PodGrouper", func() {
	It("Set Defaults", func(ctx context.Context) {
		podGrouper := &PodGrouper{}
		var replicaCount int32
		replicaCount = 1
		podGrouper.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*podGrouper.Service.Enabled).To(Equal(true))
		Expect(*podGrouper.Service.Image.Name).To(Equal("podgrouper"))
		Expect(*podGrouper.Replicas).To(Equal(int32(1)))
	})
	It("Set Defaults with replicas", func(ctx context.Context) {
		podGrouper := &PodGrouper{}
		var replicaCount int32
		replicaCount = 3
		podGrouper.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*podGrouper.Replicas).To(Equal(int32(3)))
	})
})
