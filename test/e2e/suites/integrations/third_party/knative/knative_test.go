/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package knative

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/pointer"
	"knative.dev/pkg/ptr"
	knativeserving "knative.dev/serving/pkg/apis/serving/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	minScaleAnnotation = "autoscaling.knative.dev/min-scale"
	maxScaleAnnotation = "autoscaling.knative.dev/max-scale"
	metricAnnotation   = "autoscaling.knative.dev/metric"
	targetAnnotation   = "autoscaling.knative.dev/target"
)

var _ = Describe("Knative integration", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		skipIfKnativeNotInstalled(ctx, testCtx)

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

			knativeService := getKnativeServiceObject(serviceName, namespace, testCtx.Queues[0].Name)
			Expect(testCtx.ControllerClient.Create(ctx, knativeService)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, knativeService)).To(Succeed())
			}()

			pods := getServicePods(ctx, testCtx, serviceName, namespace, 2)

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

			knativeService := getKnativeServiceObject(serviceName, namespace, testCtx.Queues[0].Name)

			// Remove scale annotation
			delete(knativeService.Spec.ConfigurationSpec.Template.Annotations, minScaleAnnotation)

			Expect(testCtx.ControllerClient.Create(ctx, knativeService)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, knativeService)).To(Succeed())
			}()

			pods := getServicePods(ctx, testCtx, serviceName, namespace, 1)

			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

			var podGroups v2alpha2.PodGroupList
			Expect(testCtx.ControllerClient.List(ctx, &podGroups, client.InNamespace(namespace))).To(Succeed())
			Expect(len(podGroups.Items)).To(Equal(1),
				"Expected one podgroup for the revision")
			Expect(podGroups.Items[0].Spec.MinMember).To(Equal(int32(1)),
				"Expected minmember of podgroup to be 1")
		})

		Context("Non-Gang scheduling", func() {
			BeforeAll(func(ctx context.Context) {
				setKnativeGangAndWait(ctx, testCtx, pointer.Bool(false))

			})

			AfterAll(func(ctx context.Context) {
				setKnativeGangAndWait(ctx, testCtx, nil)
			})

			It("Should create a podgroup per pod", func(ctx context.Context) {
				namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
				serviceName := "knative-service-" + utils.GenerateRandomK8sName(10)

				knativeService := getKnativeServiceObject(serviceName, namespace, testCtx.Queues[0].Name)
				Expect(testCtx.ControllerClient.Create(ctx, knativeService)).To(Succeed())
				defer func() {
					Expect(testCtx.ControllerClient.Delete(ctx, knativeService)).To(Succeed())
				}()

				pods := getServicePods(ctx, testCtx, serviceName, namespace, 2)

				wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

				var podGroups v2alpha2.PodGroupList
				Expect(testCtx.ControllerClient.List(ctx, &podGroups, client.InNamespace(namespace))).To(Succeed())
				Expect(len(podGroups.Items)).To(Equal(len(pods)), "Expected a podgroup per pod")
				for _, pg := range podGroups.Items {
					Expect(pg.Spec.MinMember).To(Equal(int32(1)), "podgroup per pod need min member 1")
				}
			})

			It("Backwards Compatibility: Should not brake existing revisions", func(ctx context.Context) {
				serviceScale := 2
				namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
				serviceName := "knative-service-" + utils.GenerateRandomK8sName(10)

				knativeService := getKnativeServiceObject(serviceName, namespace, testCtx.Queues[0].Name)
				knativeService.Spec.ConfigurationSpec.Template.Annotations[minScaleAnnotation] = fmt.Sprintf("%d", serviceScale)
				knativeService.Spec.ConfigurationSpec.Template.Annotations[maxScaleAnnotation] = "2"
				knativeService.Spec.ConfigurationSpec.Template.Labels["testbrakerevisions"] = "true"
				labelSelector := labels.SelectorFromSet(map[string]string{
					"testbrakerevisions": "true",
				})
				Expect(testCtx.ControllerClient.Create(ctx, knativeService)).To(Succeed())
				defer func() {
					Expect(testCtx.ControllerClient.Delete(ctx, knativeService)).To(Succeed())
				}()

				pods := getServicePods(ctx, testCtx, serviceName, namespace, 2)

				wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

				var podGroups v2alpha2.PodGroupList
				Expect(testCtx.ControllerClient.List(ctx, &podGroups, client.InNamespace(namespace))).To(Succeed())
				Expect(len(pods)).To(Equal(serviceScale), fmt.Sprintf("Expected %d pods for the service", serviceScale))
				Expect(len(podGroups.Items)).To(Equal(serviceScale), "Expected a podgroup per pod")
				for _, pg := range podGroups.Items {
					Expect(pg.Spec.MinMember).To(Equal(int32(1)), "podgroup per pod need min member 1")
				}

				setKnativeGangAndWait(ctx, testCtx, pointer.Bool(true))

				waitForKnativeServiceScale(ctx, testCtx, namespace, serviceScale, labelSelector)
				// kill one pod
				Expect(testCtx.ControllerClient.Delete(ctx, pods[0])).To(Succeed())
				wait.ForPodsToBeDeleted(ctx, testCtx.ControllerClient,
					runtimeClient.MatchingFields{"metadata.name": pods[0].Name})
				waitForKnativeServiceScale(ctx, testCtx, namespace, serviceScale, labelSelector)

				pods = getServicePods(ctx, testCtx, serviceName, namespace, 2)

				Expect(testCtx.ControllerClient.List(ctx, &podGroups, client.InNamespace(namespace))).To(Succeed())
				Expect(len(pods)).To(Equal(serviceScale), fmt.Sprintf("Expected %d pods for the service", serviceScale))
				Expect(len(podGroups.Items)).To(Equal(len(pods)), "Expected a podgroup per pod")
				for _, pg := range podGroups.Items {
					Expect(pg.Spec.MinMember).To(Equal(int32(1)), "podgroup per pod need min member 1")
				}
				setKnativeGangAndWait(ctx, testCtx, nil)
			})
		})
	})
})

func getServicePods(ctx context.Context, testCtx *testcontext.TestContext, serviceName, namespace string,
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

func waitForKnativeServiceScale(ctx context.Context, testCtx *testcontext.TestContext, namespace string, minScale int,
	labelSelector labels.Selector) {
	wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(event watch.Event) bool {
		pods, ok := event.Object.(*v1.PodList)
		if !ok {
			return false
		}
		return len(pods.Items) == minScale
	}, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: labelSelector})
}

func setKnativeGangAndWait(ctx context.Context, testCtx *testcontext.TestContext, gang *bool) {
	if gang == nil {
		Expect(feature_flags.UnsetKnativeGangScheduling(ctx, testCtx)).To(Succeed(),
			"Failed to unset knative gang scheduling")
	} else {
		Expect(feature_flags.SetKnativeGangScheduling(ctx, testCtx, gang)).To(Succeed(),
			"Failed to set knative gang scheduling")
	}
	Eventually(func(g Gomega) bool {
		podGrouperPod := &v1.PodList{}
		err := testCtx.ControllerClient.List(
			ctx, podGrouperPod,
			client.InNamespace(constant.SystemPodsNamespace), client.MatchingLabels{"app": "podgrouper"},
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(podGrouperPod.Items)).To(Equal(1))

		var expectedArgValue bool
		if gang == nil {
			// this is the default, but we will make sure no value is set
			expectedArgValue = true
		} else {
			expectedArgValue = *gang
		}

		for _, arg := range podGrouperPod.Items[0].Spec.Containers[0].Args {
			if strings.HasPrefix(arg, "--knative-gang-schedule") && gang == nil {
				return false
			}
			if arg == fmt.Sprintf("--knative-gang-schedule=%v", expectedArgValue) {
				return true
			}
		}

		return gang == nil
	}).WithTimeout(time.Minute).WithPolling(time.Second).Should(BeTrue())
}

func getKnativeServiceObject(serviceName, namespace, queueName string) *knativeserving.Service {
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
							"kai.scheduler/queue":  queueName,
						},
					},
					Spec: knativeserving.RevisionSpec{
						PodSpec: corev1.PodSpec{
							SchedulerName:                 constant.SchedulerName,
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

func skipIfKnativeNotInstalled(ctx context.Context, testCtx *testcontext.TestContext) {
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
