/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package leader_worker_set

import (
	"context"
	"maps"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	lws "sigs.k8s.io/lws/api/leaderworkerset/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	v2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	lwsCrdName    = "leaderworkersets.leaderworkerset.x-k8s.io"
	lwsCrdVersion = "v1"
)

var _ = Describe("Leader worker set Integration", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, lwsCrdName, lwsCrdVersion)
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	It("Leader worker set - 2 groups", func(ctx context.Context) {
		singleGPURequest := v1.ResourceRequirements{
			Limits: v1.ResourceList{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
			Requests: v1.ResourceList{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		numOfGroups := 2
		numWorkersReplicasPerGroup := 2

		lwsJob, expectedPodsPerGrop := createExampleLWS(
			testCtx.Queues[0], numOfGroups,
			singleGPURequest, singleGPURequest, numWorkersReplicasPerGroup,
		)
		Expect(testCtx.ControllerClient.Create(ctx, lwsJob)).To(Succeed())
		defer func() {
			Expect(testCtx.ControllerClient.Delete(ctx, lwsJob)).To(Succeed())
		}()

		Eventually(func(g Gomega) bool {
			podGroups := &v2alpha2.PodGroupList{}
			testCtx.ControllerClient.List(ctx, podGroups, runtimeClient.InNamespace(lwsJob.Namespace))

			g.Expect(len(podGroups.Items)).To(Equal(numOfGroups))
			for _, podGroup := range podGroups.Items {
				g.Expect(podGroup.Spec.MinMember).To(Equal(int32(numWorkersReplicasPerGroup + 1)))
			}
			return true
		}, time.Minute).Should(BeTrue())
		Eventually(func(g Gomega) bool {
			pods := &v1.PodList{}
			testCtx.ControllerClient.List(ctx, pods, runtimeClient.InNamespace(lwsJob.Namespace))

			g.Expect(len(pods.Items)).To(Equal(expectedPodsPerGrop * numOfGroups))
			for _, pod := range pods.Items {
				g.Expect(rd.IsPodReady(&pod)).To(BeTrue())
			}
			return true
		}, time.Minute).Should(BeTrue())
	})

	It("Leader worker set - handle leader ready before workers startup policy", func(ctx context.Context) {
		singleGPURequest := v1.ResourceRequirements{
			Limits: v1.ResourceList{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
			Requests: v1.ResourceList{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		numOfGroups := 2
		numWorkersReplicasPerGroup := 2

		lwsJob, expectedPodsPerGrop := createExampleLWS(
			testCtx.Queues[0], numOfGroups,
			singleGPURequest, singleGPURequest, numWorkersReplicasPerGroup,
		)
		lwsJob.Spec.StartupPolicy = lws.LeaderReadyStartupPolicy
		Expect(testCtx.ControllerClient.Create(ctx, lwsJob)).To(Succeed())
		defer func() {
			Expect(testCtx.ControllerClient.Delete(ctx, lwsJob)).To(Succeed())
		}()

		Eventually(func(g Gomega) bool {
			pods := &v1.PodList{}
			testCtx.ControllerClient.List(ctx, pods, runtimeClient.InNamespace(lwsJob.Namespace))

			g.Expect(len(pods.Items)).To(Equal(expectedPodsPerGrop * numOfGroups))
			for _, pod := range pods.Items {
				g.Expect(rd.IsPodReady(&pod)).To(BeTrue())
			}
			return true
		}, time.Minute).Should(BeTrue())

		// At first the podgrpup min member is 1 per podGroup, later changes to numWorkersReplicasPerGroup + 1
		Eventually(func(g Gomega) bool {
			podGroups := &v2alpha2.PodGroupList{}
			testCtx.ControllerClient.List(ctx, podGroups, runtimeClient.InNamespace(lwsJob.Namespace))

			g.Expect(len(podGroups.Items)).To(Equal(numOfGroups))
			for _, podGroup := range podGroups.Items {
				g.Expect(podGroup.Spec.MinMember).To(Equal(int32(numWorkersReplicasPerGroup + 1)))
			}
			return true
		}, time.Minute).Should(BeTrue())
	})
})

func createExampleLWS(
	testQueue *v2.Queue,
	numOfGroups int,
	leadersRequest v1.ResourceRequirements,
	workersRequest v1.ResourceRequirements,
	numberOfWorkersReplicasPerGroup int,
) (*lws.LeaderWorkerSet, int) {

	sampleLeaderPod := rd.CreatePodObject(testQueue, leadersRequest)
	sampleLeaderPod.Spec.Containers[0].Name = "lws"
	sampleWorkerPod := rd.CreatePodObject(testQueue, workersRequest)
	sampleWorkerPod.Spec.Containers[0].Name = "lws"

	lws := &lws.LeaderWorkerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRandomK8sName(10),
			Namespace:   queue.GetConnectedNamespaceToQueue(testQueue),
			Labels:      maps.Clone(sampleLeaderPod.Labels),
			Annotations: maps.Clone(sampleLeaderPod.Annotations),
		},
		Spec: lws.LeaderWorkerSetSpec{
			Replicas:      ptr.To(int32(numOfGroups)),
			StartupPolicy: lws.LeaderCreatedStartupPolicy,
			LeaderWorkerTemplate: lws.LeaderWorkerTemplate{
				LeaderTemplate: &v1.PodTemplateSpec{
					ObjectMeta: sampleLeaderPod.ObjectMeta,
					Spec:       sampleLeaderPod.Spec,
				},
				WorkerTemplate: v1.PodTemplateSpec{
					ObjectMeta: sampleWorkerPod.ObjectMeta,
					Spec:       sampleWorkerPod.Spec,
				},
				Size: ptr.To(int32(numberOfWorkersReplicasPerGroup + 1)),
			},
		},
	}

	return lws, numberOfWorkersReplicasPerGroup + 1
}
