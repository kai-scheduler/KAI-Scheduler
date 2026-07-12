// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/queuehooks"
	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
)

var queueSeq int

func uniqueName(prefix string) string {
	queueSeq++
	return fmt.Sprintf("%s-%d", prefix, queueSeq)
}

func newQueue(name, parent string, cpu, gpu, memory float64) *v2.Queue {
	return &v2.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v2.QueueSpec{
			ParentQueue: parent,
			Resources: &v2.QueueResources{
				CPU:    v2.QueueResource{Quota: cpu, Limit: -1, OverQuotaWeight: 1},
				GPU:    v2.QueueResource{Quota: gpu, Limit: -1, OverQuotaWeight: 1},
				Memory: v2.QueueResource{Quota: memory, Limit: -1, OverQuotaWeight: 1},
			},
		},
	}
}

var _ = Describe("Queue validator webhook (envtest)", func() {
	BeforeEach(func() {
		warnings.reset()
		currentMode = queuehooks.OverSubscriptionModeNone
	})

	It("admits an over-subscribing child with no warnings in none mode", func() {
		currentMode = queuehooks.OverSubscriptionModeNone
		parent := newQueue(uniqueName("parent"), "", 100, 1, 1024)
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(deleteQueue, parent.Name)

		child := newQueue(uniqueName("child"), parent.Name, 200, 2, 2048)
		Expect(k8sClient.Create(ctx, child)).To(Succeed())
		DeferCleanup(deleteQueue, child.Name)

		Expect(warnings.all()).To(BeEmpty())
	})

	It("admits an over-subscribing child but surfaces a warning in warning mode", func() {
		parent := newQueue(uniqueName("parent"), "", 100, 1, 1024)
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(deleteQueue, parent.Name)

		currentMode = queuehooks.OverSubscriptionModeWarning
		child := newQueue(uniqueName("child"), parent.Name, 200, 2, 2048)
		Expect(k8sClient.Create(ctx, child)).To(Succeed())
		DeferCleanup(deleteQueue, child.Name)

		Expect(warnings.all()).To(ContainElement(ContainSubstring("CPU quota")))
	})

	It("rejects an over-subscribing child in block mode", func() {
		parent := newQueue(uniqueName("parent"), "", 100, 1, 1024)
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(deleteQueue, parent.Name)

		currentMode = queuehooks.OverSubscriptionModeBlock
		child := newQueue(uniqueName("child"), parent.Name, 200, 2, 2048)
		err := k8sClient.Create(ctx, child)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("over-subscription"))

		// The rejected child must not have been persisted.
		got := &v2.Queue{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: child.Name}, got)).NotTo(Succeed())
	})

	It("admits a within-quota child in block mode", func() {
		parent := newQueue(uniqueName("parent"), "", 100, 4, 4096)
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(deleteQueue, parent.Name)

		currentMode = queuehooks.OverSubscriptionModeBlock
		child := newQueue(uniqueName("child"), parent.Name, 50, 2, 2048)
		Expect(k8sClient.Create(ctx, child)).To(Succeed())
		DeferCleanup(deleteQueue, child.Name)
	})

	It("rejects lowering a parent's quota below its children's sum in block mode (update path)", func() {
		parent := newQueue(uniqueName("parent"), "", 200, 8, 8192)
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(deleteQueue, parent.Name)

		child1 := newQueue(uniqueName("child"), parent.Name, 60, 1, 1024)
		Expect(k8sClient.Create(ctx, child1)).To(Succeed())
		DeferCleanup(deleteQueue, child1.Name)
		child2 := newQueue(uniqueName("child"), parent.Name, 60, 1, 1024)
		Expect(k8sClient.Create(ctx, child2)).To(Succeed())
		DeferCleanup(deleteQueue, child2.Name)

		// Populate the parent's status with its children (no controller runs here).
		fresh := &v2.Queue{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parent.Name}, fresh)).To(Succeed())
		fresh.Status.ChildQueues = []string{child1.Name, child2.Name}
		Expect(k8sClient.Status().Update(ctx, fresh)).To(Succeed())

		// Lower the parent CPU quota (100) below the children sum (120) -> reject.
		currentMode = queuehooks.OverSubscriptionModeBlock
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: parent.Name}, fresh)).To(Succeed())
		fresh.Spec.Resources.CPU.Quota = 100
		err := k8sClient.Update(ctx, fresh)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("over-subscription"))
	})
})

func deleteQueue(name string) {
	q := &v2.Queue{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, q); err != nil {
		return
	}
	// Deletion of a queue with children is blocked by the validator; clear
	// status children first via the read client is unnecessary because
	// DeferCleanup runs children before parents (LIFO).
	_ = k8sClient.Delete(ctx, q)
}
