// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

// These tests verify that DRA informers are only registered when DRA is
// determined to be available against the cluster. Unconditional registration
// caused WaitForCacheSync to block forever on clusters without the
// resource.k8s.io API group, preventing the scheduler from starting.
var _ = Describe("InitializeInternalPlugins", func() {
	Context("DRA unavailable on the cluster", func() {
		It("should not create ResourceSliceTracker", func() {
			// featuregates.SetDRAFeatureGate was not called, so the process-wide
			// DynamicResourcesEnabled flag defaults to false.
			fakeClient := fake.NewSimpleClientset()
			factory := informers.NewSharedInformerFactory(fakeClient, 0)

			result := InitializeInternalPlugins(fakeClient, factory, nil)

			Expect(result.ResourceSliceTracker).To(BeNil())
		})
	})
})
