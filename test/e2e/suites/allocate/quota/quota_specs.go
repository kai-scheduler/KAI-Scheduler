/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package quota

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

func DescribeQuotaSpecs() bool {
	return Describe("Deserved quota tests", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
				v2.QueueResource{
					Quota:           3,
					OverQuotaWeight: 1,
					Limit:           3,
				}, "")
			queues := []*v2.Queue{
				queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
					v2.QueueResource{
						Quota:           1,
						OverQuotaWeight: 1,
						Limit:           3,
					}, parentQueue.Name),
				queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
					v2.QueueResource{
						Quota:           1,
						OverQuotaWeight: 1,
						Limit:           3,
					}, parentQueue.Name),
				queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
					v2.QueueResource{
						Quota:           1,
						OverQuotaWeight: 1,
						Limit:           3,
					}, parentQueue.Name),
			}
			queue.ConnectQueuesWithSharedParent(parentQueue, queues...)
			testCtx.InitQueues(append([]*v2.Queue{parentQueue}, queues...))

			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("6"),
					PodCount: 2,
				},
			)
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("Equal deserving queues receive equal share.", func(ctx context.Context) {
			numOfSharingQueues := len(testCtx.Queues) - 1
			testPodsIdentifiyingLabel := utils.GenerateRandomK8sName(10)

			firstPods := quotaCreatePodForEachQueue(ctx, testCtx, numOfSharingQueues, testPodsIdentifiyingLabel)
			for _, firstPod := range firstPods {
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, firstPod)
			}
			_ = quotaCreatePodForEachQueue(ctx, testCtx, numOfSharingQueues, testPodsIdentifiyingLabel)

			perQueueScheduled := map[string]int{}
			for queueIndex := 1; queueIndex <= numOfSharingQueues; queueIndex++ {
				perQueueScheduled[testCtx.Queues[queueIndex].Name] = 0

				namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[queueIndex])
				pods, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("singletest=%s", testPodsIdentifiyingLabel),
				})
				Expect(err).To(Succeed())

				for _, pod := range pods.Items {
					if rd.IsPodScheduled(&pod) {
						perQueueScheduled[testCtx.Queues[queueIndex].Name] += 1
					} else {
						wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, &pod)
					}
				}
			}

			for queueName, queueScheduledCounter := range perQueueScheduled {
				Expect(1).To(Equal(queueScheduledCounter),
					fmt.Sprintf("Expected to see 1 pod scheduled for queue %s, but got %v", queueName, queueScheduledCounter))
			}
		})
	})
}

func quotaCreatePodForEachQueue(ctx context.Context, testCtx *testcontext.TestContext, numOfSharingQueues int,
	testPodsIdentifiyingLabel string) []*v1.Pod {
	var allPods []*v1.Pod
	for queueIndex := 1; queueIndex <= numOfSharingQueues; queueIndex++ {
		pod := rd.CreatePodObject(testCtx.Queues[queueIndex], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		})
		pod.Labels["singletest"] = testPodsIdentifiyingLabel
		pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).To(Succeed())
		allPods = append(allPods, pod)
	}
	return allPods
}
