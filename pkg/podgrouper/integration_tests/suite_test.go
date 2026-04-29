// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	controllers "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper"
	pluginshub "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/hub"
)

const (
	testSchedulerName  = "kai-scheduler"
	assertTimeout      = 30 * time.Second
	consistentlyWindow = 2 * time.Second
	assertInterval     = 200 * time.Millisecond
)

var (
	testCtx    context.Context
	cancelCtx  context.CancelFunc
	cfg        *rest.Config
	k8sClient  client.Client
	kubeClient *kubernetes.Clientset
	testEnv    *envtest.Environment
	k8sManager ctrl.Manager
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Podgrouper Workload API Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	testCtx, cancelCtx = context.WithCancel(context.Background())

	By("bootstrapping envtest with the GenericWorkload feature gate enabled")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "deployments", "kai-scheduler", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}
	// GenericWorkload Alpha gate is required for scheduling.k8s.io/v1alpha1 and Pod.Spec.WorkloadRef.
	testEnv.ControlPlane.GetAPIServer().Configure().
		Append("feature-gates", "GenericWorkload=true")
	testEnv.ControlPlane.GetAPIServer().Configure().
		Append("runtime-config", "scheduling.k8s.io/v1alpha1=true")

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(schedulingv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(schedulingv1alpha2.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(schedulingv2.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(schedulingv2alpha2.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(kaiv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	kubeClient, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Client: client.Options{
			Cache: &client.CacheOptions{Unstructured: true},
		},
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	featuregates.SetWorkloadAPIEnabledForTest(true)

	pluginsHub := pluginshub.NewDefaultPluginsHub(
		k8sManager.GetClient(),
		false,                                              /* searchForLegacyPodGroups */
		false,                                              /* knativeGangSchedule */
		"" /* scheduling queue label key — defaults */, "", /* node-pool label key */
		"" /* default config per-type configmap name */, "", /* default config per-type configmap namespace */
	)

	err = (&controllers.PodReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager, controllers.Configs{
		SchedulerName:           testSchedulerName,
		MaxConcurrentReconciles: 1,
		WorkloadAPIEnabled:      true,
	}, pluginsHub)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := k8sManager.Start(testCtx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// Pre-warm unstructured Pod informer; controller-runtime starts it lazily and
	// races with the first Reconcile, producing transient "Pod not found" errors.
	Eventually(func() error {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PodList"))
		return k8sManager.GetClient().List(testCtx, list)
	}, 10*time.Second, 100*time.Millisecond).Should(Succeed())

	// PriorityClasses are created by Helm in production; envtest starts with none.
	for _, name := range []string{"build", "train", "inference"} {
		Expect(k8sClient.Create(testCtx, &schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Value:      50,
		})).To(Succeed())
	}
})

var _ = AfterSuite(func() {
	cancelCtx()
	By("tearing down envtest")
	Expect(testEnv.Stop()).To(Succeed())
})
