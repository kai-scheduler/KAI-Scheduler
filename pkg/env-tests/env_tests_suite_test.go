// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package env_tests

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	resourceapi "k8s.io/api/resource/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	featuregate "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kaiv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	kaiv2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	kaiv2v2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

var (
	cfg        *rest.Config
	ctrlClient client.Client
	testEnv    *envtest.Environment
	tt         *testing.T
)

func TestEnvTests(t *testing.T) {
	tt = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "EnvTests Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "deployments", "kai-scheduler", "crds")},
		ErrorIfCRDPathMissing: true,
	}
	testEnv.ControlPlane.GetAPIServer().Configure().Append("feature-gates", "DynamicResourceAllocation=true")
	testEnv.ControlPlane.GetAPIServer().Configure().Append("runtime-config", "api/all=true")
	featuregate.SetFeatureGateDuringTest(tt, utilfeature.DefaultFeatureGate,
		features.DynamicResourceAllocation, true)

	var err error
	// cfg is defined in this file globally
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Add any scheme registration here if needed for your custom CRDs
	kaiv2.AddToScheme(scheme.Scheme)
	kaiv1alpha2.AddToScheme(scheme.Scheme)
	kaiv2v2alpha2.AddToScheme(scheme.Scheme)
	resourceapi.AddToScheme(scheme.Scheme)
	// +kubebuilder:scaffold:scheme

	ctrlClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(ctrlClient).NotTo(BeNil())

	// This is needed since we don't have the conversion webhook installed in the test environment.
	// To be removed once v2 is the stored version by default.
	storeQueueV2CRD(ctx, ctrlClient)
})

var _ = AfterSuite(func(ctx context.Context) {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func storeQueueV2CRD(ctx context.Context, ctrlClient client.Client) {
	var crd apiextensionsv1.CustomResourceDefinition
	err := ctrlClient.Get(ctx, client.ObjectKey{Name: "queues.scheduling.run.ai"}, &crd)
	Expect(err).NotTo(HaveOccurred(), "Failed to get CRD")

	foundV2 := false
	for i, version := range crd.Spec.Versions {
		crd.Spec.Versions[i].Storage = false
		if version.Name == "v2" {
			crd.Spec.Versions[i].Storage = true
			foundV2 = true
		}
	}
	Expect(foundV2).To(BeTrue(), "Failed to find v2 version in CRD")

	err = ctrlClient.Update(ctx, &crd)
	Expect(err).NotTo(HaveOccurred(), "Failed to update CRD")
}
