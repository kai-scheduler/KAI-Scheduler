// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestKnownTypes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KnownTypes Suite")
}

var _ = Describe("KnownTypes", func() {
	Context("Owner Keys For Indexing", func() {
		It("should return the correct owner key", func() {
			obj := &kaiv1.Config{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Config",
					APIVersion: kaiv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: SingletonInstanceName,
					UID:  SingletonInstanceName,
				},
			}

			owner := &metav1.OwnerReference{
				APIVersion: kaiv1.GroupVersion.String(),
				Kind:       "Config",
				Name:       SingletonInstanceName,
				UID:        SingletonInstanceName,
			}

			Expect(getOwnerKey(owner)).To(Equal(getReconcilerKey(obj)))
		})
	})
})

var _ = Describe("vpaIndexer", func() {
	It("should return owner key for VPA owned by KAI", func() {
		vpa := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-vpa",
				Namespace: "ns",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: kaiv1.GroupVersion.String(),
						Kind:       "Config",
						Name:       SingletonInstanceName,
						UID:        types.UID("uid-123"),
						Controller: ptrBool(true),
					},
				},
			},
		}

		keys := vpaIndexer(vpa)
		Expect(keys).To(HaveLen(1))
	})

	It("should return nil for VPA not owned by KAI", func() {
		vpa := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-vpa",
			},
		}

		keys := vpaIndexer(vpa)
		Expect(keys).To(BeNil())
	})
})

var _ = Describe("VPAFieldInherit", func() {
	It("should be a no-op when current is nil", func() {
		desired := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "vpa"},
		}
		VPAFieldInherit(nil, desired)
		Expect(desired.GetResourceVersion()).To(BeEmpty())
	})

	It("should copy metadata and status from current to desired", func() {
		current := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "vpa",
				ResourceVersion: "42",
				UID:             types.UID("abc"),
				Generation:      3,
				Annotations:     map[string]string{"server-added": "val"},
			},
			Status: vpav1.VerticalPodAutoscalerStatus{
				Recommendation: &vpav1.RecommendedPodResources{},
			},
		}
		desired := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "vpa",
				Annotations: map[string]string{"user-set": "keep"},
			},
		}

		VPAFieldInherit(current, desired)

		Expect(desired.GetResourceVersion()).To(Equal("42"))
		Expect(desired.GetUID()).To(Equal(types.UID("abc")))
		Expect(desired.GetGeneration()).To(Equal(int64(3)))
		Expect(desired.GetAnnotations()).To(HaveKeyWithValue("user-set", "keep"))
		Expect(desired.GetAnnotations()).To(HaveKeyWithValue("server-added", "val"))
		Expect(desired.Status.Recommendation).ToNot(BeNil())
	})
})

var _ = Describe("mergeAnnotations", func() {
	It("should return current annotations when desired is nil", func() {
		result := mergeAnnotations(nil, map[string]string{"a": "1"})
		Expect(result).To(Equal(map[string]string{"a": "1"}))
	})

	It("should not override desired annotations with current", func() {
		result := mergeAnnotations(
			map[string]string{"key": "desired"},
			map[string]string{"key": "current"},
		)
		Expect(result["key"]).To(Equal("desired"))
	})

	It("should merge non-overlapping annotations", func() {
		result := mergeAnnotations(
			map[string]string{"a": "1"},
			map[string]string{"b": "2"},
		)
		Expect(result).To(Equal(map[string]string{"a": "1", "b": "2"}))
	})
})

func ptrBool(b bool) *bool { return &b }
