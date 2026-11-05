// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	ksf "k8s.io/kube-scheduler/framework"
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

	// The future cluster view removes terminating pods from prefilter state by calling only the pod
	// affinity plugin's RemovePod. A new plugin with prefilter extensions must be handled there too.
	It("has prefilter extensions only on the pod affinity plugin", func() {
		fakeClient := fake.NewSimpleClientset()
		result := InitializeInternalPlugins(fakeClient, informers.NewSharedInformerFactory(fakeClient, 0), nil)

		var withExtensions []string
		fields := reflect.ValueOf(result).Elem()
		for i := range fields.NumField() {
			plugin, ok := reflect.TypeAssert[ksf.PreFilterPlugin](fields.Field(i))
			if ok && plugin.PreFilterExtensions() != nil {
				withExtensions = append(withExtensions, fields.Type().Field(i).Name)
			}
		}
		Expect(withExtensions).To(Equal([]string{"PodAffinity"}),
			"handle the new plugin's RemovePod in futureClusterView.CycleState")
	})
})
