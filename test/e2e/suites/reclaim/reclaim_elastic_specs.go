/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeReclaimElasticSpecs() bool {
	return Describe("Reclaim with Elastic Jobs", Ordered, func() {
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

		It("Reclaim tasks from elastic jobs", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue = CreateQueues(4, 2, 2)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})
			reclaimeeNamespace = queue.GetConnectedNamespaceToQueue(reclaimeeQueue)

			reclaimeePodRequirements := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			}

			reclaimeeJob1, _, pods1, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimeeQueue,
				rd.DistributedBatchJobOptions{
					Parallelism:  ptr.To(int32(2)),
					MinMember:    ptr.To(int32(1)),
					BackoffLimit: ptr.To(int32(0)),
					NamePrefix:   "elastic-reclaimee-1-",
					Resources:    reclaimeePodRequirements,
				})
			Expect(err).To(Succeed())

			reclaimeeJob2, _, pods2, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, reclaimeeQueue,
				rd.DistributedBatchJobOptions{
					Parallelism:  ptr.To(int32(2)),
					MinMember:    ptr.To(int32(1)),
					BackoffLimit: ptr.To(int32(0)),
					NamePrefix:   "elastic-reclaimee-2-",
					Resources:    reclaimeePodRequirements,
				})
			Expect(err).To(Succeed())

			var preempteePods []*v1.Pod
			preempteePods = append(preempteePods, pods1...)
			preempteePods = append(preempteePods, pods2...)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, preempteePods)

			reclaimerPodRequirements := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("2"),
				},
			}

			reclaimerPod := rd.CreatePodObject(reclaimerQueue, reclaimerPodRequirements)
			reclaimerPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, reclaimerPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimerPod)

			wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
				pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", rd.JobNameLabel, reclaimeeJob1.Name),
				})
				Expect(err).To(Succeed())
				return len(pods.Items) == 1
			})

			wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
				pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", rd.JobNameLabel, reclaimeeJob2.Name),
				})
				Expect(err).To(Succeed())
				return len(pods.Items) == 1
			})
		})

		It("Reclaim entire elastic job", func(ctx context.Context) {
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
					Parallelism:  ptr.To(int32(2)),
					MinMember:    ptr.To(int32(1)),
					BackoffLimit: ptr.To(int32(0)),
					NamePrefix:   "elastic-reclaimee-",
					Resources:    reclaimeePodRequirements,
				})
			Expect(err).To(Succeed())
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, reclaimeePods)

			reclaimerPodRequirements := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("2"),
				},
			}
			reclaimerPod := rd.CreatePodObject(reclaimerQueue, reclaimerPodRequirements)
			reclaimerPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, reclaimerPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimerPod)

			wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
				pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", rd.JobNameLabel, reclaimeeJob.Name),
				})
				Expect(err).To(Succeed())
				return len(pods.Items) == 0
			})
		})
	})
}
