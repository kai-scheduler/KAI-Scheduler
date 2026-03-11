// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
				utilruntime.Must(monitoringv1.AddToScheme(testScheme))
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

var _ = Describe("MergeAffinities", func() {
	It("should return globalAffinity when localAffinity is nil", func() {
		globalAffinity := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{},
		}
		result := MergeAffinities(nil, globalAffinity, nil, false)
		Expect(result).To(Equal(globalAffinity))
	})

	It("should return localAffinity when globalAffinity is nil", func() {
		localAffinity := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{},
		}
		result := MergeAffinities(localAffinity, nil, nil, false)
		Expect(result).To(Equal(localAffinity))
	})

	It("should prefer local NodeAffinity over global", func() {
		local := &v1.NodeAffinity{}
		global := &v1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{}}
		localAffinity := &v1.Affinity{NodeAffinity: local}
		globalAffinity := &v1.Affinity{NodeAffinity: global}
		result := MergeAffinities(localAffinity, globalAffinity, nil, false)
		Expect(result.NodeAffinity).To(Equal(local))
	})

	It("should prefer global NodeAffinity when local is nil", func() {
		global := &v1.NodeAffinity{}
		localAffinity := &v1.Affinity{}
		globalAffinity := &v1.Affinity{NodeAffinity: global}
		result := MergeAffinities(localAffinity, globalAffinity, nil, false)
		Expect(result.NodeAffinity).To(Equal(global))
	})

	It("should prefer local PodAffinity over global", func() {
		local := &v1.PodAffinity{}
		global := &v1.PodAffinity{}
		localAffinity := &v1.Affinity{PodAffinity: local}
		globalAffinity := &v1.Affinity{PodAffinity: global}
		result := MergeAffinities(localAffinity, globalAffinity, nil, false)
		Expect(result.PodAffinity).To(Equal(local))
	})

	It("should prefer global PodAffinity when local is nil", func() {
		global := &v1.PodAffinity{}
		localAffinity := &v1.Affinity{}
		globalAffinity := &v1.Affinity{PodAffinity: global}
		result := MergeAffinities(localAffinity, globalAffinity, nil, false)
		Expect(result.PodAffinity).To(Equal(global))
	})

	It("should use local PodAntiAffinity if present", func() {
		local := &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{TopologyKey: "foo"},
			},
		}
		localAffinity := &v1.Affinity{PodAntiAffinity: local}
		globalAffinity := &v1.Affinity{}
		result := MergeAffinities(localAffinity, globalAffinity, nil, false)
		Expect(result.PodAntiAffinity).To(Equal(local))
	})

	It("should use global PodAntiAffinity if local is nil", func() {
		global := &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{TopologyKey: "bar"},
			},
		}
		localAffinity := &v1.Affinity{}
		globalAffinity := &v1.Affinity{PodAntiAffinity: global}
		result := MergeAffinities(localAffinity, globalAffinity, nil, false)
		Expect(result.PodAntiAffinity).To(Equal(global))
	})

	It("should add default preferred podAntiAffinity when both are nil and label exists", func() {
		labelMap := map[string]string{"app": "my-app"}
		localAffinity := &v1.Affinity{}
		globalAffinity := &v1.Affinity{}
		result := MergeAffinities(localAffinity, globalAffinity, labelMap, true)
		Expect(result.PodAntiAffinity).ToNot(BeNil())
		Expect(result.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		Expect(result.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight).To(Equal(int32(100)))
		Expect(result.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.LabelSelector.MatchLabels).To(Equal(labelMap))
		Expect(result.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey).To(Equal("kubernetes.io/hostname"))
	})
})
