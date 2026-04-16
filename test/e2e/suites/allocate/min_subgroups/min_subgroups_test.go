/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package min_subgroups

import (
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var cpuPerPod = v1.ResourceRequirements{
	Limits: map[v1.ResourceName]resource.Quantity{
		v1.ResourceCPU: resource.MustParse("100m"),
	},
}

func TestMinSubGroups(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "MinSubGroups Suite")
}

var _ = Describe("Single-level hierarchy with minSubGroup", Ordered, func() {
	var testCtx *testcontext.TestContext

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		childQueue.Spec.Resources.CPU.Quota = 800
		childQueue.Spec.Resources.CPU.Limit = 800
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
			{
				Cpu:      resource.MustParse("800m"),
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
		testCtx.TestContextCleanup(ctx)
	})

	It("should schedule when minSubGroup threshold is met", func(ctx context.Context) {
		_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10), testCtx.Queues[0], ptr.To[int32](3),
			flatLeaves("prefill", 4, 2), nil, "", cpuPerPod)

		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		for _, name := range []string{"prefill-0", "prefill-1", "prefill-2", "prefill-3"} {
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.Pods[name], 2)
		}
	})

	It("should schedule with only minSubGroup subgroups when resources are constrained", func(ctx context.Context) {
		fillerPod := createFillerPod(ctx, testCtx.KubeClientset, testCtx.Queues[0], "200m")
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, fillerPod)

		_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10), testCtx.Queues[0], ptr.To[int32](3),
			flatLeaves("prefill", 4, 2), nil, "", cpuPerPod)

		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.AllPods, 6)
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, h.AllPods, 2)
	})

	It("should not schedule when minSubGroup cannot be satisfied", func(ctx context.Context) {
		fillerPod := createFillerPod(ctx, testCtx.KubeClientset, testCtx.Queues[0], "500m")
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, fillerPod)

		_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10), testCtx.Queues[0], ptr.To[int32](4),
			flatLeaves("prefill", 4, 2), nil, "", cpuPerPod)

		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, h.AllPods, 8)
	})
})

var _ = Describe("Multi-level hierarchy with minSubGroup", Ordered, func() {
	var testCtx *testcontext.TestContext

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		childQueue.Spec.Resources.CPU.Quota = 1000
		childQueue.Spec.Resources.CPU.Limit = 1000
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
			{
				Cpu:      resource.MustParse("1000m"),
				PodCount: 10,
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

	It("should schedule 2-level hierarchy when all minSubGroup thresholds are met", func(ctx context.Context) {
		_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10), testCtx.Queues[0], ptr.To[int32](2),
			leaderWorkerGroups([]string{"decode", "prefill"}, ptr.To[int32](2), 1, 4),
			nil, "", cpuPerPod)

		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.Pods["decode-leaders"], 1)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.Pods["decode-workers"], 4)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.Pods["prefill-leaders"], 1)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.Pods["prefill-workers"], 4)
	})

	It("should not schedule 2-level hierarchy when child minSubGroup cannot be met", func(ctx context.Context) {
		fillerPod := createFillerPod(ctx, testCtx.KubeClientset, testCtx.Queues[0], "700m")
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, fillerPod)

		_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10), testCtx.Queues[0], ptr.To[int32](2),
			leaderWorkerGroups([]string{"decode", "prefill"}, ptr.To[int32](2), 1, 4),
			nil, "", cpuPerPod)

		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, h.AllPods, 10)
	})

	It("should schedule 2-level hierarchy with elastic mid-level subgroups", func(ctx context.Context) {
		fillerPod := createFillerPod(ctx, testCtx.KubeClientset, testCtx.Queues[0], "400m")
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, fillerPod)

		_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10), testCtx.Queues[0], ptr.To[int32](2),
			leaderWorkerGroups([]string{"group1", "group2", "group3"}, ptr.To[int32](2), 1, 2),
			nil, "", cpuPerPod)

		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.AllPods, 6)
		wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, h.AllPods, 3)
	})
})

var _ = Describe("MinSubGroup backward compatibility", Ordered, func() {
	var testCtx *testcontext.TestContext

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

	It("should require all SubGroups when minSubGroup is nil (existing behavior)", func(ctx context.Context) {
		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		pgName := utils.GenerateRandomK8sName(10)

		h := pod_group.BuildHierarchy(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgName,
			[]pod_group.SubGroupNode{
				{Name: "sub-1", MinMember: ptr.To(int32(2)), PodCount: 3},
				{Name: "sub-2", MinMember: ptr.To(int32(2)), PodCount: 3},
			}, cpuPerPod)

		pg := pod_group.Create(namespace, pgName, testCtx.Queues[0].Name)
		pg.Spec.MinMember = ptr.To[int32](4)
		pg.Spec.SubGroups = h.SubGroups
		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.Pods["sub-1"], 2)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.Pods["sub-2"], 2)
	})

	It("should not schedule when any SubGroup fails and minSubGroup is nil", func(ctx context.Context) {
		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		pgName := utils.GenerateRandomK8sName(10)

		h := pod_group.BuildHierarchy(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgName,
			[]pod_group.SubGroupNode{
				{Name: "sub-1", MinMember: ptr.To(int32(2)), PodCount: 3},
				{Name: "sub-2", MinMember: ptr.To(int32(5)), PodCount: 3}, // Only 3 pods, can't reach 5
			}, cpuPerPod)

		pg := pod_group.Create(namespace, pgName, testCtx.Queues[0].Name)
		pg.Spec.MinMember = ptr.To[int32](7)
		pg.Spec.SubGroups = h.SubGroups
		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		wait.ForPodGroupNotReadyEvent(ctx, testCtx.ControllerClient, namespace, pgName)
	})

	It("should handle existing PodGroups with only minMember (no SubGroups)", func(ctx context.Context) {
		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		pgName := utils.GenerateRandomK8sName(10)

		pg := pod_group.Create(namespace, pgName, testCtx.Queues[0].Name)
		pg.Spec.MinMember = ptr.To[int32](2)
		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		var pods []*v1.Pod
		for i := 0; i < 3; i++ {
			pod := rd.CreatePodWithPodGroupReference(testCtx.Queues[0], pgName, cpuPerPod)
			pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).To(Succeed())
			pods = append(pods, pod)
		}

		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 2)
	})
})

// flatLeaves generates N leaf SubGroupNodes named "{prefix}-0", "{prefix}-1", etc.
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

// leaderWorkerGroups generates a 2-level hierarchy: each group has leaders and workers leaves.
func leaderWorkerGroups(groupNames []string, minSubGroup *int32, leaderCount, workerCount int) []pod_group.SubGroupNode {
	nodes := make([]pod_group.SubGroupNode, 0, len(groupNames))
	for _, name := range groupNames {
		nodes = append(nodes, pod_group.SubGroupNode{
			Name:        name,
			MinSubGroup: minSubGroup,
			Children: []pod_group.SubGroupNode{
				{Name: fmt.Sprintf("%s-leaders", name), MinMember: ptr.To(int32(leaderCount)), PodCount: leaderCount},
				{Name: fmt.Sprintf("%s-workers", name), MinMember: ptr.To(int32(workerCount)), PodCount: workerCount},
			},
		})
	}
	return nodes
}

func createFillerPod(ctx context.Context, client *kubernetes.Clientset, q *v2.Queue, cpu string) *v1.Pod {
	pod := rd.CreatePodObject(q, v1.ResourceRequirements{
		Limits: map[v1.ResourceName]resource.Quantity{
			v1.ResourceCPU: resource.MustParse(cpu),
		},
	})
	pod, err := rd.CreatePod(ctx, client, pod)
	Expect(err).To(Succeed())
	return pod
}
