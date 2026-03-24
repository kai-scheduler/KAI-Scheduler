// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package skiptopowner

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	grouperplugin "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/grouper"
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
			supportedTypes map[metav1.GroupVersionKind]grouperplugin.Grouper
		)

		BeforeEach(func() {
			client = fake.NewFakeClient()
			defaultGrouper = defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
			supportedTypes = map[metav1.GroupVersionKind]grouperplugin.Grouper{
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
				pod.TypeMeta = metav1.TypeMeta{Kind: examplePod.Kind, APIVersion: examplePod.APIVersion} // https://github.com/kubernetes-sigs/controller-runtime/commit/685f27bb500fe40ede53379da1675cfa71387a94

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
						Name:      "middle-owner",
						Namespace: "test",
						Labels:    map[string]string{},
					},
				}
				replicaSet = &appsv1.ReplicaSet{
					TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-replicaset",
						Namespace: "test",
						Labels: map[string]string{
							queueLabelKey: queueName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind:       "Deployment",
								APIVersion: "apps/v1",
								Name:       "middle-owner",
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
				pod.TypeMeta = metav1.TypeMeta{Kind: examplePod.Kind, APIVersion: examplePod.APIVersion} // https://github.com/kubernetes-sigs/controller-runtime/commit/685f27bb500fe40ede53379da1675cfa71387a94
				replicaSet.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"}         // https://github.com/kubernetes-sigs/controller-runtime/commit/685f27bb500fe40ede53379da1675cfa71387a94
				deployment.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}         // https://github.com/kubernetes-sigs/controller-runtime/commit/685f27bb500fe40ede53379da1675cfa71387a94
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

		Context("propagateMetadataDownChain behavior", func() {
			It("propagates labels and annotations twice through the chain", func() {
				// Create a chain: topOwner -> middleOwner -> lastOwner -> pod
				topOwner := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"kind":       "Top",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"name":      "top-owner",
							"namespace": "default",
							"labels": map[string]interface{}{
								queueLabelKey:  queueName,
								"top-label":    "top-value",
								"shared-label": "top-shared",
							},
							"annotations": map[string]interface{}{
								"top-annotation":    "top-ann-value",
								"shared-annotation": "top-shared-ann",
							},
						},
					},
				}
				middleOwner := &appsv1.StatefulSet{
					TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "middle-owner",
						Namespace: "default",
						Labels: map[string]string{
							"middle-label": "middle-value",
							"shared-label": "middle-shared", // This should NOT be overridden
						},
						Annotations: map[string]string{
							"middle-annotation": "middle-ann-value",
							"shared-annotation": "middle-shared-ann", // This should NOT be overridden
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind:       "Top",
								APIVersion: "v1",
								Name:       "top-owner",
							},
						},
					},
				}
				lastOwner := &appsv1.ReplicaSet{
					TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "last-owner",
						Namespace: "default",
						Labels: map[string]string{
							queueLabelKey: queueName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind:       "StatefulSet",
								APIVersion: "apps/v1",
								Name:       "middle-owner",
							},
						},
					},
				}
				testPod := examplePod.DeepCopy()
				testPod.OwnerReferences = []metav1.OwnerReference{
					{
						Kind:       "ReplicaSet",
						APIVersion: "apps/v1",
						Name:       "last-owner",
					},
				}

				Expect(client.Create(context.TODO(), topOwner)).To(Succeed())
				Expect(client.Create(context.TODO(), middleOwner)).To(Succeed())
				Expect(client.Create(context.TODO(), lastOwner)).To(Succeed())
				Expect(client.Create(context.TODO(), testPod)).To(Succeed())
				testPod.TypeMeta = metav1.TypeMeta{Kind: examplePod.Kind, APIVersion: examplePod.APIVersion}
				middleOwner.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"}
				lastOwner.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"}

				// First propagation: skip topOwner, propagate to middleOwner
				// Manually propagate to simulate the first call (since propagateMetadataDownChain modifies in-memory only)
				middleOwnerPartial1 := objectToPartial(middleOwner)
				middleOwnerRetrieved1, err := plugin.getObjectInstance(middleOwnerPartial1)
				Expect(err).NotTo(HaveOccurred())
				plugin.propagateMetadataDownChain(middleOwnerRetrieved1, topOwner)
				// Update client with modified object so the second call can see the changes
				Expect(client.Update(context.TODO(), middleOwnerRetrieved1)).To(Succeed())

				// Verify middleOwner got labels/annotations from topOwner (but kept its own shared values)
				middleOwnerAfterFirst := &appsv1.StatefulSet{}
				Expect(client.Get(context.TODO(), types.NamespacedName{Namespace: middleOwner.Namespace, Name: middleOwner.Name}, middleOwnerAfterFirst)).To(Succeed())
				Expect(middleOwnerAfterFirst.Labels).To(HaveKeyWithValue("top-label", "top-value"))
				Expect(middleOwnerAfterFirst.Labels).To(HaveKeyWithValue("middle-label", "middle-value"))
				Expect(middleOwnerAfterFirst.Labels).To(HaveKeyWithValue("shared-label", "middle-shared")) // NOT overridden
				Expect(middleOwnerAfterFirst.Annotations).To(HaveKeyWithValue("top-annotation", "top-ann-value"))
				Expect(middleOwnerAfterFirst.Annotations).To(HaveKeyWithValue("middle-annotation", "middle-ann-value"))
				Expect(middleOwnerAfterFirst.Annotations).To(HaveKeyWithValue("shared-annotation", "middle-shared-ann")) // NOT overridden

				// Convert StatefulSet to unstructured for the second call
				middleOwnerUnstructured := &unstructured.Unstructured{}
				middleOwnerObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(middleOwnerAfterFirst)
				Expect(err).NotTo(HaveOccurred())
				middleOwnerUnstructured.SetUnstructuredContent(middleOwnerObj)
				middleOwnerUnstructured.SetGroupVersionKind(middleOwnerAfterFirst.GroupVersionKind())

				// Second propagation: skip middleOwner (which now has topOwner's labels), propagate to lastOwner
				// Manually propagate to simulate the second call
				lastOwnerPartial2 := objectToPartial(lastOwner)
				lastOwnerRetrieved2, err := plugin.getObjectInstance(lastOwnerPartial2)
				Expect(err).NotTo(HaveOccurred())
				plugin.propagateMetadataDownChain(lastOwnerRetrieved2, middleOwnerUnstructured)
				// Update client with modified object
				Expect(client.Update(context.TODO(), lastOwnerRetrieved2)).To(Succeed())

				// Verify lastOwner got labels/annotations from both topOwner and middleOwner
				lastOwnerAfterSecond := &appsv1.ReplicaSet{}
				Expect(client.Get(context.TODO(), types.NamespacedName{Namespace: lastOwner.Namespace, Name: lastOwner.Name}, lastOwnerAfterSecond)).To(Succeed())
				Expect(lastOwnerAfterSecond.Labels).To(HaveKeyWithValue("top-label", "top-value"))
				Expect(lastOwnerAfterSecond.Labels).To(HaveKeyWithValue("middle-label", "middle-value"))
				Expect(lastOwnerAfterSecond.Labels).To(HaveKeyWithValue("shared-label", "middle-shared"))
				Expect(lastOwnerAfterSecond.Annotations).To(HaveKeyWithValue("top-annotation", "top-ann-value"))
				Expect(lastOwnerAfterSecond.Annotations).To(HaveKeyWithValue("middle-annotation", "middle-ann-value"))
				Expect(lastOwnerAfterSecond.Annotations).To(HaveKeyWithValue("shared-annotation", "middle-shared-ann"))
			})

			It("does not override existing labels and annotations", func() {
				skippedOwner := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"kind":       "Skipped",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"name":      "skipped-owner",
							"namespace": "default",
							"labels": map[string]interface{}{
								queueLabelKey:       queueName,
								"skipped-label":     "skipped-value",
								"conflicting-label": "skipped-conflict",
							},
							"annotations": map[string]interface{}{
								"skipped-annotation":     "skipped-ann-value",
								"conflicting-annotation": "skipped-conflict-ann",
							},
						},
					},
				}
				lastOwner := &appsv1.StatefulSet{
					TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "last-owner",
						Namespace: "default",
						Labels: map[string]string{
							"conflicting-label": "existing-value", // This should NOT be overridden
							"existing-label":    "existing-value",
						},
						Annotations: map[string]string{
							"conflicting-annotation": "existing-ann-value", // This should NOT be overridden
							"existing-annotation":    "existing-ann-value",
						},
					},
				}
				testPod := examplePod.DeepCopy()

				Expect(client.Create(context.TODO(), skippedOwner)).To(Succeed())
				Expect(client.Create(context.TODO(), lastOwner)).To(Succeed())
				Expect(client.Create(context.TODO(), testPod)).To(Succeed())
				testPod.TypeMeta = metav1.TypeMeta{Kind: examplePod.Kind, APIVersion: examplePod.APIVersion}
				lastOwner.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"}

				// Manually propagate to test the behavior
				lastOwnerPartial := objectToPartial(lastOwner)
				lastOwnerRetrieved, err := plugin.getObjectInstance(lastOwnerPartial)
				Expect(err).NotTo(HaveOccurred())
				plugin.propagateMetadataDownChain(lastOwnerRetrieved, skippedOwner)
				Expect(client.Update(context.TODO(), lastOwnerRetrieved)).To(Succeed())

				// Verify lastOwner got new labels/annotations but kept existing conflicting ones
				lastOwnerAfter := &appsv1.StatefulSet{}
				Expect(client.Get(context.TODO(), types.NamespacedName{Namespace: lastOwner.Namespace, Name: lastOwner.Name}, lastOwnerAfter)).To(Succeed())
				Expect(lastOwnerAfter.Labels).To(HaveKeyWithValue("skipped-label", "skipped-value"))
				Expect(lastOwnerAfter.Labels).To(HaveKeyWithValue("conflicting-label", "existing-value")) // NOT overridden
				Expect(lastOwnerAfter.Labels).To(HaveKeyWithValue("existing-label", "existing-value"))
				Expect(lastOwnerAfter.Annotations).To(HaveKeyWithValue("skipped-annotation", "skipped-ann-value"))
				Expect(lastOwnerAfter.Annotations).To(HaveKeyWithValue("conflicting-annotation", "existing-ann-value")) // NOT overridden
				Expect(lastOwnerAfter.Annotations).To(HaveKeyWithValue("existing-annotation", "existing-ann-value"))
			})
		})

		Context("default plugin usage", func() {
			It("uses default plugin when GVK does not have custom plugin", func() {
				// Use StatefulSet which is not in supportedTypes
				statefulSet := &appsv1.StatefulSet{
					TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-statefulset",
						Namespace: "default",
						Labels: map[string]string{
							queueLabelKey: queueName,
						},
					},
				}
				skippedOwner := &unstructured.Unstructured{
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
				testPod := examplePod.DeepCopy()
				testPod.OwnerReferences = []metav1.OwnerReference{
					{
						Kind:       "StatefulSet",
						APIVersion: "apps/v1",
						Name:       "test-statefulset",
					},
				}

				Expect(client.Create(context.TODO(), statefulSet)).To(Succeed())
				Expect(client.Create(context.TODO(), testPod)).To(Succeed())
				testPod.TypeMeta = metav1.TypeMeta{Kind: examplePod.Kind, APIVersion: examplePod.APIVersion}
				statefulSet.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"}

				// StatefulSet is NOT in supportedTypes, so it should use default plugin
				// Need at least 2 owners so it doesn't default to pod
				Expect(client.Create(context.TODO(), skippedOwner)).To(Succeed())
				otherOwners := []*metav1.PartialObjectMetadata{
					objectToPartial(statefulSet),
					objectToPartial(skippedOwner),
				}
				metadata, err := plugin.GetPodGroupMetadata(skippedOwner, testPod, otherOwners...)

				Expect(err).NotTo(HaveOccurred())
				Expect(metadata).NotTo(BeNil())
				Expect(metadata.Queue).To(Equal(queueName))
				// Verify it used default plugin by checking the owner is StatefulSet
				Expect(metadata.Owner.Kind).To(Equal("StatefulSet"))
				Expect(metadata.Owner.APIVersion).To(Equal("apps/v1"))
				Expect(metadata.Owner.Name).To(Equal("test-statefulset"))
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
