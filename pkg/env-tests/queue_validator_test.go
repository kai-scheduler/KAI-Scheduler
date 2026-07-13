// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package env_tests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/queuehooks"
	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/env-tests/utils"
)

var _ = Describe("QueueValidator", func() {
	var (
		parent *schedulingv2.Queue
		child  *schedulingv2.Queue
	)

	newQueue := func(name, parentName string, cpu, gpu, memory float64) *schedulingv2.Queue {
		q := utils.CreateQueueObject(name, parentName)
		q.Spec.Resources.CPU.Quota = cpu
		q.Spec.Resources.GPU.Quota = gpu
		q.Spec.Resources.Memory.Quota = memory
		return q
	}

	AfterEach(func(ctx context.Context) {
		for _, q := range []*schedulingv2.Queue{child, parent} {
			if q != nil {
				_ = ctrlClient.Delete(ctx, q)
			}
		}
		parent, child = nil, nil
	})

	It("admits an over-subscribing child in none mode", func(ctx context.Context) {
		parent = newQueue("qv-parent", "", 100, 1, 1024)
		Expect(ctrlClient.Create(ctx, parent)).To(Succeed())
		child = newQueue("qv-child", parent.Name, 200, 2, 2048)

		validator := queuehooks.NewQueueValidator(ctrlClient, queuehooks.OverSubscriptionModeNone)
		warnings, err := validator.ValidateCreate(ctx, child)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeEmpty())
	})

	It("warns on an over-subscribing child in warning mode", func(ctx context.Context) {
		parent = newQueue("qv-parent", "", 100, 1, 1024)
		Expect(ctrlClient.Create(ctx, parent)).To(Succeed())
		child = newQueue("qv-child", parent.Name, 200, 2, 2048)

		validator := queuehooks.NewQueueValidator(ctrlClient, queuehooks.OverSubscriptionModeWarning)
		warnings, err := validator.ValidateCreate(ctx, child)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(ContainElement(ContainSubstring("CPU quota")))
	})

	It("rejects an over-subscribing child in block mode", func(ctx context.Context) {
		parent = newQueue("qv-parent", "", 100, 1, 1024)
		Expect(ctrlClient.Create(ctx, parent)).To(Succeed())
		child = newQueue("qv-child", parent.Name, 200, 2, 2048)

		validator := queuehooks.NewQueueValidator(ctrlClient, queuehooks.OverSubscriptionModeBlock)
		_, err := validator.ValidateCreate(ctx, child)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("over-subscription"))
	})

	It("admits a within-quota child in block mode", func(ctx context.Context) {
		parent = newQueue("qv-parent", "", 100, 4, 4096)
		Expect(ctrlClient.Create(ctx, parent)).To(Succeed())
		child = newQueue("qv-child", parent.Name, 50, 2, 2048)

		validator := queuehooks.NewQueueValidator(ctrlClient, queuehooks.OverSubscriptionModeBlock)
		warnings, err := validator.ValidateCreate(ctx, child)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeEmpty())
	})

	It("rejects lowering a parent below its children's quota sum in block mode", func(ctx context.Context) {
		parent = newQueue("qv-parent", "", 200, 8, 8192)
		Expect(ctrlClient.Create(ctx, parent)).To(Succeed())
		child = newQueue("qv-child", parent.Name, 120, 1, 1024)
		Expect(ctrlClient.Create(ctx, child)).To(Succeed())

		// No controller runs here, so populate the parent's children status directly.
		Expect(ctrlClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		parent.Status.ChildQueues = []string{child.Name}
		Expect(ctrlClient.Status().Update(ctx, parent)).To(Succeed())

		lowered := parent.DeepCopy()
		lowered.Spec.Resources.CPU.Quota = 100 // below child's 120

		validator := queuehooks.NewQueueValidator(ctrlClient, queuehooks.OverSubscriptionModeBlock)
		_, err := validator.ValidateUpdate(ctx, parent, lowered)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("over-subscription"))
	})
})
