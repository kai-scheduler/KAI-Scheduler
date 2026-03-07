/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package ray

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

var _ = DescribeRaySpecs()

var _ = Describe("Ray integration", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		SkipIfRayNotInstalled(ctx, testCtx)

		Expect(rayv1.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	Context("RayJob submission", func() {
		AfterEach(func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			Expect(testCtx.ControllerClient.DeleteAllOf(ctx, &rayv1.RayJob{}, client.InNamespace(namespace))).To(Succeed())
			Expect(testCtx.ControllerClient.DeleteAllOf(ctx, &v1.ConfigMap{}, client.InNamespace(namespace))).To(Succeed())

			testCtx.TestContextCleanup(ctx)
		})

		It("should run an elastic RayJob", func(ctx context.Context) {
			rayJob, configMap := CreateExampleRayJob(testCtx.Queues[0], v1.ResourceRequirements{}, v1.ResourceRequirements{}, 1)
			rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas = ptr.To(int32(10))
			rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].MinReplicas = ptr.To(int32(1))
			Expect(testCtx.ControllerClient.Create(ctx, configMap)).To(Succeed())
			Expect(testCtx.ControllerClient.Create(ctx, rayJob)).To(Succeed())

			rayCluster := WaitForRayCluster(ctx, testCtx, rayJob)
			rayPods := WaitForRayPodsCreation(ctx, testCtx, rayCluster, 2)

			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, rayJob.Namespace, rayPods)
		})

		It("should not run an elastic RayJob if not all subgroups are satisfied", func(ctx context.Context) {
			rayJob, configMap := CreateExampleRayJob(testCtx.Queues[0], v1.ResourceRequirements{}, v1.ResourceRequirements{}, 1)

			workerGroupA := rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0]
			workerGroupA.GroupName = "worker-group-a"
			workerGroupA.Replicas = ptr.To(int32(5))
			workerGroupA.MinReplicas = ptr.To(int32(1))
			workerGroupA.MaxReplicas = ptr.To(int32(5))

			workerGroupB := workerGroupA.DeepCopy()
			workerGroupB.GroupName = "worker-group-b"
			workerGroupB.Replicas = ptr.To(int32(5))
			workerGroupB.MinReplicas = ptr.To(int32(1))
			workerGroupB.MaxReplicas = ptr.To(int32(5))
			workerGroupB.Template.Spec.Containers[0].Resources = v1.ResourceRequirements{
				Requests: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("99"),
				},
				Limits: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("99"),
				},
			}

			rayJob.Spec.RayClusterSpec.WorkerGroupSpecs = []rayv1.WorkerGroupSpec{workerGroupA, *workerGroupB}

			Expect(testCtx.ControllerClient.Create(ctx, configMap)).To(Succeed())
			Expect(testCtx.ControllerClient.Create(ctx, rayJob)).To(Succeed())

			rayCluster := WaitForRayCluster(ctx, testCtx, rayJob)
			rayPods := WaitForRayPodsCreation(ctx, testCtx, rayCluster, 2)

			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, rayJob.Namespace, rayPods, 2)

			// Get the PodGroup name from one of the ray pods and verify it has 3 subgroups
			var updatedPod v1.Pod
			Expect(testCtx.ControllerClient.Get(ctx, client.ObjectKeyFromObject(rayPods[0]), &updatedPod)).To(Succeed())
			podGroupName := updatedPod.Annotations[constants.PodGroupAnnotationForPod]
			Expect(podGroupName).NotTo(BeEmpty(), "PodGroup annotation not found on pod")

			podGroup, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(rayJob.Namespace).Get(
				ctx, podGroupName, metav1.GetOptions{})
			Expect(err).To(Succeed())
			Expect(len(podGroup.Spec.SubGroups)).To(Equal(3),
				"Expected 3 subgroups (1 head + 2 worker groups)")
		})
	})
})
