/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package timeaware

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	e2econstant "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/scheduling_shard"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	defaultShardName        = "default"
	prometheusPodName       = "prometheus-prometheus-0"
	prometheusReadyTimeout  = 2 * time.Minute
	schedulerRestartTimeout = 30 * time.Second
	prometheusCrdName       = "prometheuses.monitoring.coreos.com"
	prometheusCrdVersion    = "v1"
	draShardName            = "dra-shard"
	draPartitionLabelValue  = "dra"
	draNodeLabel            = "nvidia.com/gpu.deploy.dra-plugin-gpu"
)

var testCtx *testcontext.TestContext

func TestTimeAware(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Time Aware Fairness Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	By("Setting up test context")
	testCtx = testcontext.GetConnectivity(ctx, Default)

	By("Creating SchedulingShard for DRA nodes")
	err := scheduling_shard.CreateShardForLabeledNodes(
		ctx,
		testCtx.ControllerClient,
		draShardName,
		client.MatchingLabels{draNodeLabel: "true"},
		kaiv1.SchedulingShardSpec{
			PartitionLabelValue: draPartitionLabelValue,
		},
	)
	Expect(err).NotTo(HaveOccurred())

	By("Checking if Prometheus CRD is installed")
	crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, prometheusCrdName, prometheusCrdVersion)

	By("Saving original KAI config for restoration")
	originalKAIConfig := &kaiv1.Config{}
	err = testCtx.ControllerClient.Get(ctx, client.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, originalKAIConfig)
	Expect(err).NotTo(HaveOccurred(), "Failed to get original KAI config")

	By("Saving original SchedulingShard for restoration")
	originalSchedulingShard := &kaiv1.SchedulingShard{}
	err = testCtx.ControllerClient.Get(ctx, client.ObjectKey{Name: defaultShardName}, originalSchedulingShard)
	Expect(err).NotTo(HaveOccurred(), "Failed to get original SchedulingShard")

	By("Enabling time-aware fairness")
	config := defaultTimeAwareConfig()
	err = configureTimeAwareFairness(ctx, testCtx, defaultShardName, config)
	Expect(err).NotTo(HaveOccurred(), "Failed to enable time-aware fairness")
	DeferCleanup(func(ctx context.Context) {
		By("Deleting DRA SchedulingShard and removing labels")
		if err := scheduling_shard.DeleteShardAndRemoveLabels(ctx, testCtx.ControllerClient, draShardName); err != nil {
			GinkgoWriter.Printf("Warning: Failed to delete DRA shard: %v\n", err)
		}

		By("Restoring original SchedulingShard configuration")
		if err := configurations.PatchSchedulingShard(ctx, testCtx, defaultShardName, func(shard *kaiv1.SchedulingShard) {
			shard.Spec = originalSchedulingShard.Spec
		}); err != nil {
			GinkgoWriter.Printf("Warning: Failed to restore original SchedulingShard: %v\n", err)
		}

		By("Restoring original KAI config")
		if err := configurations.PatchKAIConfig(ctx, testCtx, func(config *kaiv1.Config) {
			config.Spec = originalKAIConfig.Spec
		}); err != nil {
			GinkgoWriter.Printf("Warning: Failed to restore original KAI config: %v\n", err)
		}
	})

	By("Waiting for Prometheus pod to be ready (operator should have created it)")
	wait.ForPodReady(ctx, testCtx.ControllerClient, &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: e2econstant.SystemPodsNamespace,
			Name:      prometheusPodName,
		},
	})

	By("Waiting for scheduler to restart with new configuration (including auto-resolved prometheus URL)")
	err = wait.ForRolloutRestartDeployment(ctx, testCtx.ControllerClient, e2econstant.SystemPodsNamespace, e2econstant.SchedulerDeploymentName)
	Expect(err).NotTo(HaveOccurred(), "Failed waiting for scheduler rollout restart")

	// TODO: Uncomment this when KAI operator triggers reconciliation on Prometheus changes properly (https://github.com/kai-scheduler/KAI-scheduler/issues/877)
	// By("Waiting for KAI config status to be healthy (operator reconciled)")
	// wait.ForKAIConfigStatusOK(ctx, testCtx.ControllerClient)

	By("Waiting for SchedulingShard status to be healthy (operator reconciled)")
	wait.ForSchedulingShardStatusOK(ctx, testCtx.ControllerClient, defaultShardName)
})
