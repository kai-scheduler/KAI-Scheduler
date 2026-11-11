// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	kubefeatures "k8s.io/kubernetes/pkg/features"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
)

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
		featuregates.SetServerMinorVersionForTest(0)
	})

	It("keeps the vendored defaults on a server older than 1.36", func() {
		featuregates.SetDynamicResourcesEnabledForTest(true)
		featuregates.SetServerMinorVersionForTest(35)

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
		featuregates.SetServerMinorVersionForTest(37)

		f := getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAPartitionableDevices).To(BeTrue())
		Expect(f.EnableDRAConsumableCapacity).To(BeTrue())
	})

	It("does not enable them when DRA is unavailable", func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
		featuregates.SetServerMinorVersionForTest(36)

		f := getK8sFeatures(newGates(nil))
		Expect(f.EnableDRAPartitionableDevices).To(BeFalse())
		Expect(f.EnableDRAConsumableCapacity).To(BeFalse())
	})

	It("respects gates set explicitly with --feature-gates", func() {
		featuregates.SetDynamicResourcesEnabledForTest(true)
		featuregates.SetServerMinorVersionForTest(36)

		f := getK8sFeatures(newGates(map[string]bool{
			string(kubefeatures.DRAPartitionableDevices): false,
		}))
		Expect(f.EnableDRAPartitionableDevices).To(BeFalse())
		Expect(f.EnableDRAConsumableCapacity).To(BeTrue())
	})
})
