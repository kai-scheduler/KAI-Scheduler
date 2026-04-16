/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeReclaimMinSubGroupSpecs() bool {
	return Describe("Reclaim with MinSubGroup", Ordered, func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)

			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Cpu:      resource.MustParse("600m"),
					PodCount: 8,
				},
			})
		})

		AfterAll(func(ctx context.Context) {
			err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
			Expect(err).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("should reclaim resources from elastic subgroups for fair share", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			reclaimeeQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			reclaimeeQueue.Spec.Resources.CPU.Quota = 200
			reclaimeeQueue.Spec.Resources.CPU.Limit = 600
			reclaimerQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			reclaimerQueue.Spec.Resources.CPU.Quota = 200
			reclaimerQueue.Spec.Resources.CPU.Limit = 600
			testCtx.InitQueues([]*v2.Queue{reclaimeeQueue, reclaimerQueue, parentQueue})

			reclaimeeNamespace := queue.GetConnectedNamespaceToQueue(reclaimeeQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
				utils.GenerateRandomK8sName(10), reclaimeeQueue, ptr.To[int32](2),
				reclaimFlatLeaves("sg", 4, 1), nil, "", cpuPerPod)

			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, h.AllPods, 4)

			reclaimerPod := rd.CreatePodObject(reclaimerQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			reclaimerPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, reclaimerPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimerPod)

			// Elastic subgroups reclaimed, minSubGroup=2 preserved
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, h.AllPods, 2)
		})
	})
}

func reclaimFlatLeaves(prefix string, count, podsPerLeaf int) []pod_group.SubGroupNode {
	nodes := make([]pod_group.SubGroupNode, 0, count)
	for i := 0; i < count; i++ {
		nodes = append(nodes, pod_group.SubGroupNode{
			Name:      fmt.Sprintf("%s-%d", prefix, i),
			MinMember: ptr.To(int32(podsPerLeaf)),
			PodCount:  podsPerLeaf,
		})
	}
	return nodes
}
