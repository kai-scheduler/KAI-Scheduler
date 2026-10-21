// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	faketesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	kubeaischedfake "github.com/kai-scheduler/api/client/clientset/versioned/fake"
	fakeschedulingv2alpha2 "github.com/kai-scheduler/api/client/clientset/versioned/typed/scheduling/v2alpha2/fake"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

var _ = Describe("Status Updater - pod group status conflict retry", func() {
	const (
		nodePool      = "pool-a"
		otherNodePool = "pool-b"
	)

	var (
		kaiClient         *kubeaischedfake.Clientset
		statusUpdater     *defaultStatusUpdater
		updateStatusCalls int
		conflictsLeft     int
		transitionTime    metav1.Time
	)

	conflictError := func() error {
		return apierrors.NewConflict(
			schema.GroupResource{Group: "scheduling.run.ai", Resource: "podgroups"}, "test-pg",
			errors.New("the object has been modified; please apply your changes to the latest version"))
	}

	// server state after the podgroup-controller and another node pool's scheduler wrote to it
	latestPodGroup := func() *enginev2alpha2.PodGroup {
		return &enginev2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pg", Namespace: "test-ns", UID: "test-uid", Generation: 1,
				ResourceVersion: "2",
				Labels:          map[string]string{nodePoolLabelKey: nodePool},
			},
			Status: enginev2alpha2.PodGroupStatus{
				SchedulingConditions: []enginev2alpha2.SchedulingCondition{{
					Type: enginev2alpha2.UnschedulableOnNodePool, NodePool: otherNodePool, TransitionID: "5",
					Reason: "other", Message: "other", Status: v1.ConditionTrue,
				}},
				ResourcesStatus: enginev2alpha2.PodGroupResourcesStatus{
					Requested: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")},
				},
			},
		}
	}

	// queues an Unschedulable verdict computed from a snapshot older than latestPodGroup
	queueVerdict := func() (updatePayloadKey, *inflightUpdate) {
		snapshot := latestPodGroup()
		snapshot.ResourceVersion = "1"
		snapshot.Status = enginev2alpha2.PodGroupStatus{
			SchedulingConditions: []enginev2alpha2.SchedulingCondition{{
				Type: enginev2alpha2.UnschedulableOnNodePool, NodePool: nodePool, TransitionID: "1",
				Reason: enginev2alpha2.PodGroupReasonUnschedulable, Message: "not enough GPUs",
				Status: v1.ConditionTrue, LastTransitionTime: transitionTime,
			}},
		}
		key := statusUpdater.keyForPodGroupPayload(snapshot.Name, snapshot.Namespace, snapshot.UID)
		update := &inflightUpdate{
			object:       snapshot,
			updateStatus: true,
			statusIntent: &podGroupStatusIntent{
				uid: snapshot.UID, generation: snapshot.Generation, nodePool: nodePool,
				condition: snapshot.Status.SchedulingConditions[0].DeepCopy(),
			},
		}
		statusUpdater.inFlightPodGroups.Store(key, update)
		return key, update
	}

	storedPodGroup := func() *enginev2alpha2.PodGroup {
		podGroup, err := kaiClient.SchedulingV2alpha2().PodGroups("test-ns").Get(
			context.Background(), "test-pg", metav1.GetOptions{})
		Expect(err).To(Succeed())
		return podGroup
	}

	createLatest := func(mutate func(*enginev2alpha2.PodGroup)) {
		podGroup := latestPodGroup()
		if mutate != nil {
			mutate(podGroup)
		}
		Expect(kaiClient.Tracker().Add(podGroup)).To(Succeed())
	}

	BeforeEach(func() {
		kaiClient = kubeaischedfake.NewSimpleClientset()
		statusUpdater = New(fake.NewSimpleClientset(), kaiClient, record.NewFakeRecorder(100), 1, false,
			nodePoolLabelKey)
		updateStatusCalls = 0
		conflictsLeft = 1
		transitionTime = metav1.NewTime(time.Now().Add(-time.Minute).Truncate(time.Second))

		kaiClient.SchedulingV2alpha2().(*fakeschedulingv2alpha2.FakeSchedulingV2alpha2).PrependReactor(
			"update", "podgroups", func(action faketesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() != "status" {
					return false, nil, nil
				}
				updateStatusCalls++
				if conflictsLeft > 0 {
					conflictsLeft--
					return true, nil, conflictError()
				}
				return false, nil, nil
			},
		)
	})

	It("retries the verdict on the latest pod group", func() {
		createLatest(nil)
		key, update := queueVerdict()

		statusUpdater.updatePodGroup(context.Background(), key, update)

		Expect(updateStatusCalls).To(Equal(2))
		stored := storedPodGroup()
		Expect(stored.Status.ResourcesStatus.Requested).To(HaveKey(v1.ResourceCPU))
		Expect(stored.Status.SchedulingConditions).To(HaveLen(2))
		Expect(stored.Status.SchedulingConditions[0].NodePool).To(Equal(otherNodePool))

		verdict := stored.Status.SchedulingConditions[1]
		Expect(verdict.NodePool).To(Equal(nodePool))
		Expect(verdict.Message).To(Equal("not enough GPUs"))
		Expect(verdict.TransitionID).To(Equal("6"))
		Expect(verdict.LastTransitionTime.Equal(&transitionTime)).To(BeTrue())

		_, inFlight := statusUpdater.inFlightPodGroups.Load(key)
		Expect(inFlight).To(BeFalse())
		applied, found := statusUpdater.appliedPodGroupUpdates.Load(key)
		Expect(found).To(BeTrue())
		appliedUpdate := applied.(*inflightUpdate)
		Expect(appliedUpdate.object.(*enginev2alpha2.PodGroup).Status.SchedulingConditions).To(
			Equal(stored.Status.SchedulingConditions))
		Expect(appliedUpdate.writtenResourceVersions).To(ContainElements("2", stored.ResourceVersion))
	})

	It("replays the core pods of a semi-preemptible pod group", func() {
		createLatest(nil)
		key, update := queueVerdict()
		update.statusIntent.condition = nil
		update.statusIntent.schedulingState = &enginev2alpha2.PodGroupSchedulingState{CorePods: []string{"pod-0"}}

		statusUpdater.updatePodGroup(context.Background(), key, update)

		stored := storedPodGroup()
		Expect(stored.Status.SchedulingState).ToNot(BeNil())
		Expect(stored.Status.SchedulingState.CorePods).To(Equal([]string{"pod-0"}))
		Expect(stored.Status.SchedulingConditions).To(HaveLen(1))
		Expect(stored.Status.ResourcesStatus.Requested).To(HaveKey(v1.ResourceCPU))
	})

	DescribeTable("drops the update without resending it when the pod group changed",
		func(mutate func(*enginev2alpha2.PodGroup)) {
			createLatest(mutate)
			key, update := queueVerdict()

			statusUpdater.updatePodGroup(context.Background(), key, update)

			Expect(updateStatusCalls).To(Equal(1))
			_, inFlight := statusUpdater.inFlightPodGroups.Load(key)
			Expect(inFlight).To(BeFalse())
			_, applied := statusUpdater.appliedPodGroupUpdates.Load(key)
			Expect(applied).To(BeFalse())
		},
		Entry("recreated", func(pg *enginev2alpha2.PodGroup) { pg.UID = "new-uid" }),
		Entry("being deleted", func(pg *enginev2alpha2.PodGroup) {
			pg.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			pg.Finalizers = []string{"test"}
		}),
		Entry("spec changed", func(pg *enginev2alpha2.PodGroup) { pg.Generation = 2 }),
		Entry("moved to another node pool", func(pg *enginev2alpha2.PodGroup) {
			pg.Labels[nodePoolLabelKey] = otherNodePool
		}),
		Entry("newer verdict for the same node pool", func(pg *enginev2alpha2.PodGroup) {
			pg.Status.SchedulingConditions = append(pg.Status.SchedulingConditions, enginev2alpha2.SchedulingCondition{
				Type: enginev2alpha2.UnschedulableOnNodePool, NodePool: nodePool, TransitionID: "6",
				Reason: "newer", Message: "newer", Status: v1.ConditionTrue,
			})
		}),
	)

	It("drops the update when the pod group was deleted", func() {
		key, update := queueVerdict()

		statusUpdater.updatePodGroup(context.Background(), key, update)

		Expect(updateStatusCalls).To(Equal(1))
		_, inFlight := statusUpdater.inFlightPodGroups.Load(key)
		Expect(inFlight).To(BeFalse())
	})

	It("leaves a newer queued update in flight", func() {
		createLatest(nil)
		key, update := queueVerdict()
		newer := &inflightUpdate{object: update.object, updateStatus: true}
		statusUpdater.inFlightPodGroups.Store(key, newer)

		statusUpdater.updatePodGroup(context.Background(), key, update)

		Expect(updateStatusCalls).To(Equal(1))
		current, inFlight := statusUpdater.inFlightPodGroups.Load(key)
		Expect(inFlight).To(BeTrue())
		Expect(current).To(BeIdenticalTo(newer))
	})

	It("stops after a bounded number of conflicts", func() {
		createLatest(nil)
		conflictsLeft = 100
		key, update := queueVerdict()

		statusUpdater.updatePodGroup(context.Background(), key, update)

		// first write + retry.DefaultRetry's 5 steps
		Expect(updateStatusCalls).To(Equal(6))
		_, inFlight := statusUpdater.inFlightPodGroups.Load(key)
		Expect(inFlight).To(BeFalse())
		Expect(storedPodGroup().Status.SchedulingConditions).To(HaveLen(1))
	})

	Context("RecordJobStatusEvent", func() {
		BeforeEach(func() {
			// no workers, so queued updates stay in flight
			stopCh := make(chan struct{})
			go statusUpdater.queueBufferWorker(stopCh)
			DeferCleanup(func() { close(stopCh) })
		})

		buildJob := func(state pod_status.PodStatus) *podgroup_info.PodGroupInfo {
			jobInfos, _, _ := jobs_fake.BuildJobsAndTasksMaps([]*jobs_fake.TestJobBasic{{
				Name: "job-0", Namespace: "default", QueueName: "queue-1",
				Tasks: []*tasks_fake.TestTaskBasic{{Name: "task-0", State: state}},
			}}, resource_info.NewResourceVectorMap())
			for _, job := range jobInfos {
				return job
			}
			return nil
		}

		queuedUpdate := func(job *podgroup_info.PodGroupInfo) *inflightUpdate {
			key := statusUpdater.keyForPodGroupPayload(job.PodGroup.Name, job.PodGroup.Namespace, job.PodGroup.UID)
			update, found := statusUpdater.inFlightPodGroups.Load(key)
			Expect(found).To(BeTrue())
			return update.(*inflightUpdate)
		}

		It("records the node pool verdict as the update's intent", func() {
			job := buildJob(pod_status.Pending)

			Expect(statusUpdater.RecordJobStatusEvent(job, nil)).To(Succeed())

			intent := queuedUpdate(job).statusIntent
			Expect(intent).ToNot(BeNil())
			Expect(intent.condition).ToNot(BeNil())
			Expect(intent.condition.Reason).To(Equal(enginev2alpha2.PodGroupReasonUnschedulable))
			Expect(intent.condition.TransitionID).To(Equal("1"))
		})

		It("records no intent for a clear", func() {
			job := buildJob(pod_status.Running)
			job.PodGroup.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{{
				Type: enginev2alpha2.UnschedulableOnNodePool, TransitionID: "1", Status: v1.ConditionTrue,
			}}

			Expect(statusUpdater.RecordJobStatusEvent(job, nil)).To(Succeed())

			update := queuedUpdate(job)
			Expect(update.updateStatus).To(BeTrue())
			Expect(update.statusIntent).To(BeNil())
		})
	})
})
