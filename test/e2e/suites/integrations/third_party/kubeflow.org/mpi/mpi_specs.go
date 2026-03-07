/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package mpi

import (
	"context"
	"fmt"

	mpiOperator "github.com/kubeflow/mpi-operator/pkg/client/clientset/versioned"
	trainingOperator "github.com/kubeflow/training-operator/pkg/client/clientset/versioned"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v12 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/mpi"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

const (
	mpiCrdName          = "mpijobs.kubeflow.org"
	mpiCrdVersion1      = "v1"
	mpiCrdVersion2Beta1 = "v2beta1"
)

func DescribeMpiSpecs() bool {
	return Describe("Kubeflow.org MPIJob Integration", Ordered, func() {
		var (
			testCtx   *testcontext.TestContext
			namespace string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
			namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		Context("Training Operator", func() {
			var trainingOperatorSdk *trainingOperator.Clientset

			BeforeAll(func(ctx context.Context) {
				crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, mpiCrdName, mpiCrdVersion1)
				trainingOperatorSdk = trainingOperator.NewForConfigOrDie(testCtx.KubeConfig)
				mpiOperator.NewForConfigOrDie(testCtx.KubeConfig)
			})

			It("MPIJob - V1", func(ctx context.Context) {
				obj := mpi.CreateV1Object(namespace, testCtx.Queues[0].Name)
				_, err := trainingOperatorSdk.KubeflowV1().MPIJobs(namespace).Create(ctx, obj, v1.CreateOptions{})
				Expect(err).To(Succeed())
				defer func() {
					err = trainingOperatorSdk.KubeflowV1().MPIJobs(namespace).Delete(ctx, obj.Name, v1.DeleteOptions{})
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
		})

		Context("MPI Operator", func() {
			var mpiOperatorSdk *mpiOperator.Clientset

			BeforeAll(func(ctx context.Context) {
				crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, mpiCrdName, mpiCrdVersion2Beta1)
				mpiOperatorSdk = mpiOperator.NewForConfigOrDie(testCtx.KubeConfig)
			})

			It("MPIJob - V2beta1", func(ctx context.Context) {
				obj := mpi.CreateV2beta1Object(namespace, testCtx.Queues[0].Name)
				_, err := mpiOperatorSdk.KubeflowV2beta1().MPIJobs(namespace).Create(ctx, obj, v1.CreateOptions{})
				Expect(err).To(Succeed())
				defer func() {
					err = mpiOperatorSdk.KubeflowV2beta1().MPIJobs(namespace).Delete(ctx, obj.Name, v1.DeleteOptions{})
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
		})
	})
}
