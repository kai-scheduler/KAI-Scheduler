// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pluginshub

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
