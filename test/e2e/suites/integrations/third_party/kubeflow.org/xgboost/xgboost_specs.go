/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package xgboost

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
	xgBoostCrdName    = "xgboostjobs.kubeflow.org"
	xgBoostCrdVersion = "v1"
	numberOfWorkers   = 3
)

func DescribeXGBoostSpecs() bool {
	return Describe("XGBoost integration", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, xgBoostCrdName, xgBoostCrdVersion)
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

		It("should run the pods of the XGBoost", func(ctx context.Context) {
			singleGPURequest := v1.ResourceRequirements{
				Limits: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
				Requests: v1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			}

			xgBoostJob, expectedPods := createExampleXGBoostJob(
				testCtx.Queues[0],
				singleGPURequest, singleGPURequest,
				numberOfWorkers,
			)
			Expect(testCtx.ControllerClient.Create(ctx, xgBoostJob)).To(Succeed())
			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, xgBoostJob)).To(Succeed())
			}()
			Eventually(func(g Gomega) bool {
				pods := &v1.PodList{}
				testCtx.ControllerClient.List(ctx, pods, runtimeClient.InNamespace(xgBoostJob.Namespace))

				g.Expect(len(pods.Items)).To(Equal(expectedPods))
				for _, pod := range pods.Items {
					g.Expect(rd.IsPodReady(&pod)).To(BeTrue())
				}
				return true
			}, time.Minute).Should(BeTrue())
		})
	})
}

func createExampleXGBoostJob(
	testQueue *v2.Queue,
	masterRequest v1.ResourceRequirements,
	workersRequest v1.ResourceRequirements,
	numberOfWorkers int,
) (*trainingoperatorv1.XGBoostJob, int) {

	sampleMasterPod := rd.CreatePodObject(testQueue, masterRequest)
	sampleMasterPod.Spec.Containers[0].Name = "xgboost"
	sampleWorkerPod := rd.CreatePodObject(testQueue, workersRequest)
	sampleWorkerPod.Spec.Containers[0].Name = "xgboost"

	xgBoostJob := &trainingoperatorv1.XGBoostJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRandomK8sName(10),
			Namespace:   queue.GetConnectedNamespaceToQueue(testQueue),
			Labels:      maps.Clone(sampleMasterPod.Labels),
			Annotations: maps.Clone(sampleMasterPod.Annotations),
		},
		Spec: trainingoperatorv1.XGBoostJobSpec{
			RunPolicy: trainingoperatorv1.RunPolicy{
				BackoffLimit:   ptr.To(int32(5)),
				CleanPodPolicy: ptr.To(trainingoperatorv1.CleanPodPolicyRunning),
			},
			XGBReplicaSpecs: map[trainingoperatorv1.ReplicaType]*trainingoperatorv1.ReplicaSpec{
				"Master": {
					Replicas: ptr.To(int32(1)),
					Template: v1.PodTemplateSpec{
						ObjectMeta: sampleMasterPod.ObjectMeta,
						Spec:       sampleMasterPod.Spec,
					},
				},
				"Worker": {
					Replicas: ptr.To(int32(numberOfWorkers)),
					Template: v1.PodTemplateSpec{
						ObjectMeta: sampleWorkerPod.ObjectMeta,
						Spec:       sampleWorkerPod.Spec,
					},
				},
			},
		},
	}

	return xgBoostJob, 1 + numberOfWorkers
}
