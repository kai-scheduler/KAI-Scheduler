/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package pytorch

import (
	"context"
	"fmt"
	"time"

	trainingoperatorv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	trainingOperator "github.com/kubeflow/training-operator/pkg/client/clientset/versioned"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pytorch"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	pytorchCrdName    = "pytorchjobs.kubeflow.org"
	pytorchCrdVersion = "v1"
	podRoleLabelKey   = "training.kubeflow.org/job-role"
)

func DescribePytorchSpecs() bool {
	return Describe("Kubeflow.org Pytorch Integration", Ordered, func() {
		var (
			testCtx             *testcontext.TestContext
			namespace           string
			trainingOperatorSdk *trainingOperator.Clientset
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
			namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, pytorchCrdName, pytorchCrdVersion)
			trainingOperatorSdk = trainingOperator.NewForConfigOrDie(testCtx.KubeConfig)
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("PytorchJob submit", func(ctx context.Context) {
			obj := pytorch.CreateObject(namespace, testCtx.Queues[0].Name)
			_, err := trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Create(ctx, obj, v1.CreateOptions{})
			Expect(err).To(Succeed())
			defer func() {
				err = trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Delete(ctx, obj.Name, v1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			}()

			labelSelector := v1.LabelSelector{MatchLabels: map[string]string{constants.AppLabelName: obj.Labels[constants.AppLabelName]}}
			wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, labelSelector, 2)

			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx,
				v1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", constants.AppLabelName, obj.Labels[constants.AppLabelName])},
			)
			Expect(err).To(Succeed())

			pods := make([]*v12.Pod, len(podList.Items))
			for i, pod := range podList.Items {
				pods[i] = &pod
			}
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)
		})

		It("Elastic pytorch - do not schedule less then elastic min replicas", func(ctx context.Context) {
			pytorchElasticInvalid := pytorch.CreateWorkerOnlyObject(namespace, testCtx.Queues[0].Name, 1)

			backendC10D := trainingoperatorv1.BackendC10D
			pytorchElasticInvalid.Spec.ElasticPolicy = &trainingoperatorv1.ElasticPolicy{
				RDZVBackend: &backendC10D,
				MinReplicas: ptr.To(int32(2)),
			}

			_, err := trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Create(
				ctx, pytorchElasticInvalid, v1.CreateOptions{})
			Expect(err).To(Succeed())
			defer func() {
				err = trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Delete(ctx, pytorchElasticInvalid.Name,
					v1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			}()

			labelSelector := v1.LabelSelector{
				MatchLabels: map[string]string{
					constants.AppLabelName: pytorchElasticInvalid.Labels[constants.AppLabelName]},
			}
			wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, labelSelector, 1)

			var podGroupName string
			Eventually(func(g Gomega) {
				podGroups, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).List(
					ctx, v1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", constants.AppLabelName,
						pytorchElasticInvalid.Labels[constants.AppLabelName])},
				)
				g.Expect(err).To(Succeed())
				g.Expect(len(podGroups.Items)).To(Equal(1),
					"Couldn't find the matching podgroup for the pytorchJob")
				podGroupName = podGroups.Items[0].Name
			}).WithPolling(time.Second).WithTimeout(time.Minute)

			wait.ForPodGroupNotReadyEvent(ctx, testCtx.ControllerClient, namespace, podGroupName)
		})

		It("Elastic pytorch - do not schedule less then elastic run policy min available", func(ctx context.Context) {
			pytorchElasticInvalid := pytorch.CreateWorkerOnlyObject(namespace, testCtx.Queues[0].Name, 1)

			backendC10D := trainingoperatorv1.BackendC10D
			pytorchElasticInvalid.Spec.ElasticPolicy = &trainingoperatorv1.ElasticPolicy{
				RDZVBackend: &backendC10D,
				MinReplicas: ptr.To(int32(2)),
			}
			pytorchElasticInvalid.Spec.RunPolicy = trainingoperatorv1.RunPolicy{
				SchedulingPolicy: &trainingoperatorv1.SchedulingPolicy{
					MinAvailable: ptr.To(int32(2)),
				},
			}

			_, err := trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Create(
				ctx, pytorchElasticInvalid, v1.CreateOptions{})
			Expect(err).To(Succeed())
			defer func() {
				err = trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Delete(ctx, pytorchElasticInvalid.Name,
					v1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			}()

			labelSelector := v1.LabelSelector{
				MatchLabels: map[string]string{
					constants.AppLabelName: pytorchElasticInvalid.Labels[constants.AppLabelName]},
			}
			wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, labelSelector, 1)

			var podGroupName string
			Eventually(func(g Gomega) {
				podGroups, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).List(
					ctx, v1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", constants.AppLabelName,
						pytorchElasticInvalid.Labels[constants.AppLabelName])},
				)
				g.Expect(err).To(Succeed())
				g.Expect(len(podGroups.Items)).To(Equal(1),
					"Couldn't find the matching podgroup for the pytorchJob")
				podGroupName = podGroups.Items[0].Name
			}).WithPolling(time.Second).WithTimeout(time.Minute)

			wait.ForPodGroupNotReadyEvent(ctx, testCtx.ControllerClient, namespace, podGroupName)
		})

		It("Elastic pytorch - partial pods scheduling + prioritize master pod", func(ctx context.Context) {
			limitedQueueName := "limited-" + utils.GenerateRandomK8sName(10)
			parentQueue := testCtx.Queues[1]
			limitedQueue := queue.CreateQueueObject(limitedQueueName, parentQueue.Name)
			limitedQueue.Spec.Resources.CPU.Quota = 2000
			limitedQueue.Spec.Resources.CPU.Limit = 2000
			testCtx.AddQueues(ctx, []*v2.Queue{limitedQueue})

			pytorchElastic := pytorch.CreateObject(namespace, limitedQueue.Name)
			pytorchElastic.Spec.RunPolicy = trainingoperatorv1.RunPolicy{
				SchedulingPolicy: &trainingoperatorv1.SchedulingPolicy{
					MinAvailable: ptr.To(int32(1)),
				},
			}
			pytorchElastic.Spec.PyTorchReplicaSpecs[pytorch.MasterReplicaType].Template.Spec.Containers[0].Resources =
				v12.ResourceRequirements{
					Requests: map[v12.ResourceName]resource.Quantity{
						v12.ResourceCPU: resource.MustParse("2"),
					},
				}
			pytorchElastic.Spec.PyTorchReplicaSpecs[pytorch.WorkerReplicaType].Template.Spec.Containers[0].Resources =
				v12.ResourceRequirements{
					Requests: map[v12.ResourceName]resource.Quantity{
						v12.ResourceCPU: resource.MustParse("2"),
					},
				}

			_, err := trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Create(
				ctx, pytorchElastic, v1.CreateOptions{})
			Expect(err).To(Succeed())
			defer func() {
				err = trainingOperatorSdk.KubeflowV1().PyTorchJobs(namespace).Delete(ctx, pytorchElastic.Name,
					v1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			}()

			labelSelector := v1.LabelSelector{
				MatchLabels: map[string]string{constants.AppLabelName: pytorchElastic.Labels[constants.AppLabelName]},
			}
			wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, labelSelector, 2)

			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx,
				v1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", constants.AppLabelName,
						pytorchElastic.Labels[constants.AppLabelName]),
				},
			)
			Expect(err).To(Succeed())

			for _, pod := range podList.Items {
				role, ok := pod.Labels[podRoleLabelKey]
				if ok && role == "master" {
					wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pod)
				} else {
					wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, &pod)
				}
			}
		})
	})
}
