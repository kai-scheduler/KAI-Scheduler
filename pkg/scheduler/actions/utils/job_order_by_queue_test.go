// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/elastic"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/priority"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

const (
	testDepartment = "d1"
	testQueue      = "q1"
	testPod        = "p1"
)

func TestNumericalPriorityWithinSameQueue(t *testing.T) {
	ssn := newPrioritySession()

	ssn.ClusterInfo.Queues = map[common_info.QueueID]*queue_info.QueueInfo{
		testQueue: {
			UID:         testQueue,
			ParentQueue: testDepartment,
		},
		testDepartment: {
			UID:         testDepartment,
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
		name                 string
		jobsOrderInitOptions JobsOrderInitOptions
		queues               map[common_info.QueueID]*queue_info.QueueInfo
		initJobs             map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
		expectedJobNames     []string
	}{
		{
			name: "single podgroup insert - empty queue",
			jobsOrderInitOptions: JobsOrderInitOptions{
				VictimQueue:       true,
				FilterNonPending:  false,
				FilterUnready:     true,
				MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
			},
			queues: map[common_info.QueueID]*queue_info.QueueInfo{
				"q1": {ParentQueue: "d1", UID: "q1", CreationTimestamp: now,
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
				"q2": {ParentQueue: "d1", UID: "q2", CreationTimestamp: nowMinus1,
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
				"d1": {UID: "d1", CreationTimestamp: now},
			},
			initJobs: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				"q1j1": {
					Name:     "q1j1",
					Priority: 100,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID: "p1",
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
				"q1j2": {
					Name:     "q1j2",
					Priority: 99,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID: "p1",
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
				"q1j3": {
					Name:     "q1j3",
					Priority: 98,
					Queue:    "q1",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID: "p1",
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
								UID: "p1",
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
								UID: "p1",
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
				"q2j3": {
					Name:     "q2j3",
					Priority: 98,
					Queue:    "q2",
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Allocated: {
							"p1": {
								UID: "p1",
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
			},
			expectedJobNames: []string{"q1j3", "q2j3", "q1j2", "q2j2", "q1j1", "q2j1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := newPrioritySession()
			ssn.ClusterInfo.Queues = tt.queues
			ssn.ClusterInfo.PodGroupInfos = tt.initJobs
			proportion.New(map[string]string{}).OnSessionOpen(ssn)

			activeDepartments := scheduler_util.NewPriorityQueue(func(l, r interface{}) bool {
				return !ssn.JobOrderFn(l, r)
			}, scheduler_util.QueueCapacityInfinite)

			jobsOrder := &JobsOrderByQueues{
				activeDepartments:                activeDepartments,
				queueIdToQueueMetadata:           map[common_info.QueueID]*jobsQueueMetadata{},
				departmentIdToDepartmentMetadata: map[common_info.QueueID]*departmentMetadata{},
				ssn:                              ssn,
				jobsOrderInitOptions:             tt.jobsOrderInitOptions,
				queuePopsMap:                     map[common_info.QueueID][]*podgroup_info.PodGroupInfo{},
			}
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
		jobsOrderInitOptions JobsOrderInitOptions
		Queues               map[common_info.QueueID]*queue_info.QueueInfo
		InsertedJob          map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
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
				jobsOrderInitOptions: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1": {ParentQueue: "d1", UID: "q1"},
					"d1": {UID: "d1"},
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
				jobsOrderInitOptions: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1": {ParentQueue: "d1", UID: "q1"},
					"d1": {UID: "d1"},
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
				jobsOrderInitOptions: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1": {ParentQueue: "d1", UID: "q1"},
					"d1": {UID: "d1"},
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
			ssn := newPrioritySession()
			ssn.ClusterInfo.Queues = tt.fields.Queues
			activeDepartments := scheduler_util.NewPriorityQueue(func(l, r interface{}) bool {
				if tt.fields.jobsOrderInitOptions.VictimQueue {
					return !ssn.JobOrderFn(l, r)
				}
				return ssn.JobOrderFn(l, r)
			}, scheduler_util.QueueCapacityInfinite)

			jobsOrder := &JobsOrderByQueues{
				activeDepartments:                activeDepartments,
				queueIdToQueueMetadata:           map[common_info.QueueID]*jobsQueueMetadata{},
				departmentIdToDepartmentMetadata: map[common_info.QueueID]*departmentMetadata{},
				ssn:                              ssn,
				jobsOrderInitOptions:             tt.fields.jobsOrderInitOptions,
				queuePopsMap:                     map[common_info.QueueID][]*podgroup_info.PodGroupInfo{},
			}
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
		queueIdToQueueMetadata           map[common_info.QueueID]*jobsQueueMetadata
		departmentIdToDepartmentMetadata map[common_info.QueueID]*departmentMetadata
		jobsOrderInitOptions             JobsOrderInitOptions
		Queues                           map[common_info.QueueID]*queue_info.QueueInfo
		InsertedJob                      map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
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
				queueIdToQueueMetadata:           map[common_info.QueueID]*jobsQueueMetadata{},
				departmentIdToDepartmentMetadata: map[common_info.QueueID]*departmentMetadata{},
				jobsOrderInitOptions: JobsOrderInitOptions{
					VictimQueue:       false,
					FilterNonPending:  true,
					FilterUnready:     true,
					MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
				},
				Queues: map[common_info.QueueID]*queue_info.QueueInfo{
					"q1": {ParentQueue: "d1", UID: "q1"},
					"d1": {UID: "d1"},
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
			ssn := newPrioritySession()
			ssn.ClusterInfo.Queues = tt.fields.Queues
			activeDepartments := scheduler_util.NewPriorityQueue(func(l, r interface{}) bool {
				if tt.fields.jobsOrderInitOptions.VictimQueue {
					return !ssn.JobOrderFn(l, r)
				}
				return ssn.JobOrderFn(l, r)
			}, scheduler_util.QueueCapacityInfinite)

			jobsOrder := &JobsOrderByQueues{
				activeDepartments:                activeDepartments,
				queueIdToQueueMetadata:           tt.fields.queueIdToQueueMetadata,
				departmentIdToDepartmentMetadata: tt.fields.departmentIdToDepartmentMetadata,
				ssn:                              ssn,
				jobsOrderInitOptions:             tt.fields.jobsOrderInitOptions,
				queuePopsMap:                     map[common_info.QueueID][]*podgroup_info.PodGroupInfo{},
			}
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

func newPrioritySession() *framework.Session {
	return &framework.Session{
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
