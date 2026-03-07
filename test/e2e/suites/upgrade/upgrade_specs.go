/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package upgrade

import (
	"context"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

func DescribeUpgradeSpecs() bool {
	return Describe("Upgrade", Label(labels.Upgrade), Ordered, func() {
		var (
			testCtx          *testcontext.TestContext
			preUpgradePod    *v1.Pod
			parentQueue      *v2.Queue
			childQueue       *v2.Queue
			upgradeChartPath string
		)

		BeforeAll(func(ctx context.Context) {
			upgradeChartPath = os.Getenv("UPGRADE_CHART_PATH")
			Expect(upgradeChartPath).NotTo(BeEmpty(),
				"UPGRADE_CHART_PATH environment variable must be set to the path of the new chart")

			By("Connecting to cluster running the previous KAI version")
			testCtx = testcontext.GetConnectivity(ctx, Default)

			By("Creating test queues on the old version")
			parentQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			By("Creating a pod on the old version and verifying it is scheduled")
			preUpgradePod = rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			var err error
			preUpgradePod, err = rd.CreatePod(ctx, testCtx.KubeClientset, preUpgradePod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, preUpgradePod)
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("should upgrade KAI scheduler via helm", func(ctx context.Context) {
			By("Running helm upgrade to the new version")
			upgradeKAIScheduler(upgradeChartPath)

			By("Waiting for KAI config to report healthy status")
			wait.ForKAIConfigStatusOK(ctx, testCtx.ControllerClient)

			By("Waiting for default scheduling shard to report healthy status")
			wait.ForSchedulingShardStatusOK(ctx, testCtx.ControllerClient, "default")
		})

		It("should keep pre-upgrade workload running after upgrade", func(ctx context.Context) {
			By(fmt.Sprintf("Verifying pre-upgrade pod %s/%s is still running", preUpgradePod.Namespace, preUpgradePod.Name))
			updatedPod := &v1.Pod{}
			err := testCtx.ControllerClient.Get(ctx, types.NamespacedName{
				Namespace: preUpgradePod.Namespace,
				Name:      preUpgradePod.Name,
			}, updatedPod)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedPod.Status.Phase).To(Equal(v1.PodRunning),
				"Pre-upgrade pod should still be running after upgrade")
		})

		It("should schedule new workloads after upgrade", func(ctx context.Context) {
			By("Creating a new pod after upgrade")
			postUpgradePod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			var err error
			postUpgradePod, err = rd.CreatePod(ctx, testCtx.KubeClientset, postUpgradePod)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the new pod to be scheduled")
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, postUpgradePod)
		})

		It("should allow creating new queues after upgrade", func(ctx context.Context) {
			By("Creating a new child queue after upgrade")
			newQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.AddQueues(ctx, []*v2.Queue{newQueue})

			By("Creating a pod in the new queue and verifying it is scheduled")
			pod := rd.CreatePodObject(newQueue, v1.ResourceRequirements{})
			var err error
			pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})
	})
}
