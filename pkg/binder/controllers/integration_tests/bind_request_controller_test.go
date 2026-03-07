// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	scheudlingv1alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/binder/common"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	namespaceName = "my-ns"
)

var _ = Describe("BindRequestReconciler", Ordered, func() {
	BeforeAll(func() {
		Expect(scheudlingv1alpha2.AddToScheme(scheme.Scheme)).To(Succeed())
	})

	Context("envtest", Ordered, func() {
		var (
			pod = &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      "my-pod",
				},
				Spec: v1.PodSpec{
					SchedulerName: "kai-scheduler",
					Containers: []v1.Container{
						{
							Name:  "my-container",
							Image: "my-image",
						},
					},
				},
			}
			namespace = &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			node = &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node-1",
				},
			}
			bindRequest = &scheudlingv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      "my-bind-request",
				},
				Spec: scheudlingv1alpha2.BindRequestSpec{
					PodName:              pod.Name,
					SelectedNode:         node.Name,
					ReceivedResourceType: common.ReceivedTypeRegular,
					ReceivedGPU:          &scheudlingv1alpha2.ReceivedGPU{Count: 1, Portion: "0"},
				},
			}
		)

		BeforeAll(func() {
			Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
			Expect(k8sClient.Create(context.Background(), namespace)).To(Succeed())
		})

		It("Binds a cpu only pod with bind request", func() {
			Expect(k8sClient.Create(context.Background(), pod.DeepCopy())).To(Succeed())
			Expect(k8sClient.Create(context.Background(), bindRequest.DeepCopy())).To(Succeed())

			Eventually(func(g Gomega) {
				updatedBindRequest := &scheudlingv1alpha2.BindRequest{}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(bindRequest), updatedBindRequest)).To(Succeed())
				g.Expect(updatedBindRequest.Status.Phase).To(Equal(scheudlingv1alpha2.BindRequestPhaseSucceeded))

				updatedPod := &v1.Pod{}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(pod), updatedPod)).To(Succeed())
				g.Expect(updatedPod.Spec.NodeName).To(Equal(node.Name))

			}, "3s", "20ms").Should(Succeed())
		})

		It("Binds multiple pods with bind request", func() {
			numberOfPods := 10
			pods := make([]*v1.Pod, numberOfPods)
			bindRequests := make([]*scheudlingv1alpha2.BindRequest, numberOfPods)

			for i := 0; i < numberOfPods; i++ {
				newPod := pod.DeepCopy()
				newPod.Name = fmt.Sprintf("%s-%d", pod.Name, i)
				pods[i] = newPod
				Expect(k8sClient.Create(context.Background(), newPod)).To(Succeed())

				newBindRequest := bindRequest.DeepCopy()
				newBindRequest.Name = fmt.Sprintf("%s-%d", bindRequest.Name, i)
				newBindRequest.Spec.PodName = newPod.Name
				bindRequests[i] = newBindRequest
			}

			for i := 0; i < numberOfPods; i++ {
				Expect(k8sClient.Create(context.Background(), bindRequests[i])).To(Succeed())
			}

			Eventually(func(g Gomega) {
				for i := 0; i < numberOfPods; i++ {
					updatedBindRequest := &scheudlingv1alpha2.BindRequest{}
					g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(bindRequests[i]), updatedBindRequest)).To(Succeed())
					g.Expect(updatedBindRequest.Status.Phase).To(Equal(scheudlingv1alpha2.BindRequestPhaseSucceeded))

					updatedPod := &v1.Pod{}
					g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(pods[i]), updatedPod)).To(Succeed())
					g.Expect(updatedPod.Spec.NodeName).To(Equal(node.Name))
				}
			}, "3s", "20ms").Should(Succeed())
		})

		AfterAll(func() {
			Expect(k8sClient.DeleteAllOf(
				context.Background(),
				&scheudlingv1alpha2.BindRequest{},
				client.InNamespace(namespaceName))).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), pod)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
		})
	})
})
