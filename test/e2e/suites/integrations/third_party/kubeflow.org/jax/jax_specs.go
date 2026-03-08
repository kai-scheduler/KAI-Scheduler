/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package jax

import (
	"context"
	"maps"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"

	trainingoperatorv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
)

const (
	JaxCrdName      = "jaxjobs.kubeflow.org"
	JaxCrdVersion   = "v1"
	numberOfWorkers = 3
)

func DescribeJaxSpecs() bool {
	return Describe("JAX integration", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, JaxCrdName, JaxCrdVersion)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
				Gpu: *resource.NewMilliQuantity(1+numberOfWorkers, resource.DecimalSI),
			})

			Expect(trainingoperatorv1.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("should run the pods of the Jax", func(ctx context.Context) {
			singleGPURequest := v1.ResourceRequirements{
				Limits: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
				Requests: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			}

			jaxJob, expectedPods := createExampleJaxJob(
				testCtx.Queues[0],
				singleGPURequest,
				numberOfWorkers,
			)
			Expect(testCtx.ControllerClient.Create(ctx, jaxJob)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, jaxJob)).To(Succeed())
			}()
			Eventually(func(g Gomega) bool {
				pods := &v1.PodList{}
				testCtx.ControllerClient.List(ctx, pods, runtimeClient.InNamespace(jaxJob.Namespace))

				g.Expect(len(pods.Items)).To(Equal(expectedPods))
				for _, pod := range pods.Items {
					g.Expect(rd.IsPodReady(&pod)).To(BeTrue())
				}
				return true
			}, time.Minute).Should(BeTrue())
		})
	})
}

func createExampleJaxJob(
	testQueue *v2.Queue,
	workersRequest v1.ResourceRequirements,
	numberOfWorkers int,
) (*trainingoperatorv1.JAXJob, int) {

	sampleWorkerPod := rd.CreatePodObject(testQueue, workersRequest)
	sampleWorkerPod.Spec.Containers[0].Name = "jax"

	jaxJob := &trainingoperatorv1.JAXJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRandomK8sName(10),
			Namespace:   queue.GetConnectedNamespaceToQueue(testQueue),
			Labels:      maps.Clone(sampleWorkerPod.Labels),
			Annotations: maps.Clone(sampleWorkerPod.Annotations),
		},
		Spec: trainingoperatorv1.JAXJobSpec{
			RunPolicy: trainingoperatorv1.RunPolicy{
				BackoffLimit:   ptr.To(int32(5)),
				CleanPodPolicy: ptr.To(trainingoperatorv1.CleanPodPolicyRunning),
			},
			JAXReplicaSpecs: map[trainingoperatorv1.ReplicaType]*trainingoperatorv1.ReplicaSpec{
				trainingoperatorv1.JAXJobReplicaTypeWorker: {
					Replicas: ptr.To(int32(numberOfWorkers)),
					Template: v1.PodTemplateSpec{
						ObjectMeta: sampleWorkerPod.ObjectMeta,
						Spec:       sampleWorkerPod.Spec,
					},
				},
			},
		},
	}

	return jaxJob, numberOfWorkers
}
