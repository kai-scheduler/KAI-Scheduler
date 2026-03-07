/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package applying_options

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

const specNumCreatePodRetries = 10

func DescribeNamespaceSpecs() bool {
	return Describe("Namespace options", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("Namespace in Request", func(ctx context.Context) {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})

			targetNamespace := pod.Namespace
			pod.Namespace = ""

			actualPod, err := specCreatePodInNamespace(ctx, testCtx.KubeClientset, pod, targetNamespace)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, actualPod)
		})

		Context("Binding Webhook", func() {
			BeforeAll(func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
					&capacity.ResourceList{
						Gpu:      resource.MustParse("1"),
						PodCount: 1,
					},
				)
			})

			It("Fraction GPU request", Label(labels.ReservationPod), func(ctx context.Context) {
				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pod.Annotations = map[string]string{
					constants.GpuFraction: "0.5",
				}

				targetNamespace := pod.Namespace
				pod.Namespace = ""

				actualPod, err := specCreatePodInNamespace(ctx, testCtx.KubeClientset, pod, targetNamespace)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodReady(ctx, testCtx.ControllerClient, actualPod)
			})
		})
	})
}

func specCreatePodInNamespace(ctx context.Context, client *kubernetes.Clientset, pod *v1.Pod,
	namespace string) (*v1.Pod, error) {
	actualPod, err := rd.GetPod(ctx, client, pod.Namespace, pod.Name)
	if err == nil {
		return nil, fmt.Errorf("pod %s/%s already exists in the cluster", namespace, pod.Name)
	}

	for range specNumCreatePodRetries {
		actualPod, err = client.
			CoreV1().
			Pods(namespace).
			Create(ctx, pod, metav1.CreateOptions{})
		if err == nil {
			return actualPod, nil
		}
		if errors.IsAlreadyExists(err) {
			return rd.GetPod(ctx, client, pod.Namespace, pod.Name)
		}
		time.Sleep(time.Second * 2)
	}
	return nil, fmt.Errorf("failed to create pod <%s/%s>, error: %s",
		pod.Namespace, pod.Name, err)
}
