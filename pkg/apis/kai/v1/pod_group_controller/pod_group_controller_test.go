// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_controller

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPodGroupController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PodGroupController type suite")
}

var _ = Describe("PodGroupController", func() {
	It("Set Defaults", func(ctx context.Context) {
		podGroupController := &PodGroupController{}
		podGroupController.SetDefaultsWhereNeeded(nil, nil)
		Expect(*podGroupController.Service.Enabled).To(Equal(true))
		Expect(*podGroupController.Service.Image.Name).To(Equal(imageName))
	})
	It("Set Defaults With replicas", func(ctx context.Context) {
		podGroupController := &PodGroupController{}
		var replicaCount int32
		replicaCount = 3
		podGroupController.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*podGroupController.Replicas).To(Equal(int32(3)))
	})
})
