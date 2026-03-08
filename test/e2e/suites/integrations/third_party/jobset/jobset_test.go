/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package jobset

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobsetv1alpha2 "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/jobset"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	jobSetCrdName         = "jobsets.jobset.x-k8s.io"
	jobSetCrdVersion      = "v1alpha2"
	jobSetLabelJobSetName = "jobset.sigs.k8s.io/jobset-name"

	startupPolicyOrderInOrder = "InOrder"
)

var _ = Describe("JobSet integration", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, jobSetCrdName, jobSetCrdVersion)
		Expect(jobsetv1alpha2.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	Context("JobSet submission", func() {

		Context("tests with different parallelism and completions values", func() {
			BeforeAll(func(ctx context.Context) {
				err := feature_flags.SetDefaultStalenessGracePeriod(ctx, testCtx, ptr.To("1s"))
				Expect(err).To(Succeed())
			})

			It("should schedule pods with parallelism=2 and completions=3, first pod completes 5 seconds earlier", func(ctx context.Context) {
				testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
				jobSetName := "test-jobset-" + utils.GenerateRandomK8sName(10)

				jobSet := jobset.CreateUnequalCompletionJobSetObject(jobSetName, testNamespace, testCtx.Queues[0].Name, 2, 3)
				Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
				defer func() {
					Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
				}()

				// Wait for pods to be created
				pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 3)
				Expect(err).To(Succeed())

				// Wait for all pods to be scheduled
				wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

				// Wait for first pod to complete successfully (completes 5s earlier)
				firstPod := pods[0]
				wait.ForPodSucceeded(ctx, testCtx.ControllerClient, firstPod)

				// Wait for remaining pods to complete successfully (see that they are not evicted by stale gang eviction)
				for i := 1; i < len(pods); i++ {
					wait.ForPodSucceeded(ctx, testCtx.ControllerClient, pods[i])
				}
			})

			AfterAll(func(ctx context.Context) {
				err := feature_flags.SetDefaultStalenessGracePeriod(ctx, testCtx, nil)
				Expect(err).To(Succeed())
			})
		})

		It("should create separate PodGroups per replicatedJob when startupPolicyOrder is InOrder", func(ctx context.Context) {
			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "inorder-jobset-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.CreateObjectWithStartupPolicy(jobSetName, testNamespace, testCtx.Queues[0].Name, startupPolicyOrderInOrder)
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			// Wait for pods and verify PodGroups are created separately
			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 2)
			Expect(err).To(Succeed())
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			// Verify 2 separate PodGroups are created (one per replicatedJob)
			job1PG := calcJobSetPodGroupName(jobSetName, string(jobSet.UID), ptr.To("job1"), startupPolicyOrderInOrder)
			job2PG := calcJobSetPodGroupName(jobSetName, string(jobSet.UID), ptr.To("job2"), startupPolicyOrderInOrder)

			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, job1PG)
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, job2PG)
		})

		It("should create a single PodGroup for all replicatedJobs when startupPolicyOrder is not InOrder", func(ctx context.Context) {
			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "anyorder-jobset-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.CreateObjectWithStartupPolicy(jobSetName, testNamespace, testCtx.Queues[0].Name, "AnyOrder")
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			// Wait for pods and verify they all use the same PodGroup
			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 2)
			Expect(err).To(Succeed())
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			// Verify only 1 PodGroup is created for all replicatedJobs with AnyOrder
			Eventually(func(g Gomega) {
				podGroups := &v2alpha2.PodGroupList{}
				err := testCtx.ControllerClient.List(ctx, podGroups, runtimeClient.InNamespace(testNamespace))
				g.Expect(err).To(Succeed())

				// Should have 1 PodGroup (shared by all replicatedJobs with AnyOrder)
				g.Expect(len(podGroups.Items)).To(Equal(1), "Expected 1 PodGroup for AnyOrder startup policy")

				// Verify PodGroup name doesn't contain replicatedJob name
				// PodGroup name format: pg-<jobset-name>-<jobset-uid> (no replicatedJob suffix)
				if len(podGroups.Items) > 0 {
					pgName := podGroups.Items[0].Name
					g.Expect(pgName).NotTo(ContainSubstring("-job1"), "PodGroup name should not contain replicatedJob name")
					g.Expect(pgName).NotTo(ContainSubstring("-job2"), "PodGroup name should not contain replicatedJob name")
				}
			}, time.Minute).Should(Succeed())
		})
	})

	Context("JobSet PodGroup creation scenarios", func() {
		It("should create PodGroup with MinMember=8 for single ReplicatedJob with high parallelism", func(ctx context.Context) {
			// Check if cluster has enough GPU resources (8 GPUs needed)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("8"),
					PodCount: 8,
				},
			)

			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "high-parallelism-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.CreateObjectWithHighParallelism(jobSetName, testNamespace, testCtx.Queues[0].Name)
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			// Wait for pods to be created
			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 8)
			Expect(err).To(Succeed())
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			// Verify PodGroup is created with correct MinMember
			pgName := calcJobSetPodGroupName(jobSetName, string(jobSet.UID), nil, string(jobSet.Spec.StartupPolicy.StartupPolicyOrder))
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, pgName)

			podGroup := &v2alpha2.PodGroup{}
			err = testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKey{Namespace: testNamespace, Name: pgName}, podGroup)
			Expect(err).To(Succeed())
			Expect(podGroup.Spec.MinMember).To(Equal(int32(8)))
		})

		It("should create separate PodGroups for multiple ReplicatedJobs with different parallelism", func(ctx context.Context) {
			// Check if cluster has enough GPU resources (2 GPUs needed for worker jobs)
			// Coordinator: 2 pods (0 GPU), Worker: 2 pods (2 GPU), Total: 4 pods, 2 GPU
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("2"),
					PodCount: 4, // Coordinator (2) + Worker (2) = 4 pods
				},
			)

			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "multi-parallelism-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.CreateObjectWithMultipleReplicatedJobs(jobSetName, testNamespace, testCtx.Queues[0].Name)
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			// Wait for pods to be created (coordinator: 2 pods, worker: 2 pods)
			// With AnyOrder, all replicatedJobs are created simultaneously
			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 4) // Total: 2 + 2 = 4 pods
			Expect(err).To(Succeed())
			Expect(len(pods)).To(Equal(4))
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			// Verify 1 PodGroup is created for all replicatedJobs with AnyOrder
			pgName := calcJobSetPodGroupName(jobSetName, string(jobSet.UID), nil, string(jobSet.Spec.StartupPolicy.StartupPolicyOrder))
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, pgName)

			podGroup := &v2alpha2.PodGroup{}
			err = testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKey{Namespace: testNamespace, Name: pgName}, podGroup)
			Expect(err).To(Succeed())
			Expect(podGroup.Spec.MinMember).To(Equal(int32(4)))
		})

		It("should create PodGroup with MinMember=1 for single replica with default parallelism", func(ctx context.Context) {
			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "default-parallelism-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.CreateObjectWithDefaultParallelism(jobSetName, testNamespace, testCtx.Queues[0].Name)
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			// Wait for pods to be created
			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 1)
			Expect(err).To(Succeed())
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			// Verify PodGroup is created with correct MinMember (defaults to 1)
			pgName := calcJobSetPodGroupName(jobSetName, string(jobSet.UID), nil, string(jobSet.Spec.StartupPolicy.StartupPolicyOrder))
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, pgName)

			podGroup := &v2alpha2.PodGroup{}
			err = testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKey{Namespace: testNamespace, Name: pgName}, podGroup)
			Expect(err).To(Succeed())
			Expect(podGroup.Spec.MinMember).To(Equal(int32(1)))
		})
	})
})

func waitForJobSetPods(ctx context.Context, testCtx *testcontext.TestContext, jobSetName, namespace string, expectedCount int) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, metav1.LabelSelector{
		MatchLabels: map[string]string{
			jobSetLabelJobSetName: jobSetName,
		},
	}, expectedCount)

	podList := &v1.PodList{}
	err := testCtx.ControllerClient.List(ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels{
			jobSetLabelJobSetName: jobSetName,
		})
	if err != nil {
		return nil, err
	}
	for i := range podList.Items {
		pods = append(pods, &podList.Items[i])
	}

	return pods, nil
}

func calcJobSetPodGroupName(jobSetName string, jobSetUID string, replicatedJobName *string, startupPolicyOrder string) string {
	if startupPolicyOrder == startupPolicyOrderInOrder {
		return fmt.Sprintf(
			"%s-%s-%s-%s",
			"pg",
			jobSetName,
			string(jobSetUID),
			*replicatedJobName,
		)
	} else {
		return fmt.Sprintf(
			"%s-%s-%s",
			"pg",
			jobSetName,
			string(jobSetUID),
		)
	}
}
