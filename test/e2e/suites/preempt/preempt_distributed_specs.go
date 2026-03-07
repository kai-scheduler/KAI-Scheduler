/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/fillers"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

func DescribePreemptDistributedSpecs() bool {
	return Describe("preempt Distributed Jobs", Ordered, func() {
		Context("Over more than one nodes", func() {
			var (
				testCtx      *testcontext.TestContext
				testQueue    *v2.Queue
				lowPriority  string
				highPriority string
			)

			BeforeAll(func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
					{
						Gpu:      resource.MustParse("4"),
						PodCount: 4,
					},
					{
						Gpu:      resource.MustParse("4"),
						PodCount: 4,
					},
				})

				parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
				testQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
				testCtx.InitQueues([]*v2.Queue{parentQueue, testQueue})

				var err error
				lowPriority, highPriority, err = rd.CreatePreemptibleAndNonPriorityClass(ctx, testCtx.KubeClientset)
				Expect(err).To(Succeed())
			})

			AfterEach(func(ctx context.Context) {
				testCtx.TestContextCleanup(ctx)
			})

			AfterAll(func(ctx context.Context) {
				err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
				Expect(err).To(Succeed())
				testCtx.ClusterCleanup(ctx)
			})

			It("should preempt jobs", func(ctx context.Context) {
				gpusPerNode := capacity.SkipIfNonHomogeneousGpuCounts(testCtx.KubeClientset)

				resources := v1.ResourceRequirements{
					Limits: v1.ResourceList{
						constants.NvidiaGpuResource: resource.MustParse("1"),
					},
					Requests: v1.ResourceList{
						constants.NvidiaGpuResource: resource.MustParse("1"),
					},
				}

				_, fillerPods, err := fillers.FillAllNodesWithJobs(
					ctx, testCtx, testQueue, resources, nil, nil, lowPriority,
				)
				Expect(err).To(Succeed())
				nodesNamesMap := map[string]bool{}
				for _, pod := range fillerPods {
					Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), pod)).To(Succeed())
					if pod.Spec.NodeName != "" {
						nodesNamesMap[pod.Spec.NodeName] = true
					}
				}

				preemptorGpu := gpusPerNode
				preemptorResources := resources.DeepCopy()
				preemptorResources.Requests[constants.NvidiaGpuResource] = resource.MustParse(fmt.Sprintf("%d", preemptorGpu))
				preemptorResources.Limits[constants.NvidiaGpuResource] = resource.MustParse(fmt.Sprintf("%d", preemptorGpu))

				_, pods := pod_group.CreateDistributedJob(
					ctx, testCtx.KubeClientset, testCtx.ControllerClient,
					testQueue, 2, *preemptorResources, highPriority,
				)

				namespace := queue.GetConnectedNamespaceToQueue(testQueue)
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 2)
			})
		})
	})
}
