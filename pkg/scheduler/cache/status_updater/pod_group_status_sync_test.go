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

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/utils"
	kubeaischedfake "github.com/kai-scheduler/api/client/clientset/versioned/fake"
	fakeschedulingv2alpha2 "github.com/kai-scheduler/api/client/clientset/versioned/typed/scheduling/v2alpha2/fake"
	commonconstants "github.com/kai-scheduler/api/constants"
	schedulingv2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
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
			Expect(statusUpdater.RecordJobStatusEvent(job, nil)).To(Succeed())
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

	It("should drop applied updates once the cluster moved past the written object", func() {
		close(finishUpdatesChan)
		wg.Wait()
		Eventually(func() int {
			appliedPodGroupUpdatesCount := 0
			statusUpdater.appliedPodGroupUpdates.Range(func(key any, value any) bool {
				appliedPodGroupUpdatesCount += 1
				return true
			})
			return appliedPodGroupUpdatesCount
		}, 5*time.Second).Should(Equal(len(podGroupsOriginals)))

		// An external writer (e.g. pod-group-assigner) cleared the scheduling conditions after our update landed.
		podGroupsFromCluster := make([]*schedulingv2alpha2.PodGroup, 0, len(podGroupsOriginals))
		for _, podGroup := range podGroupsOriginals {
			podGroupCopy := podGroup.DeepCopy()
			podGroupCopy.ResourceVersion = "external-write"
			podGroupCopy.Status.SchedulingConditions = nil
			podGroupsFromCluster = append(podGroupsFromCluster, podGroupCopy)
		}

		statusUpdater.SyncPodGroupsWithPendingUpdates(podGroupsFromCluster)

		for _, podGroup := range podGroupsFromCluster {
			Expect(podGroup.Status.SchedulingConditions).To(BeEmpty())
		}
		appliedPodGroupUpdatesCount := 0
		statusUpdater.appliedPodGroupUpdates.Range(func(key any, value any) bool {
			appliedPodGroupUpdatesCount += 1
			return true
		})
		Expect(appliedPodGroupUpdatesCount).To(Equal(0))
	})
})

var _ = Describe("syncPodGroup annotations", func() {
	It("removes annotations missing from the in-flight pod group instead of blanking them", func() {
		su := &defaultStatusUpdater{}
		snapshot := &schedulingv2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			commonconstants.StalePodgroupTimeStamp: "2025-01-01T00:00:00Z",
			commonconstants.LastStartTimeStamp:     "2025-01-01T00:00:00Z",
		}}}
		inFlight := &schedulingv2alpha2.PodGroup{}

		su.syncPodGroup(inFlight, snapshot)

		Expect(snapshot.Annotations).ToNot(HaveKey(commonconstants.StalePodgroupTimeStamp))
		Expect(snapshot.Annotations).ToNot(HaveKey(commonconstants.LastStartTimeStamp))
	})
})

var _ = Describe("Status Updater - annotation removal across cycles", func() {
	It("does not build a remove patch for annotations the in-flight update already cleared", func() {
		kubeClient := fake.NewSimpleClientset()
		kubeAiSchedClient := kubeaischedfake.NewSimpleClientset()
		su := New(kubeClient, kubeAiSchedClient, record.NewFakeRecorder(100), 4, false, nodePoolLabelKey)

		finishUpdates := make(chan struct{})
		kubeAiSchedClient.SchedulingV2alpha2().(*fakeschedulingv2alpha2.FakeSchedulingV2alpha2).PrependReactor(
			"update", "podgroups", func(faketesting.Action) (bool, runtime.Object, error) {
				<-finishUpdates
				return false, nil, nil
			},
		)
		stopCh := make(chan struct{})
		su.Run(stopCh)
		defer close(stopCh)
		defer close(finishUpdates)

		vectorMap := resource_info.NewResourceVectorMap()
		jobInfos, _, _ := jobs_fake.BuildJobsAndTasksMaps([]*jobs_fake.TestJobBasic{{
			Name:      "job-0",
			Namespace: "default",
			QueueName: "queue-1",
			Tasks:     []*tasks_fake.TestTaskBasic{{Name: "task-0", State: pod_status.Pending}},
		}}, vectorMap)
		var job *podgroup_info.PodGroupInfo
		for _, j := range jobInfos {
			job = j
		}

		stale := time.Now().UTC().Format(time.RFC3339)
		job.PodGroup.Annotations = map[string]string{
			commonconstants.StalePodgroupTimeStamp: stale,
			commonconstants.LastStartTimeStamp:     stale,
		}
		// Informer view of the cluster, which still carries the annotations.
		informerPodGroup := job.PodGroup.DeepCopy()

		// Cycle N: the scheduler clears both annotations and the update stays in flight.
		job.StalenessInfo.TimeStamp = nil
		job.LastStartTimestamp = nil
		Expect(su.RecordJobStatusEvent(job, nil)).To(Succeed())
		Expect(job.PodGroup.Annotations).ToNot(HaveKey(commonconstants.StalePodgroupTimeStamp))

		// Cycle N+1: the informer snapshot is merged with the in-flight update.
		su.SyncPodGroupsWithPendingUpdates([]*schedulingv2alpha2.PodGroup{informerPodGroup})
		Expect(informerPodGroup.Annotations).ToNot(HaveKey(commonconstants.StalePodgroupTimeStamp))
		Expect(informerPodGroup.Annotations).ToNot(HaveKey(commonconstants.LastStartTimeStamp))

		// The job built from the synced snapshot must not try to remove keys the server no longer has.
		job.PodGroup = informerPodGroup
		patch, err := su.updatePodGroupAnnotations(job)
		Expect(err).To(Succeed())
		Expect(patch).To(BeEmpty())
	})
})
