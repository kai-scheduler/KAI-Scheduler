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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobsetv1alpha2 "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/jobset"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
	"github.com/kai-scheduler/api/scheduling/v2alpha2"
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

		It("should create a single PodGroup with root MinSubGroup equals 1 when startupPolicyOrder is InOrder", func(ctx context.Context) {
			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "inorder-jobset-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.CreateSetWith2ReplicatedJobs(jobSetName, testNamespace, testCtx.Queues[0].Name, startupPolicyOrderInOrder,
				"sleep 5 && echo 'Job1 completed'", 1, "sleep 5 && echo 'Job2 completed'", 1)
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 2)
			Expect(err).To(Succeed())
			Expect(pods).To(HaveLen(2))
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			pgName := calcJobSetPodGroupName(jobSetName, string(jobSet.UID))
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, pgName)

			podGroup := &v2alpha2.PodGroup{}
			Expect(testCtx.ControllerClient.Get(ctx,
				runtimeClient.ObjectKey{Namespace: testNamespace, Name: pgName}, podGroup)).To(Succeed())

			Expect(podGroup.Spec.MinSubGroup).To(Equal(ptr.To(int32(1))), "InOrder → root MinSubGroup=1")
			Expect(podGroup.Spec.MinMember).To(BeNil())
			assertSubGroupTree(podGroup, map[string]subGroupExpectation{
				"job1":           {parent: nil, minSubGroup: ptr.To(int32(1))},
				"job1-replica-0": {parent: ptr.To("job1"), minMember: ptr.To(int32(1))},
				"job2":           {parent: nil, minSubGroup: ptr.To(int32(1))},
				"job2-replica-0": {parent: ptr.To("job2"), minMember: ptr.To(int32(1))},
			})

			// Only one PodGroup should exist for the JobSet.
			Eventually(func(g Gomega) {
				podGroups := &v2alpha2.PodGroupList{}
				g.Expect(testCtx.ControllerClient.List(ctx, podGroups, runtimeClient.InNamespace(testNamespace))).To(Succeed())
				g.Expect(podGroups.Items).To(HaveLen(1), "expected exactly one PodGroup per JobSet")
			}, time.Minute).Should(Succeed())
		})

		It("should create a single PodGroup with root MinSubGroup equals len(replicatedJobs) when startupPolicyOrder is AnyOrder", func(ctx context.Context) {
			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "anyorder-jobset-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.CreateSetWith2ReplicatedJobs(jobSetName, testNamespace, testCtx.Queues[0].Name, "AnyOrder",
				"sleep 5 && echo 'Job1 completed'", 1, "sleep 5 && echo 'Job2 completed'", 1)
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 2)
			Expect(err).To(Succeed())
			Expect(pods).To(HaveLen(2))
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			pgName := calcJobSetPodGroupName(jobSetName, string(jobSet.UID))
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, pgName)

			podGroup := &v2alpha2.PodGroup{}
			Expect(testCtx.ControllerClient.Get(ctx,
				runtimeClient.ObjectKey{Namespace: testNamespace, Name: pgName}, podGroup)).To(Succeed())

			Expect(podGroup.Spec.MinSubGroup).To(Equal(ptr.To(int32(2))), "AnyOrder → root MinSubGroup = len(replicatedJobs)")
			Expect(podGroup.Spec.MinMember).To(BeNil())
			assertSubGroupTree(podGroup, map[string]subGroupExpectation{
				"job1":           {parent: nil, minSubGroup: ptr.To(int32(1))},
				"job1-replica-0": {parent: ptr.To("job1"), minMember: ptr.To(int32(1))},
				"job2":           {parent: nil, minSubGroup: ptr.To(int32(1))},
				"job2-replica-0": {parent: ptr.To("job2"), minMember: ptr.To(int32(1))},
			})

			Eventually(func(g Gomega) {
				podGroups := &v2alpha2.PodGroupList{}
				g.Expect(testCtx.ControllerClient.List(ctx, podGroups, runtimeClient.InNamespace(testNamespace))).To(Succeed())
				g.Expect(podGroups.Items).To(HaveLen(1), "expected exactly one PodGroup per JobSet")
			}, time.Minute).Should(Succeed())
		})

		It("should create a leaf SubGroup with MinMember equals parallelism for a single ReplicatedJob with high parallelism", func(ctx context.Context) {
			// Check if cluster has enough GPU resources (8 GPUs needed)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("8"),
					PodCount: 8,
				},
			)

			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			jobSetName := "high-parallelism-" + utils.GenerateRandomK8sName(10)

			jobSet := jobset.NewJobSet(jobSetName, testNamespace, testCtx.Queues[0].Name,
				jobsetv1alpha2.JobSetSpec{
					SuccessPolicy: &jobsetv1alpha2.SuccessPolicy{
						Operator: jobsetv1alpha2.OperatorAll,
					},
					FailurePolicy: &jobsetv1alpha2.FailurePolicy{
						MaxRestarts: 3,
					},
					ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
						{
							Name:     "worker",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism:  pointer.Int32(8),
									Completions:  pointer.Int32(8),
									BackoffLimit: pointer.Int32(0),
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyNever,
											SchedulerName: testconfig.GetConfig().SchedulerName,
											Containers: []corev1.Container{
												{
													Name:  "worker",
													Image: testconfig.GetConfig().ContainerImage,
													Command: []string{
														"sleep",
														"infinity",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				})
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 8)
			Expect(err).To(Succeed())
			Expect(pods).To(HaveLen(8))
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			pgName := calcJobSetPodGroupName(jobSetName, string(jobSet.UID))
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, pgName)

			podGroup := &v2alpha2.PodGroup{}
			Expect(testCtx.ControllerClient.Get(ctx,
				runtimeClient.ObjectKey{Namespace: testNamespace, Name: pgName}, podGroup)).To(Succeed())

			// No startupPolicy on this JobSet → JobSet default is InOrder → root MinSubGroup=1.
			Expect(podGroup.Spec.MinSubGroup).To(Equal(ptr.To(int32(1))))
			Expect(podGroup.Spec.MinMember).To(BeNil())
			assertSubGroupTree(podGroup, map[string]subGroupExpectation{
				"worker":           {parent: nil, minSubGroup: ptr.To(int32(1))},
				"worker-replica-0": {parent: ptr.To("worker"), minMember: ptr.To(int32(8))},
			})
		})

		It("should create per-replicatedJob parent SubGroups with leaf MinMember equals parallelism for multiple ReplicatedJobs", func(ctx context.Context) {
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

			jobSet := jobset.CreateSetWith2ReplicatedJobs(jobSetName, testNamespace, testCtx.Queues[0].Name, "AnyOrder",
				"sleep infinity", 2, "sleep infinity", 2)
			Expect(testCtx.ControllerClient.Create(ctx, jobSet)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jobSet)).To(Succeed())
			}()

			pods, err := waitForJobSetPods(ctx, testCtx, jobSetName, testNamespace, 4) // 2 coordinator + 2 worker
			Expect(err).To(Succeed())
			Expect(pods).To(HaveLen(4))
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, testNamespace, pods)

			pgName := calcJobSetPodGroupName(jobSetName, string(jobSet.UID))
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, testNamespace, pgName)

			podGroup := &v2alpha2.PodGroup{}
			Expect(testCtx.ControllerClient.Get(ctx,
				runtimeClient.ObjectKey{Namespace: testNamespace, Name: pgName}, podGroup)).To(Succeed())

			// AnyOrder, 2 replicatedJobs → root MinSubGroup=2.
			Expect(podGroup.Spec.MinSubGroup).To(Equal(ptr.To(int32(2))))
			Expect(podGroup.Spec.MinMember).To(BeNil())
			assertSubGroupTree(podGroup, map[string]subGroupExpectation{
				"job1":           {parent: nil, minSubGroup: ptr.To(int32(1))},
				"job1-replica-0": {parent: ptr.To("job1"), minMember: ptr.To(int32(2))},
				"job2":           {parent: nil, minSubGroup: ptr.To(int32(1))},
				"job2-replica-0": {parent: ptr.To("job2"), minMember: ptr.To(int32(2))},
			})
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

func calcJobSetPodGroupName(jobSetName string, jobSetUID string) string {
	return fmt.Sprintf("pg-%s-%s", jobSetName, jobSetUID)
}

// subGroupExpectation describes the expected shape of a single SubGroup. Either
// minSubGroup or minMember should be set, not both.
type subGroupExpectation struct {
	parent      *string
	minSubGroup *int32
	minMember   *int32
}

// assertSubGroupTree verifies that the PodGroup's SubGroups exactly match the
// expected map (keyed by SubGroup name).
func assertSubGroupTree(pg *v2alpha2.PodGroup, expected map[string]subGroupExpectation) {
	GinkgoHelper()
	Expect(pg.Spec.SubGroups).To(HaveLen(len(expected)), "unexpected number of SubGroups")

	byName := map[string]v2alpha2.SubGroup{}
	for _, sg := range pg.Spec.SubGroups {
		byName[sg.Name] = sg
	}
	for name, want := range expected {
		got, ok := byName[name]
		Expect(ok).To(BeTrue(), "SubGroup %q missing", name)
		Expect(got.Parent).To(Equal(want.parent), "SubGroup %q parent mismatch", name)
		Expect(got.MinSubGroup).To(Equal(want.minSubGroup), "SubGroup %q MinSubGroup mismatch", name)
		Expect(got.MinMember).To(Equal(want.minMember), "SubGroup %q MinMember mismatch", name)
	}
}
