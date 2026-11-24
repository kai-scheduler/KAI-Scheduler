/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package predicates

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

func DescribeTopologySpreadSpecs() bool {
	return Describe("Topology spread constraints", Ordered, func() {
		var (
			testCtx   *testcontext.TestContext
			namespace string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
			namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{PodCount: 1},
				{PodCount: 1},
			})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("spreads two replicas over different nodes with DoNotSchedule on hostname", func(ctx context.Context) {
			deployment := rd.CreateDeploymentObject(namespace, testCtx.Queues[0].Name)
			deployment.Spec.Replicas = ptr.To(int32(2))
			deployment.Spec.Template.Spec.TopologySpreadConstraints = []v1.TopologySpreadConstraint{{
				MaxSkew:           1,
				TopologyKey:       constant.NodeNamePodLabelName,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     deployment.Spec.Selector,
			}}
			Expect(testCtx.ControllerClient.Create(ctx, deployment)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				Expect(runtimeClient.IgnoreNotFound(testCtx.ControllerClient.Delete(ctx, deployment))).To(Succeed())
			})

			wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, *deployment.Spec.Selector, 2)
			pods := listDeploymentPods(ctx, testCtx.ControllerClient, deployment)
			Expect(pods).To(HaveLen(2))
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, []*v1.Pod{&pods[0], &pods[1]})

			pods = listDeploymentPods(ctx, testCtx.ControllerClient, deployment)
			Expect(pods).To(HaveLen(2))
			Expect(pods[0].Spec.NodeName).NotTo(Equal(pods[1].Spec.NodeName))
		})
	})
}

func listDeploymentPods(ctx context.Context, client runtimeClient.Client, deployment *appsv1.Deployment) []v1.Pod {
	podList := &v1.PodList{}
	Expect(client.List(ctx, podList,
		runtimeClient.InNamespace(deployment.Namespace),
		runtimeClient.MatchingLabels(deployment.Spec.Selector.MatchLabels),
	)).To(Succeed())
	return podList.Items
}
