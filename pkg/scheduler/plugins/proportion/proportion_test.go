// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package proportion

import (
	"fmt"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

const schedulerName = "kai-scheduler"

var testVectorMap = resource_info.NewResourceVectorMap()

func TestSetFairShare(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proportion Suite")
}

var _ = Describe("Set Fair Share in Proportion", func() {
	Context("Set fair share for multi hierarchy queues", func() {
		tests := map[string]struct {
			queues            map[common_info.QueueID]*rs.QueueAttributes
			totalResources    rs.ResourceQuantities
			expectedFairShare map[common_info.QueueID]float64
		}{
			"simple scenario with single top queue": {
				queues: map[common_info.QueueID]*rs.QueueAttributes{
					"top-queue": {
						UID:               "top-queue",
						Name:              "top-queue",
						ParentQueue:       "",
						ChildQueues:       []common_info.QueueID{"mid-queue"},
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   3,
								Request:    3,
								FairShare:  0,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"mid-queue": {
						UID:               "mid-queue",
						Name:              "mid-queue",
						ParentQueue:       "top-queue",
						ChildQueues:       []common_info.QueueID{"leaf-queue-1", "leaf-queue-2"},
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   3,
								Request:    3,
								FairShare:  0,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"leaf-queue-1": {
						UID:               "leaf-queue-1",
						Name:              "leaf-queue-1",
						ParentQueue:       "mid-queue",
						ChildQueues:       nil,
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   2,
								Request:    2,
								FairShare:  0,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"leaf-queue-2": {
						UID:               "leaf-queue-2",
						Name:              "leaf-queue-2",
						ParentQueue:       "mid-queue",
						ChildQueues:       nil,
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   1,
								Request:    1,
								FairShare:  0,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
				},
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    3,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"top-queue":    3,
					"mid-queue":    3,
					"leaf-queue-1": 2,
					"leaf-queue-2": 1,
				},
			},
			"two top parent queues": {
				queues: map[common_info.QueueID]*rs.QueueAttributes{
					"top-queue-1": {
						UID:               "top-queue-1",
						Name:              "top-queue-1",
						ChildQueues:       []common_info.QueueID{"child-queue-1", "child-queue-2"},
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   2,
								Request:    2,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"child-queue-1": {
						UID:               "child-queue-1",
						Name:              "child-queue-1",
						ParentQueue:       "top-queue-1",
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   1,
								Request:    1,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"child-queue-2": {
						UID:               "child-queue-2",
						Name:              "child-queue-2",
						ParentQueue:       "top-queue-1",
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   1,
								Request:    1,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"top-queue-2": {
						UID:               "top-queue-2",
						Name:              "top-queue-2",
						ChildQueues:       []common_info.QueueID{"child-queue-3", "child-queue-4"},
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   2,
								Request:    2,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"child-queue-3": {
						UID:               "child-queue-3",
						Name:              "child-queue-3",
						ParentQueue:       "top-queue-2",
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed: commonconstants.UnlimitedResourceQuantity,
								Deserved:   0,
								Request:    0,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
					"child-queue-4": {
						UID:               "child-queue-4",
						Name:              "child-queue-4",
						ParentQueue:       "top-queue-2",
						CreationTimestamp: metav1.Time{},
						QueueResourceShare: rs.QueueResourceShare{
							GPU: rs.ResourceShare{
								MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
								Deserved:        1,
								OverQuotaWeight: 1,
								Request:         2,
							},
							CPU:    rs.ResourceShare{},
							Memory: rs.ResourceShare{},
						},
					},
				},
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    4,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"top-queue-1":   2,
					"child-queue-1": 1,
					"child-queue-2": 1,
					"top-queue-2":   2,
					"child-queue-3": 0,
					"child-queue-4": 2,
				},
			},
		}

		for name, data := range tests {
			testName := name
			testData := data
			It(testName, func() {
				proportion := &proportionPlugin{
					totalResource:   testData.totalResources,
					queues:          testData.queues,
					pluginArguments: map[string]string{},
				}

				proportion.setFairShare()
				for _, queue := range testData.queues {
					actual := queue.ResourceShare(rs.GpuResource).FairShare
					expected := testData.expectedFairShare[queue.UID]
					if actual != expected {
						Fail(fmt.Sprintf("Queue %s was supposed to have %v resources, got %v",
							queue.Name, expected, actual))
					}
				}
			})
		}
	})

	Context("Set fair share for 2 hierarchy queues - simplified", func() {
		getBaseQueues := func() map[common_info.QueueID]*rs.QueueAttributes {
			return map[common_info.QueueID]*rs.QueueAttributes{
				"d1": {
					UID:               "d1",
					Name:              "dep-1",
					ParentQueue:       "",
					ChildQueues:       []common_info.QueueID{"q1"},
					CreationTimestamp: metav1.Time{},
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
							Deserved:        2,
							OverQuotaWeight: 1,
							Request:         100,
							FairShare:       0,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				"q1": {
					UID:               "q1",
					Name:              "queue-1",
					ParentQueue:       "d1",
					ChildQueues:       []common_info.QueueID{},
					CreationTimestamp: metav1.Time{},
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
							Deserved:        2,
							OverQuotaWeight: 1,
							Request:         100,
							FairShare:       0,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				"d2": {
					UID:               "d2",
					Name:              "dep-2",
					ParentQueue:       "",
					ChildQueues:       []common_info.QueueID{"q2"},
					CreationTimestamp: metav1.Time{},
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
							Deserved:        2,
							OverQuotaWeight: 1,
							Request:         100,
							FairShare:       0,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
				"q2": {
					UID:               "q2",
					Name:              "queue-2",
					ParentQueue:       "d2",
					ChildQueues:       []common_info.QueueID{},
					CreationTimestamp: metav1.Time{},
					QueueResourceShare: rs.QueueResourceShare{
						GPU: rs.ResourceShare{
							MaxAllowed:      commonconstants.UnlimitedResourceQuantity,
							Deserved:        2,
							OverQuotaWeight: 1,
							Request:         100,
							FairShare:       0,
						},
						CPU:    rs.ResourceShare{},
						Memory: rs.ResourceShare{},
					},
				},
			}
		}
		type testData struct {
			priorityOverride  map[common_info.QueueID]int
			deservedOverride  map[common_info.QueueID]int
			weightOverride    map[common_info.QueueID]int
			totalResources    rs.ResourceQuantities
			expectedFairShare map[common_info.QueueID]float64
		}
		DescribeTable("Set fair share for 2 hierarchy queues - simplified", func(data testData) {
			queues := getBaseQueues()

			for id, deserved := range data.deservedOverride {
				queues[id].QueueResourceShare.GPU.Deserved = float64(deserved)
			}

			for id, priority := range data.priorityOverride {
				queues[id].Priority = priority
			}

			for id, weight := range data.weightOverride {
				queues[id].QueueResourceShare.GPU.OverQuotaWeight = float64(weight)
			}

			proportion := &proportionPlugin{
				totalResource:   data.totalResources,
				queues:          queues,
				pluginArguments: map[string]string{},
			}

			proportion.setFairShare()
			for _, queue := range queues {
				actual := queue.ResourceShare(rs.GpuResource).FairShare
				expected := data.expectedFairShare[queue.UID]
				if actual != expected {
					Fail(fmt.Sprintf("Queue %s was supposed to have %v GPUs, got %v",
						queue.Name, expected, actual))
				}
			}
		},
			Entry("two top parent queues - sanity", testData{
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    4,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"d1": 2,
					"q1": 2,
					"d2": 2,
					"q2": 2,
				},
			}),
			Entry("two top parent queues - deserved department quota", testData{
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    4,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				deservedOverride: map[common_info.QueueID]int{
					"d1": 3,
					"d2": 1,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"d1": 3,
					"q1": 3,
					"d2": 1,
					"q2": 2, // since we didn't change q2 deserved, it will get 2 as it's fair share - this is oversubscription
				},
			}),
			Entry("two top parent queues - over-quota", testData{
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    12,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				deservedOverride: map[common_info.QueueID]int{
					"d1": 3,
					"d2": 1,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"d1": 7,
					"q1": 7,
					"d2": 5,
					"q2": 5,
				},
			}),
			Entry("two top parent queues - over-quota weight", testData{
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    12,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				deservedOverride: map[common_info.QueueID]int{
					"d1": 3,
					"d2": 1,
				},
				weightOverride: map[common_info.QueueID]int{
					"d1": 1,
					"d2": 7,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"d1": 4,
					"q1": 4,
					"d2": 8,
					"q2": 8,
				},
			}),
			Entry("two top parent queues - over-quota weight - reversed", testData{
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    12,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				deservedOverride: map[common_info.QueueID]int{
					"d1": 3,
					"d2": 1,
				},
				weightOverride: map[common_info.QueueID]int{
					"d1": 7,
					"d2": 1,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"d1": 10,
					"q1": 10,
					"d2": 2,
					"q2": 2,
				},
			}),
			Entry("two top parent queues - priority", testData{
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    12,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				deservedOverride: map[common_info.QueueID]int{
					"d1": 1,
					"q1": 1,
					"d2": 1,
					"q2": 1,
				},
				weightOverride: map[common_info.QueueID]int{
					"d1": 7,
					"d2": 1,
				},
				priorityOverride: map[common_info.QueueID]int{
					"d1": 1,
					"d2": 2,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"d1": 1,
					"q1": 1,
					"d2": 11,
					"q2": 11,
				},
			}),
			Entry("two top parent queues - priority - reversed", testData{
				totalResources: rs.ResourceQuantities{
					rs.GpuResource:    12,
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
				},
				deservedOverride: map[common_info.QueueID]int{
					"d1": 1,
					"q1": 1,
					"d2": 1,
					"q2": 1,
				},
				weightOverride: map[common_info.QueueID]int{
					"d1": 1,
					"d2": 7,
				},
				priorityOverride: map[common_info.QueueID]int{
					"d1": 2,
					"d2": 1,
				},
				expectedFairShare: map[common_info.QueueID]float64{
					"d1": 11,
					"q1": 11,
					"d2": 1,
					"q2": 1,
				},
			}),
		)
	})

	Context("Get Node Resources", func() {
		tests := []struct {
			name           string
			isRestrictNode bool
			node           *node_info.NodeInfo
			allocatable    *resource_info.Resource
			want           rs.ResourceQuantities
		}{
			{
				name:           "cpu + memory node",
				isRestrictNode: true,
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{},
				},
				allocatable: common_info.BuildResource("8000m", "10G"),
				want: rs.ResourceQuantities{
					rs.CpuResource:    8000,
					rs.MemoryResource: 10000000000,
					rs.GpuResource:    0,
				},
			},
			{
				name:           "gpu node",
				isRestrictNode: true,
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"node-role.kubernetes.io/gpu-worker": "true",
							},
						},
					},
				},
				allocatable: resource_info.ResourceFromResourceList(
					common_info.BuildResourceListWithGPU("8000m", "10G", "2"),
				),
				want: rs.ResourceQuantities{
					rs.CpuResource:    8000,
					rs.MemoryResource: 10000000000,
					rs.GpuResource:    2,
				},
			},
			{
				name:           "mig gpu node",
				isRestrictNode: true,
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"node-role.kubernetes.io/gpu-worker": "true",
							},
						},
					},
				},
				allocatable: resource_info.ResourceFromResourceList(
					common_info.BuildResourceListWithMig("8000m", "10G", "nvidia.com/mig-1g.5gb"),
				),
				want: rs.ResourceQuantities{
					rs.CpuResource:    8000,
					rs.MemoryResource: 10000000000,
					rs.GpuResource:    1,
				},
			},
			{
				name:           "ignore extra resources",
				isRestrictNode: true,
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{},
				},
				allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
					"A": resource.MustParse("4"),
				}),
				want: rs.ResourceQuantities{
					rs.CpuResource:    0,
					rs.MemoryResource: 0,
					rs.GpuResource:    0,
				},
			},
			{
				name:           "Count out resources for non-related pods",
				isRestrictNode: true,
				allocatable:    common_info.BuildResource("8000m", "10G"),
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{},
					PodInfos: map[common_info.PodID]*pod_info.PodInfo{
						"1": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: schedulerName,
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("2", "2G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
						"2": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("1", "1G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
					},
				},
				want: rs.ResourceQuantities{
					rs.CpuResource:    7000,
					rs.MemoryResource: 9000000000,
					rs.GpuResource:    0,
				},
			},
			{
				name:           "Count out resources for non-related pods - consider reservation pods",
				isRestrictNode: true,
				allocatable:    common_info.BuildResource("8000m", "10G"),
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{},
					PodInfos: map[common_info.PodID]*pod_info.PodInfo{
						"1": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: schedulerName,
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("2", "2G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
						"2": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("1", "1G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
						"reservation": {
							Pod: &v1.Pod{
								ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
									commonconstants.AppLabelName: conf.GetConfig().ResourceReservationAppLabelValue,
								}},
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("1", "1G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
					},
				},
				want: rs.ResourceQuantities{
					rs.CpuResource:    7000,
					rs.MemoryResource: 9000000000,
					rs.GpuResource:    0,
				},
			},
			{
				name:           "Count out resources for non-related pods - consider scaler pods",
				isRestrictNode: true,
				allocatable:    common_info.BuildResource("8000m", "10G"),
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{},
					PodInfos: map[common_info.PodID]*pod_info.PodInfo{
						"1": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: schedulerName,
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("2", "2G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
						"2": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("1", "1G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
						"scaler": {
							Pod: &v1.Pod{
								ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
									commonconstants.AppLabelName: conf.GetConfig().ScalingPodAppLabelValue,
								}},
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status:       pod_status.Running,
							ResReqVector: common_info.BuildResourceRequirements("1", "1G").ToVector(testVectorMap),
							VectorMap:    testVectorMap,
						},
					},
				},
				want: rs.ResourceQuantities{
					rs.CpuResource:    7000,
					rs.MemoryResource: 9000000000,
					rs.GpuResource:    0,
				},
			},
			{
				name:           "Do not count out resources for non-related pods if non active",
				isRestrictNode: true,
				allocatable:    common_info.BuildResource("8000m", "10G"),
				node: &node_info.NodeInfo{
					Name: "n1",
					Node: &v1.Node{},
					PodInfos: map[common_info.PodID]*pod_info.PodInfo{
						"2": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status: pod_status.Pending,
						},
						"3": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status: pod_status.Succeeded,
						},
						"4": {
							Pod: &v1.Pod{
								Spec: v1.PodSpec{
									SchedulerName: "default-scheduler",
								},
							},
							Status: pod_status.Deleted,
						},
					},
				},
				want: rs.ResourceQuantities{
					rs.CpuResource:    8000,
					rs.MemoryResource: 10000000000,
					rs.GpuResource:    0,
				},
			},
		}

		for _, data := range tests {
			testData := data
			It(testData.name, func() {
				controller := gomock.NewController(GinkgoT())
				mockCache := cache.NewMockCache(controller)
				mockCache.EXPECT().Snapshot().Times(1).Return(api.NewClusterInfo(), nil)
				mockCache.EXPECT().InternalK8sPlugins().AnyTimes().Return(&k8splugins.K8sPlugins{})
				session, _ := framework.OpenSession(
					mockCache,
					&conf.SchedulerConfiguration{Tiers: []conf.Tier{}},
					&conf.SchedulerParams{
						RestrictSchedulingNodes: testData.isRestrictNode,
						SchedulerName:           schedulerName,
					},
					"1", nil)
				vectorMap := resource_info.NewResourceVectorMap()
				if testData.allocatable != nil {
					for rName := range testData.allocatable.ScalarResources() {
						vectorMap.AddResource(rName)
					}
				}
				testData.node.VectorMap = vectorMap
				if testData.allocatable != nil {
					testData.node.AllocatableVector = testData.allocatable.ToVector(vectorMap)
				}
				if got := getNodeResources(session, testData.node); !reflect.DeepEqual(got, testData.want) {
					Fail(fmt.Sprintf("getNodeResources() = %v, want %v", got, testData.want))
				}
			})
		}

	})

	Context("getVictimResources", func() {
		It("should handle case where MinAvailable is greater than number of tasks (panic fix)", func() {
			plugin := &proportionPlugin{
				allowConsolidatingReclaim: true,
			}

			// Create a victim with only 1 task but MinAvailable = 2
			// This should cause a slice bounds panic without the fix
			victim := &api.VictimInfo{
				Job: &podgroup_info.PodGroupInfo{
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(
							podgroup_info.DefaultSubGroup, 2, nil,
						),
					},
				},
				Tasks: []*pod_info.PodInfo{
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
				},
			}

			// This should not panic
			result := plugin.getVictimResources(victim)
			// Should return resources for the single task that exists
			Expect(len(result)).To(Equal(1))
			Expect(result[0]).ToNot(BeNil())
			Expect(result[0].Get(resource_info.CPUIndex)).To(Equal(1000.0))
		})

		It("should correctly split elastic and core tasks when MinAvailable is less than task count", func() {
			plugin := &proportionPlugin{
				allowConsolidatingReclaim: true,
			}

			// Create a victim with 3 tasks but MinAvailable = 1
			victim := &api.VictimInfo{
				Job: &podgroup_info.PodGroupInfo{
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(
							podgroup_info.DefaultSubGroup, 1, nil,
						),
					},
				},
				Tasks: []*pod_info.PodInfo{
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
				},
			}

			result := plugin.getVictimResources(victim)

			// Should return 3 resources: 2 elastic tasks + 1 core task group
			Expect(len(result)).To(Equal(3))
			for _, res := range result {
				Expect(res).ToNot(BeNil())
				Expect(res.Get(resource_info.CPUIndex)).To(Equal(1000.0))
			}
		})

		It("should handle case where MinAvailable equals task count", func() {
			plugin := &proportionPlugin{
				allowConsolidatingReclaim: true,
			}

			// Create a victim with 2 tasks and MinAvailable = 2
			victim := &api.VictimInfo{
				Job: &podgroup_info.PodGroupInfo{
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(
							podgroup_info.DefaultSubGroup, 2, nil,
						),
					},
				},
				Tasks: []*pod_info.PodInfo{
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
				},
			}

			result := plugin.getVictimResources(victim)

			// Should return 1 resource for all core tasks (no elastic tasks)
			Expect(len(result)).To(Equal(1))
			Expect(result[0]).ToNot(BeNil())
			Expect(result[0].Get(resource_info.CPUIndex)).To(Equal(2000.0)) // Combined resources
		})

		It("should handle zero MinAvailable", func() {
			plugin := &proportionPlugin{
				allowConsolidatingReclaim: true,
			}

			victim := &api.VictimInfo{
				Job: &podgroup_info.PodGroupInfo{
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(
							podgroup_info.DefaultSubGroup, 0, nil,
						),
					},
				},
				Tasks: []*pod_info.PodInfo{
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
					{
						Status:                 pod_status.Pending,
						AcceptedResourceVector: common_info.BuildResourceRequirements("1", "1Gi").ToVector(testVectorMap),
						VectorMap:              testVectorMap,
					},
				},
			}

			result := plugin.getVictimResources(victim)

			// Should return 2 resources (each task individually as elastic)
			Expect(len(result)).To(Equal(2))
			for _, res := range result {
				Expect(res).ToNot(BeNil())
				Expect(res.Get(resource_info.CPUIndex)).To(Equal(1000.0))
			}
		})
	})
})

var _ = Describe("New", func() {
	Context("Initializing proportion plugin", func() {
		var args framework.PluginArguments

		BeforeEach(func() {
			args = framework.PluginArguments{}
		})

		It("should create plugin with empty state and default multiplier", func() {
			plugin := New(args).(*proportionPlugin)
			Expect(plugin).NotTo(BeNil())
			Expect(plugin.totalResource).To(Equal(rs.EmptyResourceQuantities()))
			Expect(plugin.queues).To(HaveLen(0))
			Expect(plugin.pluginArguments).To(Equal(args))
		})

		It("should handle malformed Saturation Multiplier arg", func() {
			args := framework.PluginArguments{"relcaimerSaturationMultiplier": "wrong"}
			plugin := New(args).(*proportionPlugin)
			Expect(plugin.pluginArguments).To(Equal(args))
			Expect(plugin.relcaimerSaturationMultiplier).To(Equal(1.0))
		})

		It("should handle Saturation Multiplier arg", func() {
			args := framework.PluginArguments{"relcaimerSaturationMultiplier": "1.5"}
			plugin := New(args).(*proportionPlugin)
			Expect(plugin.pluginArguments).To(Equal(args))
			Expect(plugin.relcaimerSaturationMultiplier).To(Equal(1.5))
		})

		It("should prevent Saturation Multiplier lower than 1", func() {
			args := framework.PluginArguments{"relcaimerSaturationMultiplier": "0.5"}
			plugin := New(args).(*proportionPlugin)
			Expect(plugin.pluginArguments).To(Equal(args))
			Expect(plugin.relcaimerSaturationMultiplier).To(Equal(1.0))
		})
	})
})
