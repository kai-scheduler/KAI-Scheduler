/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package queuehooks

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
)

func TestQueueValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Queue Validator Suite")
}

var _ = Describe("Queue Validator", func() {
	var (
		ctx       context.Context
		validator *queueValidator
		scheme    *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		_ = v2.AddToScheme(scheme)
	})

	Context("ValidateCreate", func() {
		It("should reject queue without resources", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeNone}

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Spec:       v2.QueueSpec{},
			}

			warnings, err := validator.ValidateCreate(ctx, queue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal(missingResourcesError))
			Expect(warnings).To(ContainElement(missingResourcesError))
		})

		It("should accept queue with resources", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeNone}

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{
						CPU:    v2.QueueResource{Quota: 1000},
						GPU:    v2.QueueResource{Quota: 4},
						Memory: v2.QueueResource{Quota: 8192},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, queue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should not check quota when mode is none even if child exceeds parent", func() {
			parent := newQueueWithQuota("parent", "", 100, 1, 1024)
			child := newQueueWithQuota("child", "parent", 200, 2, 2048)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeNone}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should warn when child quota exceeds parent in warning mode", func() {
			parent := newQueueWithQuota("parent", "", 100, 1, 1024)
			child := newQueueWithQuota("child", "parent", 200, 2, 2048)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeWarning}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).NotTo(BeEmpty())
			Expect(warnings).To(ContainElement(ContainSubstring("CPU quota")))
		})

		It("should reject when child quota exceeds parent in block mode", func() {
			parent := newQueueWithQuota("parent", "", 100, 1, 1024)
			child := newQueueWithQuota("child", "parent", 200, 2, 2048)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeBlock}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("over-subscription"))
			Expect(warnings).To(BeEmpty())
		})

		It("should accept a child within parent quota in block mode", func() {
			parent := newQueueWithQuota("parent", "", 100, 4, 2048)
			child := newQueueWithQuota("child", "parent", 50, 2, 1024)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeBlock}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should warn on GPU-only over-subscription", func() {
			parent := newQueueWithQuota("parent", "", 1000, 1, 8192)
			child := newQueueWithQuota("child", "parent", 10, 4, 100)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeWarning}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("GPU quota")))
			Expect(warnings).NotTo(ContainElement(ContainSubstring("CPU quota")))
		})

		It("should warn on Memory-only over-subscription", func() {
			parent := newQueueWithQuota("parent", "", 1000, 8, 1024)
			child := newQueueWithQuota("child", "parent", 10, 2, 4096)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeWarning}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("Memory quota")))
			Expect(warnings).NotTo(ContainElement(ContainSubstring("GPU quota")))
		})

		It("should aggregate multiple violations into a single block error", func() {
			parent := newQueueWithQuota("parent", "", 100, 1, 1024)
			child := newQueueWithQuota("child", "parent", 200, 4, 4096)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeBlock}

			_, err := validator.ValidateCreate(ctx, child)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU quota"))
			Expect(err.Error()).To(ContainSubstring("GPU quota"))
			Expect(err.Error()).To(ContainSubstring("Memory quota"))
			Expect(err.Error()).To(ContainSubstring(";"))
		})

		It("should return a hard error when the parent queue does not exist", func() {
			child := newQueueWithQuota("child", "missing-parent", 10, 1, 100)
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeWarning}

			_, err := validator.ValidateCreate(ctx, child)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get parent queue"))
		})
	})

	Context("ValidateUpdate", func() {
		var (
			parent    *v2.Queue
			child1    *v2.Queue
			child2    *v2.Queue
			oldParent *v2.Queue
		)

		BeforeEach(func() {
			// children sum (120 CPU) exceeds parent quota (100 CPU).
			// Status.ChildQueues is a subresource carried onto the updated
			// object, so it is present on both old and new.
			parent = newQueueWithQuota("parent", "", 100, 8, 8192)
			parent.Status.ChildQueues = []string{"child-1", "child-2"}
			child1 = newQueueWithQuota("child-1", "parent", 60, 1, 1024)
			child2 = newQueueWithQuota("child-2", "parent", 60, 1, 1024)
			oldParent = parent.DeepCopy()
		})

		It("should skip children-sum checks in none mode", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child1, child2).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeNone}

			warnings, err := validator.ValidateUpdate(ctx, oldParent, parent)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should warn when children quota sum exceeds parent in warning mode", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child1, child2).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeWarning}

			warnings, err := validator.ValidateUpdate(ctx, oldParent, parent)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("total children CPU quota")))
		})

		It("should reject when children quota sum exceeds parent in block mode", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child1, child2).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeBlock}

			warnings, err := validator.ValidateUpdate(ctx, oldParent, parent)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("over-subscription"))
			Expect(warnings).To(BeEmpty())
		})

		It("should accept when children quota sum is within parent quota", func() {
			withinParent := newQueueWithQuota("parent", "", 200, 8, 8192)
			withinParent.Status.ChildQueues = []string{"child-1", "child-2"}
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child1, child2).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeBlock}

			warnings, err := validator.ValidateUpdate(ctx, oldParent, withinParent)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("ValidateDelete", func() {
		It("should reject deletion of queue with children", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeNone}

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Status: v2.QueueStatus{
					ChildQueues: []string{"child-1", "child-2"},
				},
			}

			warnings, err := validator.ValidateDelete(ctx, queue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot delete queue"))
			Expect(warnings).To(BeNil())
		})

		It("should allow deletion of queue without children", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeNone}

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "leaf-queue"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
				},
			}

			warnings, err := validator.ValidateDelete(ctx, queue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
	})

	Context("unlimited quota sentinel (-1)", func() {
		It("should flag an unlimited child under a finite parent", func() {
			parent := newQueueWithQuota("parent", "", 10, 10, 10)
			child := newQueueWithQuota("child", "parent", -1, 0, 0)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeWarning}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("child queue CPU quota (unlimited)")))
		})

		It("should not flag a finite child under an unlimited parent", func() {
			parent := newQueueWithQuota("parent", "", -1, -1, -1)
			child := newQueueWithQuota("child", "parent", 1000, 8, 8192)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeBlock}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should reject an unlimited child under a finite parent in block mode", func() {
			parent := newQueueWithQuota("parent", "", 10, 10, 10)
			child := newQueueWithQuota("child", "parent", -1, 0, 0)
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeBlock}

			_, err := validator.ValidateCreate(ctx, child)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU quota (unlimited)"))
		})

		It("should treat an unlimited child as making the children sum exceed a finite parent", func() {
			// parent CPU=10, child-1 CPU=-1 (unlimited), child-2 CPU=5:
			// naive addition gives 4 (<10) and masks the over-subscription.
			parent := newQueueWithQuota("parent", "", 10, 10, 10)
			parent.Status.ChildQueues = []string{"child-1", "child-2"}
			child1 := newQueueWithQuota("child-1", "parent", -1, 0, 0)
			child2 := newQueueWithQuota("child-2", "parent", 5, 0, 0)
			oldParent := parent.DeepCopy()
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child1, child2).Build()
			validator = &queueValidator{kubeClient: client, overSubscriptionMode: OverSubscriptionModeWarning}

			warnings, err := validator.ValidateUpdate(ctx, oldParent, parent)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("total children CPU quota (unlimited)")))
		})
	})

	Context("ParseOverSubscriptionMode", func() {
		It("should default empty to none", func() {
			mode, err := ParseOverSubscriptionMode("")
			Expect(err).NotTo(HaveOccurred())
			Expect(mode).To(Equal(OverSubscriptionModeNone))
		})

		It("should parse warning and block", func() {
			mode, err := ParseOverSubscriptionMode("warning")
			Expect(err).NotTo(HaveOccurred())
			Expect(mode).To(Equal(OverSubscriptionModeWarning))

			mode, err = ParseOverSubscriptionMode("block")
			Expect(err).NotTo(HaveOccurred())
			Expect(mode).To(Equal(OverSubscriptionModeBlock))
		})

		It("should reject unknown values", func() {
			_, err := ParseOverSubscriptionMode("bogus")
			Expect(err).To(HaveOccurred())
		})
	})
})

func newQueueWithQuota(name, parent string, cpu, gpu, memory float64) *v2.Queue {
	return &v2.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v2.QueueSpec{
			ParentQueue: parent,
			Resources: &v2.QueueResources{
				CPU:    v2.QueueResource{Quota: cpu},
				GPU:    v2.QueueResource{Quota: gpu},
				Memory: v2.QueueResource{Quota: memory},
			},
		},
	}
}
