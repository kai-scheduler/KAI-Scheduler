// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"

	kubeaischedulerfake "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
)

type test struct {
	name                             string
	podGroupMinMember                int   // if not set, uses len(pods)
	detailedErrors                   *bool // nil for default
	pods                             map[v1.PodPhase][]common_info.PodID
	nodeErrors                       map[common_info.PodID]map[string]string
	podErrors                        map[common_info.PodID]error
	jobErrors                        enginev2alpha2.UnschedulableExplanations
	expectedPodgroupErrorPatterns    []string
	expectedPodgroupConditionReasons []enginev2alpha2.UnschedulableReason
	expectedPodgroupEventPatterns    []string
	expectedPodErrorPatterns         map[common_info.PodID][]string // to assert no condition - specify podID with empty slice
	expectedPodEventPatterns         map[common_info.PodID][]string
}

func TestRecordJobStatusEvent(t *testing.T) {
	for _, tt := range []test{
		{
			name: "no errors - assert default error message",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors:                    map[common_info.PodID]map[string]string{},
			podErrors:                     map[common_info.PodID]error{},
			jobErrors:                     enginev2alpha2.UnschedulableExplanations{},
			expectedPodgroupErrorPatterns: []string{"Unable to schedule podgroup"},
			expectedPodgroupEventPatterns: []string{"Unable to schedule podgroup"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"Unable to schedule pod"},
			},
			expectedPodEventPatterns: map[common_info.PodID][]string{
				"pod-1": {"Unable to schedule pod"},
			},
		},
		{
			name: "job over capacity",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors:                    map[common_info.PodID]map[string]string{},
			podErrors:                     map[common_info.PodID]error{},
			jobErrors:                     newUnschedulabeReasons(map[string]string{podgroup_info.OverCapacity: "job is over capacity"}),
			expectedPodgroupErrorPatterns: []string{"job is over capacity"},
			expectedPodgroupEventPatterns: []string{"job is over capacity"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"job is over capacity"},
			},
			expectedPodEventPatterns: map[common_info.PodID][]string{
				"pod-1": {"job is over capacity"},
			},
		},
		{
			name: "generic job error",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors:                    map[common_info.PodID]map[string]string{},
			podErrors:                     map[common_info.PodID]error{},
			jobErrors:                     newUnschedulabeReasons(map[string]string{"some generic error": "generic error"}),
			expectedPodgroupErrorPatterns: []string{"generic error"},
			expectedPodgroupEventPatterns: []string{"generic error"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"generic error"},
			},
			expectedPodEventPatterns: map[common_info.PodID][]string{
				"pod-1": {"generic error"},
			},
		},
		{
			name: "pod errors",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{},
			podErrors: map[common_info.PodID]error{
				"pod-1": fmt.Errorf("pod is too fat for cluster"),
			},
			jobErrors:                     enginev2alpha2.UnschedulableExplanations{},
			expectedPodgroupErrorPatterns: []string{"Unable to schedule podgroup"},
			expectedPodgroupEventPatterns: []string{"Unable to schedule podgroup"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for cluster"},
			},
			expectedPodEventPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for cluster"},
			},
		},
		{
			name: "pod errors on single pod",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1", "pod-2"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{},
			podErrors: map[common_info.PodID]error{
				"pod-1": fmt.Errorf("pod is too fat for cluster"),
			},
			jobErrors:                     enginev2alpha2.UnschedulableExplanations{},
			expectedPodgroupErrorPatterns: []string{"Unable to schedule podgroup"},
			expectedPodgroupEventPatterns: []string{"Unable to schedule podgroup"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for cluster"},
				"pod-2": {},
			},
			expectedPodEventPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for cluster"},
				"pod-2": {},
			},
		},
		{
			name: "different pod errors on different pods",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1", "pod-2"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{},
			podErrors: map[common_info.PodID]error{
				"pod-1": fmt.Errorf("pod is too fat for cluster"),
				"pod-2": fmt.Errorf("pod is too heavy for cluster"),
			},
			jobErrors:                     enginev2alpha2.UnschedulableExplanations{},
			expectedPodgroupErrorPatterns: []string{"Unable to schedule podgroup"},
			expectedPodgroupEventPatterns: []string{"Unable to schedule podgroup"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for cluster"},
				"pod-2": {"pod is too heavy for cluster"},
			},
			expectedPodEventPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for cluster"},
				"pod-2": {"pod is too heavy for cluster"},
			},
		},
		{
			name: "node fit errors",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{
				"pod-1": {"node-1": "pod is too fat for node"},
			},
			podErrors:                     map[common_info.PodID]error{},
			jobErrors:                     enginev2alpha2.UnschedulableExplanations{},
			expectedPodgroupErrorPatterns: []string{"Unable to schedule podgroup"},
			expectedPodgroupEventPatterns: []string{"Unable to schedule podgroup"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for node"},
			},
		},
		{
			name: "node fit errors on single pod",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1", "pod-2"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{
				"pod-1": {"node-1": "pod is too fat for node"},
			},
			podErrors:                     map[common_info.PodID]error{},
			jobErrors:                     enginev2alpha2.UnschedulableExplanations{},
			expectedPodgroupErrorPatterns: []string{"Unable to schedule podgroup"},
			expectedPodgroupEventPatterns: []string{"Unable to schedule podgroup"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for node"},
				"pod-2": {},
			},
		},
		{
			name: "different node fit errors on different pods",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1", "pod-2"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{
				"pod-1": {"node-1": "pod is too fat for node"},
				"pod-2": {"node-2": "different error"},
			},
			podErrors:                     map[common_info.PodID]error{},
			jobErrors:                     enginev2alpha2.UnschedulableExplanations{},
			expectedPodgroupErrorPatterns: []string{"Unable to schedule podgroup"},
			expectedPodgroupEventPatterns: []string{"Unable to schedule podgroup"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for node"},
				"pod-2": {"different error"},
			},
		},
		{
			name: "job errors and pod errors",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1", "pod-2"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{
				"pod-1": {"node-1": "pod is too fat for node"},
				"pod-2": {"node-2": "different error"},
			},
			podErrors:                     map[common_info.PodID]error{},
			jobErrors:                     newUnschedulabeReasons(map[string]string{podgroup_info.OverCapacity: "job is over capacity"}),
			expectedPodgroupErrorPatterns: []string{"job is over capacity"},
			expectedPodgroupEventPatterns: []string{"job is over capacity"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"pod is too fat for node"},
				"pod-2": {"different error"},
			},
		},
		{
			name: "condition explanation contains reason",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors:                       map[common_info.PodID]map[string]string{},
			podErrors:                        map[common_info.PodID]error{},
			jobErrors:                        newUnschedulabeReasons(map[string]string{podgroup_info.OverCapacity: "job is over capacity"}),
			expectedPodgroupErrorPatterns:    []string{"job is over capacity"},
			expectedPodgroupConditionReasons: []enginev2alpha2.UnschedulableReason{podgroup_info.OverCapacity},
			expectedPodgroupEventPatterns:    []string{"job is over capacity"},
			expectedPodErrorPatterns:         map[common_info.PodID][]string{},
		},
		{
			name: "condition explanation contains multiple reasons",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors: map[common_info.PodID]map[string]string{},
			podErrors:  map[common_info.PodID]error{},
			jobErrors: newUnschedulabeReasons(map[string]string{
				podgroup_info.OverCapacity: "job is over capacity",
				"someOtherReason":          "some other reason",
			}),
			expectedPodgroupErrorPatterns:    []string{"job is over capacity"},
			expectedPodgroupConditionReasons: []enginev2alpha2.UnschedulableReason{podgroup_info.OverCapacity, "someOtherReason"},
			expectedPodgroupEventPatterns:    []string{"job is over capacity"},
			expectedPodErrorPatterns:         map[common_info.PodID][]string{},
		},
		{
			name: "condition explanation contains single nodepool prefix",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1", "pod-2"},
			},
			nodeErrors:                       map[common_info.PodID]map[string]string{},
			podErrors:                        map[common_info.PodID]error{},
			jobErrors:                        newUnschedulabeReasons(map[string]string{podgroup_info.OverCapacity: "job is over capacity"}),
			expectedPodgroupErrorPatterns:    []string{"^Node-Pool 'default': OverCapacity: job is over capacity"},
			expectedPodgroupConditionReasons: []enginev2alpha2.UnschedulableReason{podgroup_info.OverCapacity},
			expectedPodgroupEventPatterns:    []string{"^Node-Pool 'default': OverCapacity: job is over capacity"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"^Node-Pool 'default': OverCapacity: job is over capacity"},
				"pod-2": {"^Node-Pool 'default': OverCapacity: job is over capacity"},
			},
		},
		{
			name: "queue does not exist error",
			pods: map[v1.PodPhase][]common_info.PodID{
				v1.PodPending: {"pod-1"},
			},
			nodeErrors:                       map[common_info.PodID]map[string]string{},
			podErrors:                        map[common_info.PodID]error{},
			jobErrors:                        newUnschedulabeReasons(map[string]string{string(enginev2alpha2.QueueDoesNotExist): "Queue 'nonexistent-queue' does not exist"}),
			expectedPodgroupErrorPatterns:    []string{"Queue 'nonexistent-queue' does not exist"},
			expectedPodgroupConditionReasons: []enginev2alpha2.UnschedulableReason{enginev2alpha2.QueueDoesNotExist},
			expectedPodgroupEventPatterns:    []string{"Queue 'nonexistent-queue' does not exist"},
			expectedPodErrorPatterns: map[common_info.PodID][]string{
				"pod-1": {"Queue 'nonexistent-queue' does not exist"},
			},
			expectedPodEventPatterns: map[common_info.PodID][]string{
				"pod-1": {"Queue 'nonexistent-queue' does not exist"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var (
				podInfos          map[common_info.PodID]*pod_info.PodInfo
				podsAsObjects     []runtime.Object
				podGroup          *enginev2alpha2.PodGroup
				kubeClient        *fake.Clientset
				detailedFitErrors = false
			)

			podInfos, podsAsObjects = getPods(tt.pods)

			minMember := tt.podGroupMinMember
			if minMember == 0 {
				for _, pods := range tt.pods {
					minMember += len(pods)
				}
			}

			podGroup = &enginev2alpha2.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.run.ai/v2alpha2",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "group-1",
					Namespace: "namespace-1",
				},
				Spec: enginev2alpha2.PodGroupSpec{
					SchedulingBackoff: ptr.To(int32(1)),
					MinMember:         ptr.To(int32(minMember)),
				},
			}

			kubeClient = fake.NewSimpleClientset(podsAsObjects...)
			kubeAiSchedulerClient := kubeaischedulerfake.NewSimpleClientset(podGroup)

			if tt.detailedErrors != nil {
				detailedFitErrors = *tt.detailedErrors
			}

			cache := New(&SchedulerCacheParams{
				KubeClient:                  kubeClient,
				KAISchedulerClient:          kubeAiSchedulerClient,
				NodePoolParams:              &conf.SchedulingNodePoolParams{},
				DetailedFitErrors:           detailedFitErrors,
				FullHierarchyFairness:       true,
				NumOfStatusRecordingWorkers: 4,
				DiscoveryClient:             kubeClient.Discovery(),
			})

			stopCh := make(chan struct{})
			cache.Run(stopCh)
			cache.WaitForCacheSync(stopCh)
			defer close(stopCh)

			podGroupInfo := podgroup_info.NewPodGroupInfo("group-1", maps.Values(podInfos)...)
			podGroupInfo.SetPodGroup(podGroup)

			for podID, nodeErrors := range tt.nodeErrors {
				fitErrors := common_info.NewFitErrors()
				for node, msg := range nodeErrors {
					fitError := common_info.NewFitError(string(podID), "namespace-1", node, msg)
					fitErrors.SetNodeError(node, fitError)
				}
				podGroupInfo.AddTaskFitErrors(podGroupInfo.GetAllPodsMap()[podID], fitErrors)
			}

			for podID, err := range tt.podErrors {
				fitErrors := common_info.NewFitErrors()
				fitErrors.SetError(err.Error())
				podGroupInfo.AddTaskFitErrors(podGroupInfo.GetAllPodsMap()[podID], fitErrors)
			}

			for _, explanation := range tt.jobErrors {
				podGroupInfo.AddSimpleJobFitError(explanation.Reason, explanation.Message)
			}

			err := cache.RecordJobStatusEvent(podGroupInfo)
			assert.Nil(t, err)

			newPodGroupObj, err := waitForCondition(func() (runtime.Object, error) {
				podGroup, err := kubeAiSchedulerClient.SchedulingV2alpha2().PodGroups("namespace-1").Get(context.TODO(), "group-1", metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				if len(podGroup.Status.SchedulingConditions) > 0 {
					return podGroup, nil
				}
				return nil, fmt.Errorf("no scheduling conditions found")
			})

			if err != nil {
				t.Fatal(err)
			}

			newPodGroup := newPodGroupObj.(*enginev2alpha2.PodGroup)
			condition := newPodGroup.Status.SchedulingConditions[0]

			for _, pattern := range tt.expectedPodgroupErrorPatterns {
				matcher, err := regexp.Compile(pattern)
				assert.Nil(t, err)
				assert.True(t, matcher.Match([]byte(condition.Message)), "expected podgroup condition message:\n\"%s\"\nto match:\n\"%s\"", condition.Message, pattern)

				var conditionReasons []enginev2alpha2.UnschedulableReason
				for _, reason := range condition.Reasons {
					conditionReasons = append(conditionReasons, reason.Reason)
				}

				for _, reason := range tt.expectedPodgroupConditionReasons {
					assert.Containsf(t, conditionReasons, reason, "expected podgroup condition reasons:\n%v\nto contain:\n%v", conditionReasons, reason)
				}
			}

			for podID, patterns := range tt.expectedPodErrorPatterns {
				podObj, err := waitForCondition(func() (runtime.Object, error) {
					pod, err := kubeClient.CoreV1().Pods("namespace-1").Get(context.TODO(), string(podID), metav1.GetOptions{})
					if err != nil {
						return nil, err
					}
					if pod.Status.Conditions != nil && len(pod.Status.Conditions) > 0 {
						return pod, nil
					}
					return nil, fmt.Errorf("no conditions found for pod %s", pod.Name)
				})
				if err != nil {
					t.Fatal(err)
				}
				pod := podObj.(*v1.Pod)

				if patterns == nil {
					assert.Empty(t, pod.Status.Conditions)
					continue
				}

				podCondition := pod.Status.Conditions[0]
				for _, pattern := range patterns {
					matcher, err := regexp.Compile(pattern)
					assert.Nil(t, err)
					assert.True(t, matcher.Match([]byte(podCondition.Message)), "expected pod %s condition message:\n\"%s\"\nto match:\n\"%s\"", pod.Name, podCondition.Message, pattern)
				}
			}

			eventPerPod := getPodEvents(t, kubeClient)
			validatePodEvents(t, eventPerPod, tt.expectedPodEventPatterns)

			podGroupEvent := getPodGroupEvent(t, kubeClient)
			assert.NotNilf(t, podGroupEvent, "expected podgroup event, found none")
			if podGroupEvent != nil {
				for _, pattern := range tt.expectedPodgroupEventPatterns {
					matcher, err := regexp.Compile(pattern)
					assert.Nil(t, err)
					assert.True(t, matcher.Match([]byte(podGroupEvent.Message)), "expected podgroup event message:\n\"%s\"\nto match:\n\"%s\"", podGroupEvent.Message, pattern)
				}
			}
		})
	}
}

func waitForCondition(condition func() (runtime.Object, error)) (runtime.Object, error) {
	timer := time.NewTimer(1 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for podgroup update")
		case <-ticker.C:
			obj, err := condition()
			if err == nil {
				return obj, nil
			}
		}
	}
}

func validatePodEvents(t *testing.T, eventsPerPod map[common_info.PodID]*v1.Event, expectedPatterns map[common_info.PodID][]string) {
	for podID, expectedMessagePatterns := range expectedPatterns {
		event, found := eventsPerPod[podID]

		if expectedMessagePatterns == nil {
			assert.False(t, found, "expected no events for pod %s", podID)
		} else {
			assert.True(t, found, "expected event for pod %s", podID)
		}

		for _, expectedMessagePattern := range expectedMessagePatterns {
			matcher := regexp.MustCompile(expectedMessagePattern)
			assert.True(t, matcher.Match([]byte(event.Message)), "expected pod %s event:\n\"%s\"\nto match:\n\"%s\"", podID, event.Message, expectedMessagePattern)
		}
	}
}

func getPodEvents(t *testing.T, kubeClient clientset.Interface) (eventsPerPod map[common_info.PodID]*v1.Event) {
	events, err := kubeClient.CoreV1().Events("namespace-1").List(context.TODO(), metav1.ListOptions{})
	assert.Nil(t, err)

	eventsPerPod = make(map[common_info.PodID]*v1.Event)
	for _, event := range events.Items {
		if event.InvolvedObject.Kind != "Pod" {
			continue
		}
		podID := common_info.PodID(event.InvolvedObject.UID)
		if existing, found := eventsPerPod[podID]; found {
			t.Errorf("Expected single event for pod %s, got:\n%v\n%v", podID, existing, event)
		}
		eventsPerPod[podID] = &event
	}

	return eventsPerPod
}

func getPodGroupEvent(t *testing.T, kubeClient clientset.Interface) *v1.Event {
	events, err := kubeClient.CoreV1().Events("namespace-1").List(context.TODO(), metav1.ListOptions{})
	assert.Nil(t, err)

	for _, event := range events.Items {
		if event.InvolvedObject.Kind == "PodGroup" {
			return &event
		}

	}

	return nil
}

func getPods(pods map[v1.PodPhase][]common_info.PodID) (podsInfos map[common_info.PodID]*pod_info.PodInfo, podsAsObjects []runtime.Object) {
	podsInfos = make(map[common_info.PodID]*pod_info.PodInfo)
	for phase, podNames := range pods {
		for _, name := range podNames {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      string(name),
					Namespace: "namespace-1",
					UID:       types.UID(name),
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"cpu":            resource.MustParse("2000m"),
									"memory":         resource.MustParse("5Gi"),
									"nvidia.com/gpu": resource.MustParse("2"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Phase: phase,
				},
			}

			podInfo := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())

			podsInfos[podInfo.UID] = podInfo
			podsAsObjects = append(podsAsObjects, pod)
		}
	}
	return podsInfos, podsAsObjects
}

func newUnschedulabeReasons(reasons map[string]string) enginev2alpha2.UnschedulableExplanations {
	var explanations enginev2alpha2.UnschedulableExplanations
	for reason, message := range reasons {
		explanations = append(explanations, enginev2alpha2.UnschedulableExplanation{
			Reason:  enginev2alpha2.UnschedulableReason(reason),
			Message: message,
		})
	}
	return explanations
}
