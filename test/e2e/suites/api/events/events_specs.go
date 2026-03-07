/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package events

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
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

			podGroupName := utils.GenerateRandomK8sName(10)
			podGroup := pod_group.Create(namespace, podGroupName, testQueue.Name)
			podGroup.Spec.MinMember = 2
			podGroup, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx, podGroup,
				metav1.CreateOptions{})
			Expect(err).To(Succeed())

			pod := rd.CreatePodWithPodGroupReference(testQueue, podGroupName,
				v1.ResourceRequirements{})
			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).To(Succeed())

			wait.ForPodGroupNotReadyEvent(ctx, testCtx.ControllerClient, namespace, podGroupName)
		})
	})
}
