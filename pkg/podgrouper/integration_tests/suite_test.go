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
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	controllers "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper"
	pluginshub "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/hub"
)

const (
	testSchedulerName = "kai-scheduler"
	// 30s gives slow CI runners headroom — locally the suite finishes in <10s.
	assertTimeout = 30 * time.Second
	// 2s is enough Consistently coverage to confirm no PodGroup is created
	// before triggering the next step.
	consistentlyWindow = 2 * time.Second
	assertInterval     = 200 * time.Millisecond
)

// These tests exercise the podgrouper's Workload API translation layer end
// to end against an envtest-started kube-apiserver. The GenericWorkload
// feature gate is Alpha and off by default, so we explicitly enable it on
// the apiserver arguments.

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
	// GenericWorkload is Alpha in k8s 1.35. Without the feature gate the
	// apiserver won't serve scheduling.k8s.io/v1alpha1 Workloads nor
	// persist Pod.Spec.WorkloadRef.
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
	})
	Expect(err).NotTo(HaveOccurred())

	// Start a Workload informer against the apiserver and hand its lister to
	// the podgrouper, matching the wiring in cmd/podgrouper/app/app.go.
	featuregates.SetWorkloadAPIEnabledForTest(true)
	factory := informers.NewSharedInformerFactory(kubeClient, 0)
	workloadLister := factory.Scheduling().V1alpha1().Workloads().Lister()
	factory.Scheduling().V1alpha1().Workloads().Informer()
	factory.Start(testCtx.Done())
	factory.WaitForCacheSync(testCtx.Done())

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
	}, pluginsHub, workloadLister)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := k8sManager.Start(testCtx)
		Expect(err).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	cancelCtx()
	By("tearing down envtest")
	Expect(testEnv.Stop()).To(Succeed())
})
