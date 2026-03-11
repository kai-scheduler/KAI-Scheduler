/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package subgroups

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconsts "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSubGroups(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "SubGroups Allocation Suite")
}

var _ = Describe("Allocation scenario with subgroups", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		childQueue.Spec.Resources.CPU.Quota = 600
		childQueue.Spec.Resources.CPU.Limit = 600
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
			{
				Cpu:      resource.MustParse("600m"),
				PodCount: 6,
			},
		})
	})

	AfterAll(func(ctx context.Context) {
		err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
		Expect(err).To(Succeed())
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	It("Partial allocation", func(ctx context.Context) {
		pgName := utils.GenerateRandomK8sName(10)
		subGroup1Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgName, "sub-1", 5)
		subGroup2Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgName, "sub-2", 5)

		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		podGroup := pod_group.Create(namespace, pgName, testCtx.Queues[0].Name)
		podGroup.Spec.MinMember = 6
		podGroup.Spec.SubGroups = []schedulingv2alpha2.SubGroup{
			{Name: "sub-1", MinMember: 3},
			{Name: "sub-2", MinMember: 3},
		}
		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			podGroup, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, podGroup.Namespace, subGroup1Pods, 3)
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, podGroup.Namespace, subGroup1Pods, 2)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, podGroup.Namespace, subGroup2Pods, 3)
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, podGroup.Namespace, subGroup2Pods, 2)
	})

	It("Balance 2 jobs with subgroups", func(ctx context.Context) {
		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

		pg1Name := utils.GenerateRandomK8sName(10)
		pg1SubGroup1Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg1Name, "sub-1", 3)
		pg1SubGroup2Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg1Name, "sub-2", 3)
		pg1 := pod_group.Create(namespace, pg1Name, testCtx.Queues[0].Name)
		pg1.Spec.MinMember = 2
		pg1.Spec.SubGroups = []schedulingv2alpha2.SubGroup{
			{Name: "sub-1", MinMember: 1},
			{Name: "sub-2", MinMember: 1},
		}

		pg2Name := utils.GenerateRandomK8sName(10)
		pg2SubGroup1Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg2Name, "sub-1", 3)
		pg2SubGroup2Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg2Name, "sub-2", 3)
		pg2 := pod_group.Create(namespace, pg2Name, testCtx.Queues[0].Name)
		pg2.Spec.MinMember = 2
		pg2.Spec.SubGroups = []schedulingv2alpha2.SubGroup{
			{Name: "sub-1", MinMember: 1},
			{Name: "sub-2", MinMember: 1},
		}

		// impose quota limit on queue to ensure that PodGroups will be scheduled before testing
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			queue, err := testCtx.KubeAiSchedClientset.SchedulingV2().Queues("").Get(ctx, testCtx.Queues[0].Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			queue.Spec.Resources.GPU.Quota = 0
			queue.Spec.Resources.GPU.Limit = 0
			_, err = testCtx.KubeAiSchedClientset.SchedulingV2().Queues("").Update(ctx, queue, metav1.UpdateOptions{})
			return err
		})
		Expect(err).To(Succeed())

		_, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			pg1, metav1.CreateOptions{})
		Expect(err).To(Succeed())
		_, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			pg2, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		// remove quota limit
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			queue, getErr := testCtx.KubeAiSchedClientset.SchedulingV2().Queues("").Get(ctx, testCtx.Queues[0].Name, metav1.GetOptions{})
			if getErr != nil {
				return getErr
			}
			queue.Spec.Resources.GPU.Quota = -1
			queue.Spec.Resources.GPU.Limit = -1
			_, updateErr := testCtx.KubeAiSchedClientset.SchedulingV2().Queues("").Update(ctx, queue, metav1.UpdateOptions{})
			return updateErr
		})
		Expect(err).To(Succeed())

		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg1SubGroup1Pods, 1)
		pg1Pods := append(pg1SubGroup1Pods, pg1SubGroup2Pods...)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg1SubGroup1Pods, 1)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg1SubGroup2Pods, 1)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg1Pods, 3)
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, pg1Pods, 3)

		pg2Pods := append(pg2SubGroup1Pods, pg2SubGroup2Pods...)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg2SubGroup1Pods, 1)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg2SubGroup2Pods, 1)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg2Pods, 3)
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, pg2Pods, 3)
	})

	It("Don't schedule job if subgroup gang is not satisfied", func(ctx context.Context) {
		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

		pg1Name := utils.GenerateRandomK8sName(10)
		pg1SubGroup1Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg1Name, "sub-1", 3)
		pg1SubGroup2Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg1Name, "sub-2", 3)
		pg1 := pod_group.Create(namespace, pg1Name, testCtx.Queues[0].Name)
		pg1.Spec.MinMember = 4
		pg1.Spec.SubGroups = []schedulingv2alpha2.SubGroup{
			{Name: "sub-1", MinMember: 2},
			{Name: "sub-2", MinMember: 2},
		}
		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			pg1, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		// wait until pg1 is scheduled to ensure that it will be the one running at the end of the test
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg1SubGroup1Pods, 1)

		pg2Name := utils.GenerateRandomK8sName(10)
		pg2SubGroup1Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg2Name, "sub-1", 3)
		pg2SubGroup2Pods := createSubGroupPods(ctx, testCtx.KubeClientset, testCtx.Queues[0], pg2Name, "sub-2", 3)
		pg2 := pod_group.Create(namespace, pg2Name, testCtx.Queues[0].Name)
		pg2.Spec.MinMember = 4
		pg2.Spec.SubGroups = []schedulingv2alpha2.SubGroup{
			{Name: "sub-1", MinMember: 2},
			{Name: "sub-2", MinMember: 2},
		}

		_, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			pg2, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		pg1Pods := append(pg1SubGroup1Pods, pg1SubGroup2Pods...)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pg1Pods, 6)

		pg2Pods := append(pg2SubGroup1Pods, pg2SubGroup2Pods...)
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, pg2Pods, 6)
	})
})

func createSubGroupPods(ctx context.Context, client *kubernetes.Clientset, queue *v2.Queue,
	podGroupName string, subGroupName string, numPods int) []*v1.Pod {
	var pods []*v1.Pod
	for i := 0; i < numPods; i++ {
		pod := createPod(ctx, client, queue, podGroupName, subGroupName, "100m")
		pods = append(pods, pod)
	}
	return pods
}

func createPod(ctx context.Context, client *kubernetes.Clientset, queue *v2.Queue, podGroupName string,
	subGroupName string, cpuPerPod string) *v1.Pod {
	pod := rd.CreatePodWithPodGroupReference(queue, podGroupName, v1.ResourceRequirements{
		Limits: map[v1.ResourceName]resource.Quantity{
			v1.ResourceCPU: resource.MustParse(cpuPerPod),
		},
	})
	pod.Labels[commonconsts.SubGroupLabelKey] = subGroupName
	pod, err := rd.CreatePod(ctx, client, pod)
	Expect(err).To(Succeed())
	return pod
}
