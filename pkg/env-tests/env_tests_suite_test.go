// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package env_tests

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	featuregate "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
	featuregate.SetFeatureGateDuringTest(tt, utilfeature.DefaultFeatureGate,
		features.DynamicResourceAllocation, true)
	var err error
	cfg, ctrlClient, testEnv, err = SetupEnvTest(nil)
	Expect(err).To(Succeed())
})

var _ = AfterSuite(func(ctx context.Context) {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
