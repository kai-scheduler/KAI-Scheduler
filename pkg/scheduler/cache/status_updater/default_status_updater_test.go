// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	faketesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	kaifake "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	kubeaischedfake "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	fakeschedulingv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/typed/scheduling/v2alpha2/fake"
	kaiv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type UpdatePodGroupConditionTest struct {
	name                string
	podGroup            *kaiv2alpha2.PodGroup
	schedulingCondition *kaiv2alpha2.SchedulingCondition
	expectedConditions  []kaiv2alpha2.SchedulingCondition
	expectedUpdated     bool
}

func TestUpdatePodGroupSchedulingCondition(t *testing.T) {
	for i, test := range []UpdatePodGroupConditionTest{
		{
			name: "No conditions",
			podGroup: &kaiv2alpha2.PodGroup{
				Status: kaiv2alpha2.PodGroupStatus{
					SchedulingConditions: []kaiv2alpha2.SchedulingCondition{},
				},
			},
			schedulingCondition: &kaiv2alpha2.SchedulingCondition{
				Type:               kaiv2alpha2.UnschedulableOnNodePool,
				NodePool:           "default",
				Reason:             "reason",
				Message:            "message",
				TransitionID:       "0",
				LastTransitionTime: metav1.Time{},
				Status:             v1.ConditionTrue,
			},
			expectedConditions: []kaiv2alpha2.SchedulingCondition{
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "default",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "1",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
			},

			expectedUpdated: true,
		},
		{
			name: "Correct transition ID",
			podGroup: &kaiv2alpha2.PodGroup{
				Status: kaiv2alpha2.PodGroupStatus{
					SchedulingConditions: []kaiv2alpha2.SchedulingCondition{
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "existingConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "99",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
					},
				},
			},
			schedulingCondition: &kaiv2alpha2.SchedulingCondition{
				Type:               kaiv2alpha2.UnschedulableOnNodePool,
				NodePool:           "default",
				Reason:             "reason",
				Message:            "message",
				TransitionID:       "0",
				LastTransitionTime: metav1.Time{},
				Status:             v1.ConditionTrue,
			},
			expectedConditions: []kaiv2alpha2.SchedulingCondition{
				{
					Type:         kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:     "existingConditionNodepool",
					Reason:       "reason",
					Message:      "message",
					TransitionID: "99",
					Status:       v1.ConditionTrue,
				},
				{
					Type:         kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:     "default",
					Reason:       "reason",
					Message:      "message",
					TransitionID: "100",
					Status:       v1.ConditionTrue,
				},
			},

			expectedUpdated: true,
		},
		{
			name: "Override existing condition",
			podGroup: &kaiv2alpha2.PodGroup{
				Status: kaiv2alpha2.PodGroupStatus{
					SchedulingConditions: []kaiv2alpha2.SchedulingCondition{
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "existingConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "1",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "newerConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "2",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
					},
				},
			},
			schedulingCondition: &kaiv2alpha2.SchedulingCondition{
				Type:               kaiv2alpha2.UnschedulableOnNodePool,
				NodePool:           "existingConditionNodepool",
				Reason:             "reason",
				Message:            "message",
				TransitionID:       "0",
				LastTransitionTime: metav1.Time{},
				Status:             v1.ConditionTrue,
			},
			expectedConditions: []kaiv2alpha2.SchedulingCondition{
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "newerConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "2",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "existingConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "3",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
			},

			expectedUpdated: true,
		},
		{
			name: "Don't update if not necessary",
			podGroup: &kaiv2alpha2.PodGroup{
				Status: kaiv2alpha2.PodGroupStatus{
					SchedulingConditions: []kaiv2alpha2.SchedulingCondition{
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "newerConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "2",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "existingConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "3",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
					},
				},
			},
			schedulingCondition: &kaiv2alpha2.SchedulingCondition{
				Type:               kaiv2alpha2.UnschedulableOnNodePool,
				NodePool:           "existingConditionNodepool",
				Reason:             "reason",
				Message:            "message",
				TransitionID:       "0",
				LastTransitionTime: metav1.Time{},
				Status:             v1.ConditionTrue,
			},
			expectedConditions: []kaiv2alpha2.SchedulingCondition{
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "newerConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "2",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "existingConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "3",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
			},

			expectedUpdated: false,
		},
		{
			name: "Update even if just the order is wrong - latest condition by transition ID should be last in the list",
			podGroup: &kaiv2alpha2.PodGroup{
				Status: kaiv2alpha2.PodGroupStatus{
					SchedulingConditions: []kaiv2alpha2.SchedulingCondition{
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "existingConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "3",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "newerConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "2",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
					},
				},
			},
			schedulingCondition: &kaiv2alpha2.SchedulingCondition{
				Type:               kaiv2alpha2.UnschedulableOnNodePool,
				NodePool:           "existingConditionNodepool",
				Reason:             "reason",
				Message:            "message",
				TransitionID:       "0",
				LastTransitionTime: metav1.Time{},
				Status:             v1.ConditionTrue,
			},
			expectedConditions: []kaiv2alpha2.SchedulingCondition{
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "newerConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "2",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "existingConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "4",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
			},

			expectedUpdated: true,
		},
		{
			name: "Squash conditions",
			podGroup: &kaiv2alpha2.PodGroup{
				Status: kaiv2alpha2.PodGroupStatus{
					SchedulingConditions: []kaiv2alpha2.SchedulingCondition{
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "existingConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "1",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "newerConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "2",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
						{
							Type:               kaiv2alpha2.UnschedulableOnNodePool,
							NodePool:           "existingConditionNodepool",
							Reason:             "reason",
							Message:            "message",
							TransitionID:       "3",
							LastTransitionTime: metav1.Time{},
							Status:             v1.ConditionTrue,
						},
					},
				},
			},
			schedulingCondition: &kaiv2alpha2.SchedulingCondition{
				Type:               kaiv2alpha2.UnschedulableOnNodePool,
				NodePool:           "existingConditionNodepool",
				Reason:             "reason",
				Message:            "message",
				TransitionID:       "0",
				LastTransitionTime: metav1.Time{},
				Status:             v1.ConditionTrue,
			},
			expectedConditions: []kaiv2alpha2.SchedulingCondition{
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "newerConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "2",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
				{
					Type:               kaiv2alpha2.UnschedulableOnNodePool,
					NodePool:           "existingConditionNodepool",
					Reason:             "reason",
					Message:            "message",
					TransitionID:       "4",
					LastTransitionTime: metav1.Time{},
					Status:             v1.ConditionTrue,
				},
			},

			expectedUpdated: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test %d: %s", i, test.name)
			updatedPodGroup := setPodGroupSchedulingCondition(test.podGroup, test.schedulingCondition)
			assert.Equal(t, test.expectedUpdated, updatedPodGroup)
			assertPodGroupConditions(t, test.podGroup.Status.SchedulingConditions, test.expectedConditions)
		})
	}
}

func assertPodGroupConditions(t *testing.T, actualConditions, expectedConditions []kaiv2alpha2.SchedulingCondition) {
	assert.Equal(t, len(expectedConditions), len(actualConditions))
	for i, expectedCondition := range expectedConditions {
		assert.Equal(t, expectedCondition.Status, actualConditions[i].Status)
		assert.Equal(t, expectedCondition.Type, actualConditions[i].Type)
		assert.Equal(t, expectedCondition.NodePool, actualConditions[i].NodePool)
		assert.Equal(t, expectedCondition.Reason, actualConditions[i].Reason)
		assert.Equal(t, expectedCondition.Message, actualConditions[i].Message)
		assert.Equal(t, expectedCondition.TransitionID, actualConditions[i].TransitionID)
	}
}

type UpdatePodGroupStaleTimeStampTest struct {
	name               string
	podGroup           *kaiv2alpha2.PodGroup
	staleTimeStamp     *time.Time
	expectedAnnotation *string
	expectedUpdated    bool
}

func TestUpdatePodGroupStaleTimeStamp(t *testing.T) {
	for i, test := range []UpdatePodGroupStaleTimeStampTest{
		{
			name: "No stale timestamp and no need to update",
			podGroup: &kaiv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{},
			},
			staleTimeStamp:     nil,
			expectedAnnotation: nil,
			expectedUpdated:    false,
		},
		{
			name: "Stale timestamp and need to remove",
			podGroup: &kaiv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						commonconstants.StalePodgroupTimeStamp: "2021-01-01T00:00:00Z",
					},
				},
			},
			staleTimeStamp:     nil,
			expectedAnnotation: nil,
			expectedUpdated:    true,
		},
		{
			name: "No stale timestamp and need to add",
			podGroup: &kaiv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{},
			},
			staleTimeStamp:     getTimePointer("2021-01-01T00:00:00Z"),
			expectedAnnotation: ptr.To("2021-01-01T00:00:00Z"),
			expectedUpdated:    true,
		},
		{
			name: "Existing stale timestamp, no need to update",
			podGroup: &kaiv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						commonconstants.StalePodgroupTimeStamp: "2021-01-01T00:00:00Z",
					},
				},
			},
			staleTimeStamp:     getTimePointer("2021-01-01T00:00:00Z"),
			expectedAnnotation: ptr.To("2021-01-01T00:00:00Z"),
			expectedUpdated:    false,
		},
		{
			name: "Existing stale timestamp, need to update",
			podGroup: &kaiv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						commonconstants.StalePodgroupTimeStamp: "2020-01-01T00:00:00Z",
					},
				},
			},
			staleTimeStamp:     getTimePointer("2021-01-01T00:00:00Z"),
			expectedAnnotation: ptr.To("2021-01-01T00:00:00Z"),
			expectedUpdated:    true,
		},
		{
			name: "Existing invalid value, need to update",
			podGroup: &kaiv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						commonconstants.StalePodgroupTimeStamp: "quick brown fox",
					},
				},
			},
			staleTimeStamp:     getTimePointer("2021-01-01T00:00:00Z"),
			expectedAnnotation: ptr.To("2021-01-01T00:00:00Z"),
			expectedUpdated:    true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test %d: %s", i, test.name)
			updatedPodGroup := setPodGroupStaleTimeStamp(test.podGroup, test.staleTimeStamp)

			if test.expectedUpdated {
				assert.True(t, updatedPodGroup, "Expected pod group to be updated")
			} else {
				assert.False(t, updatedPodGroup, "Expected pod group not to be updated")
			}

			value, found := test.podGroup.Annotations[commonconstants.StalePodgroupTimeStamp]
			if test.expectedAnnotation == nil {
				assert.False(t, found, "Expected annotation not to be found")
			} else {
				assert.Equal(t, *test.expectedAnnotation, value, "Expected annotation value")
			}
		})
	}
}

func getTimePointer(ts string) *time.Time {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return &t
}

type SimpleRecorder struct {
	events []string
}

func (r *SimpleRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	r.events = append(r.events, eventtype+":"+reason+":"+message)
}

func (r *SimpleRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	r.events = append(r.events, eventtype+":"+reason+":"+messageFmt)
}

func (r *SimpleRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	r.events = append(r.events, eventtype+":"+reason+":"+messageFmt)
}

func TestDefaultStatusUpdater_RecordJobStatusEvent(t *testing.T) {
	tests := []struct {
		name                          string
		job                           jobs_fake.TestJobBasic
		numPodGroupStatusUpdateCalled int
		expectedEventActions          []string
		expectedInFlightPodGroups     int
	}{
		{
			name: "Running job",
			job: jobs_fake.TestJobBasic{
				Name:         "test-job",
				Namespace:    "test-ns",
				QueueName:    "test-queue",
				MinAvailable: ptr.To(int32(1)),
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						Name:  "test-task",
						State: pod_status.Running,
					},
				},
			},
			expectedEventActions:      []string{},
			expectedInFlightPodGroups: 1,
		},
		{
			name: "No ready job",
			job: jobs_fake.TestJobBasic{
				Name:         "test-job",
				Namespace:    "test-ns",
				QueueName:    "test-queue",
				MinAvailable: ptr.To(int32(2)),
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						Name:  "test-task",
						State: pod_status.Pending,
					},
				},
			},
			expectedEventActions:      []string{"Normal NotReady Job is not ready for scheduling. Waiting for 2 pods, currently 1 exist, 0 are gated"},
			expectedInFlightPodGroups: 0,
		},
		{
			name: "Unscheduleable job",
			job: jobs_fake.TestJobBasic{
				Name:         "test-job",
				Namespace:    "test-ns",
				QueueName:    "test-queue",
				MinAvailable: ptr.To(int32(1)),
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						Name:  "test-task",
						State: pod_status.Pending,
					},
				},
			},
			numPodGroupStatusUpdateCalled: 1,
			expectedEventActions:          []string{"Warning Unschedulable Unable to schedule pod", "Normal Unschedulable Unable to schedule podgroup"},
			expectedInFlightPodGroups:     1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var podGroups []runtime.Object
			jobInfos, _, _ := jobs_fake.BuildJobsAndTasksMaps([]*jobs_fake.TestJobBasic{&test.job})
			for _, job := range jobInfos {
				podGroups = append(podGroups, job.PodGroup)
			}

			kubeClient := fake.NewSimpleClientset()
			kubeAiSchedClient := kaifake.NewSimpleClientset(podGroups...)
			recorder := record.NewFakeRecorder(100)
			statusUpdater := New(kubeClient, kubeAiSchedClient, recorder, 1, false, nodePoolLabelKey)
			wg := sync.WaitGroup{}
			if test.numPodGroupStatusUpdateCalled > 0 {
				wg.Add(test.numPodGroupStatusUpdateCalled)
			}
			finishUpdatesChan := make(chan struct{})
			// wait with pod groups update until signal is given.
			kubeAiSchedClient.SchedulingV2alpha2().(*fakeschedulingv2alpha2.FakeSchedulingV2alpha2).PrependReactor(
				"update", "podgroups", func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
					<-finishUpdatesChan
					wg.Done()
					return false, nil, nil
				},
			)

			stopCh := make(chan struct{})
			statusUpdater.Run(stopCh)

			statusUpdater.RecordJobStatusEvent(jobInfos["test-job"])

			events := []string{}
			close(recorder.Events)
			for event := range recorder.Events {
				events = append(events, event)
			}
			assert.Equal(t, test.expectedEventActions, events)
			inFlightPodGroups := 0
			statusUpdater.inFlightPodGroups.Range(func(key, value any) bool {
				inFlightPodGroups += 1
				return true
			})
			assert.Equal(t, test.expectedInFlightPodGroups, inFlightPodGroups)

			close(finishUpdatesChan)
			wg.Wait()
			close(stopCh)
		})
	}
}

func TestDefaultStatusUpdater_RetryAfterError(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	kubeAiSchedClient := kubeaischedfake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(100)
	statusUpdater := New(kubeClient, kubeAiSchedClient, recorder, 1, false, nodePoolLabelKey)

	updateCalls := 0
	// wait with pod groups update until signal is given.
	kubeAiSchedClient.SchedulingV2alpha2().(*fakeschedulingv2alpha2.FakeSchedulingV2alpha2).PrependReactor(
		"update", "podgroups", func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
			updateCalls += 1
			return false, nil, errors.New("test")
		},
	)

	stopCh := make(chan struct{})
	statusUpdater.Run(stopCh)
	defer close(stopCh)

	job := &kaiv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "retry-test",
		},
		Status: kaiv2alpha2.PodGroupStatus{},
	}
	jobCopy := job.DeepCopy()

	jobCopy.Status.SchedulingConditions = []kaiv2alpha2.SchedulingCondition{
		{
			TransitionID: "1",
			Type:         kaiv2alpha2.UnschedulableOnNodePool,
			NodePool:     "test",
			Reason:       "test",
			Message:      "test",
		},
	}

	patchData, err := getPodGroupPatch(job, jobCopy)
	assert.NoError(t, err)

	go func() {
		time.Sleep(time.Millisecond * 75)
		statusUpdater.pushToUpdateQueue(&updatePayload{
			key:        "test",
			objectType: "podgroup",
		}, &inflightUpdate{
			object:       job,
			patchData:    patchData,
			updateStatus: true,
			subResources: nil,
		})
	}()

	// Wait for an initial update call
	assert.NoError(t, waitForIncrease(&updateCalls), "failed to wait for initial update call")

	// Wait for a retry after error
	assert.NoError(t, waitForIncrease(&updateCalls), "update was not retried after error")
}

func waitForIncrease(callCount *int) error {
	originalValue := *callCount
	startTime := time.Now()
	timeout := time.Second * 5

	for time.Since(startTime) < timeout {
		if *callCount > originalValue {
			break
		}
		time.Sleep(time.Millisecond * 50)
	}

	if *callCount > originalValue {
		return nil
	}
	return errors.New("update calls did not increase")
}
