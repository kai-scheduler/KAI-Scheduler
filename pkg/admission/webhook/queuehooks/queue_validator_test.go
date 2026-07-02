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
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			validator = &queueValidator{kubeClient: client, enableQuotaValidation: false}

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
			validator = &queueValidator{kubeClient: client, enableQuotaValidation: false}

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
	})

	Context("ValidateDelete", func() {
		It("should reject deletion of queue with children", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			validator = &queueValidator{kubeClient: client, enableQuotaValidation: false}

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
			validator = &queueValidator{kubeClient: client, enableQuotaValidation: false}

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

	Context("resource value validation (quota validation enabled)", func() {
		newValidator := func(objs ...client.Object) *queueValidator {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			return &queueValidator{kubeClient: c, enableQuotaValidation: true}
		}

		It("warns when a limit is set below its quota", func() {
			validator = newValidator()

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{
						CPU:    v2.QueueResource{Quota: 2000, Limit: 1000},
						GPU:    v2.QueueResource{Quota: 4, Limit: 2},
						Memory: v2.QueueResource{Quota: 8192, Limit: 4096},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, queue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("CPU limit (1000) is below its quota (2000)")))
			Expect(warnings).To(ContainElement(ContainSubstring("GPU limit (2) is below its quota (4)")))
			Expect(warnings).To(ContainElement(ContainSubstring("Memory limit (4096) is below its quota (8192)")))
		})

		It("does not warn when a limit is unlimited (-1) or unset (0)", func() {
			validator = newValidator()

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{
						CPU:    v2.QueueResource{Quota: 2000, Limit: -1},
						GPU:    v2.QueueResource{Quota: 4, Limit: 0},
						Memory: v2.QueueResource{Quota: 8192, Limit: 8192},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, queue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("warns on invalid negative values", func() {
			validator = newValidator()

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{
						CPU:    v2.QueueResource{Quota: -5},
						GPU:    v2.QueueResource{Quota: 1, Limit: -5},
						Memory: v2.QueueResource{Quota: 1, OverQuotaWeight: -1},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, queue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("CPU quota (-5) is invalid")))
			Expect(warnings).To(ContainElement(ContainSubstring("GPU limit (-5) is invalid")))
			Expect(warnings).To(ContainElement(ContainSubstring("Memory overQuotaWeight (-1) is invalid")))
		})

		It("does not run resource value checks when quota validation is disabled", func() {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()
			validator = &queueValidator{kubeClient: c, enableQuotaValidation: false}

			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{
						CPU: v2.QueueResource{Quota: 2000, Limit: 1000},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, queue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("parent-child quota validation (quota validation enabled)", func() {
		newValidator := func(objs ...client.Object) *queueValidator {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			return &queueValidator{kubeClient: c, enableQuotaValidation: true}
		}

		It("warns when the sum of children GPU quota exceeds the parent", func() {
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{GPU: v2.QueueResource{Quota: 2}},
				},
				Status: v2.QueueStatus{ChildQueues: []string{"existing-child"}},
			}
			existingChild := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-child"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{GPU: v2.QueueResource{Quota: 1.5}},
				},
			}
			validator = newValidator(parent, existingChild)

			newChild := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "new-child"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{GPU: v2.QueueResource{Quota: 1.5}},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("total children GPU quota (3) exceeds parent queue parent-queue GPU quota (2)")))
		})

		It("warns when the sum of children Memory quota exceeds the parent", func() {
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{Memory: v2.QueueResource{Quota: 8192}},
				},
				Status: v2.QueueStatus{ChildQueues: []string{"existing-child"}},
			}
			existingChild := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-child"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{Memory: v2.QueueResource{Quota: 5000}},
				},
			}
			validator = newValidator(parent, existingChild)

			newChild := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "new-child"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{Memory: v2.QueueResource{Quota: 5000}},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("total children Memory quota (10000) exceeds parent queue parent-queue Memory quota (8192)")))
		})

		It("warns when an unlimited (-1) child is under a bounded parent", func() {
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{GPU: v2.QueueResource{Quota: 2}},
				},
			}
			validator = newValidator(parent)

			child := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child-queue"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{GPU: v2.QueueResource{Quota: -1}},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("child queue GPU quota (unlimited) exceeds parent queue parent-queue GPU quota (2)")))
		})

		It("warns when an existing unlimited (-1) sibling makes the children sum exceed a bounded parent", func() {
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{GPU: v2.QueueResource{Quota: 2}},
				},
				Status: v2.QueueStatus{ChildQueues: []string{"existing-child"}},
			}
			existingChild := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-child"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{GPU: v2.QueueResource{Quota: -1}},
				},
			}
			validator = newValidator(parent, existingChild)

			newChild := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "new-child"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{GPU: v2.QueueResource{Quota: 1}},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("total children GPU quota (unlimited) exceeds parent queue parent-queue GPU quota (2)")))
		})

		It("does not warn when the parent quota is unlimited (-1)", func() {
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{CPU: v2.QueueResource{Quota: -1}},
				},
			}
			validator = newValidator(parent)

			child := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child-queue"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{CPU: v2.QueueResource{Quota: 5000}},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, child)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})
})
