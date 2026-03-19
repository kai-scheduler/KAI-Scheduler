// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	featuregate "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"
)

// These tests verify that DRA informers are only registered when the DynamicResourceAllocation
// feature gate is enabled. Unconditional registration caused WaitForCacheSync to block forever
// on clusters without the resource.k8s.io API group, preventing the scheduler from starting.
var _ = Describe("InitializeInternalPlugins", func() {
	Context("DRA feature gate disabled", func() {
		It("should not create ResourceSliceTracker", func() {
			featuregate.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultMutableFeatureGate,
				features.DynamicResourceAllocation, false)

			fakeClient := fake.NewSimpleClientset()
			factory := informers.NewSharedInformerFactory(fakeClient, 0)

			result := InitializeInternalPlugins(fakeClient, factory, nil)

			Expect(result.ResourceSliceTracker).To(BeNil())
		})
	})
})
