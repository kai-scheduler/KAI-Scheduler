/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package k8s_native

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait/watcher"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DescribeK8sNativeSpecs() bool {
	return Describe("K8S Native object integrations", Ordered, func() {
		var testCtx *testcontext.TestContext

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

		It("Pod", func(ctx context.Context) {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			if err != nil {
				Expect(err).NotTo(HaveOccurred(), "Failed to create pod-job")
			}

			defer func() {
				err = testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
				Expect(err).To(Succeed())
			}()

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		It("ReplicaSet", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			rs := rd.CreateReplicasetObject(namespace, testCtx.Queues[0].Name)

			rs, err := testCtx.KubeClientset.AppsV1().ReplicaSets(rs.Namespace).Create(ctx, rs, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			defer func() {
				err = testCtx.KubeClientset.AppsV1().ReplicaSets(rs.Namespace).Delete(ctx, rs.Name, metav1.DeleteOptions{})
				Expect(err).To(Succeed())
			}()

			wait.ForAtLeastOnePodCreation(ctx, testCtx.ControllerClient, *rs.Spec.Selector)
			pods, err := testCtx.KubeClientset.CoreV1().Pods(rs.Namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", rd.ReplicaSetAppLabel, rs.Labels[rd.ReplicaSetAppLabel]),
			})
			Expect(err).To(Succeed())
			Expect(len(pods.Items)).To(BeNumerically(">", 0))

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pods.Items[0])
		})

		Context("Deployment", Ordered, func() {
			It("Allocates deployment pods", func(ctx context.Context) {
				namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
				deployment := rd.CreateDeploymentObject(namespace, testCtx.Queues[0].Name)

				deployment, err := testCtx.KubeClientset.AppsV1().Deployments(deployment.Namespace).
					Create(ctx, deployment, metav1.CreateOptions{})
				Expect(err).To(Succeed())

				defer func() {
					err = testCtx.KubeClientset.AppsV1().Deployments(deployment.Namespace).
						Delete(ctx, deployment.Name, metav1.DeleteOptions{})
					Expect(err).To(Succeed())
				}()

				wait.ForAtLeastOnePodCreation(ctx, testCtx.ControllerClient, *deployment.Spec.Selector)
				pods, err := testCtx.KubeClientset.CoreV1().Pods(deployment.Namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", rd.DeploymentAppLabel, deployment.Labels[rd.DeploymentAppLabel]),
				})
				Expect(err).To(Succeed())
				Expect(len(pods.Items)).To(BeNumerically(">", 0))

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pods.Items[0])
			})

			It("Doesn't leak config maps after scale down", func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
					&capacity.ResourceList{
						Gpu:      resource.MustParse("1"),
						PodCount: 1,
					},
				)

				namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
				deployment := rd.CreateDeploymentObject(namespace, testCtx.Queues[0].Name)
				deployment.Spec.Template.Annotations = map[string]string{
					constants.GpuFraction: "0.1",
				}

				Expect(countSharedConfigmaps(ctx, testCtx.ControllerClient, namespace)).To(Equal(0))

				deploymentMaxReplicas := 10
				deployment.Spec.Replicas = ptr.To(int32(deploymentMaxReplicas))
				Expect(testCtx.ControllerClient.Create(ctx, deployment)).To(Succeed())

				defer func() {
					Expect(testCtx.ControllerClient.Delete(ctx, deployment)).To(Succeed())
				}()

				wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, *deployment.Spec.Selector, deploymentMaxReplicas)
				podsList := &v1.PodList{}
				Expect(testCtx.ControllerClient.List(
					ctx, podsList,
					client.InNamespace(namespace),
					client.MatchingLabels{
						rd.DeploymentAppLabel: deployment.Labels[rd.DeploymentAppLabel],
					},
				)).To(Succeed())

				Expect(len(podsList.Items)).To(Equal(deploymentMaxReplicas))
				pods := make([]*v1.Pod, deploymentMaxReplicas)
				for i := range pods {
					pods[i] = &podsList.Items[i]
				}
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, deploymentMaxReplicas)

				Eventually(func(g Gomega) {
					g.Expect(countSharedConfigmaps(ctx, testCtx.ControllerClient, namespace)).To(Equal(2 * deploymentMaxReplicas))
				}).WithTimeout(watcher.FlowTimeout).WithPolling(1 * time.Second).WithContext(ctx).Should(Succeed())

				deployment2 := deployment.DeepCopy()
				deployment2.Spec.Replicas = ptr.To(int32(0))
				Expect(testCtx.ControllerClient.Patch(ctx, deployment2, client.MergeFrom(deployment))).To(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(countSharedConfigmaps(ctx, testCtx.ControllerClient, namespace)).To(Equal(0))
				}).WithTimeout(watcher.FlowTimeout).WithPolling(1 * time.Second).WithContext(ctx).Should(Succeed())
			})
		})

		It("StatefulSet", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			statefulSet := rd.CreateStatefulSetObject(namespace, testCtx.Queues[0].Name)

			statefulSet, err := testCtx.KubeClientset.AppsV1().StatefulSets(statefulSet.Namespace).
				Create(ctx, statefulSet, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			defer func() {
				err = testCtx.KubeClientset.AppsV1().StatefulSets(statefulSet.Namespace).
					Delete(ctx, statefulSet.Name, metav1.DeleteOptions{})
				Expect(err).To(Succeed())
			}()

			wait.ForAtLeastOnePodCreation(ctx, testCtx.ControllerClient, *statefulSet.Spec.Selector)
			pods, err := testCtx.KubeClientset.CoreV1().Pods(statefulSet.Namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", rd.StatefulSetAppLabel,
					statefulSet.Labels[rd.StatefulSetAppLabel]),
			})
			Expect(err).To(Succeed())
			Expect(len(pods.Items)).To(BeNumerically(">", 0))

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pods.Items[0])
		})

		It("BatchJob", func(ctx context.Context) {
			job := rd.CreateBatchJobObject(testCtx.Queues[0], v1.ResourceRequirements{})

			job, err := testCtx.KubeClientset.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			defer func() {
				rd.DeleteJob(ctx, testCtx.KubeClientset, job)
			}()

			labelSelector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					rd.BatchJobAppLabel: job.Labels[rd.BatchJobAppLabel],
				},
			}
			wait.ForAtLeastOnePodCreation(ctx, testCtx.ControllerClient, labelSelector)

			pods, err := testCtx.KubeClientset.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", rd.BatchJobAppLabel, job.Labels[rd.BatchJobAppLabel]),
			})
			Expect(err).To(Succeed())
			Expect(len(pods.Items)).To(BeNumerically(">", 0))

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pods.Items[0])
		})

		It("CronJob", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			cronJob := rd.CreateCronJobObject(namespace, testCtx.Queues[0].Name)

			cronJob, err := testCtx.KubeClientset.BatchV1().CronJobs(cronJob.Namespace).
				Create(ctx, cronJob, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			defer func() {
				err := testCtx.KubeClientset.BatchV1().CronJobs(cronJob.Namespace).Delete(ctx, cronJob.Name,
					metav1.DeleteOptions{})
				Expect(err).To(Succeed())
			}()
			labelSelector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					rd.CronJobAppLabel: cronJob.Labels[rd.CronJobAppLabel],
				},
			}

			wait.ForAtLeastOnePodCreation(ctx, testCtx.ControllerClient, labelSelector)
			pods, err := testCtx.KubeClientset.CoreV1().Pods(cronJob.Namespace).List(ctx,
				metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s",
					rd.CronJobAppLabel, cronJob.Labels[rd.CronJobAppLabel])},
			)
			Expect(err).To(Succeed())
			Expect(len(pods.Items)).To(BeNumerically(">", 0))

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pods.Items[0])
		})
	})
}

func countSharedConfigmaps(ctx context.Context, kubeClient client.Client, namespace string) int {
	shardConfigMapNamePart := "shared-gpu"

	configMapList := &v1.ConfigMapList{}
	Expect(kubeClient.List(ctx, configMapList, client.InNamespace(namespace))).To(Succeed())

	sharedConfigMaps := 0
	for _, configMap := range configMapList.Items {
		if strings.Contains(configMap.Name, shardConfigMapNamePart) {
			sharedConfigMaps++
		}
	}

	return sharedConfigMaps
}
