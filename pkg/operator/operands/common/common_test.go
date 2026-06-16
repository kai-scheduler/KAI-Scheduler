// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"

	nvidiav1 "github.com/NVIDIA/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCommon(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Common Functions Suite")
}

var _ = Describe("AllControllersAvailable", func() {
	Context("No api errors", func() {
		DescribeTable(
			"should check given controller types for availability",
			func(existingObjects, objectsToCheck []client.Object, expected bool) {
				runtimeExistingObjects := make([]runtime.Object, len(existingObjects))
				for i := range existingObjects {
					runtimeExistingObjects[i] = existingObjects[i]
				}
				testScheme := scheme.Scheme
				utilruntime.Must(nvidiav1.AddToScheme(testScheme))
				fakeKubeClient := fake.NewClientBuilder().WithScheme(testScheme).
					WithRuntimeObjects(runtimeExistingObjects...).Build()

				available, err := AllControllersAvailable(context.Background(), fakeKubeClient, objectsToCheck)
				if expected && !errors.IsNotFound(err) {
					Expect(err).To(BeNil())
				}
				Expect(available).To(Equal(expected))
			},
			Entry("empty list", []client.Object{}, []client.Object{}, true),
			Entry("fail to find object", []client.Object{}, []client.Object{
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: "bar",
					},
				},
			}, false),
			Entry(
				"Deployment not available - replicas defined",
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: ptr.To(int32(1)),
						},
						Status: appsv1.DeploymentStatus{
							UpdatedReplicas: 0,
							Conditions: []appsv1.DeploymentCondition{
								{
									Type:   appsv1.DeploymentAvailable,
									Status: v1.ConditionFalse,
								},
							},
						},
					},
				},
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				false,
			),
			Entry(
				"Deployment not available - condition is true but not enough pods are updated",
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: ptr.To(int32(1)),
						},
						Status: appsv1.DeploymentStatus{
							UpdatedReplicas: 0,
							Conditions: []appsv1.DeploymentCondition{
								{
									Type:   appsv1.DeploymentAvailable,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				false,
			),
			Entry(
				"Deployment available - replicas not defined",
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
						Status: appsv1.DeploymentStatus{
							Conditions: []appsv1.DeploymentCondition{
								{
									Type:   appsv1.DeploymentAvailable,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				false,
			),
			Entry(
				"Deployment available - replicas are defined",
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: ptr.To(int32(1)),
						},
						Status: appsv1.DeploymentStatus{
							UpdatedReplicas: 1,
							Conditions: []appsv1.DeploymentCondition{
								{
									Type:   appsv1.DeploymentAvailable,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				true,
			),
		)
	})
})

var _ = Describe("AllObjectsExists", func() {
	Context("No api errors", func() {
		DescribeTable(
			"should check given objects for existence",
			func(existingObjects []client.Object, objectsToCheck []client.Object, expected bool) {
				runtimeExistingObjects := make([]runtime.Object, len(existingObjects))
				for i := range existingObjects {
					runtimeExistingObjects[i] = existingObjects[i]
				}
				fakeKubeClient := fake.NewFakeClient(runtimeExistingObjects...)
				exists, err := AllObjectsExists(context.Background(), fakeKubeClient, objectsToCheck)
				Expect(err).To(BeNil())
				Expect(exists).To(Equal(expected))
			},
			Entry("empty list", []client.Object{}, []client.Object{}, true),
			Entry(
				"fail to find object",
				[]client.Object{},
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				false,
			),
			Entry(
				"Only some objects are missing",
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
					&v1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				false,
			),
			Entry("All Objects exist",
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
					&v1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				[]client.Object{
					&appsv1.Deployment{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
					&v1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					},
				},
				true,
			),
		)
	})
})
