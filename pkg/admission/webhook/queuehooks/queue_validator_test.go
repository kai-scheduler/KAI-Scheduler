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
			validator = &queueValidator{kubeClient: client, mode: EnforcementNone}

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
			validator = &queueValidator{kubeClient: client, mode: EnforcementNone}

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
			validator = &queueValidator{kubeClient: client, mode: EnforcementNone}

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
			validator = &queueValidator{kubeClient: client, mode: EnforcementNone}

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
		newValidator := func(mode EnforcementMode) *queueValidator {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()
			return &queueValidator{kubeClient: c, mode: mode}
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

		It("rejects reducing a CPU limit below the currently allocated amount when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 500}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU limit (0.5 cores) is below the currently allocated 0.75 cores"))
		})

		It("warns instead of rejecting in Warning mode", func() {
			validator = &queueValidator{
				kubeClient: fake.NewClientBuilder().WithScheme(scheme).Build(),
				mode:       EnforcementWarning,
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
			validator = &queueValidator{kubeClient: fake.NewClientBuilder().WithScheme(scheme).Build(), mode: EnforcementNone}
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 500}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("rejects reducing a GPU quota below the non-preemptible allocation when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 4}},
				nil, v1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")})
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 4}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GPU quota (1) is below the non-preemptible allocation (2)"))
		})

		It("rejects reducing a Memory limit below the currently allocated amount when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{Memory: v2.QueueResource{Quota: 8000, Limit: 8000}},
				v1.ResourceList{v1.ResourceMemory: resource.MustParse("2Gi")}, nil)
			newQueue := spec(&v2.QueueResources{Memory: v2.QueueResource{Quota: 8000, Limit: 1000}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Memory limit (1000000000 bytes) is below the currently allocated 2147483648 bytes"))
		})

		It("rejects reducing a Memory quota below the non-preemptible allocation when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{Memory: v2.QueueResource{Quota: 8000}},
				nil, v1.ResourceList{v1.ResourceMemory: resource.MustParse("2Gi")})
			newQueue := spec(&v2.QueueResources{Memory: v2.QueueResource{Quota: 1000}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Memory quota (1000000000 bytes) is below the non-preemptible allocation (2147483648 bytes)"))
		})

		It("rejects reducing a CPU quota below the non-preemptible allocation when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000}},
				nil, v1.ResourceList{v1.ResourceCPU: resource.MustParse("1500m")})
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU quota (1 cores) is below the non-preemptible allocation (1.5 cores)"))
		})

		It("reports every violating resource in a single update", func() {
			validator = newValidator(EnforcementBlock)
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
			validator = newValidator(EnforcementBlock)
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

		It("allows setting a limit or quota to unlimited (-1)", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 2000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")},
			)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: -1, Limit: -1}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("rejects reducing a limit to 0 (hard zero cap) below the allocation when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 2000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 0}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU limit (0 cores) is below the currently allocated 0.75 cores"))
		})

		It("rejects reducing a quota to 0 below the non-preemptible allocation when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000}},
				nil, v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")})
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 0}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU quota (0 cores) is below the non-preemptible allocation (0.75 cores)"))
		})

		It("treats an update from unlimited (-1) to 0 as a reduction when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: -1}},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("3")}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 0}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GPU limit (0) is below the currently allocated 3"))
		})

		It("does not treat an invalid negative limit (< -1) as unbounded when Block", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 4}},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("3")}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: -2}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
		})

		It("warns on a limit reduction to 0 in Warning mode", func() {
			validator = newValidator(EnforcementWarning)
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 2000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 0}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("CPU limit (0 cores) is below the currently allocated 0.75 cores")))
		})

		It("allows raising a limit from 0 (increase, not a reduction)", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 0}},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("3")}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 2}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("allows an update when the queue status has no allocation yet", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}}, nil, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 1000, Limit: 500}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("does not block an unrelated edit on an already-over-limit queue", func() {
			validator = newValidator(EnforcementBlock)
			resources := &v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 4}}
			oldQueue := queueWith(resources, v1.ResourceList{"nvidia.com/gpu": resource.MustParse("5")}, nil)
			newQueue := queueWith(resources, nil, nil)
			newQueue.Spec.DisplayName = "renamed"

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("does not block raising a limit on an already-over-limit queue", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 4}},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("5")}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 4, Limit: 6}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("allows reducing a fractional GPU limit to exactly the current allocation", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 1}},
				v1.ResourceList{"nvidia.com/gpu": resource.MustParse("700m")}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 1, Limit: 0.7}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("sums GPU allocation across multiple vendor resources when checking a limit reduction", func() {
			validator = newValidator(EnforcementBlock)
			oldQueue := queueWith(
				&v2.QueueResources{GPU: v2.QueueResource{Quota: 6, Limit: 6}},
				v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("3"),
					"amd.com/gpu":    resource.MustParse("3"),
				}, nil)
			newQueue := spec(&v2.QueueResources{GPU: v2.QueueResource{Quota: 6, Limit: 4}})

			_, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GPU limit (4) is below the currently allocated 6"))
		})

		It("warns but does not block on parent/child overcommit when quota validation is enabled", func() {
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec:       v2.QueueSpec{Resources: &v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}}},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: c, enableQuotaValidation: true, mode: EnforcementBlock}

			child := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child-queue"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{CPU: v2.QueueResource{Quota: 2000}},
				},
			}

			warnings, err := validator.ValidateUpdate(ctx, child, child.DeepCopy())
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ContainElement(ContainSubstring("exceeds parent queue parent-queue CPU quota")))
		})

		It("does not enforce allocation reduction when only quota validation is enabled (mode None)", func() {
			validator = &queueValidator{
				kubeClient:            fake.NewClientBuilder().WithScheme(scheme).Build(),
				enableQuotaValidation: true,
				mode:                  EnforcementNone,
			}
			oldQueue := queueWith(
				&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 4000}},
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("750m")}, nil)
			newQueue := spec(&v2.QueueResources{CPU: v2.QueueResource{Quota: 2000, Limit: 500}})

			warnings, err := validator.ValidateUpdate(ctx, oldQueue, newQueue)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("does not run parent/child validation under enforcement mode alone", func() {
			parent := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent-queue"},
				Spec:       v2.QueueSpec{Resources: &v2.QueueResources{CPU: v2.QueueResource{Quota: 1000}}},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			validator = &queueValidator{kubeClient: c, enableQuotaValidation: false, mode: EnforcementBlock}

			child := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child-queue"},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
					Resources:   &v2.QueueResources{CPU: v2.QueueResource{Quota: 2000}},
				},
			}

			warnings, err := validator.ValidateUpdate(ctx, child, child.DeepCopy())
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("ParseEnforcementMode", func() {
		DescribeTable("parses mode strings case-insensitively",
			func(input string, expected EnforcementMode) {
				mode, err := ParseEnforcementMode(input)
				Expect(err).NotTo(HaveOccurred())
				Expect(mode).To(Equal(expected))
			},
			Entry("empty defaults to None", "", EnforcementNone),
			Entry("None", "None", EnforcementNone),
			Entry("lowercase none", "none", EnforcementNone),
			Entry("Warning", "Warning", EnforcementWarning),
			Entry("uppercase WARNING", "WARNING", EnforcementWarning),
			Entry("Block", "Block", EnforcementBlock),
			Entry("lowercase block", "block", EnforcementBlock),
		)

		It("rejects an unknown mode", func() {
			_, err := ParseEnforcementMode("strict")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid quota enforcement mode"))
		})
	})
})
