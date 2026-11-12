// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/util/feature"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/component-base/featuregate"
	kubefeatures "k8s.io/kubernetes/pkg/features"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
)

// setServerVersion runs SetDRAFeatureGate against a fake API server of version
// 1.<minor> that serves resource.k8s.io/v1, so the server version takes the same
// parsing path as in production. The DRA availability flag is left as it was.
// An empty minor resets the version to unknown.
func setServerVersion(minor string) {
	draEnabled := featuregates.DynamicResourcesEnabled()
	defer featuregates.SetDynamicResourcesEnabledForTest(draEnabled)

	client := fake.NewClientset()
	client.Resources = []*metav1.APIResourceList{{GroupVersion: "resource.k8s.io/v1"}}
	discovery := client.Discovery().(*fakediscovery.FakeDiscovery)
	discovery.FakedServerVersion = &version.Info{Major: "1", Minor: minor}
	if err := featuregates.SetDRAFeatureGate(discovery); err != nil {
		panic(err)
	}
}

func TestK8sUtilsFeatures(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "k8s_utils features")
}

var _ = Describe("GetK8sFeatures", func() {
	AfterEach(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	It("reports EnableDynamicResourceAllocation=false when KAI determines DRA is unavailable", func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)

		Expect(GetK8sFeatures().EnableDynamicResourceAllocation).To(BeFalse())
	})

	It("reports EnableDynamicResourceAllocation=true when KAI determines DRA is available", func() {
		featuregates.SetDynamicResourcesEnabledForTest(true)

		Expect(GetK8sFeatures().EnableDynamicResourceAllocation).To(BeTrue())
	})
})

var _ = Describe("GetK8sFeatures DRA defaults by server version", func() {
	newGates := func(set map[string]bool) featuregate.MutableVersionedFeatureGate {
		gates := feature.DefaultMutableFeatureGate.DeepCopyAndReset()
		if set != nil {
			Expect(gates.SetFromMap(set)).To(Succeed())
		}
		return gates
	}

	AfterEach(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
		setServerVersion("")
	})

	It("keeps the vendored defaults on a server older than 1.36", func() {
		featuregates.SetDynamicResourcesEnabledForTest(true)
		setServerVersion("35")

		f := getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAPartitionableDevices).To(BeFalse())
		Expect(f.EnableDRAConsumableCapacity).To(BeFalse())
	})

	It("keeps the vendored defaults when the server version is unknown", func() {
		featuregates.SetDynamicResourcesEnabledForTest(true)

		f := getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAPartitionableDevices).To(BeFalse())
		Expect(f.EnableDRAConsumableCapacity).To(BeFalse())
	})

	It("enables the gates that are default-on in 1.36 on a 1.36+ server", func() {
		featuregates.SetDynamicResourcesEnabledForTest(true)
		setServerVersion("37")

		f := getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAPartitionableDevices).To(BeTrue())
		Expect(f.EnableDRAConsumableCapacity).To(BeTrue())
	})

	It("does not enable them when DRA is unavailable", func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
		setServerVersion("36")

		f := getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAPartitionableDevices).To(BeFalse())
		Expect(f.EnableDRAConsumableCapacity).To(BeFalse())
	})

	It("enables each gate only from its own server version", func() {
		original := draDefaultOnByServerVersion
		DeferCleanup(func() { draDefaultOnByServerVersion = original })
		draDefaultOnByServerVersion = append(original[:1:1], original[1])
		draDefaultOnByServerVersion[1].since = 37

		featuregates.SetDynamicResourcesEnabledForTest(true)
		setServerVersion("36")
		f := getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAPartitionableDevices).To(BeTrue())
		Expect(f.EnableDRAConsumableCapacity).To(BeFalse())

		setServerVersion("37")
		f = getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAConsumableCapacity).To(BeTrue())
	})

	It("respects gates set explicitly with --feature-gates", func() {
		featuregates.SetDynamicResourcesEnabledForTest(true)
		setServerVersion("36")

		f := getK8sFeatures(newGates(map[string]bool{
			string(kubefeatures.DRAPartitionableDevices): false,
		}))
		Expect(f.EnableDRAPartitionableDevices).To(BeFalse())
		Expect(f.EnableDRAConsumableCapacity).To(BeTrue())
	})
})
