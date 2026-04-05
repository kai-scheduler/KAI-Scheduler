// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
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

var _ = Describe("MutatingWebhookConfigurationFieldInherit", func() {
	It("should preserve cloud-provider-injected namespaceSelector matchExpressions", func() {
		aksExpressions := []metav1.LabelSelectorRequirement{
			{Key: "control-plane", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"true"}},
			{Key: "kubernetes.azure.com/managedby", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"aks"}},
		}
		desiredExpression := metav1.LabelSelectorRequirement{
			Key: "kai-injection", Operator: metav1.LabelSelectorOpIn, Values: []string{"enabled"},
		}

		current := &admissionv1.MutatingWebhookConfiguration{
			Webhooks: []admissionv1.MutatingWebhook{
				{
					Name: "test-webhook",
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: append([]metav1.LabelSelectorRequirement{desiredExpression}, aksExpressions...),
					},
				},
			},
		}
		desired := &admissionv1.MutatingWebhookConfiguration{
			Webhooks: []admissionv1.MutatingWebhook{
				{
					Name: "test-webhook",
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{desiredExpression},
					},
				},
			},
		}

		MutatingWebhookConfigurationFieldInherit(current, desired)

		exprs := desired.Webhooks[0].NamespaceSelector.MatchExpressions
		Expect(exprs).To(ContainElement(desiredExpression))
		Expect(exprs).To(ContainElement(aksExpressions[0]))
		Expect(exprs).To(ContainElement(aksExpressions[1]))
	})

	It("should not duplicate namespaceSelector keys already present in desired", func() {
		expr := metav1.LabelSelectorRequirement{
			Key: "control-plane", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"true"},
		}
		current := &admissionv1.MutatingWebhookConfiguration{
			Webhooks: []admissionv1.MutatingWebhook{
				{Name: "wh", NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{expr}}},
			},
		}
		desired := &admissionv1.MutatingWebhookConfiguration{
			Webhooks: []admissionv1.MutatingWebhook{
				{Name: "wh", NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{expr}}},
			},
		}

		MutatingWebhookConfigurationFieldInherit(current, desired)

		Expect(desired.Webhooks[0].NamespaceSelector.MatchExpressions).To(HaveLen(1))
	})

	It("should inherit TimeoutSeconds and MatchPolicy from current when not set in desired", func() {
		timeout := int32(10)
		policy := admissionv1.Equivalent
		current := &admissionv1.MutatingWebhookConfiguration{
			Webhooks: []admissionv1.MutatingWebhook{
				{Name: "wh", TimeoutSeconds: &timeout, MatchPolicy: &policy},
			},
		}
		desired := &admissionv1.MutatingWebhookConfiguration{
			Webhooks: []admissionv1.MutatingWebhook{
				{Name: "wh"},
			},
		}

		MutatingWebhookConfigurationFieldInherit(current, desired)

		Expect(desired.Webhooks[0].TimeoutSeconds).To(Equal(&timeout))
		Expect(desired.Webhooks[0].MatchPolicy).To(Equal(&policy))
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
