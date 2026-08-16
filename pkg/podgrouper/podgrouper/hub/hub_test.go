// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pluginshub

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestSupportedTypes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SupportedTypes Suite")
}

var _ = Describe("SupportedTypes", func() {
	Context("Exact Match Tests", func() {
		var (
			kubeClient client.Client
			hub        *DefaultPluginsHub
		)

		BeforeEach(func() {
			kubeClient = fake.NewFakeClient()
			hub = NewDefaultPluginsHub(
				kubeClient, false, false, queueLabelKey, nodePoolLabelKey, "", "",
			)
		})

		It("should return plugin for exact GVK match", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "kubeflow.org",
				Version: "v1",
				Kind:    "TFJob",
			}
			plugin := hub.GetPodGrouperPlugin(gvk)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("TensorFlow Grouper"))
		})

		It("should return plugin for exact GVK match - HasMatchingPlugin function", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "kubeflow.org",
				Version: "v1",
				Kind:    "TFJob",
			}
			hasPlugin := hub.HasMatchingPlugin(gvk)
			Expect(hasPlugin).To(BeTrue())
		})

		It("should return default plugin for non-existent GVK", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "non-existent-group",
				Version: "v1",
				Kind:    "NonExistentKind",
			}
			plugin := hub.GetPodGrouperPlugin(gvk)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("Default Grouper"))
		})

		It("non-existent GVK - HasMatchingPlugin returns false", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "non-existent-group",
				Version: "v1",
				Kind:    "NonExistentKind",
			}
			hasPlugin := hub.HasMatchingPlugin(gvk)
			Expect(hasPlugin).To(BeFalse())
		})

		It("should return skipTopOwner plugin for WorkloadRunner", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "run.ai",
				Version: "v1alpha1",
				Kind:    "WorkloadRunner",
			}
			plugin := hub.GetPodGrouperPlugin(gvk)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("SkipTopOwner Grouper"))
		})

		It("should return skipTopOwner plugin for WorkloadRunner of any served version", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "run.ai",
				Version: "v2beta1",
				Kind:    "WorkloadRunner",
			}
			plugin := hub.GetPodGrouperPlugin(gvk)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("SkipTopOwner Grouper"))
		})

		It("should return skipTopOwner plugin for TrainJob", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "trainer.kubeflow.org",
				Version: "v1alpha1",
				Kind:    "TrainJob",
			}
			plugin := hub.GetPodGrouperPlugin(gvk)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("SkipTopOwner Grouper"))
		})
	})

	Context("Skip Top Owner Resolution Tests", func() {
		It("should resolve the owner below a skipped WorkloadRunner through the hub", func() {
			statefulSet := &appsv1.StatefulSet{
				TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrapped-sts",
					Namespace: "default",
					Labels:    map[string]string{queueLabelKey: "wrapped-queue"},
				},
			}
			kubeClient := fake.NewFakeClient(statefulSet)
			hub := NewDefaultPluginsHub(
				kubeClient, false, false, queueLabelKey, nodePoolLabelKey, "", "",
			)

			runner := &unstructured.Unstructured{}
			runner.SetAPIVersion("run.ai/v1alpha1")
			runner.SetKind("WorkloadRunner")
			runner.SetNamespace("default")
			runner.SetName("runner")

			pod := &v1.Pod{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			}
			owners := []*metav1.PartialObjectMetadata{
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
					ObjectMeta: metav1.ObjectMeta{Name: "wrapped-sts", Namespace: "default"},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "run.ai/v1alpha1", Kind: "WorkloadRunner"},
					ObjectMeta: metav1.ObjectMeta{Name: "runner", Namespace: "default"},
				},
			}

			plugin := hub.GetPodGrouperPlugin(metav1.GroupVersionKind{
				Group: apiGroupRunai, Version: "v1alpha1", Kind: "WorkloadRunner",
			})
			Expect(plugin.Name()).To(BeEquivalentTo("SkipTopOwner Grouper"))

			metadata, err := plugin.GetPodGroupMetadata(runner, pod, owners...)

			Expect(err).NotTo(HaveOccurred())
			Expect(metadata).NotTo(BeNil())
			Expect(metadata.Queue).To(Equal("wrapped-queue"))
			Expect(metadata.Owner.Kind).To(Equal("StatefulSet"))
		})
	})

	Context("Wildcard Version Tests", func() {
		var (
			kubeClient client.Client
			hub        *DefaultPluginsHub
		)

		BeforeEach(func() {
			kubeClient = fake.NewFakeClient()
			hub = NewDefaultPluginsHub(
				kubeClient, false, false, queueLabelKey, nodePoolLabelKey, "", "",
			)
		})

		It("should successfully retrieve with any version for kind set with wildcard", func() {
			gvkWithWildcard := metav1.GroupVersionKind{
				Group:   apiGroupRunai,
				Version: "v100",
				Kind:    kindTrainingWorkload,
			}
			plugin := hub.GetPodGrouperPlugin(gvkWithWildcard)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("SkipTopOwner Grouper"))
		})

		It("should successfully retrieve with wildcard version for existing kinds", func() {
			gvkWithWildcard := metav1.GroupVersionKind{
				Group:   apiGroupRunai,
				Version: "*",
				Kind:    kindTrainingWorkload,
			}
			plugin := hub.GetPodGrouperPlugin(gvkWithWildcard)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("SkipTopOwner Grouper"))
		})

		It("should return default for non-existent kind with wildcard version", func() {
			gvkWithWildcard := metav1.GroupVersionKind{
				Group:   "non-existent-group",
				Version: "*",
				Kind:    "NonExistentKind",
			}
			plugin := hub.GetPodGrouperPlugin(gvkWithWildcard)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("Default Grouper"))
		})
	})
})
