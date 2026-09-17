/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package events

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeEventsSpecs() bool {
	return Describe("Events", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					PodCount: 1,
				})

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			testQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{testQueue, parentQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("NotReady job", func(ctx context.Context) {
			testQueue := testCtx.Queues[0]
			namespace := queue.GetConnectedNamespaceToQueue(testQueue)

			_, podGroup, _, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, testQueue,
				rd.DistributedBatchJobOptions{
					MinMember: ptr.To(int32(2)),
				})
			Expect(err).To(Succeed())

			wait.ForPodGroupNotReadyEvent(ctx, testCtx.ControllerClient, namespace, podGroup.Name)
		})
	})
}
