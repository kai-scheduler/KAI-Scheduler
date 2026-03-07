// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	kaiprometheus "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/prometheus"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common"
	test_utils "github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common/test_utils"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"                                                // nolint:unused
	rbacv1 "k8s.io/api/rbac/v1"                                                // nolint:unused
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1" // nolint:unused
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPrometheus(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Prometheus operand Suite")
}

func createFakeClientWithScheme() client.Client {
	testScheme := runtime.NewScheme()
	Expect(kaiv1.AddToScheme(testScheme)).To(Succeed())
	Expect(monitoringv1.AddToScheme(testScheme)).To(Succeed())
	Expect(corev1.AddToScheme(testScheme)).To(Succeed())
	Expect(rbacv1.AddToScheme(testScheme)).To(Succeed())
	Expect(apiextensionsv1.AddToScheme(testScheme)).To(Succeed())
	Expect(metav1.AddMetaToScheme(testScheme)).To(Succeed())

	return fake.NewClientBuilder().WithScheme(testScheme).Build()
}

var _ = Describe("Prometheus", func() {
	Describe("DesiredState", func() {
		var (
			fakeKubeClient client.Client
			prometheus     *Prometheus
			kaiConfig      *kaiv1.Config
		)

		BeforeEach(func(ctx context.Context) {
			fakeKubeClient = createFakeClientWithScheme()
			prometheus = &Prometheus{}
			kaiConfig = kaiConfigForPrometheus()
		})

		Context("when Prometheus is disabled", func() {
			It("should return no objects when no Prometheus instance exists", func(ctx context.Context) {
				kaiConfig.Spec.Prometheus.Enabled = ptr.To(false)
				objects, err := prometheus.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(0))
			})

			It("should add deprecation timestamp when Prometheus instance exists", func(ctx context.Context) {
				kaiConfig.Spec.Prometheus.Enabled = ptr.To(false)

				Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())

				// Create existing Prometheus instance
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				objects, err := prometheus.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(2))

				prometheusObjs := test_utils.FindTypesInObjects[*monitoringv1.Prometheus](objects)
				Expect(len(prometheusObjs)).To(Equal(1))
				prometheusObj := prometheusObjs[0]
				Expect(prometheusObj).NotTo(BeNil())
				Expect((*prometheusObj).Annotations).To(HaveKey(deprecationTimestampKey))
			})
		})

		Context("when Prometheus is enabled", func() {
			BeforeEach(func(ctx context.Context) {
				Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
				Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())
			})

			It("should return Prometheus object when Prometheus Operator is installed", func(ctx context.Context) {
				objects, err := prometheus.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(5)) // ServiceAccount, Prometheus, 2 ServiceMonitors, usage-prometheus Service

				prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
				Expect(prometheusObj).NotTo(BeNil())
				Expect((*prometheusObj).Name).To(Equal(mainResourceName))
				Expect((*prometheusObj).Namespace).To(Equal(kaiConfig.Spec.Namespace))
			})

			It("should return existing Prometheus object if it already exists", func(ctx context.Context) {
				// Create an existing Prometheus instance
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
						Labels: map[string]string{
							"existing": "label",
						},
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				objects, err := prometheus.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(5)) // ServiceAccount, Prometheus, 2 ServiceMonitor, usage-prometheus Service

				prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
				Expect(prometheusObj).NotTo(BeNil())
				Expect((*prometheusObj).Name).To(Equal(mainResourceName))
				Expect((*prometheusObj).Labels).To(HaveKeyWithValue("existing", "label"))
			})
		})

		Context("when Prometheus Operator is not installed", func() {
			It("should return ServiceAccount only", func(ctx context.Context) {
				objects, err := prometheus.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(1)) // ServiceAccount only
			})
		})
	})

	Describe("CheckPrometheusOperatorInstalled", func() {
		var (
			fakeKubeClient client.Client
		)

		BeforeEach(func(ctx context.Context) {
			fakeKubeClient = createFakeClientWithScheme()
		})

		Context("when Prometheus CRD exists", func() {
			It("should return true", func(ctx context.Context) {
				Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
				Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())

				installed, err := common.CheckPrometheusCRDsAvailable(ctx, fakeKubeClient, "prometheus", "serviceMonitor")
				Expect(err).To(BeNil())
				Expect(installed).To(BeTrue())
			})
		})

		Context("when Prometheus CRD does not exist", func() {
			It("should return false", func(ctx context.Context) {
				installed, err := common.CheckPrometheusCRDsAvailable(ctx, fakeKubeClient, "prometheus", "serviceMonitor")
				Expect(err).To(BeNil())
				Expect(installed).To(BeFalse())
			})
		})
	})

	Describe("Operand methods", func() {
		var (
			fakeKubeClient client.Client
			prometheus     *Prometheus
		)

		BeforeEach(func(ctx context.Context) {
			fakeKubeClient = createFakeClientWithScheme()
			prometheus = &Prometheus{
				namespace: "test-namespace",
			}
		})

		Context("IsDeployed", func() {
			It("should return true when all objects exist", func(ctx context.Context) {
				// Create a Prometheus object
				prometheusObj := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: "test-namespace",
					},
				}
				Expect(fakeKubeClient.Create(ctx, prometheusObj)).To(Succeed())

				prometheus.lastDesiredState = []client.Object{prometheusObj}

				deployed, err := prometheus.IsDeployed(ctx, fakeKubeClient)
				Expect(err).To(BeNil())
				Expect(deployed).To(BeTrue())
			})

			It("should return false when objects do not exist", func(ctx context.Context) {
				prometheusObj := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: "test-namespace",
					},
				}

				prometheus.lastDesiredState = []client.Object{prometheusObj}

				deployed, err := prometheus.IsDeployed(ctx, fakeKubeClient)
				Expect(err).To(BeNil())
				Expect(deployed).To(BeFalse())
			})
		})

		Context("IsAvailable", func() {
			It("should return true when all controllers are available", func(ctx context.Context) {
				// Create a Prometheus object with proper status conditions
				prometheusObj := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: "test-namespace",
					},
					Status: monitoringv1.PrometheusStatus{
						Conditions: []monitoringv1.Condition{
							{
								Type:   monitoringv1.ConditionType("Available"),
								Status: monitoringv1.ConditionTrue,
							},
						},
					},
				}
				Expect(fakeKubeClient.Create(ctx, prometheusObj)).To(Succeed())

				prometheus.lastDesiredState = []client.Object{prometheusObj}

				available, err := prometheus.IsAvailable(ctx, fakeKubeClient)
				Expect(err).To(BeNil())
				Expect(available).To(BeTrue())
			})

			It("should return false when Prometheus is not available", func(ctx context.Context) {
				// Create a Prometheus object with unavailable status
				prometheusObj := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: "test-namespace",
					},
					Status: monitoringv1.PrometheusStatus{
						Conditions: []monitoringv1.Condition{
							{
								Type:   monitoringv1.ConditionType("Available"),
								Status: monitoringv1.ConditionFalse,
							},
						},
					},
				}
				Expect(fakeKubeClient.Create(ctx, prometheusObj)).To(Succeed())

				prometheus.lastDesiredState = []client.Object{prometheusObj}

				available, err := prometheus.IsAvailable(ctx, fakeKubeClient)
				Expect(err).To(BeNil())
				Expect(available).To(BeFalse())
			})

			It("should return false when Prometheus object does not exist", func(ctx context.Context) {
				// Create a Prometheus object in desired state but don't create it in the cluster
				prometheusObj := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: "test-namespace",
					},
				}
				prometheus.lastDesiredState = []client.Object{prometheusObj}

				available, err := prometheus.IsAvailable(ctx, fakeKubeClient)
				Expect(err).ToNot(BeNil())
				Expect(available).To(BeFalse())
			})
		})

		Context("Name", func() {
			It("should return correct operand name", func(ctx context.Context) {
				name := prometheus.Name()
				Expect(name).To(Equal("KAI-prometheus"))
			})
		})
	})
})

var _ = Describe("prometheusForKAIConfig", func() {
	var (
		fakeKubeClient client.Client
		kaiConfig      *kaiv1.Config
	)

	BeforeEach(func(ctx context.Context) {
		fakeKubeClient = createFakeClientWithScheme()
		kaiConfig = kaiConfigForPrometheus()
	})

	Context("when Prometheus is disabled", func() {
		It("should return empty objects list", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.Enabled = ptr.To(false)
			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0))
		})
	})

	Context("when Prometheus is enabled", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
			Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())
		})

		It("should create new Prometheus object when none exists", func(ctx context.Context) {
			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1)) // Prometheus only

			prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
			Expect(prometheusObj).NotTo(BeNil())
			Expect((*prometheusObj).Name).To(Equal(mainResourceName))
			Expect((*prometheusObj).Namespace).To(Equal(kaiConfig.Spec.Namespace))
			Expect((*prometheusObj).Labels).To(HaveKeyWithValue("app", mainResourceName))
		})

		It("should return existing Prometheus object if it already exists", func(ctx context.Context) {
			// Create an existing Prometheus instance
			existingPrometheus := &monitoringv1.Prometheus{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Prometheus",
					APIVersion: "monitoring.coreos.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      mainResourceName,
					Namespace: kaiConfig.Spec.Namespace,
					Labels: map[string]string{
						"existing": "label",
						"app":      mainResourceName,
					},
				},
			}
			Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1)) // Prometheus only

			prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
			Expect(prometheusObj).NotTo(BeNil())
			Expect((*prometheusObj).Name).To(Equal(mainResourceName))
			Expect((*prometheusObj).Labels).To(HaveKeyWithValue("existing", "label"))
			Expect((*prometheusObj).Labels).To(HaveKeyWithValue("app", mainResourceName))
		})
	})

	Context("when Prometheus Operator is not installed", func() {
		It("should return empty objects list", func(ctx context.Context) {
			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0))
		})
	})

	Context("when external Prometheus URL is provided", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())
		})

		It("should return ServiceMonitors only when external Prometheus URL is valid", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ExternalPrometheusUrl = ptr.To("http://prometheus.example.com:9090")

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)

			// The function skips Prometheus CR creation and only creates ServiceMonitors
			Expect(err).To(BeNil())
			Expect(objects).NotTo(BeNil())
			Expect(len(objects)).To(Equal(2)) // 2 ServiceMonitors

			serviceMonitor := test_utils.FindTypeInObjects[*monitoringv1.ServiceMonitor](objects)
			Expect(serviceMonitor).NotTo(BeNil())
			Expect((*serviceMonitor).Name).To(Equal("queue-controller"))
		})

		It("should return empty objects list when ServiceMonitors are disabled", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ExternalPrometheusUrl = ptr.To("http://prometheus.example.com:9090")
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(false)

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)

			// When ServiceMonitors are disabled, function returns empty list
			Expect(err).To(BeNil())
			Expect(objects).NotTo(BeNil())
			Expect(len(objects)).To(Equal(0))
		})

		It("should return empty objects list when ServiceMonitor CRD is not available", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ExternalPrometheusUrl = ptr.To("http://prometheus.example.com:9090")

			// Remove ServiceMonitor CRD
			Expect(fakeKubeClient.Delete(ctx, getServiceMonitorCRD())).To(Succeed())

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)

			// When CRD is not available, function returns empty list
			Expect(err).To(BeNil())
			Expect(objects).NotTo(BeNil())
			Expect(len(objects)).To(Equal(0))
		})
	})

	Context("when configuring storage size", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
		})

		It("should use default storage size when not configured", func(ctx context.Context) {
			// Don't set StorageSize, it should default to "50Gi"
			kaiConfig.Spec.Prometheus.StorageSize = nil

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1))

			prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
			Expect(prometheusObj).NotTo(BeNil())
			Expect((*prometheusObj).Spec.Storage).NotTo(BeNil())
			Expect((*prometheusObj).Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().String()).To(Equal("50Gi"))
		})

		It("should use custom storage size when configured", func(ctx context.Context) {
			// Set custom storage size
			kaiConfig.Spec.Prometheus.StorageSize = ptr.To("100Gi")

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1))

			prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
			Expect(prometheusObj).NotTo(BeNil())
			Expect((*prometheusObj).Spec.Storage).NotTo(BeNil())
			Expect((*prometheusObj).Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().String()).To(Equal("100Gi"))
		})

		It("should handle different storage size formats", func(ctx context.Context) {
			testCases := []struct {
				input    string
				expected string
			}{
				{"20Gi", "20Gi"},
				{"1Ti", "1Ti"},
				{"500Mi", "500Mi"},
			}

			for _, tc := range testCases {
				kaiConfig.Spec.Prometheus.StorageSize = ptr.To(tc.input)

				objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil(), "Failed for input: "+tc.input)
				Expect(len(objects)).To(Equal(1))

				prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
				Expect(prometheusObj).NotTo(BeNil())
				Expect((*prometheusObj).Spec.Storage).NotTo(BeNil())
				Expect((*prometheusObj).Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().String()).To(Equal(tc.expected), "Failed for input: "+tc.input)
			}
		})
	})
})

var _ = Describe("getStorageSpecForPrometheus", func() {
	Context("when configuring persistent storage", func() {
		It("should return nil when persistent storage is explicitly disabled", func(ctx context.Context) {
			config := &kaiprometheus.Prometheus{
				EnablePersistentStorage: ptr.To(false),
			}

			storageSpec := getStorageSpecForPrometheus(config)
			Expect(storageSpec).To(BeNil())
		})

		It("should return storage spec with default size when persistent storage is not specified", func(ctx context.Context) {
			config := &kaiprometheus.Prometheus{
				EnablePersistentStorage: nil,
				StorageClassName:        ptr.To("standard"),
			}

			storageSpec := getStorageSpecForPrometheus(config)
			Expect(storageSpec).NotTo(BeNil())
			Expect(storageSpec.VolumeClaimTemplate.Spec.Resources.Requests.Storage().String()).To(Equal("50Gi"))
			Expect(*storageSpec.VolumeClaimTemplate.Spec.StorageClassName).To(Equal("standard"))
		})

		It("should return storage spec when persistent storage is explicitly enabled", func(ctx context.Context) {
			config := &kaiprometheus.Prometheus{
				EnablePersistentStorage: ptr.To(true),
				StorageSize:             ptr.To("100Gi"),
				StorageClassName:        ptr.To("fast-ssd"),
			}

			storageSpec := getStorageSpecForPrometheus(config)
			Expect(storageSpec).NotTo(BeNil())
			Expect(storageSpec.VolumeClaimTemplate.Spec.Resources.Requests.Storage().String()).To(Equal("100Gi"))
			Expect(*storageSpec.VolumeClaimTemplate.Spec.StorageClassName).To(Equal("fast-ssd"))
			Expect(storageSpec.VolumeClaimTemplate.Spec.AccessModes).To(ContainElement(corev1.ReadWriteOnce))
		})

		It("should use default storage size when custom size is not provided", func(ctx context.Context) {
			config := &kaiprometheus.Prometheus{
				EnablePersistentStorage: ptr.To(true),
				StorageClassName:        ptr.To("standard"),
			}

			storageSpec := getStorageSpecForPrometheus(config)
			Expect(storageSpec).NotTo(BeNil())
			Expect(storageSpec.VolumeClaimTemplate.Spec.Resources.Requests.Storage().String()).To(Equal("50Gi"))
		})
	})
})

func kaiConfigForPrometheus() *kaiv1.Config {
	kaiConfig := &kaiv1.Config{}
	kaiConfig.Spec.SetDefaultsWhereNeeded()
	kaiConfig.Spec.Prometheus = &kaiprometheus.Prometheus{
		Enabled: ptr.To(true),
	}
	kaiConfig.Spec.Prometheus.SetDefaultsWhereNeeded()
	kaiConfig.Spec.Namespace = "test-namespace"

	return kaiConfig
}

var _ = Describe("deprecatePrometheusForKAIConfig", func() {
	var (
		fakeKubeClient client.Client
		kaiConfig      *kaiv1.Config
	)

	BeforeEach(func(ctx context.Context) {
		fakeKubeClient = createFakeClientWithScheme()
		kaiConfig = kaiConfigForPrometheus()
		kaiConfig.Spec.Prometheus.Enabled = ptr.To(false)
		kaiConfig.Spec.Prometheus.RetentionPeriod = ptr.To("720h") // 30 days
	})

	Context("when Prometheus CRD is not available", func() {
		It("should return empty objects list", func(ctx context.Context) {
			objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0))
		})
	})

	Context("when Prometheus CRD is available", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
		})

		Context("when no Prometheus instance exists", func() {
			It("should return empty objects list", func(ctx context.Context) {
				objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(0))
			})
		})

		Context("when Prometheus instance exists without deprecation annotation", func() {
			It("should add deprecation timestamp annotation", func(ctx context.Context) {
				// Create existing Prometheus instance
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(1))

				prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
				Expect(prometheusObj).NotTo(BeNil())
				Expect((*prometheusObj).Annotations).To(HaveKey(deprecationTimestampKey))
			})
		})

		Context("when Prometheus instance exists with deprecation annotation", func() {
			It("should keep instance when grace period has not passed", func(ctx context.Context) {
				// Create Prometheus with recent deprecation timestamp
				recentTime := metav1.NewTime(metav1.Now().Add(-24 * time.Hour))
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
						Annotations: map[string]string{
							deprecationTimestampKey: recentTime.Format(time.RFC3339),
						},
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(1))

				prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
				Expect(prometheusObj).NotTo(BeNil())
			})

			It("should return empty list when grace period has passed", func(ctx context.Context) {
				// Create Prometheus with old deprecation timestamp (more than 30 days ago)
				oldTime := metav1.NewTime(metav1.Now().Add(-31 * 24 * time.Hour))
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
						Annotations: map[string]string{
							deprecationTimestampKey: oldTime.Format(time.RFC3339),
						},
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(0))
			})

			It("should re-set annotation when timestamp is invalid", func(ctx context.Context) {
				// Create Prometheus with invalid deprecation timestamp
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
						Annotations: map[string]string{
							deprecationTimestampKey: "invalid-timestamp",
						},
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(Equal(1))

				prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
				Expect(prometheusObj).NotTo(BeNil())
				// Should have updated the annotation with a valid timestamp
				Expect((*prometheusObj).Annotations[deprecationTimestampKey]).NotTo(Equal("invalid-timestamp"))
			})
		})

		Context("when retention period is invalid", func() {
			It("should return error for invalid retention period", func(ctx context.Context) {
				kaiConfig.Spec.Prometheus.RetentionPeriod = ptr.To("invalid-duration")

				// Create Prometheus with old deprecation timestamp
				oldTime := metav1.NewTime(metav1.Now().Add(-31 * 24 * time.Hour))
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
						Annotations: map[string]string{
							deprecationTimestampKey: oldTime.Format(time.RFC3339),
						},
					},
					Spec: monitoringv1.PrometheusSpec{
						Retention: monitoringv1.Duration("invalid-duration"),
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).NotTo(BeNil())
				Expect(err.Error()).To(ContainSubstring("failed to get retention period"))
				Expect(objects).To(HaveLen(0))
			})
		})

		Context("when retention period uses Prometheus-style formats", func() {
			It("should successfully parse duration formats not supported by standard time.ParseDuration", func(ctx context.Context) {
				// Test various Prometheus/str2duration formats that standard time.ParseDuration doesn't support
				// str2duration supports: d (days), w (weeks), but not y (years)
				testCases := []struct {
					duration string
					// We'll test that it's parseable and deprecation works correctly
				}{
					{duration: "2w"},   // weeks - not supported by time.ParseDuration
					{duration: "30d"},  // days - not supported by time.ParseDuration
					{duration: "1w2d"}, // combined - not supported by time.ParseDuration
					{duration: "168h"}, // hours - supported by both (for comparison)
				}

				for _, tc := range testCases {
					By(fmt.Sprintf("testing duration format: %s", tc.duration))

					// Create Prometheus with recent deprecation timestamp (retention not passed)
					recentTime := metav1.NewTime(metav1.Now().Add(-1 * time.Hour))
					existingPrometheus := &monitoringv1.Prometheus{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Prometheus",
							APIVersion: "monitoring.coreos.com/v1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      mainResourceName,
							Namespace: kaiConfig.Spec.Namespace,
							Annotations: map[string]string{
								deprecationTimestampKey: recentTime.Format(time.RFC3339),
							},
						},
						Spec: monitoringv1.PrometheusSpec{
							// Set the retention period on the Prometheus object
							Retention: monitoringv1.Duration(tc.duration),
						},
					}
					Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

					// Should successfully parse the Prometheus-style duration
					objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
					Expect(err).To(BeNil(), fmt.Sprintf("Expected no error for duration %s, got: %v", tc.duration, err))
					Expect(objects).NotTo(BeNil())
					Expect(objects).To(HaveLen(1), fmt.Sprintf("Expected 1 object for duration %s", tc.duration))

					// Clean up for next iteration
					Expect(fakeKubeClient.Delete(ctx, existingPrometheus)).To(Succeed())
				}
			})

			It("should correctly calculate deletion time with week-based retention", func(ctx context.Context) {
				kaiConfig.Spec.Prometheus.RetentionPeriod = ptr.To("2w") // 2 weeks

				// Create Prometheus with deprecation timestamp from 3 weeks ago (should be deleted)
				oldTime := metav1.NewTime(metav1.Now().Add(-3 * 7 * 24 * time.Hour))
				existingPrometheus := &monitoringv1.Prometheus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Prometheus",
						APIVersion: "monitoring.coreos.com/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      mainResourceName,
						Namespace: kaiConfig.Spec.Namespace,
						Annotations: map[string]string{
							deprecationTimestampKey: oldTime.Format(time.RFC3339),
						},
					},
					Spec: monitoringv1.PrometheusSpec{
						// Set the retention period on the Prometheus object
						Retention: monitoringv1.Duration("2w"),
					},
				}
				Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

				// Should allow deletion as retention period (2 weeks) has passed
				objects, err := deprecatePrometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(objects).To(BeEmpty(), "Expected empty objects slice as retention period passed")
			})
		})
	})
})

var _ = Describe("prometheusForKAIConfig deprecation annotation removal", func() {
	var (
		fakeKubeClient client.Client
		kaiConfig      *kaiv1.Config
	)

	BeforeEach(func(ctx context.Context) {
		fakeKubeClient = createFakeClientWithScheme()
		kaiConfig = kaiConfigForPrometheus()
		kaiConfig.Spec.Prometheus.Enabled = ptr.To(true)

		Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
	})

	Context("when Prometheus is re-enabled", func() {
		It("should remove deprecation timestamp annotation if it exists", func(ctx context.Context) {
			// Create Prometheus with deprecation annotation
			existingPrometheus := &monitoringv1.Prometheus{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Prometheus",
					APIVersion: "monitoring.coreos.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      mainResourceName,
					Namespace: kaiConfig.Spec.Namespace,
					Annotations: map[string]string{
						deprecationTimestampKey: metav1.Now().Format(time.RFC3339),
						"other-annotation":      "should-remain",
					},
				},
			}
			Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1))

			prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
			Expect(prometheusObj).NotTo(BeNil())
			Expect((*prometheusObj).Annotations).NotTo(HaveKey(deprecationTimestampKey))
			Expect((*prometheusObj).Annotations).To(HaveKeyWithValue("other-annotation", "should-remain"))
		})

		It("should not fail when Prometheus has no annotations", func(ctx context.Context) {
			// Create Prometheus without annotations
			existingPrometheus := &monitoringv1.Prometheus{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Prometheus",
					APIVersion: "monitoring.coreos.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      mainResourceName,
					Namespace: kaiConfig.Spec.Namespace,
				},
			}
			Expect(fakeKubeClient.Create(ctx, existingPrometheus)).To(Succeed())

			objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1))

			prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
			Expect(prometheusObj).NotTo(BeNil())
		})
	})
})

var _ = Describe("External Prometheus Validation", func() {
	Context("validateExternalPrometheusConnection", func() {
		var (
			ctx    context.Context
			server *httptest.Server
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
		})

		It("should successfully validate connection to a working Prometheus instance", func() {
			// Create a test server that responds to /api/v1/status/config
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/status/config" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"status":"success"}`))
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))

			err := validateExternalPrometheusConnection(ctx, server.URL)
			Expect(err).To(BeNil())
		})

		It("should fail validation for invalid URL", func() {
			err := validateExternalPrometheusConnection(ctx, "://invalid-url")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid Prometheus URL"))
		})

		It("should fail validation when Prometheus returns error status", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			err := validateExternalPrometheusConnection(ctx, server.URL)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("status code 500"))
		})

		It("should fail validation when connection cannot be established", func() {
			err := validateExternalPrometheusConnection(ctx, "http://nonexistent.example.com:9090")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("failed to connect"))
		})

		It("should add http scheme when missing", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/status/config" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"status":"success"}`))
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))

			// Remove the scheme from the URL
			urlWithoutScheme := server.URL[7:] // Remove "http://"
			err := validateExternalPrometheusConnection(ctx, urlWithoutScheme)
			Expect(err).To(BeNil())
		})
	})
})

var _ = Describe("serviceMonitorsForKAIConfig", func() {
	var (
		fakeKubeClient client.Client
		kaiConfig      *kaiv1.Config
	)

	BeforeEach(func(ctx context.Context) {
		fakeKubeClient = createFakeClientWithScheme()
		kaiConfig = kaiConfigForPrometheus()
	})

	Context("when ServiceMonitors are enabled by default", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())
		})

		It("should create ServiceMonitors when Enabled is not explicitly set (nil)", func(ctx context.Context) {
			// ServiceMonitor.Enabled is nil by default in kaiConfigForPrometheus
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = nil

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(BeNumerically(">", 0), "Expected ServiceMonitors to be created by default")

			// Verify we have ServiceMonitor objects
			serviceMonitor := test_utils.FindTypeInObjects[*monitoringv1.ServiceMonitor](objects)
			Expect(serviceMonitor).NotTo(BeNil(), "Expected at least one ServiceMonitor")
		})

		It("should create ServiceMonitors when Enabled is explicitly set to true", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(true)

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(BeNumerically(">", 0), "Expected ServiceMonitors to be created when enabled")

			// Verify we have ServiceMonitor objects
			serviceMonitor := test_utils.FindTypeInObjects[*monitoringv1.ServiceMonitor](objects)
			Expect(serviceMonitor).NotTo(BeNil(), "Expected at least one ServiceMonitor")
		})

		It("should create ServiceMonitors with correct labels", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(true)

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(BeNumerically(">", 0))

			// Verify all ServiceMonitors have the correct accounting label
			for _, obj := range objects {
				if sm, ok := obj.(*monitoringv1.ServiceMonitor); ok {
					Expect(sm.Labels).To(HaveKeyWithValue(constants.DefaultAccountingLabelKey, constants.DefaultAccountingLabelValue))
				}
			}
		})
	})

	Context("when ServiceMonitors are explicitly disabled", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())
		})

		It("should NOT create ServiceMonitors when Enabled is set to false", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(false)

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0), "Expected no ServiceMonitors to be created when disabled")
		})

		It("should log that ServiceMonitors are disabled", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(false)

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(objects).To(BeEmpty())
		})
	})

	Context("when ServiceMonitor config is nil", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())
		})

		It("should create ServiceMonitors when ServiceMonitor config is nil (default enabled)", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ServiceMonitor = nil

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(BeNumerically(">", 0), "Expected ServiceMonitors to be created by default when config is nil")
		})
	})

	Context("when ServiceMonitor CRD is not available", func() {
		It("should return empty objects list even if ServiceMonitors are enabled", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(true)

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0), "Expected no ServiceMonitors when CRD is not available")
		})
	})

	Context("when ServiceMonitor has custom configuration", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getServiceMonitorCRD())).To(Succeed())
		})

		It("should apply custom interval and scrape timeout", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ServiceMonitor.Enabled = ptr.To(true)
			kaiConfig.Spec.Prometheus.ServiceMonitor.Interval = ptr.To("60s")
			kaiConfig.Spec.Prometheus.ServiceMonitor.ScrapeTimeout = ptr.To("30s")

			objects, err := serviceMonitorsForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(BeNumerically(">", 0))

			// Verify configuration is applied
			for _, obj := range objects {
				if sm, ok := obj.(*monitoringv1.ServiceMonitor); ok {
					Expect(sm.Spec.Endpoints).To(HaveLen(1))
					Expect(string(sm.Spec.Endpoints[0].Interval)).To(Equal("60s"))
					Expect(string(sm.Spec.Endpoints[0].ScrapeTimeout)).To(Equal("30s"))
				}
			}
		})
	})
})

var _ = Describe("usagePrometheusServiceForKAIConfig", func() {
	var (
		fakeKubeClient client.Client
		kaiConfig      *kaiv1.Config
	)

	BeforeEach(func(ctx context.Context) {
		fakeKubeClient = createFakeClientWithScheme()
		kaiConfig = kaiConfigForPrometheus()
	})

	Context("when Prometheus is enabled", func() {
		BeforeEach(func(ctx context.Context) {
			Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
		})

		It("should create usage-prometheus service with correct selectors", func(ctx context.Context) {
			objects, err := usagePrometheusServiceForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1))

			service := test_utils.FindTypeInObjects[*corev1.Service](objects)
			Expect(service).NotTo(BeNil())
			Expect((*service).Name).To(Equal(usagePrometheusService))
			Expect((*service).Namespace).To(Equal(kaiConfig.Spec.Namespace))
			Expect((*service).Spec.Selector).To(HaveKeyWithValue("app.kubernetes.io/instance", mainResourceName))
			Expect((*service).Spec.Selector).To(HaveKeyWithValue("app.kubernetes.io/name", "prometheus"))
			Expect((*service).Spec.Ports).To(HaveLen(1))
			Expect((*service).Spec.Ports[0].Port).To(Equal(int32(prometheusPort)))
		})

		It("should use custom instance name when configured", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.InstanceName = ptr.To("custom-prometheus")

			objects, err := usagePrometheusServiceForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(1))

			service := test_utils.FindTypeInObjects[*corev1.Service](objects)
			Expect(service).NotTo(BeNil())
			Expect((*service).Spec.Selector).To(HaveKeyWithValue("app.kubernetes.io/instance", "custom-prometheus"))
		})
	})

	Context("when Prometheus is disabled", func() {
		It("should return empty objects list", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.Enabled = ptr.To(false)
			objects, err := usagePrometheusServiceForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0))
		})
	})

	Context("when external Prometheus URL is provided", func() {
		It("should return empty objects list", func(ctx context.Context) {
			kaiConfig.Spec.Prometheus.ExternalPrometheusUrl = ptr.To("http://external-prometheus:9090")
			objects, err := usagePrometheusServiceForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0))
		})
	})

	Context("when Prometheus Operator is not installed", func() {
		It("should return empty objects list", func(ctx context.Context) {
			objects, err := usagePrometheusServiceForKAIConfig(ctx, fakeKubeClient, kaiConfig)
			Expect(err).To(BeNil())
			Expect(len(objects)).To(Equal(0))
		})
	})
})

var _ = Describe("getPrometheusInstanceName", func() {
	It("should return default name when config is nil", func() {
		instanceName := getPrometheusInstanceName(nil)
		Expect(instanceName).To(Equal(mainResourceName))
	})

	It("should return default name when InstanceName is nil", func() {
		config := &kaiprometheus.Prometheus{}
		instanceName := getPrometheusInstanceName(config)
		Expect(instanceName).To(Equal(mainResourceName))
	})

	It("should return default name when InstanceName is empty", func() {
		config := &kaiprometheus.Prometheus{
			InstanceName: ptr.To(""),
		}
		instanceName := getPrometheusInstanceName(config)
		Expect(instanceName).To(Equal(mainResourceName))
	})

	It("should return custom name when InstanceName is set", func() {
		config := &kaiprometheus.Prometheus{
			InstanceName: ptr.To("custom-prometheus"),
		}
		instanceName := getPrometheusInstanceName(config)
		Expect(instanceName).To(Equal("custom-prometheus"))
	})
})

var _ = Describe("Custom InstanceName", func() {
	var (
		fakeKubeClient client.Client
		kaiConfig      *kaiv1.Config
	)

	BeforeEach(func(ctx context.Context) {
		fakeKubeClient = createFakeClientWithScheme()
		kaiConfig = kaiConfigForPrometheus()
		kaiConfig.Spec.Prometheus.InstanceName = ptr.To("custom-prom")

		Expect(fakeKubeClient.Create(ctx, getPrometheusCRD())).To(Succeed())
	})

	It("should create Prometheus with custom instance name", func(ctx context.Context) {
		objects, err := prometheusForKAIConfig(ctx, fakeKubeClient, kaiConfig)
		Expect(err).To(BeNil())
		Expect(len(objects)).To(Equal(1))

		prometheusObj := test_utils.FindTypeInObjects[*monitoringv1.Prometheus](objects)
		Expect(prometheusObj).NotTo(BeNil())
		Expect((*prometheusObj).Name).To(Equal("custom-prom"))
		Expect((*prometheusObj).Spec.ServiceAccountName).To(Equal("custom-prom"))
	})

	It("should create ServiceAccount with custom instance name", func(ctx context.Context) {
		objects, err := prometheusServiceAccountForKAIConfig(ctx, fakeKubeClient, kaiConfig)
		Expect(err).To(BeNil())
		Expect(len(objects)).To(Equal(1))

		sa := test_utils.FindTypeInObjects[*corev1.ServiceAccount](objects)
		Expect(sa).NotTo(BeNil())
		Expect((*sa).Name).To(Equal("custom-prom"))
	})
})

func getServiceMonitorCRD() *apiextensionsv1.CustomResourceDefinition {
	serviceMonitorCRD := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "servicemonitors.monitoring.coreos.com",
		},
	}
	return serviceMonitorCRD
}

func getPrometheusCRD() *apiextensionsv1.CustomResourceDefinition {
	prometheusCRD := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "prometheuses.monitoring.coreos.com",
		},
	}
	return prometheusCRD
}
