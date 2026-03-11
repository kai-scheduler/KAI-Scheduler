// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_controller

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestQueueController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "QueueController type suite")
}

var _ = Describe("QueueController", func() {
	It("Set Defaults", func(ctx context.Context) {
		queueController := &QueueController{}
		var replicaCount int32
		replicaCount = 1
		queueController.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*queueController.Service.Enabled).To(Equal(true))
		Expect(*queueController.Service.Image.Name).To(Equal("queuecontroller"))
		Expect(*queueController.Replicas).To(Equal(int32(1)))
	})
	It("Set Defaults with replica count", func(ctx context.Context) {
		queueController := &QueueController{}
		var replicaCount int32
		replicaCount = 3
		queueController.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*queueController.Replicas).To(Equal(int32(3)))
	})
})
