/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package context

import (
	"context"

	"github.com/onsi/gomega"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kubeAiSchedClient "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned"
	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

type TestContext struct {
	KubeConfig           *rest.Config
	KubeClientset        *kubernetes.Clientset
	KubeAiSchedClientset *kubeAiSchedClient.Clientset
	ControllerClient     runtimeClient.WithWatch
	Queues               []*v2.Queue

	goContext context.Context
	asserter  gomega.Gomega
}

func GetConnectivity(ctx context.Context, asserter gomega.Gomega) *TestContext {
	err := initConnectivity()
	if err != nil {
		asserter.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get connectivity objects")
	}

	err = validateClientSets()
	if err != nil {
		asserter.Expect(err).NotTo(gomega.HaveOccurred(), "client-sets validation failure")
	}

	return &TestContext{
		KubeConfig:           kubeConfig,
		KubeClientset:        kubeClientset,
		KubeAiSchedClientset: kubeAiSchedClientset,
		ControllerClient:     controllerClient,
		Queues:               []*v2.Queue{},

		goContext: ctx,
		asserter:  asserter,
	}
}

func (tc *TestContext) InitQueues(queues []*v2.Queue) {
	if queues != nil {
		tc.Queues = queues
	}

	err := tc.createClusterQueues(tc.goContext)
	if err != nil {
		tc.asserter.
			Expect(err).NotTo(gomega.HaveOccurred(), "failed to create test cluster context")
	}
}

func (tc *TestContext) AddQueues(ctx context.Context, newQueues []*v2.Queue) {
	allQueues := append(tc.Queues, newQueues...)
	tc.goContext = ctx
	tc.InitQueues(newQueues)
	tc.Queues = allQueues
}

func (tc *TestContext) TestContextCleanup(ctx context.Context) {
	namespaces, err := rd.GetE2ENamespaces(ctx, tc.KubeClientset)
	tc.asserter.Expect(err).To(gomega.Succeed())

	for _, namespace := range namespaces.Items {
		tc.deleteAllObjectsInNamespace(ctx, namespace.Name)
	}

	wait.ForNoE2EPods(ctx, tc.ControllerClient)
	wait.ForNoReservationPods(ctx, tc.ControllerClient)

	wait.ForRunningSystemComponentEvent(ctx, tc.ControllerClient, "binder")
	wait.ForRunningSystemComponentEvent(ctx, tc.ControllerClient, testconfig.GetConfig().SchedulerDeploymentName)
}

func (tc *TestContext) ClusterCleanup(ctx context.Context) {
	tc.TestContextCleanup(ctx)
	namespaces, err := rd.GetE2ENamespaces(ctx, tc.KubeClientset)
	tc.asserter.Expect(err).To(gomega.Succeed())

	for _, namespace := range namespaces.Items {
		err = rd.DeleteNamespace(ctx, tc.KubeClientset, namespace.Name)
		tc.asserter.Expect(err).To(gomega.Succeed())
	}

	wait.ForNamespacesToBeDeleted(ctx, tc.ControllerClient, lo.Map(namespaces.Items, func(item corev1.Namespace, _ int) string {
		return item.Name
	}))

	tc.deleteAllQueues(ctx)

	err = rd.DeleteAllStorageObjects(ctx, tc.ControllerClient)
	tc.asserter.Expect(err).To(gomega.Succeed())
}

func (tc *TestContext) deleteAllObjectsInNamespace(ctx context.Context, namespace string) {
	err := rd.DeleteAllJobsInNamespace(ctx, tc.ControllerClient, namespace)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = rd.DeleteAllPodsInNamespace(ctx, tc.ControllerClient, namespace)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = pod_group.DeleteAllInNamespace(ctx, tc.ControllerClient, namespace)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = rd.DeleteAllConfigMapsInNamespace(ctx, tc.ControllerClient, namespace)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func (tc *TestContext) deleteAllQueues(ctx context.Context) {
	queues, err := queue.GetAllQueues(tc.KubeAiSchedClientset, ctx)
	tc.asserter.Expect(err).To(gomega.Succeed())
	for _, clusterQueue := range queues.Items {
		err = queue.Delete(tc.KubeAiSchedClientset, ctx, clusterQueue.Name,
			metav1.DeleteOptions{})
		tc.asserter.Expect(err).To(gomega.Succeed())
	}
}
