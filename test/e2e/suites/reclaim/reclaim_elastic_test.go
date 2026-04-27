/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var _ = DescribeReclaimElasticSpecs()

var _ = Describe("Reclaim with Elastic Jobs", Ordered, func() {
	var (
		testCtx            *testcontext.TestContext
		parentQueue        *v2.Queue
		reclaimeeQueue     *v2.Queue
		reclaimerQueue     *v2.Queue
		reclaimeeNamespace string
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
			Gpu:      resource.MustParse("4"),
			PodCount: 5,
		})
	})

	AfterEach(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	It("Reclaim elastic job for a distributed job", func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue, reclaimeeQueue, reclaimerQueue = CreateQueues(2, 0, 2)
		reclaimeeQueue.Spec.Resources.GPU.OverQuotaWeight = 0
		testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})
		reclaimeeNamespace = queue.GetConnectedNamespaceToQueue(reclaimeeQueue)

		reclaimeePodRequirements := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		reclaimeeJob, _, reclaimeePods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimeeQueue,
			rd.DistributedBatchJobOptions{
				Parallelism: ptr.To(int32(2)),
				MinMember:   ptr.To(int32(1)),
				NamePrefix:  "elastic-reclaimee-",
				Resources:   reclaimeePodRequirements,
			})
		Expect(err).To(Succeed())
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, reclaimeePods)

		reclaimerPodRequirements := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		_, _, reclaimerPods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimerQueue,
			rd.DistributedBatchJobOptions{
				Parallelism: ptr.To(int32(2)),
				Resources:   reclaimerPodRequirements,
			})
		Expect(err).To(Succeed())
		reclaimerNamespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimerNamespace, reclaimerPods)

		wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
			pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", rd.JobNameLabel, reclaimeeJob.Name),
			})
			Expect(err).To(Succeed())
			return len(pods.Items) == 0
		})
	})

	It("Reclaim elastic job partially for a distributed job", func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue, reclaimeeQueue, reclaimerQueue = CreateQueues(4, 2, 2)
		reclaimeeQueue.Spec.Resources.GPU.OverQuotaWeight = 0
		testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})
		reclaimeeNamespace = queue.GetConnectedNamespaceToQueue(reclaimeeQueue)

		reclaimeePodRequirements := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		reclaimeeJob, reclaimeePodGroup, reclaimeePods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimeeQueue,
			rd.DistributedBatchJobOptions{
				Parallelism: ptr.To(int32(3)),
				MinMember:   ptr.To(int32(1)),
				NamePrefix:  "elastic-reclaimee-",
				Resources:   reclaimeePodRequirements,
			})
		Expect(err).To(Succeed())
		Expect(testCtx.ControllerClient.Patch(
			ctx, reclaimeePodGroup, client.RawPatch(types.JSONPatchType, []byte(`[{"op": "replace", "path": "/spec/minMember", "value": 2}]`)))).To(Succeed())
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, reclaimeePods)

		reclaimerPodRequirements := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		_, _, reclaimerPods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimerQueue,
			rd.DistributedBatchJobOptions{
				Parallelism: ptr.To(int32(2)),
				Resources:   reclaimerPodRequirements,
			})
		Expect(err).To(Succeed())
		reclaimerNamespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimerNamespace, reclaimerPods)

		wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
			pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", rd.JobNameLabel, reclaimeeJob.Name),
			})
			Expect(err).To(Succeed())
			return len(pods.Items) == 2
		})
	})

	It("Reclaim elastic job with min runtime protecting", func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue, reclaimeeQueue, reclaimerQueue = CreateQueues(3, 0, 3)
		reclaimeeQueue.Spec.Resources.GPU.OverQuotaWeight = 0
		reclaimeeQueue.Spec.ReclaimMinRuntime = &metav1.Duration{Duration: 90 * time.Second}
		testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})
		reclaimeeNamespace = queue.GetConnectedNamespaceToQueue(reclaimeeQueue)

		reclaimeePodRequirements := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		reclaimeeJob, _, reclaimeePods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimeeQueue,
			rd.DistributedBatchJobOptions{
				Parallelism: ptr.To(int32(3)),
				MinMember:   ptr.To(int32(1)),
				NamePrefix:  "elastic-reclaimee-",
				Resources:   reclaimeePodRequirements,
			})
		Expect(err).To(Succeed())
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, reclaimeePods)

		reclaimer1PodRequirements := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		_, _, reclaimer1Pods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimerQueue,
			rd.DistributedBatchJobOptions{
				Parallelism: ptr.To(int32(2)),
				NamePrefix:  "reclaimer1-",
				Resources:   reclaimer1PodRequirements,
			})
		Expect(err).To(Succeed())
		reclaimerNamespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimerNamespace, reclaimer1Pods)

		wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
			pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", rd.JobNameLabel, reclaimeeJob.Name),
			})
			Expect(err).To(Succeed())
			return len(pods.Items) == 1
		})

		reclaimer2PodRequirements := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		_, _, reclaimer2Pods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimerQueue,
			rd.DistributedBatchJobOptions{
				Parallelism: ptr.To(int32(1)),
				NamePrefix:  "reclaimer2-",
				Resources:   reclaimer2PodRequirements,
			})
		Expect(err).To(Succeed())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, reclaimer2Pods[0])
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimerNamespace, reclaimer2Pods)
	})
})
