// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestK8sHelpers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "K8sHelpers Suite")
}

func buildPod(namespace string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: namespace,
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: "test-pod-group",
				"existing-annotation":                    "existing-value",
			},
			Labels: map[string]string{
				"existing-label": "existing-value",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "test-container",
				},
			},
		},
	}
	return pod
}

var _ = Describe("K8sHelpers", func() {
	var (
		kubeclient kubernetes.Interface
	)

	BeforeEach(func() {
		kubeclient = fake.NewSimpleClientset()
	})

	Describe("PatchPodAnnotationsAndLabelsInterface", func() {
		It("PatchPodAnnotationsAndLabelsInterface", func() {
			namespace := "ns-1"
			pod := buildPod(namespace)
			if _, err := kubeclient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{}); err != nil {
				Fail(err.Error())
			}
			labels := map[string]interface{}{
				"some-label":     "label-value",
				"existing-label": "new-value",
			}
			annotations := map[string]interface{}{
				"some-annotation": "annotation-value",
			}
			Expect(Helpers.PatchPodAnnotationsAndLabelsInterface(kubeclient, pod, annotations, labels)).To(Succeed())
			pod, err := kubeclient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			Expect(pod.Annotations["some-annotation"]).To(Equal("annotation-value"))
			Expect(pod.Annotations["existing-annotation"]).To(Equal("existing-value"))
			Expect(len(pod.Annotations)).To(Equal(3))
			Expect(pod.Labels["some-label"]).To(Equal("label-value"))
			Expect(pod.Labels["existing-label"]).To(Equal("new-value"))
			Expect(len(pod.Labels)).To(Equal(2))
		})
	})
})
