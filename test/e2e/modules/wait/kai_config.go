/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package wait

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
)

const (
	defaultKAIConfigStatusTimeout = 2 * time.Minute
	defaultStatusPollInterval     = 5 * time.Second
)

// ForKAIConfigStatusOK waits until the KAIConfig singleton has all status conditions
// (Deployed, Available, DependenciesFulfilled) set to True for the current generation.
func ForKAIConfigStatusOK(ctx context.Context, runtimeClient client.Client) {
	ForKAIConfigStatusOKWithTimeout(ctx, runtimeClient, defaultKAIConfigStatusTimeout)
}

// ForKAIConfigStatusOKWithTimeout waits until the KAIConfig singleton has all status conditions healthy,
// with a custom timeout.
func ForKAIConfigStatusOKWithTimeout(ctx context.Context, runtimeClient client.Client, timeout time.Duration) {
	Eventually(func(g Gomega) {
		kaiConfig := &kaiv1.Config{}
		err := runtimeClient.Get(ctx, client.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, kaiConfig)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(conditionIsTrueForGeneration(kaiConfig.Status.Conditions, string(kaiv1.ConditionTypeDeployed), kaiConfig.Generation)).To(BeTrue(), "Expected KAIConfig Deployed=true")
		g.Expect(conditionIsTrueForGeneration(kaiConfig.Status.Conditions, string(kaiv1.ConditionTypeAvailable), kaiConfig.Generation)).To(BeTrue(), "Expected KAIConfig Available=true")
		g.Expect(conditionIsTrueForGeneration(kaiConfig.Status.Conditions, string(kaiv1.ConditionDependenciesFulfilled), kaiConfig.Generation)).To(BeTrue(), "Expected KAIConfig DependenciesFulfilled=true")
	}, timeout, defaultStatusPollInterval).Should(Succeed())
}

func conditionIsTrueForGeneration(conditions []metav1.Condition, conditionType string, generation int64) bool {
	for _, c := range conditions {
		if c.Type != conditionType {
			continue
		}
		if c.Status == metav1.ConditionTrue && c.ObservedGeneration == generation {
			return true
		}
		return false
	}
	return false
}
