// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
