/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

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

func DescribePreemptMinSubGroupSpecs() bool {
	return Describe("Preempt with MinSubGroup", Ordered, func() {
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

		It("should preempt elastic subgroups above minSubGroup threshold", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			lowQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			lowQueue.Spec.Resources.CPU.Quota = 300
			lowQueue.Spec.Resources.CPU.Limit = 600
			highQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			highQueue.Spec.Resources.CPU.Quota = 300
			highQueue.Spec.Resources.CPU.Limit = 600
			testCtx.InitQueues([]*v2.Queue{lowQueue, highQueue, parentQueue})

			lowNamespace := queue.GetConnectedNamespaceToQueue(lowQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
				utils.GenerateRandomK8sName(10), lowQueue, ptr.To[int32](2),
				flatLeaves("sg", 4, 1), nil, "", cpuPerPod)

			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, h.AllPods, 4)

			highPod := rd.CreatePodObject(highQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("300m"),
				},
			})
			highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

			// Elastic subgroups preempted, but minSubGroup=2 preserved
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, h.AllPods, 2)
		})
	})
}

func flatLeaves(prefix string, count, podsPerLeaf int) []pod_group.SubGroupNode {
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
