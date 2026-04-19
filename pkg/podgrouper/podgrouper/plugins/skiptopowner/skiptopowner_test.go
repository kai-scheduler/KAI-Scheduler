// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package skiptopowner

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/grouper"
)

func TestSkipTopOwnerGrouper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SkipTopOwnerGrouper Suite")
}

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
	queueName        = "test-queue"
)

var examplePod = &v1.Pod{
	TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-pod",
		Namespace: "default",
		OwnerReferences: []metav1.OwnerReference{
			{
				Kind:       "Pod",
				APIVersion: "v1",
				Name:       "test-pod",
			},
		},
	},
}

var _ = Describe("SkipTopOwnerGrouper", func() {
	Describe("#GetPodGroupMetadata", func() {
		var (
			plugin         *skipTopOwnerGrouper
			client         client.Client
			defaultGrouper *defaultgrouper.DefaultGrouper
			supportedTypes map[metav1.GroupVersionKind]grouper.Grouper
		)

		BeforeEach(func() {
			client = fake.NewFakeClient()
			defaultGrouper = defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
			supportedTypes = map[metav1.GroupVersionKind]grouper.Grouper{
				{Group: "", Version: "v1", Kind: "Pod"}: defaultGrouper,
			}
			plugin = NewSkipTopOwnerGrouper(client, defaultGrouper, supportedTypes)
		})

		Context("when last owner is a pod", func() {
			It("returns metadata successfully", func() {
				pod := examplePod.DeepCopy()
				lastOwnerPartial := &metav1.PartialObjectMetadata{TypeMeta: pod.TypeMeta, ObjectMeta: pod.ObjectMeta}
				podObj := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"kind":       "Other",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"name":      lastOwnerPartial.Name,
							"namespace": "default",
							"labels": map[string]interface{}{
								queueLabelKey: queueName,
							},
						},
					},
				}
				Expect(client.Create(context.TODO(), pod)).To(Succeed())

				metadata, err := plugin.GetPodGroupMetadata(podObj, pod, lastOwnerPartial)

				Expect(err).NotTo(HaveOccurred())
				Expect(metadata).NotTo(BeNil())
				Expect(metadata.Queue).To(Equal(queueName))
			})
		})

		Context("with multiple other owners", func() {
			var (
				other      *unstructured.Unstructured
				deployment *appsv1.Deployment
				replicaSet *appsv1.ReplicaSet
				pod        *v1.Pod
			)
			BeforeEach(func() {
				other = &unstructured.Unstructured{
					Object: map[string]interface{}{
						"kind":       "Other",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"name":      "other",
							"namespace": "default",
							"labels": map[string]interface{}{
								queueLabelKey: queueName,
							},
						},
					},
				}
				deployment = &appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deployment",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind:       "Other",
								APIVersion: "v1",
								Name:       "other",
							},
						},
					},
				}

				replicaSet = &appsv1.ReplicaSet{
					TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-replicaset",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind:       "Deployment",
								APIVersion: "apps/v1",
								Name:       "test-deployment",
							},
						},
					},
				}

				pod = examplePod.DeepCopy()
				pod.OwnerReferences = []metav1.OwnerReference{
					{
						Kind:       "ReplicaSet",
						APIVersion: "apps/v1",
						Name:       "test-replicaset",
					},
				}

				Expect(client.Create(context.TODO(), deployment)).To(Succeed())
				Expect(client.Create(context.TODO(), replicaSet)).To(Succeed())
				Expect(client.Create(context.TODO(), pod)).To(Succeed())
			})

			It("uses the second last owner when there are multiple", func() {
				supportedTypes[metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}] = defaultGrouper

				otherOwners := []*metav1.PartialObjectMetadata{objectToPartial(replicaSet), objectToPartial(deployment), objectToPartial(other)}
				metadata, err := plugin.GetPodGroupMetadata(other, pod, otherOwners...)

				Expect(err).NotTo(HaveOccurred())
				Expect(metadata).NotTo(BeNil())
				Expect(metadata.Queue).To(Equal(queueName))
				Expect(metadata.Owner).To(Equal(replicaSet.OwnerReferences[0]))
			})

			It("uses the default function if no handler for owner is found", func() {
				otherOwners := []*metav1.PartialObjectMetadata{objectToPartial(replicaSet), objectToPartial(deployment), objectToPartial(other)}
				metadata, err := plugin.GetPodGroupMetadata(other, pod, otherOwners...)
				Expect(err).NotTo(HaveOccurred())
				Expect(metadata).NotTo(BeNil())
				Expect(metadata.Queue).To(Equal(queueName))
				Expect(metadata.Owner).To(Equal(replicaSet.OwnerReferences[0]))
			})
		})
	})

})

func objectToPartial(obj client.Object) *metav1.PartialObjectMetadata {
	objectMeta := metav1.ObjectMeta{
		Name:            obj.GetName(),
		Namespace:       obj.GetNamespace(),
		Labels:          obj.GetLabels(),
		OwnerReferences: obj.GetOwnerReferences(),
	}
	groupVersion := obj.GetObjectKind().GroupVersionKind()
	typeMeta := metav1.TypeMeta{
		Kind:       groupVersion.Kind,
		APIVersion: groupVersion.Group + "/" + groupVersion.Version,
	}

	return &metav1.PartialObjectMetadata{TypeMeta: typeMeta, ObjectMeta: objectMeta}
}
