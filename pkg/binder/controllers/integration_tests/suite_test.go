// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/NVIDIA/KAI-scheduler/cmd/binder/app"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	schedulingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"

	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/controllers"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins"
	k8s_plugins "github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/k8s-plugins"
	// +kubebuilder:scaffold:imports
)

const (
	resourceReservationNameSpace      = "kai-resource-reservation"
	resourceReservationServiceAccount = resourceReservationNameSpace
	resourceReservationAppLabelValue  = resourceReservationNameSpace
	scalingPodsNamespace              = "kai-scale-adjust"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var testContext context.Context
var cancelTestContext context.CancelFunc
var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var k8sManager ctrl.Manager
var rrs resourcereservation.Interface

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	testContext, cancelTestContext = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "deployments", "kai-scheduler", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = schedulingv1alpha2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	params := &controllers.ReconcilerParams{
		MaxConcurrentReconciles:     1,
		RateLimiterBaseDelaySeconds: 1,
		RateLimiterMaxDelaySeconds:  1,
	}
	options := app.Options{
		MaxConcurrentReconciles:     params.MaxConcurrentReconciles,
		RateLimiterBaseDelaySeconds: params.RateLimiterBaseDelaySeconds,
		RateLimiterMaxDelaySeconds:  params.RateLimiterMaxDelaySeconds,
	}

	kubeClient := kubernetes.NewForConfigOrDie(cfg)
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)

	binderPlugins := plugins.New()
	k8sPlugins, err := k8s_plugins.New(kubeClient, informerFactory, int64(options.VolumeBindingTimeoutSeconds))
	Expect(err).NotTo(HaveOccurred())
	binderPlugins.RegisterPlugin(k8sPlugins)
	clientWithWatch, err := client.NewWithWatch(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())

	rrs = resourcereservation.NewService(false, clientWithWatch, "", 40*time.Second,
		resourceReservationNameSpace, resourceReservationServiceAccount, resourceReservationAppLabelValue, scalingPodsNamespace, constants.DefaultRuntimeClassName,
		nil) // nil podResources to use defaults
	podBinder := binding.NewBinder(k8sManager.GetClient(), rrs, binderPlugins)

	err = controllers.NewBindRequestReconciler(
		k8sManager.GetClient(), k8sManager.GetScheme(), k8sManager.GetEventRecorderFor("binder"), params,
		podBinder, rrs).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.PodReconciler{
		Client:              k8sManager.GetClient(),
		Scheme:              k8sManager.GetScheme(),
		ResourceReservation: rrs,
	}).SetupWithManager(k8sManager, params)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(testContext)
		Expect(err).ToNot(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	cancelTestContext()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
