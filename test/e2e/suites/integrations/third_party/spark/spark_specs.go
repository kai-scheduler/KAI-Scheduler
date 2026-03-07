/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package spark

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/exp/maps"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	sparkAppLabelName         = "spark-app-name"
	sparkAppSelectorLabelName = "spark-app-selector"
	sparkRoleLabelName        = "spark-role"
	executorCountLableName    = "executor-count"

	podGroupNameAnnotation = "pod-group-name"
)

func DescribeSparkSpecs() bool {
	return Describe("Spark integration", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
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

		Describe("spark-submit pods", Ordered, func() {
			Context("With enough resources to run all pods", func() {
				It("should run all pods of a single spark job in a single PodGroup", func(ctx context.Context) {
					pods := startSparkWorkload(ctx, testCtx, 4)
					Expect(len(pods)).To(Equal(5))
					namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
					wait.ForPodsReady(ctx, testCtx.ControllerClient, namespace, pods)

					podGroupNames := map[string]bool{}
					for index, pod := range pods {
						Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), pods[index])).NotTo(HaveOccurred())
						podGroupNames[pods[index].Annotations[podGroupNameAnnotation]] = true
					}

					Expect(len(podGroupNames)).To(Equal(1))
				})

				It("will add new spark pods to the same PodGroup as the old ones", func(ctx context.Context) {
					pods := startSparkWorkload(ctx, testCtx, 2)
					Expect(len(pods)).To(Equal(3))
					var lastExecutorPod *v1.Pod
					namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
					wait.ForPodsReady(ctx, testCtx.ControllerClient, namespace, pods)

					for _, pod := range pods {
						if pod.Labels[sparkRoleLabelName] == "executor" {
							lastExecutorPod = pod
						}
					}
					Expect(lastExecutorPod).NotTo(BeNil())

					Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(lastExecutorPod), lastExecutorPod)).NotTo(HaveOccurred())
					Expect(testCtx.ControllerClient.Delete(ctx, lastExecutorPod)).NotTo(HaveOccurred())

					newExecPods := ensureExecutorPodsCount(ctx, testCtx, lastExecutorPod.Labels[sparkAppSelectorLabelName])
					Expect(len(newExecPods)).To(Equal(1))

					wait.ForPodReady(ctx, testCtx.ControllerClient, newExecPods[0])
					err := testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(newExecPods[0]), newExecPods[0])
					Expect(err).NotTo(HaveOccurred())
					Expect(newExecPods[0].Annotations[podGroupNameAnnotation]).To(Equal(lastExecutorPod.Annotations[podGroupNameAnnotation]))
				})
			})
		})
	})
}

func startSparkWorkload(ctx context.Context, testCtx *testcontext.TestContext, executorsCount int) []*v1.Pod {
	var err error
	sparkAppSelector := fmt.Sprintf("spark-%s", utils.GenerateRandomK8sName(10))

	driverPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
	maps.Copy(driverPod.Labels, map[string]string{
		sparkAppLabelName:         "spark-app",
		sparkAppSelectorLabelName: sparkAppSelector,
		sparkRoleLabelName:        "driver",
		executorCountLableName:    fmt.Sprintf("%d", executorsCount),
	})

	driverPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, driverPod)
	if err != nil {
		Expect(err).NotTo(HaveOccurred(), "Failed to create pod-job")
	}

	wait.ForPodReady(ctx, testCtx.ControllerClient, driverPod)

	return append([]*v1.Pod{driverPod}, ensureExecutorPodsCount(ctx, testCtx, sparkAppSelector)...)
}

func ensureExecutorPodsCount(ctx context.Context, testCtx *testcontext.TestContext, sparkAppSelector string) []*v1.Pod {
	pods := &v1.PodList{}
	err := testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.MatchingLabels{sparkAppSelectorLabelName: sparkAppSelector})
	Expect(err).NotTo(HaveOccurred())

	var driverPod *v1.Pod
	var lastExecutorIndex int
	var executorsID string
	var executorsCount int = 0
	createdPods := []*v1.Pod{}

	for index, pod := range pods.Items {
		switch pod.Labels[sparkRoleLabelName] {
		case "executor":
			executorsCount++
			nameParts := strings.Split(pod.Name, "-")
			executorsID = nameParts[1]
			index, err := strconv.Atoi(nameParts[len(nameParts)-1])
			Expect(err).NotTo(HaveOccurred())
			if index > lastExecutorIndex {
				lastExecutorIndex = index
			}
		case "driver":
			driverPod = &pods.Items[index]
		}
	}

	if driverPod == nil {
		return createdPods
	}
	if executorsID == "" {
		executorsID = utils.GenerateRandomK8sName(10)
	}

	desiredExecutorsCount, err := strconv.Atoi(driverPod.Labels[executorCountLableName])
	Expect(err).NotTo(HaveOccurred())

	for i := executorsCount; i < desiredExecutorsCount; i++ {
		lastExecutorIndex++
		createdPods = append(
			createdPods,
			createExecutorPod(ctx, testCtx, driverPod, sparkAppSelector, executorsID, lastExecutorIndex))
	}

	return createdPods
}

func createExecutorPod(
	ctx context.Context, testCtx *testcontext.TestContext,
	driverPod *v1.Pod,
	sparkAppSelector, executorsID string, index int,
) *v1.Pod {
	executorPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
	executorPod.Name = fmt.Sprintf("spark-%s-exec-%d", executorsID, index)
	maps.Copy(executorPod.Labels, map[string]string{
		sparkAppLabelName:         "spark-app",
		sparkAppSelectorLabelName: sparkAppSelector,
		sparkRoleLabelName:        "executor",
	})
	executorPod.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       driverPod.Name,
			UID:        driverPod.UID,
		},
	}
	executorPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, executorPod)
	Expect(err).NotTo(HaveOccurred())
	return executorPod
}
