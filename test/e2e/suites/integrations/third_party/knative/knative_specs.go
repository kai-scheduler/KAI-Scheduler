/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package knative

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
	knativeserving "knative.dev/serving/pkg/apis/serving/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	minScaleAnnotation = "autoscaling.knative.dev/min-scale"
	maxScaleAnnotation = "autoscaling.knative.dev/max-scale"
	metricAnnotation   = "autoscaling.knative.dev/metric"
	targetAnnotation   = "autoscaling.knative.dev/target"
)

func DescribeKnativeSpecs() bool {
	return Describe("Knative integration", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			SkipIfKnativeNotInstalled(ctx, testCtx)

			Expect(knativeserving.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		Context("Knative submission", func() {
			It("basic knative service", func(ctx context.Context) {
				namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
				serviceName := "knative-service-" + utils.GenerateRandomK8sName(10)

				knativeService := GetKnativeServiceObject(serviceName, namespace, testCtx.Queues[0].Name)
				Expect(testCtx.ControllerClient.Create(ctx, knativeService)).To(Succeed())
				defer func() {
					Expect(testCtx.ControllerClient.Delete(ctx, knativeService)).To(Succeed())
				}()

				pods := GetServicePods(ctx, testCtx, serviceName, namespace, 2)

				wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

				var podGroups v2alpha2.PodGroupList
				Expect(testCtx.ControllerClient.List(ctx, &podGroups, client.InNamespace(namespace))).To(Succeed())
				Expect(len(podGroups.Items)).To(Equal(1),
					"Expected one podgroup for the revision")
				Expect(podGroups.Items[0].Spec.MinMember).To(Equal(int32(2)),
					"Expected minmember of podgroup to be 2")
			})

			It("basic knative service - no minscale", func(ctx context.Context) {
				namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
				serviceName := "knative-service-" + utils.GenerateRandomK8sName(10)

				knativeService := GetKnativeServiceObject(serviceName, namespace, testCtx.Queues[0].Name)

				delete(knativeService.Spec.ConfigurationSpec.Template.Annotations, minScaleAnnotation)

				Expect(testCtx.ControllerClient.Create(ctx, knativeService)).To(Succeed())
				defer func() {
					Expect(testCtx.ControllerClient.Delete(ctx, knativeService)).To(Succeed())
				}()

				pods := GetServicePods(ctx, testCtx, serviceName, namespace, 1)

				wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

				var podGroups v2alpha2.PodGroupList
				Expect(testCtx.ControllerClient.List(ctx, &podGroups, client.InNamespace(namespace))).To(Succeed())
				Expect(len(podGroups.Items)).To(Equal(1),
					"Expected one podgroup for the revision")
				Expect(podGroups.Items[0].Spec.MinMember).To(Equal(int32(1)),
					"Expected minmember of podgroup to be 1")
			})
		})
	})
}

func GetServicePods(ctx context.Context, testCtx *testcontext.TestContext, serviceName, namespace string,
	numExpectedPods int) []*v1.Pod {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"serving.knative.dev/service": serviceName,
		},
	}
	wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, labelSelector, numExpectedPods)

	knativeServicePods := &v1.PodList{}
	err := testCtx.ControllerClient.List(ctx, knativeServicePods, client.InNamespace(namespace))
	Expect(err).NotTo(HaveOccurred())

	var pods []*v1.Pod
	for index := range knativeServicePods.Items {
		pods = append(pods, &knativeServicePods.Items[index])
	}

	return pods
}

func GetKnativeServiceObject(serviceName, namespace, queueName string) *knativeserving.Service {
	cfg := testconfig.GetConfig()
	return &knativeserving.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: knativeserving.ServiceSpec{
			ConfigurationSpec: knativeserving.ConfigurationSpec{
				Template: knativeserving.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							minScaleAnnotation: "2",
							maxScaleAnnotation: "3",
							metricAnnotation:   "rps",
							targetAnnotation:   "10",
						},
						Labels: map[string]string{
							constants.AppLabelName: "engine-e2e",
							cfg.QueueLabelKey:      queueName,
						},
					},
					Spec: knativeserving.RevisionSpec{
						PodSpec: corev1.PodSpec{
							SchedulerName:                 cfg.SchedulerName,
							TerminationGracePeriodSeconds: ptr.Int64(0),
							Containers: []corev1.Container{
								{
									Name:  "user-container",
									Image: "gcr.io/run-ai-lab/knative-autoscale-go",
									Ports: []corev1.ContainerPort{
										{
											ContainerPort: 8080,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									SecurityContext: rd.DefaultSecurityContext(),
								},
							},
						},
					},
				},
			},
		},
	}
}

func SkipIfKnativeNotInstalled(ctx context.Context, testCtx *testcontext.TestContext) {
	Expect(apiextensionsv1.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())

	for _, crdBaseName := range []string{"services", "configurations", "revisions"} {
		crdName := fmt.Sprintf("%s.%s", crdBaseName, "serving.knative.dev")
		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, crdName, "v1")
	}

	var knativeOperatorDeployment v1apps.Deployment
	err := testCtx.ControllerClient.Get(
		ctx, client.ObjectKey{Name: "knative-operator", Namespace: "knative-operator"}, &knativeOperatorDeployment)
	if err != nil {
		Skip("Knative is not installed: missing operator deployment: " + err.Error())
	}
}
