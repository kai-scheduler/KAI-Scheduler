// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	faketesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	kubeaischedfake "github.com/kai-scheduler/kai-scheduler/pkg/apis/client/clientset/versioned/fake"
	fakeschedulingv2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/client/clientset/versioned/typed/scheduling/v2alpha2/fake"
	schedulingv2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/utils"
)

var _ = Describe("Status Updater - Pod Groups Syncing", func() {
	var (
		kubeClient        *fake.Clientset
		kubeAiSchedClient *kubeaischedfake.Clientset
		statusUpdater     *defaultStatusUpdater

		stopCh             chan struct{}
		finishUpdatesChan  chan struct{}
		podGroupsOriginals []*schedulingv2alpha2.PodGroup
		wg                 sync.WaitGroup
	)

	BeforeEach(func() {
		kubeClient = fake.NewSimpleClientset()
		kubeAiSchedClient = kubeaischedfake.NewSimpleClientset()
		recorder := record.NewFakeRecorder(100)
		statusUpdater = New(kubeClient, kubeAiSchedClient, recorder, 4, false,
			nodePoolLabelKey)

		wg = sync.WaitGroup{}
		finishUpdatesChan = make(chan struct{})
		// wait with pod groups update until signal is given.
		kubeAiSchedClient.SchedulingV2alpha2().(*fakeschedulingv2alpha2.FakeSchedulingV2alpha2).PrependReactor(
			"update", "podgroups", func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
				<-finishUpdatesChan
				wg.Done()
				return false, nil, nil
			},
		)

		stopCh = make(chan struct{})
		statusUpdater.Run(stopCh)

		numberOfJobs := 10
		jobs := []*jobs_fake.TestJobBasic{}
		for i := 0; i < numberOfJobs; i++ {
			jobs = append(jobs, &jobs_fake.TestJobBasic{
				Name:      "job-" + strconv.Itoa(i),
				Namespace: "default",
				QueueName: "queue-1",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						Name:  "task-" + strconv.Itoa(i),
						State: pod_status.Pending,
					},
				},
			})
		}

		vectorMap := resource_info.NewResourceVectorMap()
		jobInfos, _, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
		podGroupsOriginals = []*schedulingv2alpha2.PodGroup{}

		for _, job := range jobInfos {
			// Can't merge the loops because the fake client is handling concurrency by using a single lock
			_, err := kubeAiSchedClient.SchedulingV2alpha2().PodGroups(job.Namespace).Create(nil, job.PodGroup, metav1.CreateOptions{})
			Expect(err).To(Succeed())
			podGroupsOriginals = append(podGroupsOriginals, job.PodGroup.DeepCopy())
		}

		for _, job := range jobInfos {
			wg.Add(1)
			if jobIndex, _ := strconv.Atoi(strings.Split(job.Name, "-")[0]); jobIndex%2 == 0 {
				job.StalenessInfo.TimeStamp = ptr.To(time.Now())
				job.StalenessInfo.Stale = true
				job.LastStartTimestamp = ptr.To(time.Now())
			}
			Expect(statusUpdater.RecordJobStatusEvent(job)).To(Succeed())
		}
	})

	AfterEach(func() {
		select {
		case <-finishUpdatesChan:
		default:
			close(finishUpdatesChan)
		}
		wg.Wait()
		close(stopCh)
	})

	It("Podgroup updates are not moved to applied before the payload processing is done", func() {
		appliedPodGroupUpdatesCount := 0
		statusUpdater.appliedPodGroupUpdates.Range(func(key any, value any) bool {
			appliedPodGroupUpdatesCount += 1
			return true
		})
		Expect(appliedPodGroupUpdatesCount).To(Equal(0))

		close(finishUpdatesChan)
		wg.Wait()
		time.Sleep(time.Second)

		appliedPodGroupUpdatesCount = 0
		statusUpdater.appliedPodGroupUpdates.Range(func(key any, value any) bool {
			appliedPodGroupUpdatesCount += 1
			return true
		})
		Expect(appliedPodGroupUpdatesCount).To(Equal(len(podGroupsOriginals)))

	})

	It("should update pod groups with in flight updates", func() {
		// check that the pods groups are now not updated anymore
		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsOriginals)
		for _, podGroup := range podGroupsOriginals {
			Expect(podGroup.Status.SchedulingConditions).NotTo(BeEmpty())
		}
	})

	It("should update pod groups with applied updates cache", func() {
		close(finishUpdatesChan)
		wg.Wait()
		time.Sleep(time.Second)

		appliedPodGroupUpdatesCount := 0
		statusUpdater.appliedPodGroupUpdates.Range(func(key any, value any) bool {
			appliedPodGroupUpdatesCount += 1
			return true
		})
		Expect(appliedPodGroupUpdatesCount).To(Equal(len(podGroupsOriginals)))

		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsOriginals)
		for _, podGroup := range podGroupsOriginals {
			Expect(podGroup.Status.SchedulingConditions).NotTo(BeEmpty())
		}
	})

	It("should NOT clear update cache if update was needed", func() {
		podGroupsOriginalsCopy := make([]*schedulingv2alpha2.PodGroup, len(podGroupsOriginals))
		for i := range podGroupsOriginals {
			podGroupsOriginalsCopy[i] = podGroupsOriginals[i].DeepCopy()
		}
		// check that the pod groups are updated with the changes that are not yet applied
		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsOriginalsCopy)
		for _, podGroup := range podGroupsOriginalsCopy {
			Expect(podGroup.Status.SchedulingConditions).NotTo(BeEmpty())
		}

		// check that the pods groups are still updated
		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsOriginals)
		for _, podGroup := range podGroupsOriginals {
			Expect(podGroup.Status.SchedulingConditions).NotTo(BeEmpty())
		}
	})

	It("should clear update cache after it syncs with pods groups that are updated", func() {
		close(finishUpdatesChan)
		wg.Wait()

		podGroupsList, _ := kubeAiSchedClient.SchedulingV2alpha2().PodGroups("default").List(context.TODO(), metav1.ListOptions{})
		podGroupsFromCluster := make([]*schedulingv2alpha2.PodGroup, 0, len(podGroupsList.Items))
		for _, podGroup := range podGroupsList.Items {
			podGroupsFromCluster = append(podGroupsFromCluster, podGroup.DeepCopy())
		}

		Eventually(func() int {
			numberOfPodGroupInFlight := 0
			statusUpdater.inFlightPodGroups.Range(func(key any, value any) bool {
				numberOfPodGroupInFlight += 1
				return true
			})
			return numberOfPodGroupInFlight
		}, time.Second*20, time.Millisecond*50).Should(Equal(0))

		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsFromCluster)

		// check that the inFlight and applied pod groups cache are empty
		appliedPodGroupUpdatesCount := 0
		statusUpdater.appliedPodGroupUpdates.Range(func(key any, value any) bool {
			appliedPodGroupUpdatesCount += 1
			return true
		})
		Expect(appliedPodGroupUpdatesCount).To(Equal(0))

		// check that the pods groups are now not updated anymore
		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsOriginals)
		for _, podGroup := range podGroupsOriginals {
			Expect(podGroup.Status.SchedulingConditions).To(BeEmpty())
		}
	})

	It("should clear pod groups that don't show on sync from inFlight cache", func() {
		close(finishUpdatesChan)
		wg.Wait()

		statusUpdater.SyncPodGroupsWithPendingUpdates([]*schedulingv2alpha2.PodGroup{})

		// check that the pods groups are now not updated anymore
		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsOriginals)
		for _, podGroup := range podGroupsOriginals {
			Expect(podGroup.Status.SchedulingConditions).To(BeEmpty())
		}
	})

	It("should accept newer pod group versions as synced", func() {
		close(finishUpdatesChan)
		wg.Wait()

		podGroupsList, _ := kubeAiSchedClient.SchedulingV2alpha2().PodGroups("default").List(context.TODO(), metav1.ListOptions{})
		podGroupsFromCluster := make([]*schedulingv2alpha2.PodGroup, 0, len(podGroupsList.Items))
		for _, podGroup := range podGroupsList.Items {
			podGroupCopy := podGroup.DeepCopy()
			lastTransitionIdStr := utils.GetLastSchedulingCondition(podGroupCopy).TransitionID
			lastTransitionId, _ := strconv.Atoi(lastTransitionIdStr)
			podGroupCopy.Status.SchedulingConditions = append(
				podGroupCopy.Status.SchedulingConditions,
				schedulingv2alpha2.SchedulingCondition{
					Type:               schedulingv2alpha2.UnschedulableOnNodePool,
					NodePool:           "other",
					Reason:             "test",
					Message:            "test",
					TransitionID:       "0" + strconv.Itoa(lastTransitionId+1),
					LastTransitionTime: metav1.Now(),
					Status:             v1.ConditionTrue,
				},
			)
			podGroupsFromCluster = append(podGroupsFromCluster, podGroupCopy)
		}

		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsFromCluster)

		// check that the inFlight and applied pod groups cache are empty - seen newer
		appliedPodGroupUpdatesCount := 0
		statusUpdater.appliedPodGroupUpdates.Range(func(key any, value any) bool {
			appliedPodGroupUpdatesCount += 1
			return true
		})
		Expect(appliedPodGroupUpdatesCount).To(Equal(0))
		numberOfPodGroupInFlight := 0
		statusUpdater.inFlightPodGroups.Range(func(key any, value any) bool {
			numberOfPodGroupInFlight += 1
			return true
		})
		Expect(numberOfPodGroupInFlight).To(Equal(0))

		// check that the pods groups are now not updated anymore
		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsOriginals)
		for _, podGroup := range podGroupsOriginals {
			Expect(podGroup.Status.SchedulingConditions).To(BeEmpty())
		}
	})
})
