// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pluginshub

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kartaplugin "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/karta"
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
				kubeClient, false, false, false, queueLabelKey, nodePoolLabelKey, "", "",
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

	Context("Generic Karta Fallback Tests", func() {
		var (
			kubeClient client.Client
		)

		It("should return generic Karta grouper only after native plugins do not match", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "test.example.com",
				Version: "v1",
				Kind:    "TestResource",
			}
			kubeClient = newHubFakeClientWithScheme(createHubTestKarta(gvk))
			hub := NewDefaultPluginsHub(
				kubeClient, false, false, true, queueLabelKey, nodePoolLabelKey, "", "",
			)

			plugin := hub.GetPodGrouperPlugin(gvk)

			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("Karta Grouper"))
		})

		It("should prefer native plugins over generic Karta fallback", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "batch",
				Version: "v1",
				Kind:    "Job",
			}
			kubeClient = newHubFakeClientWithScheme(createHubTestKarta(gvk))
			hub := NewDefaultPluginsHub(
				kubeClient, false, false, true, queueLabelKey, nodePoolLabelKey, "", "",
			)

			plugin := hub.GetPodGrouperPlugin(gvk)

			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("BatchJob Grouper"))
		})

		It("should return default plugin when generic Karta fallback is disabled", func() {
			gvk := metav1.GroupVersionKind{
				Group:   "test.example.com",
				Version: "v1",
				Kind:    "TestResource",
			}
			kubeClient = newHubFakeClientWithScheme(createHubTestKarta(gvk))
			hub := NewDefaultPluginsHub(
				kubeClient, false, false, false, queueLabelKey, nodePoolLabelKey, "", "",
			)

			plugin := hub.GetPodGrouperPlugin(gvk)

			Expect(plugin).NotTo(BeNil())
			Expect(plugin.Name()).To(BeEquivalentTo("Default Grouper"))
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
				kubeClient, false, false, false, queueLabelKey, nodePoolLabelKey, "", "",
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

func newHubFakeClientWithScheme(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(kartav1alpha1.AddToScheme(scheme)).To(Succeed())

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

func createHubTestKarta(gvk metav1.GroupVersionKind) *kartav1alpha1.Karta {
	uid := types.UID("test-uid-" + gvk.Group + "-" + gvk.Version + "-" + gvk.Kind)
	karta := &kartav1alpha1.Karta{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kartav1alpha1.GroupVersion.String(),
			Kind:       "Karta",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ri-" + string(uid),
			UID:  uid,
			Labels: map[string]string{
				kartaplugin.KartaGroupLabel:   gvk.Group,
				kartaplugin.KartaVersionLabel: gvk.Version,
				kartaplugin.KartaKindLabel:    gvk.Kind,
			},
		},
		Spec: kartav1alpha1.KartaSpec{
			StructureDefinition: kartav1alpha1.StructureDefinition{
				RootComponent: kartav1alpha1.ComponentDefinition{
					Name: "test-root",
					Kind: &kartav1alpha1.GroupVersionKind{
						Group:   gvk.Group,
						Version: gvk.Version,
						Kind:    gvk.Kind,
					},
					StatusDefinition: &kartav1alpha1.StatusDefinition{
						StatusMappings: kartav1alpha1.StatusMappings{},
					},
				},
				ChildComponents: []kartav1alpha1.ComponentDefinition{
					{
						Name:     "test-component",
						OwnerRef: ptr.To("test-root"),
						SpecDefinition: &kartav1alpha1.SpecDefinition{
							PodTemplateSpecPath: ptr.To(".spec.template"),
						},
						ScaleDefinition: &kartav1alpha1.ScaleDefinition{
							ReplicasPath: ptr.To(".spec.replicas"),
						},
						PodSelector: &kartav1alpha1.PodSelector{
							ComponentTypeSelector: &kartav1alpha1.ComponentTypeSelector{
								KeyPath: ".metadata.labels.role",
								Value:   ptr.To("worker"),
							},
						},
					},
				},
			},
			Instructions: kartav1alpha1.OptimizationInstructions{
				GangScheduling: &kartav1alpha1.GangSchedulingInstruction{
					PodGroup: &kartav1alpha1.PodGroupComponentsMapping{
						Name: "test",
					},
				},
			},
		},
	}

	return karta
}
