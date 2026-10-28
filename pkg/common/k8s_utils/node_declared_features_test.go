// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	kubefeatures "k8s.io/kubernetes/pkg/features"
)

const restartAllContainersFeature = "RestartAllContainersOnContainerExits"

func podWithRestartRules(containerAction, initContainerAction v1.ContainerRestartRuleAction) *v1.Pod {
	rulesFor := func(action v1.ContainerRestartRuleAction) []v1.ContainerRestartRule {
		if action == "" {
			return nil
		}
		return []v1.ContainerRestartRule{{Action: action}}
	}
	return &v1.Pod{
		Spec: v1.PodSpec{
			InitContainers: []v1.Container{{Name: "init", RestartPolicyRules: rulesFor(initContainerAction)}},
			Containers:     []v1.Container{{Name: "main", RestartPolicyRules: rulesFor(containerAction)}},
		},
	}
}

var _ = Describe("NodeDeclaredFeatureRequirements", func() {
	setGate := func(enabled bool) {
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), feature.DefaultFeatureGate,
			kubefeatures.NodeDeclaredFeatures, enabled)
	}

	It("returns nil when the NodeDeclaredFeatures gate is disabled", func() {
		setGate(false)

		requirements, err := NodeDeclaredFeatureRequirements(
			podWithRestartRules(v1.ContainerRestartRuleActionRestartAllContainers, ""))

		Expect(err).NotTo(HaveOccurred())
		Expect(requirements).To(BeNil())
	})

	DescribeTable("with the NodeDeclaredFeatures gate enabled",
		func(pod *v1.Pod, expected []string) {
			setGate(true)

			requirements, err := NodeDeclaredFeatureRequirements(pod)

			Expect(err).NotTo(HaveOccurred())
			Expect(requirements).To(Equal(expected))
		},
		Entry("pod without restart rules", podWithRestartRules("", ""), nil),
		Entry("restart rule that only restarts the container",
			podWithRestartRules(v1.ContainerRestartRuleActionRestart, ""), nil),
		Entry("RestartAllContainers rule on a container",
			podWithRestartRules(v1.ContainerRestartRuleActionRestartAllContainers, ""),
			[]string{restartAllContainersFeature}),
		Entry("RestartAllContainers rule on an init container",
			podWithRestartRules("", v1.ContainerRestartRuleActionRestartAllContainers),
			[]string{restartAllContainersFeature}),
	)
})
