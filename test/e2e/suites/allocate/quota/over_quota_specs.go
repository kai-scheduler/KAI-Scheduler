/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package quota

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeOverQuotaSpecs() bool {
	return Describe("Over quota", func() {
		It("Not schedule non preemptible pod that exceeds GPU quota", func(ctx context.Context) {
			testCtx := testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("1"),
					PodCount: 1,
				},
			)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			queueName := utils.GenerateRandomK8sName(10)
			childQueue := queue.CreateQueueObjectWithGpuResource(queueName,
				v2.QueueResource{
					Quota:           0,
					OverQuotaWeight: 1,
					Limit:           -1,
				}, parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
			defer testCtx.ClusterCleanup(ctx)

			nonPreemptiblePriorityClass := utils.GenerateRandomK8sName(10)
			_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().Create(ctx,
				rd.CreatePriorityClass(nonPreemptiblePriorityClass, utils.RandomIntBetween(100, 256)),
				metav1.CreateOptions{},
			)
			Expect(err).To(Succeed())
			defer func() {
				err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().Delete(ctx, nonPreemptiblePriorityClass,
					metav1.DeleteOptions{})
				Expect(err).To(Succeed())
			}()

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})
			pod.Spec.PriorityClassName = nonPreemptiblePriorityClass

			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			if err != nil {
				Expect(err).NotTo(HaveOccurred(), "Failed to create pod-job")
			}
			defer func() {
				err = testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
				Expect(err).To(Succeed())
			}()

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
		})
	})
}
