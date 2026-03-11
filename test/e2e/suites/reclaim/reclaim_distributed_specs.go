/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/fillers"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeReclaimDistributedSpecs() bool {
	return Describe("Reclaim Distributed Jobs", Ordered, func() {
		Context("Over more than one nodes", func() {
			var (
				testCtx        *testcontext.TestContext
				parentQueue    *v2.Queue
				reclaimeeQueue *v2.Queue
				reclaimerQueue *v2.Queue
				lowPriority    string
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

				parentQueue, reclaimeeQueue, reclaimerQueue = CreateQueues(100, 0, 8)
				reclaimeeQueue.Spec.Resources.GPU.OverQuotaWeight = 0
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				var err error
				lowPriority, err = rd.CreatePreemptiblePriorityClass(ctx, testCtx.KubeClientset)
				Expect(err).To(Succeed())
			})

			AfterAll(func(ctx context.Context) {
				err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
				Expect(err).To(Succeed())
			})

			AfterEach(func(ctx context.Context) {
				testCtx.ClusterCleanup(ctx)
			})

			It("should reclaim jobs", func(ctx context.Context) {
				gpusPerNode := capacity.SkipIfNonHomogeneousGpuCounts(testCtx.KubeClientset)

				resources := v1.ResourceRequirements{
					Limits: v1.ResourceList{
						constants.GpuResource: resource.MustParse("1"),
					},
					Requests: v1.ResourceList{
						constants.GpuResource: resource.MustParse("1"),
					},
				}

				_, fillerPods, err := fillers.FillAllNodesWithJobs(
					ctx, testCtx, reclaimeeQueue, resources, nil, nil, lowPriority,
				)
				Expect(err).To(Succeed())
				nodesNamesMap := map[string]bool{}
				for _, pod := range fillerPods {
					Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), pod)).To(Succeed())
					if pod.Spec.NodeName != "" {
						nodesNamesMap[pod.Spec.NodeName] = true
					}
				}

				reclaimerGPUs := gpusPerNode
				reclaimerResources := resources.DeepCopy()
				reclaimerResources.Requests[constants.GpuResource] = resource.MustParse(fmt.Sprintf("%d", reclaimerGPUs))
				reclaimerResources.Limits[constants.GpuResource] = resource.MustParse(fmt.Sprintf("%d", reclaimerGPUs))

				_, pods := pod_group.CreateDistributedJob(
					ctx, testCtx.KubeClientset, testCtx.ControllerClient,
					reclaimerQueue, 2, *reclaimerResources, lowPriority,
				)
				namespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 2)
			})
		})
	})
}
