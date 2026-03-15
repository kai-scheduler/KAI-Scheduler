// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

var _ = Describe("Capacity Policy Check", func() {
	var (
		testVectorMap = resource_info.NewResourceVectorMap()
	)
	Describe("IsJobOverQueueCapacity", func() {
		Context("max allowed", func() {
			tests := map[string]struct {
				queues         map[common_info.QueueID]*rs.QueueAttributes
				job            *podgroup_info.PodGroupInfo
				expectedResult bool
			}{
				"limited queues - results below limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  2,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  2,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  2,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.Preemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirementsWithGpus(1),
										ResReqVector: resource_info.NewResourceRequirementsWithGpus(1).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					expectedResult: true,
				},
				"limited queues - result over limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  3,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  2,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed: 3,
									Allocated:  2,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:         "job-a",
						Namespace:    "team-a",
						Queue:        "leaf-queue",
						JobFitErrors: make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirementsWithGpus(1),
										ResReqVector: resource_info.NewResourceRequirementsWithGpus(1).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					expectedResult: false,
				},
			}

			for name, data := range tests {
				testName := name
				testData := data
				It(testName, func() {
					capacityPolicy := New(testData.queues)
					tasksToAllocate := podgroup_info.GetTasksToAllocate(testData.job, dummyTasksLessThen,
						dummyTasksLessThen, true)
					result := capacityPolicy.IsJobOverQueueCapacity(testData.job, tasksToAllocate)
					Expect(result.IsSchedulable).To(Equal(testData.expectedResult))
				})
			}

		})
		Context("allocated non preemptible over quota", func() {
			tests := map[string]struct {
				queues         map[common_info.QueueID]*rs.QueueAttributes
				job            *podgroup_info.PodGroupInfo
				expectedResult bool
			}{
				"unlimited queues - preemptible job - allocated non preemptible below quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirementsWithGpus(1),
										ResReqVector: resource_info.NewResourceRequirementsWithGpus(1).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					expectedResult: true,
				},
				"unlimited queues - preemptible job -  allocated non preemptible above quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 3,
									Deserved:                3,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirementsWithGpus(1),
										ResReqVector: resource_info.NewResourceRequirementsWithGpus(1).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					expectedResult: false,
				},
			}

			for name, data := range tests {
				testName := name
				testData := data
				It(testName, func() {
					capacityPolicy := New(testData.queues)
					tasksToAllocate := podgroup_info.GetTasksToAllocate(testData.job, dummyTasksLessThen,
						dummyTasksLessThen, true)
					result := capacityPolicy.IsJobOverQueueCapacity(testData.job, tasksToAllocate)
					Expect(result.IsSchedulable).To(Equal(testData.expectedResult))
				})
			}

		})
	})

	Describe("IsNonPreemptibleJobOverQuota", func() {
		Context("allocated non preemptible over quota", func() {
			tests := map[string]struct {
				queues         map[common_info.QueueID]*rs.QueueAttributes
				job            *podgroup_info.PodGroupInfo
				expectedResult bool
			}{
				"unlimited queues - preemptible job - allocated non preemptible below quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirementsWithGpus(1),
										ResReqVector: resource_info.NewResourceRequirementsWithGpus(1).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					expectedResult: true,
				},
				"unlimited queues - preemptible job -  allocated non preemptible above quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 3,
									Deserved:                3,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								GPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2,
									Deserved:                3,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirementsWithGpus(1),
										ResReqVector: resource_info.NewResourceRequirementsWithGpus(1).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					expectedResult: false,
				},
			}

			for name, data := range tests {
				testName := name
				testData := data
				It(testName, func() {
					capacityPolicy := New(testData.queues)
					tasksToAllocate := podgroup_info.GetTasksToAllocate(testData.job, dummyTasksLessThen,
						dummyTasksLessThen, true)
					result := capacityPolicy.IsNonPreemptibleJobOverQuota(testData.job, tasksToAllocate)
					Expect(result.IsSchedulable).To(Equal(testData.expectedResult))
				})
			}

		})
	})

	Describe("IsTaskAllocationOnNodeOverCapacity", func() {
		Context("allocated non preemptible over quota", func() {
			tests := map[string]struct {
				queues         map[common_info.QueueID]*rs.QueueAttributes
				job            *podgroup_info.PodGroupInfo
				node           *node_info.NodeInfo
				expectedResult bool
			}{
				"unlimited queues - allocated non preemptible job below quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 1000, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 1000, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: true,
				},
				"unlimited queues - allocated non preemptible job above quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 3000,
									Deserved:                3000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 1000, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 1000, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: false,
				},
				"unlimited queues - allocated preemptible job below quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.Preemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 1000, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 1000, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: true,
				},
				"unlimited queues - allocated preemptible job above quota": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 3000,
									Deserved:                3000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              commonconstants.UnlimitedResourceQuantity,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.Preemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 1000, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 1000, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: true,
				},
				"limited queue -  allocated non preemptible job below limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 500, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 500, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: true,
				},
				"limited queue -  allocated non preemptible job above limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                4000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                4000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                4000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.NonPreemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 1100, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 1100, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: false,
				},
				"limited queue -  allocated preemptible job below limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                3000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.Preemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 500, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 500, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: true,
				},
				"limited queue -  allocated preemptible job above limit": {
					queues: map[common_info.QueueID]*rs.QueueAttributes{
						"top-queue": {
							UID:               "top-queue",
							Name:              "top-queue",
							ParentQueue:       "",
							ChildQueues:       []common_info.QueueID{"mid-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                4000,
								},
							},
						},
						"mid-queue": {
							UID:               "mid-queue",
							Name:              "mid-queue",
							ParentQueue:       "top-queue",
							ChildQueues:       []common_info.QueueID{"leaf-queue"},
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                4000,
								},
							},
						},
						"leaf-queue": {
							UID:               "leaf-queue",
							Name:              "leaf-queue",
							ParentQueue:       "mid-queue",
							ChildQueues:       nil,
							CreationTimestamp: metav1.Time{},
							QueueResourceShare: rs.QueueResourceShare{
								CPU: rs.ResourceShare{
									MaxAllowed:              3000,
									Allocated:               2000,
									AllocatedNotPreemptible: 2000,
									Deserved:                4000,
								},
							},
						},
					},
					job: &podgroup_info.PodGroupInfo{
						Name:           "job-a",
						Namespace:      "team-a",
						Queue:          "leaf-queue",
						Preemptibility: v2alpha2.Preemptible,
						JobFitErrors:   make([]common_info.JobFitError, 0),
						PodSets: map[string]*subgroup_info.PodSet{
							podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
								WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
									"task-a": {
										UID:          "task-a",
										Job:          "job-a",
										Name:         "task-a",
										Namespace:    "team-a",
										Status:       pod_status.Pending,
										ResReq:       resource_info.NewResourceRequirements(0, 1100, 0),
										ResReqVector: resource_info.NewResourceRequirements(0, 1100, 0).ToVector(testVectorMap),
										VectorMap:    testVectorMap,
									},
								}),
						},
					},
					node: &node_info.NodeInfo{
						Name: "worker-node",
					},
					expectedResult: false,
				},
			}

			for name, data := range tests {
				testName := name
				testData := data
				It(testName, func() {
					capacityPolicy := New(testData.queues)
					result := capacityPolicy.IsTaskAllocationOnNodeOverCapacity(testData.job.GetAllPodsMap()["task-a"],
						testData.job, testData.node)
					Expect(result.IsSchedulable).To(Equal(testData.expectedResult))
				})
			}

		})
	})
})

func dummyTasksLessThen(_ interface{}, _ interface{}) bool {
	return false
}
