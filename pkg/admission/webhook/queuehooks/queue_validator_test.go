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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	Context("ValidateUpdate allocation-reduction checks", func() {
		newValidator := func(strict bool) *queueValidator {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()
			return &queueValidator{kubeClient: c, strictQuotaValidation: strict}
		}

		// queueWith builds a queue carrying both a spec (old resources) and a status (last allocation).
		queueWith := func(resources *v2.QueueResources, allocated, nonPreemptible v1.ResourceList) *v2.Queue {
			return &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Spec:       v2.QueueSpec{Resources: resources},
				Status: v2.QueueStatus{
					Allocated:               allocated,
					AllocatedNonPreemptible: nonPreemptible,
				},
			}
		}

		spec := func(resources *v2.QueueResources) *v2.Queue {
			return queueWith(resources, nil, nil)
		}

		It("rejects reducing a CPU limit below the currently allocated amount when strict", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 500}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU limit (0.5 cores) is below the currently allocated 0.75 cores"))
		})

		It("warns instead of rejecting when quota validation is enabled but not strict", func() {
			validator = &queueValidator{
				kubeClient:            fake.NewClientBuilder().WithScheme(scheme).Build(),
				enableQuotaValidation: true,
			}
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 500}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("CPU limit (0.5 cores) is below the currently allocated 0.75 cores")))
		})

		It("neither warns nor blocks when no validation flag is set", func() {
			validator = &queueValidator{kubeClient: fake.NewClientBuilder().WithScheme(scheme).Build()}
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 500}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("rejects reducing a GPU quota below the non-preemptible allocation when strict", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 4}},
				nil, v1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")})
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 4}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GPU quota (1) is below the non-preemptible allocation (2)"))
		})

		It("rejects reducing a Memory limit below the currently allocated amount when strict", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{Memory: v2.QueueResource{Quota: 8000, Limit: 8000}},
				v1.ResourceList{v1.ResourceMemory: resource.MustParse("2Gi")}, nil)
			newQueue := spec(&v2.QueueResources{Memory: v2.QueueResource{Quota: 8000, Limit: 1000}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Memory limit (1000000000 bytes) is below the currently allocated 2147483648 bytes"))
		})

		It("rejects reducing a CPU quota below the non-preemptible allocation when strict", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000}},
				nil, v1.ResourceList{v1.ResourceCPU: resource.MustParse("1500m")})
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU quota (1 cores) is below the non-preemptible allocation (1.5 cores)"))
		})

		It("reports every violating resource in a single update", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{
					CPU: v2.QueueResource{Quota: 2000, Limit: 4000},
					GPU: v2.QueueResource{Quota: 4, Limit: 4},
				},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")},
			)
			newQueue := spec(&v2.QueueResources{
				CPU: v2.QueueResource{Quota: 2000, Limit: 500},
				GPU: v2.QueueResource{Quota: 1, Limit: 4},
			})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU limit"))
			Expect(err.Error()).To(ContainSubstring("GPU quota"))
		})

		It("allows a reduction that stays at or above the allocation", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 1000, Limit: 2000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("250m")},
			)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 1000, Limit: 1000}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("ignores unset (0) and unlimited (-1) new limit and quota values", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 2000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")},
			)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: -1, Limit: 0}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("allows an update when the queue status has no allocation yet", func() {
			validator = newValidator(true)
			oldQueue := queueWith(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}}, nil, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 1000, Limit: 500}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("does not block an unrelated edit on an already-over-limit queue", func() {
			validator = newValidator(true)
			resources := &v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 4}}
			oldQueue := queueWith(resources, v1.ResourceList{"nvidia.com/gpu": resource.MustParse("5")}, nil)
			newQueue := queueWith(resources, nil, nil)
			newQueue.Spec.DisplayName = "renamed"

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("does not block raising a limit on an already-over-limit queue", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 4}},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("5")}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 6}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("allows reducing a fractional GPU limit to exactly the current allocation", func() {
			validator = newValidator(true)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 1}},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("700m")}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 0.7}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})
})
