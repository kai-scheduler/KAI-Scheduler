/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package tfjob

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
	tfjobCrdName    = "tfjobs.kubeflow.org"
	tfjobCrdVersion = "v1"
	numberOfChiefs  = 2
	numberOfWorkers = 3
)

func DescribeTFJobSpecs() bool {
	return Describe("TFJob integration", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, tfjobCrdName, tfjobCrdVersion)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
				Gpu: *resource.NewQuantity(numberOfChiefs+numberOfWorkers, resource.DecimalSI),
			})

			Expect(trainingoperatorv1.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("should run the pods of the TFJob", func(ctx context.Context) {
			singleGPURequest := v1.ResourceRequirements{
				Limits: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
				Requests: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			}

			tfJob, expectedPods := createExampleTFJob(
				testCtx.Queues[0],
				singleGPURequest, singleGPURequest,
				numberOfChiefs, numberOfWorkers,
			)
			Expect(testCtx.ControllerClient.Create(ctx, tfJob)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, tfJob)).To(Succeed())
			}()
			Eventually(func(g Gomega) bool {
				pods := &v1.PodList{}
				err := testCtx.ControllerClient.List(ctx, pods, runtimeClient.InNamespace(tfJob.Namespace))
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(len(pods.Items)).To(Equal(expectedPods))
				for _, pod := range pods.Items {
					g.Expect(rd.IsPodReady(&pod)).To(BeTrue())
				}
				return true
			}, time.Minute).Should(BeTrue())
		})
	})
}

func createExampleTFJob(
	testQueue *v2.Queue,
	chiefsRequest v1.ResourceRequirements,
	workersRequest v1.ResourceRequirements,
	numberOfChiefs int,
	numberOfWorkers int,
) (*trainingoperatorv1.TFJob, int) {

	sampleChiefPod := rd.CreatePodObject(testQueue, chiefsRequest)
	sampleChiefPod.Spec.Containers[0].Name = "tensorflow"
	sampleWorkerPod := rd.CreatePodObject(testQueue, workersRequest)
	sampleWorkerPod.Spec.Containers[0].Name = "tensorflow"

	tfJob := &trainingoperatorv1.TFJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRandomK8sName(10),
			Namespace:   queue.GetConnectedNamespaceToQueue(testQueue),
			Labels:      maps.Clone(sampleChiefPod.Labels),
			Annotations: maps.Clone(sampleChiefPod.Annotations),
		},
		Spec: trainingoperatorv1.TFJobSpec{
			RunPolicy: trainingoperatorv1.RunPolicy{
				BackoffLimit:   ptr.To(int32(5)),
				CleanPodPolicy: ptr.To(trainingoperatorv1.CleanPodPolicyRunning),
			},
			TFReplicaSpecs: map[trainingoperatorv1.ReplicaType]*trainingoperatorv1.ReplicaSpec{
				"Cheifs": {
					Replicas: ptr.To(int32(numberOfChiefs)),
					Template: v1.PodTemplateSpec{
						ObjectMeta: sampleChiefPod.ObjectMeta,
						Spec:       sampleChiefPod.Spec,
					},
				},
				"Workers": {
					Replicas: ptr.To(int32(numberOfWorkers)),
					Template: v1.PodTemplateSpec{
						ObjectMeta: sampleWorkerPod.ObjectMeta,
						Spec:       sampleWorkerPod.Spec,
					},
				},
			},
		},
	}

	return tfJob, numberOfChiefs + numberOfWorkers
}
