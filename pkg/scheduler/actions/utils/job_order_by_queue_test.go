// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/elastic"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/priority"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testParentQueue = "pq1"
	testQueue       = "q1"
	testPod         = "p1"
)

var testVectorMap = resource_info.NewResourceVectorMap()

func TestNumericalPriorityWithinSameQueue(t *testing.T) {
	ssn := newPrioritySession(t)

	ssn.ClusterInfo.Queues = map[common_info.QueueID]*queue_info.QueueInfo{
		testQueue: {
			UID:         testQueue,
			ParentQueue: testParentQueue,
		},
		testParentQueue: {
			UID:         testParentQueue,
			ChildQueues: []common_info.QueueID{testQueue},
		},
	}
	ssn.ClusterInfo.PodGroupInfos = map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		"0": {
			Name:     "p150",
			Priority: 150,
			Queue:    testQueue,
			PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
				pod_status.Pending: {
					testPod: {},
				},
			},
			PodSets: map[string]*subgroup_info.PodSet{
				podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
					WithPodInfos(pod_info.PodsMap{
						testPod: {
							UID: testPod,
						},
					}),
			},
		},
		"1": {
			Name:     "p255",
			Priority: 255,
			Queue:    testQueue,
			PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
				pod_status.Pending: {
					testPod: {},
				},
			},
			PodSets: map[string]*subgroup_info.PodSet{
				podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
					WithPodInfos(pod_info.PodsMap{
						testPod: {
							UID: testPod,
						},
					}),
			},
		},
		"2": {
			Name:     "p160",
			Priority: 160,
			Queue:    testQueue,
			PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
				pod_status.Pending: {
					testPod: {},
				},
			},
			PodSets: map[string]*subgroup_info.PodSet{
				podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
					WithPodInfos(pod_info.PodsMap{
						testPod: {
							UID: testPod,
						},
					}),
			},
		},
		"3": {
			Name:     "p200",
			Priority: 200,
			Queue:    testQueue,
			PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
				pod_status.Pending: {
					testPod: {},
				},
			},
			PodSets: map[string]*subgroup_info.PodSet{
				podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
					WithPodInfos(pod_info.PodsMap{
						testPod: {
							UID: testPod,
						},
					}),
			},
		},
	}

	jobsOrderByQueues := NewJobsOrderByQueues(ssn, JobsOrderInitOptions{
		FilterNonPending:  true,
		FilterUnready:     true,
		MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)

	expectedJobsOrder := []string{"p255", "p200", "p160", "p150"}
	actualJobsOrder := []string{}
	for !jobsOrderByQueues.IsEmpty() {
		job := jobsOrderByQueues.PopNextJob()
		actualJobsOrder = append(actualJobsOrder, job.Name)
	}
	assert.Equal(t, expectedJobsOrder, actualJobsOrder)
}

func TestVictimQueue_PopNextJob(t *testing.T) {
	now := metav1.Time{Time: time.Now()}
	nowMinus1 := metav1.Time{Time: time.Now().Add(-time.Second)}
	tests := []struct {
		name             string
		options          JobsOrderInitOptions
		queues           map[common_info.QueueID]*queue_info.QueueInfo
		initJobs         map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
		expectedJobNames []string
	}{
		{
			name: "single podgroup insert - empty queue",
			options: JobsOrderInitOptions{
				VictimQueue:       true,
				FilterNonPending:  false,
				FilterUnready:     true,
				MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
			},
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"q1": {ParentQueue: "pq1", UID: "q1", CreationTimestamp: now,
					Resources: queue_info.QueueQuota{
						GPU: queue_info.ResourceQuota{
							Quota:           1,
							Limit:           -1,
							OverQuotaWeight: 1,
						},
						CPU: queue_info.ResourceQuota{
							Quota:           1,
							Limit:           -1,
							OverQuotaWeight: 1,
						},
						Memory: queue_info.ResourceQuota{
							Quota:           1,
							Limit:           -1,
							OverQuotaWeight: 1,
						},
					},
				},
				"q2": {ParentQueue: "pq1", UID: "q2", CreationTimestamp: nowMinus1,
					Resources: queue_info.QueueQuota{
						GPU: queue_info.ResourceQuota{
							Quota:           1,
							Limit:           -1,
							OverQuotaWeight: 1,
						},
						CPU: queue_info.ResourceQuota{
							Quota:           1,
							Limit:           -1,
							OverQuotaWeight: 1,
						},
						Memory: queue_info.ResourceQuota{
							Quota:           1,
							Limit:           -1,
							OverQuotaWeight: 1,
						},
					},
				},
				"pq1": {UID: "pq1", CreationTimestamp: now},
			},
			initJobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"q1j1": {
					Name:     "q1j1",
					Priority: 100,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID:                    "p1",
								VectorMap:              testVectorMap,
								AcceptedResource:       resource_info.NewResourceRequirements(1, 1000, 1024),
								AcceptedResourceVector: resource_info.NewResourceRequirements(1, 1000, 1024).ToVector(testVectorMap),
							},
						},
					},
					Allocated:       resource_info.NewResource(1000, 1024, 1),
					VectorMap:       testVectorMap,
					AllocatedVector: resource_info.NewResource(1000, 1024, 1).ToVector(testVectorMap),
				},
				"q1j2": {
					Name:     "q1j2",
					Priority: 99,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID:                    "p1",
								VectorMap:              testVectorMap,
								AcceptedResource:       resource_info.NewResourceRequirements(1, 1000, 1024),
								AcceptedResourceVector: resource_info.NewResourceRequirements(1, 1000, 1024).ToVector(testVectorMap),
							},
						},
					},
					Allocated:       resource_info.NewResource(1000, 1024, 1),
					VectorMap:       testVectorMap,
					AllocatedVector: resource_info.NewResource(1000, 1024, 1).ToVector(testVectorMap),
				},
				"q1j3": {
					Name:     "q1j3",
					Priority: 98,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID:                    "p1",
								VectorMap:              testVectorMap,
								AcceptedResourceVector: resource_info.NewResourceRequirements(1, 1000, 1024).ToVector(testVectorMap),
								AcceptedResource: resource_info.NewResourceRequirements(
									1,
									1000,
									1024,
								),
							},
						},
					},
					Allocated: resource_info.NewResource(1000, 1024, 1),
				},
				"q2j1": {
					Name:     "q2j1",
					Priority: 100,
					Queue:    "q2",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID:                    "p1",
								VectorMap:              testVectorMap,
								AcceptedResourceVector: resource_info.NewResourceRequirements(1, 1000, 1024).ToVector(testVectorMap),
								AcceptedResource: resource_info.NewResourceRequirements(
									1,
									1000,
									1024,
								),
							},
						},
					},
					Allocated: resource_info.NewResource(1000, 1024, 1),
				},
				"q2j2": {
					Name:     "q2j2",
					Priority: 99,
					Queue:    "q2",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID:                    "p1",
								VectorMap:              testVectorMap,
								AcceptedResourceVector: resource_info.NewResourceRequirements(1, 1000, 1024).ToVector(testVectorMap),
								AcceptedResource: resource_info.NewResourceRequirements(
									1,
									1000,
									1024,
								),
							},
						},
					},
					Allocated:       resource_info.NewResource(1000, 1024, 1),
					VectorMap:       testVectorMap,
					AllocatedVector: resource_info.NewResource(1000, 1024, 1).ToVector(testVectorMap),
				},
				"q2j3": {
					Name:     "q2j3",
					Priority: 98,
					Queue:    "q2",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID:                    "p1",
								VectorMap:              testVectorMap,
								AcceptedResourceVector: resource_info.NewResourceRequirements(1, 1000, 1024).ToVector(testVectorMap),
								AcceptedResource: resource_info.NewResourceRequirements(
									1,
									1000,
									1024,
								),
							},
						},
					},
					Allocated:       resource_info.NewResource(1000, 1024, 1),
					VectorMap:       testVectorMap,
					AllocatedVector: resource_info.NewResource(1000, 1024, 1).ToVector(testVectorMap),
				},
			},
			expectedJobNames: []string{"q1j3", "q2j3", "q1j2", "q2j2", "q1j1", "q2j1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := newPrioritySession(t)
			ssn.ClusterInfo.Queues = tt.queues
			ssn.ClusterInfo.PodGroupInfos = tt.initJobs
			proportion.New(map[string]string{}).OnSessionOpen(ssn)

			jobsOrder := NewJobsOrderByQueues(ssn, tt.options)
			jobsOrder.InitializeWithJobs(tt.initJobs)

			for _, expectedJobName := range tt.expectedJobNames {
				actualJob := jobsOrder.PopNextJob()
				assert.Equal(t, expectedJobName, actualJob.Name)
			}
		})
	}
}

func TestJobsOrderByQueues_PushJob(t *testing.T) {
	type fields struct {
		options     JobsOrderInitOptions
		Queues      map[common_info.QueueID]*queue_info.QueueInfo
		InsertedJob map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
	}
	type args struct {
		job *podgroup_info.PodGroupInfo
	}
	type expected struct {
		expectedJobsList []*podgroup_info.PodGroupInfo
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		expected expected
	}{
		{
			name: "single podgroup insert - empty queue",
			fields: fields{
				options: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1":  {ParentQueue: "pq1", UID: "q1"},
					"pq1": {UID: "pq1"},
				},
				InsertedJob: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{},
			},
			args: args{
				job: &podgroup_info.PodGroupInfo{
					Name:     "p150",
					Priority: 150,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Pending: {
							testPod: {
								UID: testPod,
							},
						},
					},
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
							WithPodInfos(pod_info.PodsMap{
								testPod: {
									UID: testPod,
								},
							}),
					},
				},
			},
			expected: expected{
				expectedJobsList: []*podgroup_info.PodGroupInfo{
					{
						Name:     "p150",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
				},
			},
		},
		{
			name: "single podgroup insert - one in queue. On pop comes second",
			fields: fields{
				options: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1":  {ParentQueue: "pq1", UID: "q1"},
					"pq1": {UID: "pq1"},
				},
				InsertedJob: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
					"p140": {
						Name:     "p140",
						UID:      "1",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
				},
			},
			args: args{
				job: &podgroup_info.PodGroupInfo{
					Name:     "p150",
					UID:      "2",
					Priority: 150,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Pending: {
							testPod: {
								UID: testPod,
							},
						},
					},
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
							WithPodInfos(pod_info.PodsMap{
								testPod: {
									UID: testPod,
								},
							}),
					},
				},
			},
			expected: expected{
				expectedJobsList: []*podgroup_info.PodGroupInfo{
					{
						Name:     "p140",
						UID:      "1",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
					{
						Name:     "p150",
						UID:      "2",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
				},
			},
		},
		{
			name: "single podgroup insert - one in queue. On pop comes first",
			fields: fields{
				options: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1":  {ParentQueue: "pq1", UID: "q1"},
					"pq1": {UID: "pq1"},
				},
				InsertedJob: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
					"p140": {
						Name:     "p140",
						UID:      "1",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
				},
			},
			args: args{
				job: &podgroup_info.PodGroupInfo{
					Name:     "p150",
					UID:      "2",
					Priority: 160,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Pending: {
							testPod: {
								UID: testPod,
							},
						},
					},
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
							WithPodInfos(pod_info.PodsMap{
								testPod: {
									UID: testPod,
								},
							}),
					},
				},
			},
			expected: expected{
				expectedJobsList: []*podgroup_info.PodGroupInfo{
					{
						Name:     "p150",
						UID:      "2",
						Priority: 160,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
					{
						Name:     "p140",
						UID:      "1",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := newPrioritySession(t)
			ssn.ClusterInfo.Queues = tt.fields.Queues

			jobsOrder := NewJobsOrderByQueues(ssn, tt.fields.options)
			jobsOrder.InitializeWithJobs(tt.fields.InsertedJob)
			jobsOrder.PushJob(tt.args.job)

			for _, expectedJob := range tt.expected.expectedJobsList {
				_ = expectedJob.GetActiveAllocatedTasksCount()
				actualJob := jobsOrder.PopNextJob()
				_ = actualJob.GetActiveAllocatedTasksCount()
				assert.Equal(t, expectedJob, actualJob)
			}
		})
	}
}

func TestJobsOrderByQueues_RequeueJob(t *testing.T) {
	type fields struct {
		options     JobsOrderInitOptions
		Queues      map[common_info.QueueID]*queue_info.QueueInfo
		InsertedJob map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
	}
	type expected struct {
		expectedJobsList []*podgroup_info.PodGroupInfo
	}
	tests := []struct {
		name     string
		fields   fields
		expected expected
	}{
		{
			name: "single job - pop and insert",
			fields: fields{
				options: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1":  {ParentQueue: "pq1", UID: "q1"},
					"pq1": {UID: "pq1"},
				},
				InsertedJob: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
					"p140": {
						Name:     "p140",
						UID:      "1",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
				},
			},
			expected: expected{
				expectedJobsList: []*podgroup_info.PodGroupInfo{
					{
						Name:     "p140",
						UID:      "1",
						Priority: 150,
						Queue:    "q1",
						PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
							pod_status.Pending: {
								testPod: {
									UID: testPod,
								},
							},
						},
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
								WithPodInfos(pod_info.PodsMap{
									testPod: {
										UID: testPod,
									},
								}),
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := newPrioritySession(t)
			ssn.ClusterInfo.Queues = tt.fields.Queues

			jobsOrder := NewJobsOrderByQueues(ssn, tt.fields.options)
			jobsOrder.InitializeWithJobs(tt.fields.InsertedJob)

			jobToRequeue := jobsOrder.PopNextJob()
			jobsOrder.PushJob(jobToRequeue)

			for _, expectedJob := range tt.expected.expectedJobsList {
				actualJob := jobsOrder.PopNextJob()
				assert.Equal(t, expectedJob, actualJob)
			}
		})
	}
}

func TestJobsOrderByQueues_OrphanQueue_AddsJobFitError(t *testing.T) {
	// Test that jobs in queues with missing parent queues get an error added
	ssn := newPrioritySession(t)

	// Create a queue with a parent that doesn't exist (orphan queue)
	orphanQueue := &queue_info.QueueInfo{
		UID:         "orphan-queue",
		Name:        "orphan-queue",
		ParentQueue: "missing-parent", // This parent doesn't exist
	}

	ssn.ClusterInfo.Queues = map[common_info.QueueID]*queue_info.QueueInfo{
		"orphan-queue": orphanQueue,
		// Note: "missing-parent" is intentionally NOT in the map
	}

	job := &podgroup_info.PodGroupInfo{
		Name:     "test-job",
		UID:      "test-job-uid",
		Priority: 100,
		Queue:    "orphan-queue",
		PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
			pod_status.Pending: {
				"pod-1": {UID: "pod-1"},
			},
		},
		PodSets: map[string]*subgroup_info.PodSet{
			podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
				WithPodInfos(pod_info.PodsMap{
					"pod-1": {UID: "pod-1"},
				}),
		},
	}

	ssn.ClusterInfo.PodGroupInfos = map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		"test-job": job,
	}

	jobsOrder := NewJobsOrderByQueues(ssn, JobsOrderInitOptions{
		FilterNonPending:  true,
		FilterUnready:     false,
		MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
	})
	jobsOrder.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)

	// The jobs order should be empty because the orphan queue's jobs are skipped
	assert.True(t, jobsOrder.IsEmpty(), "Expected empty jobs order because orphan queue jobs are skipped from scheduling")
}

// TestNLevelQueueHierarchy is a table-driven test for various queue hierarchy configurations.
// It tests single-level, two-level, three-level, four-level, mixed-depth, and multiple root queue hierarchies.
func TestNLevelQueueHierarchy(t *testing.T) {
	testCases := []struct {
		name             string
		queues           map[common_info.QueueID]*queue_info.QueueInfo
		jobs             map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
		pushJobs         []*podgroup_info.PodGroupInfo // optional: for dynamic push tests
		expectedJobOrder []string
	}{
		{
			name: "three level hierarchy",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"root":  {UID: "root", Name: "root", ParentQueue: "", ChildQueues: []common_info.QueueID{"dept1", "dept2"}},
				"dept1": {UID: "dept1", Name: "dept1", ParentQueue: "root", ChildQueues: []common_info.QueueID{"team1", "team2"}},
				"dept2": {UID: "dept2", Name: "dept2", ParentQueue: "root", ChildQueues: []common_info.QueueID{"team3"}},
				"team1": {UID: "team1", Name: "team1", ParentQueue: "dept1"},
				"team2": {UID: "team2", Name: "team2", ParentQueue: "dept1"},
				"team3": {UID: "team3", Name: "team3", ParentQueue: "dept2"},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job1": newHierarchyTestJob("job1-team1-p100", 100, "team1"),
				"job2": newHierarchyTestJob("job2-team2-p200", 200, "team2"),
				"job3": newHierarchyTestJob("job3-team3-p150", 150, "team3"),
				"job4": newHierarchyTestJob("job4-team1-p250", 250, "team1"),
			},
			expectedJobOrder: []string{"job4-team1-p250", "job1-team1-p100", "job2-team2-p200", "job3-team3-p150"},
		},
		{
			name: "four level hierarchy",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"org":   {UID: "org", Name: "org", ParentQueue: ""},
				"div1":  {UID: "div1", Name: "div1", ParentQueue: "org"},
				"dept1": {UID: "dept1", Name: "dept1", ParentQueue: "div1"},
				"team1": {UID: "team1", Name: "team1", ParentQueue: "dept1"},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job1": newHierarchyTestJob("deep-job", 100, "team1"),
			},
			expectedJobOrder: []string{"deep-job"},
		},
		{
			name: "single level hierarchy",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"default": {UID: "default", Name: "default", ParentQueue: ""},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job1": newHierarchyTestJob("job1-default-p100", 100, "default"),
				"job2": newHierarchyTestJob("job2-default-p200", 200, "default"),
			},
			expectedJobOrder: []string{"job2-default-p200", "job1-default-p100"},
		},
		{
			name: "two level hierarchy",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"root":  {UID: "root", Name: "root", ParentQueue: "", ChildQueues: []common_info.QueueID{"leaf1", "leaf2"}},
				"leaf1": {UID: "leaf1", Name: "leaf1", ParentQueue: "root"},
				"leaf2": {UID: "leaf2", Name: "leaf2", ParentQueue: "root"},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job1": newHierarchyTestJob("job1-leaf1-p100", 100, "leaf1"),
				"job2": newHierarchyTestJob("job2-leaf2-p200", 200, "leaf2"),
			},
			expectedJobOrder: []string{"job1-leaf1-p100", "job2-leaf2-p200"},
		},
		{
			name: "mixed depth hierarchy",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"root":  {UID: "root", Name: "root", ParentQueue: "", ChildQueues: []common_info.QueueID{"leaf1", "dept"}},
				"leaf1": {UID: "leaf1", Name: "leaf1", ParentQueue: "root"},
				"dept":  {UID: "dept", Name: "dept", ParentQueue: "root", ChildQueues: []common_info.QueueID{"team"}},
				"team":  {UID: "team", Name: "team", ParentQueue: "dept"},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job1": newHierarchyTestJob("job1-shallow-p150", 150, "leaf1"),
				"job2": newHierarchyTestJob("job2-deep-p200", 200, "team"),
			},
			expectedJobOrder: []string{"job2-deep-p200", "job1-shallow-p150"},
		},
		{
			name: "multiple root queues",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"root1": {UID: "root1", Name: "root1", ParentQueue: "", ChildQueues: []common_info.QueueID{"leaf1"}},
				"leaf1": {UID: "leaf1", Name: "leaf1", ParentQueue: "root1"},
				"root2": {UID: "root2", Name: "root2", ParentQueue: "", ChildQueues: []common_info.QueueID{"leaf2"}},
				"leaf2": {UID: "leaf2", Name: "leaf2", ParentQueue: "root2"},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job1": newHierarchyTestJob("job1-root1-p100", 100, "leaf1"),
				"job2": newHierarchyTestJob("job2-root2-p200", 200, "leaf2"),
			},
			expectedJobOrder: []string{"job1-root1-p100", "job2-root2-p200"},
		},
		{
			name: "multiple single level root queues",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"queue-a": {UID: "queue-a", Name: "queue-a", ParentQueue: ""},
				"queue-b": {UID: "queue-b", Name: "queue-b", ParentQueue: ""},
				"queue-c": {UID: "queue-c", Name: "queue-c", ParentQueue: ""},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job-a": newHierarchyTestJob("job-a-p100", 100, "queue-a"),
				"job-b": newHierarchyTestJob("job-b-p300", 300, "queue-b"),
				"job-c": newHierarchyTestJob("job-c-p200", 200, "queue-c"),
			},
			expectedJobOrder: []string{"job-a-p100", "job-b-p300", "job-c-p200"},
		},
		{
			name: "push job builds n-level tree",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"root": {UID: "root", Name: "root", ParentQueue: "", ChildQueues: []common_info.QueueID{"dept"}},
				"dept": {UID: "dept", Name: "dept", ParentQueue: "root", ChildQueues: []common_info.QueueID{"team"}},
				"team": {UID: "team", Name: "team", ParentQueue: "dept"},
			},
			pushJobs: []*podgroup_info.PodGroupInfo{
				newHierarchyTestJob("job1-p100", 100, "team"),
				newHierarchyTestJob("job2-p200", 200, "team"),
			},
			expectedJobOrder: []string{"job2-p200", "job1-p100"},
		},
		{
			name: "push job to single level queue",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"default": {UID: "default", Name: "default", ParentQueue: ""},
			},
			pushJobs: []*podgroup_info.PodGroupInfo{
				newHierarchyTestJob("pushed-job", 100, "default"),
			},
			expectedJobOrder: []string{"pushed-job"},
		},
		{
			name: "tree cleanup after all jobs popped",
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"root":  {UID: "root", Name: "root", ParentQueue: "", ChildQueues: []common_info.QueueID{"dept1", "dept2"}},
				"dept1": {UID: "dept1", Name: "dept1", ParentQueue: "root", ChildQueues: []common_info.QueueID{"team1"}},
				"dept2": {UID: "dept2", Name: "dept2", ParentQueue: "root", ChildQueues: []common_info.QueueID{"team2"}},
				"team1": {UID: "team1", Name: "team1", ParentQueue: "dept1"},
				"team2": {UID: "team2", Name: "team2", ParentQueue: "dept2"},
			},
			jobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"job1": newHierarchyTestJob("job1-team1", 200, "team1"),
				"job2": newHierarchyTestJob("job2-team2", 100, "team2"),
			},
			expectedJobOrder: []string{"job1-team1", "job2-team2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ssn := newPrioritySession(t)
			ssn.ClusterInfo.Queues = tc.queues

			jobsOrderByQueues := NewJobsOrderByQueues(ssn, JobsOrderInitOptions{
				FilterNonPending:  true,
				FilterUnready:     true,
				MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
			})

			if tc.pushJobs != nil {
				for _, job := range tc.pushJobs {
					jobsOrderByQueues.PushJob(job)
				}
			} else {
				ssn.ClusterInfo.PodGroupInfos = tc.jobs
				jobsOrderByQueues.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)
			}

			assert.Equal(t, len(tc.expectedJobOrder), jobsOrderByQueues.Len())

			actualJobsOrder := []string{}
			for !jobsOrderByQueues.IsEmpty() {
				job := jobsOrderByQueues.PopNextJob()
				if job != nil {
					actualJobsOrder = append(actualJobsOrder, job.Name)
				}
			}

			assert.Equal(t, tc.expectedJobOrder, actualJobsOrder)
			assert.True(t, jobsOrderByQueues.IsEmpty())
		})
	}
}

// newHierarchyTestJob creates a test job with a pending pod for hierarchy tests.
func newHierarchyTestJob(name string, priority int32, queue common_info.QueueID) *podgroup_info.PodGroupInfo {
	return &podgroup_info.PodGroupInfo{
		Name:     name,
		Priority: priority,
		Queue:    queue,
		PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
			pod_status.Pending: {testPod: {}},
		},
		PodSets: map[string]*subgroup_info.PodSet{
			podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 0, nil).
				WithPodInfos(pod_info.PodsMap{testPod: {UID: testPod}}),
		},
	}
}

func newPrioritySession(t *testing.T) *framework.Session {
	cacheMock := newCacheMock(t)

	return &framework.Session{
		Cache:       cacheMock,
		ClusterInfo: &api.ClusterInfo{},
		JobOrderFns: []common_info.CompareFn{
			priority.JobOrderFn,
			elastic.JobOrderFn,
		},
		Config: &conf.SchedulerConfiguration{
			Tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{Name: "Priority"},
						{Name: "Elastic"},
						{Name: "Proportion"},
					},
				},
			},
			QueueDepthPerAction: map[string]int{},
		},
	}
}

func newCacheMock(t *testing.T) *cache.MockCache {
	controller := gomock.NewController(t)
	cacheMock := cache.NewMockCache(controller)

	fakeClient := fake.NewSimpleClientset()
	cacheMock.EXPECT().KubeClient().AnyTimes().Return(fakeClient)

	informerFactory := informers.NewSharedInformerFactory(cacheMock.KubeClient(), 0)

	informerFactory.Resource().V1().ResourceClaims().Informer()
	informerFactory.Resource().V1().ResourceSlices().Informer()
	informerFactory.Resource().V1().DeviceClasses().Informer()

	ctx := context.Background()
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	cacheMock.EXPECT().KubeInformerFactory().AnyTimes().Return(informerFactory)
	cacheMock.EXPECT().SnapshotSharedLister().AnyTimes().Return(cache.NewK8sClusterPodAffinityInfo())

	k8sPlugins := k8splugins.InitializeInternalPlugins(
		cacheMock.KubeClient(), cacheMock.KubeInformerFactory(), cacheMock.SnapshotSharedLister(),
	)
	cacheMock.EXPECT().InternalK8sPlugins().AnyTimes().Return(k8sPlugins)
	return cacheMock
}

func TestVictimQueue_TwoQueuesWithRunningJobs(t *testing.T) {
	// This test simulates what the pod_scenario_builder_test does
	ssn := newPrioritySession(t)

	// Setup similar to initializeSession(2, 2)
	ssn.ClusterInfo.Queues = map[common_info.QueueID]*queue_info.QueueInfo{
		"default": {
			UID:         "default",
			Name:        "default",
			ParentQueue: "",
		},
		"team-0": {
			UID:         "team-0",
			Name:        "team-0",
			ParentQueue: "default",
		},
		"team-1": {
			UID:         "team-1",
			Name:        "team-1",
			ParentQueue: "default",
		},
	}

	// Jobs with Running status (like in initializeSession)
	ssn.ClusterInfo.PodGroupInfos = map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		"job0": {
			UID:      "job0",
			Name:     "job0",
			Priority: 100,
			Queue:    "team-0",
			PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
				pod_status.Running: {testPod: {}},
			},
			PodSets: map[string]*subgroup_info.PodSet{
				podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
					WithPodInfos(pod_info.PodsMap{testPod: {UID: testPod}}),
			},
		},
		"job1": {
			UID:      "job1",
			Name:     "job1",
			Priority: 100,
			Queue:    "team-1",
			PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
				pod_status.Running: {testPod: {}},
			},
			PodSets: map[string]*subgroup_info.PodSet{
				podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
					WithPodInfos(pod_info.PodsMap{testPod: {UID: testPod}}),
			},
		},
	}

	// Create victims queue similar to GetVictimsQueue
	victimsQueue := NewJobsOrderByQueues(ssn, JobsOrderInitOptions{
		VictimQueue:       true,
		MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
	})
	victimsQueue.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)

	// Should have 2 jobs
	assert.Equal(t, 2, victimsQueue.Len())

	// Pop first job
	job1 := victimsQueue.PopNextJob()
	assert.NotNil(t, job1, "First PopNextJob should return a job")

	// Pop second job
	job2 := victimsQueue.PopNextJob()
	assert.NotNil(t, job2, "Second PopNextJob should return a job")

	// Third pop should return nil
	job3 := victimsQueue.PopNextJob()
	assert.Nil(t, job3, "Third PopNextJob should return nil")
}
