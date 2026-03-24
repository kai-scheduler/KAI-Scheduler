// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("SchedulingShardReconciler", Ordered, func() {
	BeforeAll(func() {
		Expect(kaiv1.AddToScheme(scheme.Scheme)).To(Succeed())
	})

	Context("fake client", func() {
		var (
			fakeClient client.Client
		)
		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		})
		DescribeTable(
			"Reconcile",
			func(
				request ctrl.Request, objects []client.Object,
				expectedError bool, expectedObjects []client.Object,
			) {
				for _, object := range objects {
					err := fakeClient.Create(context.Background(), object)
					Expect(err).NotTo(HaveOccurred())
				}
				controller := NewSchedulingShardReconciler(
					fakeClient, scheme.Scheme,
				)

				_, err := controller.Reconcile(context.Background(), request)
				if expectedError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}

				for _, object := range expectedObjects {
					existingObject := object.DeepCopyObject().(client.Object)
					err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(object), existingObject)
					Expect(err).NotTo(HaveOccurred())
				}
			},
			Entry(
				"Empty Cluster",
				ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "kai-scheduler", Name: "test"}},
				[]client.Object{}, false, []client.Object{},
			),
			Entry(
				"Only Shard object in cluster, missing kaiConfig",
				ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "kai-scheduler", Name: "test"}},
				[]client.Object{&kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{Namespace: "kai-scheduler", Name: "test"}}}, true, []client.Object{},
			),
			Entry(
				"Shard and kaiConfig Only",
				ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "kai-scheduler", Name: "test"}},
				[]client.Object{
					&kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{Namespace: "kai-scheduler", Name: "test"}},
					&kaiv1.Config{ObjectMeta: metav1.ObjectMeta{Name: "kai-config"}, Spec: kaiv1.ConfigSpec{Namespace: "kai-scheduler"}},
				},
				false,
				[]client.Object{
					&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "kai-scheduler", Name: "kai-scheduler-test"}},
				},
			),
		)
	})
})
