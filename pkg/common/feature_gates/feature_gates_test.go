// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package featuregates

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	resourcev1 "k8s.io/api/resource/v1"
	resourcev1alhpa3 "k8s.io/api/resource/v1alpha3"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	version "k8s.io/apimachinery/pkg/version"
	featureutil "k8s.io/apiserver/pkg/util/feature"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubernetes/pkg/features"
)

func TestCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test cache")
}

var _ = Describe("New", func() {
	Context("DRA Feature Gate", func() {
		DescribeTable("should enable DRA feature gate based on Kubernetes version and resource API availability",
			func(serverMajor, serverMinor string, resourceGroupVersions []string, expectDRAFeatureEnabled bool) {
				fakeClient := fake.NewClientset()
				fakeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
					Major: serverMajor,
					Minor: serverMinor,
				}

				for _, groupVersion := range resourceGroupVersions {
					fakeClient.Resources = append(fakeClient.Resources, &metav1.APIResourceList{GroupVersion: groupVersion})
				}

				SetDRAFeatureGate(fakeClient.Discovery())

				// Check if the DynamicResourceAllocation feature gate has the expected state
				draEnabled := featureutil.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation)
				Expect(draEnabled).To(Equal(expectDRAFeatureEnabled))
			},
			Entry("compatible version (1.32) with resource API should enable DRA", "1", "32", []string{resourcev1beta1.SchemeGroupVersion.String()}, true),
			Entry("compatible version (1.32+) with resource API should enable DRA", "1", "32+", []string{resourcev1beta1.SchemeGroupVersion.String()}, true),
			Entry("compatible version (1.32) without resource API should not enable DRA", "1", "32", []string{}, false),
			Entry("incompatible version (1.25) with resource API should not enable DRA", "1", "25", []string{resourcev1beta1.SchemeGroupVersion.String()}, false),
			Entry("incompatible version (1.25) without resource API should not enable DRA", "1", "25", []string{}, false),
			Entry("edge case version (1.31) with resource API should not enable DRA", "1", "31", []string{resourcev1alhpa3.SchemeGroupVersion.String()}, false),
			Entry("higher compatible version (1.35) with resource API should enable DRA", "1", "34", []string{resourcev1.SchemeGroupVersion.String()}, true),
		)
	})
})
