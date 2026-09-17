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

	Context("ValidateCreate parent-child quota", func() {
		parentName := "parent-queue"

		newValidatorWithQueues := func(objects ...client.Object) *queueValidator {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			return &queueValidator{kubeClient: c, enableQuotaValidation: true}
		}

		parentQueue := func(resources v2.QueueResources, children ...string) *v2.Queue {
			return &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: parentName},
				Spec:       v2.QueueSpec{Resources: &resources},
				Status:     v2.QueueStatus{ChildQueues: children},
			}
		}

		childQueue := func(name string, resources v2.QueueResources) *v2.Queue {
			return &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: v2.QueueSpec{
					ParentQueue: parentName,
					Resources:   &resources,
				},
			}
		}

		It("should warn when siblings' combined GPU quota exceeds the parent", func() {
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 4}, Memory: v2.QueueResource{Quota: 8192}},
				"child-1",
			)
			existingChild := childQueue("child-1", v2.QueueResources{GPU: v2.QueueResource{Quota: 3}})
			validator = newValidatorWithQueues(parent, existingChild)

			newChild := childQueue("child-2", v2.QueueResources{GPU: v2.QueueResource{Quota: 2}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("total children GPU quota (5.00) exceeds parent queue parent-queue GPU quota (4.00)")))
		})

		It("should warn when siblings' combined Memory quota exceeds the parent", func() {
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 8}, Memory: v2.QueueResource{Quota: 8192}},
				"child-1",
			)
			existingChild := childQueue("child-1", v2.QueueResources{Memory: v2.QueueResource{Quota: 6000}})
			validator = newValidatorWithQueues(parent, existingChild)

			newChild := childQueue("child-2", v2.QueueResources{Memory: v2.QueueResource{Quota: 4000}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("total children Memory quota (10000) exceeds parent queue parent-queue Memory quota (8192)")))
		})

		It("should not warn when siblings' combined quota stays within the parent", func() {
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 8}, Memory: v2.QueueResource{Quota: 8192}},
				"child-1",
			)
			existingChild := childQueue("child-1", v2.QueueResources{CPU: v2.QueueResource{Quota: 200}, GPU: v2.QueueResource{Quota: 3}, Memory: v2.QueueResource{Quota: 2000}})
			validator = newValidatorWithQueues(parent, existingChild)

			newChild := childQueue("child-2", v2.QueueResources{CPU: v2.QueueResource{Quota: 200}, GPU: v2.QueueResource{Quota: 3}, Memory: v2.QueueResource{Quota: 2000}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should not warn when the parent quota is unlimited", func() {
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: -1}, GPU: v2.QueueResource{Quota: -1}, Memory: v2.QueueResource{Quota: -1}},
				"child-1",
			)
			existingChild := childQueue("child-1", v2.QueueResources{CPU: v2.QueueResource{Quota: 500}, GPU: v2.QueueResource{Quota: 8}, Memory: v2.QueueResource{Quota: 4000}})
			validator = newValidatorWithQueues(parent, existingChild)

			newChild := childQueue("child-2", v2.QueueResources{CPU: v2.QueueResource{Quota: 500}, GPU: v2.QueueResource{Quota: 8}, Memory: v2.QueueResource{Quota: 4000}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should treat an unlimited sibling as making the total unlimited", func() {
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 4}, Memory: v2.QueueResource{Quota: 8192}},
				"child-1",
			)
			unlimitedChild := childQueue("child-1", v2.QueueResources{GPU: v2.QueueResource{Quota: -1}})
			validator = newValidatorWithQueues(parent, unlimitedChild)

			newChild := childQueue("child-2", v2.QueueResources{GPU: v2.QueueResource{Quota: 1}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ConsistOf(ContainSubstring("total children GPU quota (unlimited) exceeds parent queue parent-queue GPU quota (4.00)")))
		})

		It("should not warn when fractional GPU quotas sum exactly to the parent", func() {
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 0.3}, Memory: v2.QueueResource{Quota: 8192}},
				"child-1",
			)
			existingChild := childQueue("child-1", v2.QueueResources{GPU: v2.QueueResource{Quota: 0.1}})
			validator = newValidatorWithQueues(parent, existingChild)

			newChild := childQueue("child-2", v2.QueueResources{GPU: v2.QueueResource{Quota: 0.2}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should warn when an unlimited child is created under a finite parent", func() {
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 4}, Memory: v2.QueueResource{Quota: 8192}},
			)
			validator = newValidatorWithQueues(parent)

			newChild := childQueue("child-1", v2.QueueResources{CPU: v2.QueueResource{Quota: 100}, GPU: v2.QueueResource{Quota: -1}, Memory: v2.QueueResource{Quota: 100}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ConsistOf(
				ContainSubstring("child queue GPU quota (unlimited) exceeds parent queue parent-queue GPU quota (4.00)"),
				ContainSubstring("total children GPU quota (unlimited) exceeds parent queue parent-queue GPU quota (4.00)"),
			))
		})

		It("admits an oversubscribing child (warns but does not reject)", func() {
			// Quota validation is advisory: oversubscription is a supported use case,
			// so an over-quota child is admitted (nil error) and only warned about.
			parent := parentQueue(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 4}, Memory: v2.QueueResource{Quota: 8192}},
				"child-1",
			)
			existingChild := childQueue("child-1", v2.QueueResources{CPU: v2.QueueResource{Quota: 800}, GPU: v2.QueueResource{Quota: 3}, Memory: v2.QueueResource{Quota: 6000}})
			validator = newValidatorWithQueues(parent, existingChild)

			newChild := childQueue("child-2", v2.QueueResources{CPU: v2.QueueResource{Quota: 800}, GPU: v2.QueueResource{Quota: 3}, Memory: v2.QueueResource{Quota: 6000}})

			warnings, err := validator.ValidateCreate(ctx, newChild)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ConsistOf(
				ContainSubstring("total children CPU quota (1600) exceeds parent queue parent-queue CPU quota (1000)"),
				ContainSubstring("total children GPU quota (6.00) exceeds parent queue parent-queue GPU quota (4.00)"),
				ContainSubstring("total children Memory quota (12000) exceeds parent queue parent-queue Memory quota (8192)"),
			))
		})
	})

	Context("ValidateUpdate children quota sum", func() {
		parentWithChildren := func(resources v2.QueueResources, children ...client.Object) *v2.Queue {
			var names []string
			for _, child := range children {
				names = append(names, child.GetName())
			}
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec:       v2.QueueSpec{Resources: &resources},
				Status:     v2.QueueStatus{ChildQueues: names},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(children, parent)...).Build()
			validator = &queueValidator{kubeClient: c, enableQuotaValidation: true}
			return parent
		}

		child := func(name string, resources v2.QueueResources) *v2.Queue {
			return &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec:       v2.QueueSpec{ParentQueue: "parent-queue", Resources: &resources},
			}
		}

		It("should treat unlimited quotas as unlimited when summing children", func() {
			parent := parentWithChildren(
				v2.QueueResources{CPU: v2.QueueResource{Quota: -1}, GPU: v2.QueueResource{Quota: 4}, Memory: v2.QueueResource{Quota: -1}},
				child("child-1", v2.QueueResources{CPU: v2.QueueResource{Quota: 500}, GPU: v2.QueueResource{Quota: -1}, Memory: v2.QueueResource{Quota: 4000}}),
				child("child-2", v2.QueueResources{CPU: v2.QueueResource{Quota: 500}, GPU: v2.QueueResource{Quota: 2}, Memory: v2.QueueResource{Quota: 4000}}),
				child("child-3", v2.QueueResources{CPU: v2.QueueResource{Quota: 500}, GPU: v2.QueueResource{Quota: 1}, Memory: v2.QueueResource{Quota: 4000}}),
			)

			warnings, err := validator.ValidateUpdate(ctx, parent, parent)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ConsistOf(
				ContainSubstring("child queue child-1 GPU quota (unlimited) exceeds parent GPU quota (4.00)"),
				ContainSubstring("total children GPU quota (unlimited) exceeds parent GPU quota (4.00)"),
			))
		})

		It("should warn per child for GPU and Memory like it does for CPU", func() {
			parent := parentWithChildren(
				v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}, GPU: v2.QueueResource{Quota: 4}, Memory: v2.QueueResource{Quota: 8192}},
				child("child-1", v2.QueueResources{CPU: v2.QueueResource{Quota: 2000}, GPU: v2.QueueResource{Quota: 5}, Memory: v2.QueueResource{Quota: 9000}}),
			)

			warnings, err := validator.ValidateUpdate(ctx, parent, parent)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ConsistOf(
				ContainSubstring("child queue child-1 CPU quota (2000) exceeds parent CPU quota (1000)"),
				ContainSubstring("child queue child-1 GPU quota (5.00) exceeds parent GPU quota (4.00)"),
				ContainSubstring("child queue child-1 Memory quota (9000) exceeds parent Memory quota (8192)"),
				ContainSubstring("total children CPU quota (2000) exceeds parent CPU quota (1000)"),
				ContainSubstring("total children GPU quota (5.00) exceeds parent GPU quota (4.00)"),
				ContainSubstring("total children Memory quota (9000) exceeds parent Memory quota (8192)"),
			))
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
})
