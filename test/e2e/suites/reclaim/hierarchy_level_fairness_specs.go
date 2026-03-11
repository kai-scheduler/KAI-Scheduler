/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/fillers"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeHierarchyLevelFairnessSpecs() bool {
	return Describe("Hierarchy level fairness", Ordered, func() {
		Context("Hierarchy level fairness", func() {
			var (
				testCtx              *testcontext.TestContext
				reclaimeeParentQueue *v2.Queue
				reclaimeeQueue       *v2.Queue
				reclaimerParentQueue *v2.Queue
				reclaimerQueue       *v2.Queue
				lowPriority          string
			)

			BeforeAll(func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
					{
						Gpu:      resource.MustParse("2"),
						PodCount: 1,
					},
				})

				reclaimeeParentQueueName := utils.GenerateRandomK8sName(10)
				reclaimeeParentQueue = queue.CreateQueueObjectWithGpuResource(reclaimeeParentQueueName,
					v2.QueueResource{
						Quota:           -1,
						OverQuotaWeight: 1,
						Limit:           -1,
					}, "")

				reclaimeeQueueName := utils.GenerateRandomK8sName(10)
				reclaimeeQueue = queue.CreateQueueObjectWithGpuResource(reclaimeeQueueName,
					v2.QueueResource{
						Quota:           0,
						OverQuotaWeight: 1,
						Limit:           -1,
					}, reclaimeeParentQueueName)

				reclaimerParentQueueName := utils.GenerateRandomK8sName(10)
				reclaimerParentQueue = queue.CreateQueueObjectWithGpuResource(reclaimerParentQueueName,
					v2.QueueResource{
						Quota:           0,
						OverQuotaWeight: 1,
						Limit:           0,
					}, "")
				reclaimerQueueName := utils.GenerateRandomK8sName(10)
				reclaimerQueue = queue.CreateQueueObjectWithGpuResource(reclaimerQueueName,
					v2.QueueResource{
						Quota:           0,
						OverQuotaWeight: 1,
						Limit:           -1,
					}, reclaimerParentQueueName)

				testCtx.InitQueues([]*v2.Queue{reclaimeeParentQueue, reclaimerParentQueue, reclaimeeQueue,
					reclaimerQueue})

				var err error
				lowPriority, err = rd.CreatePreemptiblePriorityClass(ctx, testCtx.KubeClientset)
				Expect(err).To(Succeed())

				err = feature_flags.SetFullHierarchyFairness(ctx, testCtx, nil)
				Expect(err).To(Succeed())
			})

			AfterAll(func(ctx context.Context) {
				err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
				Expect(err).To(Succeed())
				testCtx.ClusterCleanup(ctx)
			})

			AfterEach(func(ctx context.Context) {
				testCtx.TestContextCleanup(ctx)
				err := feature_flags.SetFullHierarchyFairness(ctx, testCtx, nil)
				Expect(err).To(Succeed())
			})

			It("reclaim after changing hierarchy level fairness type", func(ctx context.Context) {
				resources := v1.ResourceRequirements{
					Limits: v1.ResourceList{
						constants.GpuResource: resource.MustParse("1"),
					},
					Requests: v1.ResourceList{
						constants.GpuResource: resource.MustParse("1"),
					},
				}

				_, _, err := fillers.FillAllNodesWithJobs(
					ctx, testCtx, reclaimeeQueue, resources, nil, nil, lowPriority,
				)
				Expect(err).To(Succeed())
				namespace := queue.GetConnectedNamespaceToQueue(reclaimeeQueue)
				pods, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
				Expect(err).To(Succeed())
				Expect(len(pods.Items)).To(BeNumerically(">", 1))

				pod := CreatePod(ctx, testCtx, reclaimerQueue, 1)
				wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)

				err = feature_flags.SetFullHierarchyFairness(ctx, testCtx, ptr.To(false))
				Expect(err).To(Succeed())

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			})
		})
	})
}
