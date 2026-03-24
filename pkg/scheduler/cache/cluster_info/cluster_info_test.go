// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	v1core "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	v12 "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"

	kubeAiSchedulerClient "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned"
	kubeAiSchedulerClientFake "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	kubeAiSchedulerInfo "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/informers/externalversions"
	schedulingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	enginev2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/storageclass_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache/cluster_info/data_lister"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/utils"
)

const (
	successErrorMsg     = "SUCCESS"
	nodePoolNameLabel   = "kai.scheduler/node-pool"
	defaultNodePoolName = "default"
)

func TestSnapshot(t *testing.T) {
	tests := map[string]struct {
		kubeObjects          []runtime.Object
		kaiSchedulerObjects  []runtime.Object
		expectedNodes        int
		expectedDepartments  int
		expectedQueues       int
		expectedBindRequests int
	}{
		"SingleFromEach": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
				},
				&v1core.Pod{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod",
						Namespace: "my-ns",
					},
				},
			},
			kaiSchedulerObjects: []runtime.Object{
				&enginev2.Queue{
					ObjectMeta: v1.ObjectMeta{
						Name: "my-department",
					},
					Spec: enginev2.QueueSpec{
						Resources: &enginev2.QueueResources{},
					},
				},
				&enginev2.Queue{
					ObjectMeta: v1.ObjectMeta{
						Name: "my-queue",
					},
					Spec: enginev2.QueueSpec{
						ParentQueue: "my-department",
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod-1234",
						Namespace: "my-ns",
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      "my-pod",
						SelectedNode: "node-1",
					},
				},
			},
			expectedNodes:        1,
			expectedQueues:       2,
			expectedBindRequests: 1,
		},
		"SingleFromEach2": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
				},
			},
			kaiSchedulerObjects: []runtime.Object{
				&enginev2.Queue{
					ObjectMeta: v1.ObjectMeta{
						Name: "my-department",
					},
					Spec: enginev2.QueueSpec{
						Resources: &enginev2.QueueResources{},
					},
				},
				&enginev2.Queue{
					ObjectMeta: v1.ObjectMeta{
						Name: "my-queue",
					},
					Spec: enginev2.QueueSpec{
						ParentQueue: "my-department",
					},
				},
			},
			expectedNodes:  1,
			expectedQueues: 2,
		},
	}

	for name, test := range tests {
		t.Logf("Running test %s", name)
		clusterInfo := newClusterInfoTests(t, test.kubeObjects, test.kaiSchedulerObjects)
		snapshot, err := clusterInfo.Snapshot()
		assert.Equal(t, nil, err)
		assert.Equal(t, test.expectedNodes, len(snapshot.Nodes))
		assert.Equal(t, test.expectedQueues, len(snapshot.Queues))

		assert.Equal(t, test.expectedBindRequests, len(snapshot.BindRequests))
	}
}

func TestSnapshotNodes(t *testing.T) {
	examplePod := &v1core.Pod{
		Spec: v1core.PodSpec{
			NodeName: "node-1",
			Containers: []v1core.Container{
				{
					Resources: v1core.ResourceRequirements{
						Requests: v1core.ResourceList{
							"cpu": resource.MustParse("2"),
						},
					},
				},
			},
		},
		Status: v1core.PodStatus{
			Phase: v1core.PodRunning,
		},
	}
	exampleMIGPod := examplePod.DeepCopy()
	exampleMIGPod.Name = "mig-pod"
	exampleMIGPod.Spec.Containers[0].Resources.Requests["nvidia.com/mig-1g.5gb"] = resource.MustParse("2")
	exampleMIGPodWithPG := examplePod.DeepCopy()
	exampleMIGPodWithPG.Name = "mig-pod-with-pg"
	exampleMIGPodWithPG.Annotations = map[string]string{
		commonconstants.PodGroupAnnotationForPod: "pg-1",
	}
	exampleMIGPodWithPG.Spec.Containers[0].Resources.Requests["nvidia.com/mig-1g.5gb"] = resource.MustParse("2")
	tests := map[string]struct {
		objs          []runtime.Object
		resultNodes   []*node_info.NodeInfo
		resultPodsLen int
		nodePoolName  string
	}{
		"BasicUsage": {
			objs: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			resultNodes: []*node_info.NodeInfo{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu": resource.MustParse("8"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("0"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":    resource.MustParse("0"),
							"memory": resource.MustParse("0"),
						},
					),
				},
			},
			resultPodsLen: 1,
		},
		"Finished job": {
			objs: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				newCompletedPod(examplePod),
			},
			resultNodes: []*node_info.NodeInfo{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":    resource.MustParse("0"),
							"memory": resource.MustParse("0"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":    resource.MustParse("0"),
							"memory": resource.MustParse("0"),
						},
					),
				},
			},
			resultPodsLen: 1,
		},
		"Filter Pods by nodepool": {
			objs: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							defaultNodePoolName: "pool-a",
						},
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-2",
						Labels: map[string]string{
							defaultNodePoolName: "pool-b",
						},
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				newPodOnNode(examplePod, "node-1"),
				newPodOnNode(examplePod, "node-2"),
			},
			resultNodes: []*node_info.NodeInfo{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu": resource.MustParse("8"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("0"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":    resource.MustParse("0"),
							"memory": resource.MustParse("0"),
						},
					),
				},
			},
			resultPodsLen: 1,
			nodePoolName:  "pool-a",
		},
		"MIG Job": {
			objs: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu":                   resource.MustParse("10"),
							"nvidia.com/mig-1g.5gb": resource.MustParse("10"),
						},
					},
				},
				exampleMIGPod,
				exampleMIGPodWithPG,
			},
			resultNodes: []*node_info.NodeInfo{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":                   resource.MustParse("6"),
							"nvidia.com/mig-1g.5gb": resource.MustParse("6"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":                   resource.MustParse("4"),
							"memory":                resource.MustParse("0"),
							"nvidia.com/mig-1g.5gb": resource.MustParse("4"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						v1core.ResourceList{
							"cpu":    resource.MustParse("0"),
							"memory": resource.MustParse("0"),
						},
					),
				},
			},
			resultPodsLen: 2,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			clusterInfo := newClusterInfoTestsInner(
				t, test.objs, []runtime.Object{},
				&conf.SchedulingNodePoolParams{
					NodePoolLabelKey:   defaultNodePoolName,
					NodePoolLabelValue: test.nodePoolName,
				},
				true,
			)
			existingPods := map[common_info.PodID]*pod_info.PodInfo{}

			controller := gomock.NewController(t)
			clusterPodAffinityInfo := pod_affinity.NewMockClusterPodAffinityInfo(controller)
			clusterPodAffinityInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
			clusterPodAffinityInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()

			allPods, _ := clusterInfo.dataLister.ListPods()
			nodes, _, err := clusterInfo.snapshotNodes(clusterPodAffinityInfo)
			if err != nil {
				assert.FailNow(t, fmt.Sprintf("SnapshotNode got error in test %s", t.Name()), err)
			}
			pods, err := clusterInfo.addTasksToNodes(allPods, existingPods, nodes, nil)

			assert.Equal(t, len(test.resultNodes), len(nodes))
			assert.Equal(t, test.resultPodsLen, len(pods))

			for _, expectedNode := range test.resultNodes {
				assert.Equal(t, expectedNode.Idle, nodes[expectedNode.Name].Idle, "Expected idle resources to be equal")
				assert.Equal(t, expectedNode.Used, nodes[expectedNode.Name].Used, "Expected used resources to be equal")
				assert.Equal(t, expectedNode.Releasing, nodes[expectedNode.Name].Releasing, "Expected releasing resources to be equal")
			}
		})
	}
}

func TestBindRequests(t *testing.T) {
	examplePodName := "pod-1"
	namespace1 := "namespace-1"
	podGroupName := "podgroup-1"
	examplePod := &v1core.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      examplePodName,
			Namespace: namespace1,
			UID:       types.UID(examplePodName),
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: podGroupName,
			},
		},
		Spec: v1core.PodSpec{
			Containers: []v1core.Container{
				{
					Resources: v1core.ResourceRequirements{
						Requests: v1core.ResourceList{
							"cpu": resource.MustParse("2"),
						},
					},
				},
			},
		},
		Status: v1core.PodStatus{
			Phase: v1core.PodPending,
		},
	}

	exampleQueue := &enginev2.Queue{
		ObjectMeta: v1.ObjectMeta{
			Name: "queue-0",
		},
	}

	tests := map[string]struct {
		kubeObjects             []runtime.Object
		kaiSchedulerObjects     []runtime.Object
		expectedProcessing      int
		expectedStale           int
		expectedForDeletedNodes int
		expectedPodStatus       map[string]map[string]pod_status.PodStatus
		resultNodes             map[string]*resource.Quantity
	}{
		"Pod with PodGroup Waiting For Binding": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod-1234",
						Namespace: namespace1,
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      examplePodName,
						SelectedNode: "node-1",
					},
				},
			},
			expectedProcessing: 1,
			expectedPodStatus: map[string]map[string]pod_status.PodStatus{
				namespace1: {
					examplePodName: pod_status.Binding,
				},
			},
			resultNodes: map[string]*resource.Quantity{
				"node-1": ptr.To(resource.MustParse("8")),
			},
		},
		"Pod with PodGroup Waiting For Binding that is failing but no at backoff limit": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod-1234",
						Namespace: namespace1,
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      examplePodName,
						SelectedNode: "node-1",
						BackoffLimit: ptr.To(int32(5)),
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase:          schedulingv1alpha2.BindRequestPhaseFailed,
						FailedAttempts: 2,
					},
				},
			},
			expectedProcessing: 1,
			expectedPodStatus: map[string]map[string]pod_status.PodStatus{
				namespace1: {
					examplePodName: pod_status.Binding,
				},
			},
			resultNodes: map[string]*resource.Quantity{
				"node-1": ptr.To(resource.MustParse("8")),
			},
		},
		"Pod with PodGroup Waiting For Binding that is failing and reached backoff limit": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod-1234",
						Namespace: namespace1,
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      examplePodName,
						SelectedNode: "node-1",
						BackoffLimit: ptr.To(int32(5)),
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase:          schedulingv1alpha2.BindRequestPhaseFailed,
						FailedAttempts: 5,
					},
				},
			},
			expectedProcessing: 0,
			expectedStale:      1,
			expectedPodStatus: map[string]map[string]pod_status.PodStatus{
				namespace1: {
					examplePodName: pod_status.Pending,
				},
			},
			resultNodes: map[string]*resource.Quantity{
				"node-1": ptr.To(resource.MustParse("10")),
			},
		},
		"Pod pending and BindRequest to a different pod": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
				func() *v1core.Pod {
					pod := examplePod.DeepCopy()
					pod.Name = "not-" + examplePod.Name
					pod.UID = types.UID(fmt.Sprintf("not-%s", examplePod.UID))
					return pod
				}(),
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      fmt.Sprintf("not-%s-1234", examplePodName),
						Namespace: namespace1,
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      fmt.Sprintf("not-%s", examplePodName),
						SelectedNode: "node-1",
					},
				},
			},
			expectedProcessing: 1,
			expectedStale:      0,
			expectedPodStatus: map[string]map[string]pod_status.PodStatus{
				namespace1: {
					examplePodName: pod_status.Pending,
				},
			},
			resultNodes: map[string]*resource.Quantity{
				"node-1": ptr.To(resource.MustParse("8")),
			},
		},
		"Pod pending and BindRequest to non existing node and is failed": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod-1234",
						Namespace: namespace1,
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      examplePodName,
						SelectedNode: "node-2",
						BackoffLimit: ptr.To(int32(5)),
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase:          schedulingv1alpha2.BindRequestPhaseFailed,
						FailedAttempts: 5,
					},
				},
			},
			expectedStale:           0,
			expectedForDeletedNodes: 1,
			expectedPodStatus: map[string]map[string]pod_status.PodStatus{
				namespace1: {
					examplePodName: pod_status.Pending,
				},
			},
			resultNodes: map[string]*resource.Quantity{
				"node-1": ptr.To(resource.MustParse("10")),
			},
		},
		"Pod pending with stale bind request from another shard and node is not in our shard": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod-1234",
						Namespace: namespace1,
						Labels: map[string]string{
							nodePoolNameLabel: "other-value",
						},
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      examplePodName,
						SelectedNode: "node-2",
					},
				},
			},
			expectedProcessing: 0,
			expectedPodStatus: map[string]map[string]pod_status.PodStatus{
				namespace1: {
					examplePodName: pod_status.Pending,
				},
			},
			resultNodes: map[string]*resource.Quantity{
				"node-1": ptr.To(resource.MustParse("10")),
			},
		},
		"Pod pending with stale bind request from another shard and node is actually in our shard": {
			kubeObjects: []runtime.Object{
				&v1core.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1core.NodeStatus{
						Allocatable: v1core.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: v1.ObjectMeta{
						Name:      "my-pod-1234",
						Namespace: namespace1,
						Labels: map[string]string{
							nodePoolNameLabel: "other-value",
						},
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      examplePodName,
						SelectedNode: "node-1",
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase: schedulingv1alpha2.BindRequestPhaseFailed,
					},
				},
			},
			expectedStale: 1,
			expectedPodStatus: map[string]map[string]pod_status.PodStatus{
				namespace1: {
					examplePodName: pod_status.Pending,
				},
			},
			resultNodes: map[string]*resource.Quantity{
				"node-1": ptr.To(resource.MustParse("10")),
			},
		},
	}

	for name, test := range tests {
		t.Logf("Running test %s", name)
		clusterInfo := newClusterInfoTests(t, test.kubeObjects, test.kaiSchedulerObjects)
		snapshot, err := clusterInfo.Snapshot()
		assert.Equal(t, nil, err)

		processingBindRequests := 0
		staleBindRequests := 0
		for _, bindRequest := range snapshot.BindRequests {
			if bindRequest.IsFailed() {
				staleBindRequests++
			} else {
				processingBindRequests++
			}
		}
		assert.Equal(t, test.expectedProcessing, processingBindRequests)
		assert.Equal(t, test.expectedStale, staleBindRequests)

		assert.Equal(t, test.expectedForDeletedNodes, len(snapshot.BindRequestsForDeletedNodes))

		assertedPods := 0
		for _, podGroup := range snapshot.PodGroupInfos {
			for _, podInfo := range podGroup.PodInfos {
				byNamespace, found := test.expectedPodStatus[podInfo.Pod.Namespace]
				if !found {
					continue
				}
				expectedPodStatus, found := byNamespace[podInfo.Pod.Name]
				if !found {
					continue
				}
				assert.Equal(t, expectedPodStatus, podInfo.Status)
				assertedPods++
			}
		}

		expectedPodAsserts := 0
		for _, pods := range test.expectedPodStatus {
			expectedPodAsserts += len(pods)
		}
		assert.Equal(t, assertedPods, expectedPodAsserts)

		for _, node := range snapshot.Nodes {
			assert.Equal(t, float64(test.resultNodes[node.Name].MilliValue()), node.Idle.Cpu())
		}
	}
}

func TestSnapshotPodGroups(t *testing.T) {
	tests := map[string]struct {
		objs     []runtime.Object
		kubeObjs []runtime.Object
		results  []*podgroup_info.PodGroupInfo
	}{
		"BasicUsage": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: "queue-0",
					},
				},
			},
			kubeObjs: []runtime.Object{
				&v1core.Pod{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
					},
				},
				&policyv1.PodDisruptionBudget{
					ObjectMeta: v1.ObjectMeta{
						Name: "pdb-0",
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				{
					Name: "podGroup-0",
				},
			},
		},
		"WithPDB": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name: "",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: "queue-0",
					},
				},
			},
			kubeObjs: []runtime.Object{
				&v1core.Pod{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
					},
				},
				&policyv1.PodDisruptionBudget{
					ObjectMeta: v1.ObjectMeta{
						Name: "pdb-0",
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				{
					Name: "podGroup-0",
				},
			},
		},
		"NotExistingQueue": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: "queue-1",
					},
				},
			},
			kubeObjs: []runtime.Object{
				&v1core.Pod{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
					},
				},
				&policyv1.PodDisruptionBudget{
					ObjectMeta: v1.ObjectMeta{
						Name: "pdb-0",
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{},
		},
		"filter unassigned pod groups - no scheduling backoff": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue:             "queue-0",
						SchedulingBackoff: ptr.To(int32(utils.NoSchedulingBackoff)),
					},
					Status: enginev2alpha2.PodGroupStatus{
						SchedulingConditions: []enginev2alpha2.SchedulingCondition{
							{
								NodePool: defaultNodePoolName,
							},
						},
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				{
					Name: "podGroup-0",
				},
			},
		},
		"filter unassigned pod groups - no scheduling conditions": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue:             "queue-0",
						SchedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				{
					Name: "podGroup-0",
				},
			},
		},
		"filter unassigned pod groups - unschedulable in different nodepool": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue:             "queue-0",
						SchedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
					},
					Status: enginev2alpha2.PodGroupStatus{
						SchedulingConditions: []enginev2alpha2.SchedulingCondition{
							{
								NodePool: "some-node-pool",
							},
						},
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				{
					Name: "podGroup-0",
				},
			},
		},
		"filter unassigned pod groups - unassigned": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: v1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue:             "queue-0",
						SchedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
					},
					Status: enginev2alpha2.PodGroupStatus{
						SchedulingConditions: []enginev2alpha2.SchedulingCondition{
							{
								NodePool: defaultNodePoolName,
							},
						},
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{},
		},
	}

	for name, test := range tests {
		clusterInfo := newClusterInfoTests(t, test.kubeObjs, test.objs)
		predefinedQueue := &queue_info.QueueInfo{Name: "queue-0"}
		existingPods := map[common_info.PodID]*pod_info.PodInfo{}
		// TODO
		podGroups, err := clusterInfo.snapshotPodGroups(
			map[common_info.QueueID]*queue_info.QueueInfo{"queue-0": predefinedQueue},
			existingPods)
		if err != nil {
			assert.FailNow(t, fmt.Sprintf("SnapshotNode got error in test %v", name), err)
		}
		assert.Equal(t, len(test.results), len(podGroups))
	}
}

func TestSnapshotQueues(t *testing.T) {
	objs := []runtime.Object{
		&enginev2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "department0",
			},
			Spec: enginev2.QueueSpec{
				DisplayName: "department-zero",
				Resources: &enginev2.QueueResources{
					GPU: enginev2.QueueResource{
						Quota: 4,
					},
				},
			},
		},
		&enginev2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "department0-a",
				Labels: map[string]string{
					nodePoolNameLabel: "nodepool-a",
				},
			},
			Spec: enginev2.QueueSpec{
				Resources: &enginev2.QueueResources{
					GPU: enginev2.QueueResource{
						Quota: 2,
					},
				},
			},
		},
		&enginev2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name:   "queue0",
				Labels: map[string]string{},
			},
			Spec: enginev2.QueueSpec{
				ParentQueue: "department0",
				Resources: &enginev2.QueueResources{
					GPU: enginev2.QueueResource{
						Quota: 2,
					},
				},
			},
		},
	}
	kubeObjs := []runtime.Object{}

	clusterInfo := newClusterInfoTests(t, kubeObjs, objs)
	snapshot, err := clusterInfo.Snapshot()
	assert.Nil(t, err)
	assert.Equal(t, 2, len(snapshot.Queues))
	assert.Equal(t, common_info.QueueID("queue0"), snapshot.Queues["queue0"].UID)
	assert.Equal(t, common_info.QueueID("department0"), snapshot.Queues["department0"].UID)
	assert.Equal(t, "queue0", snapshot.Queues["queue0"].Name)
	assert.Equal(t, "department-zero", snapshot.Queues["department0"].Name)
	assert.Equal(t, common_info.QueueID(""), snapshot.Queues["department0"].ParentQueue)
	assert.Equal(t, common_info.QueueID("department0"), snapshot.Queues["queue0"].ParentQueue)
	assert.Equal(t, []common_info.QueueID{"queue0"}, snapshot.Queues["department0"].ChildQueues)
	assert.Equal(t, []common_info.QueueID{}, snapshot.Queues["queue0"].ChildQueues)
}

func TestSnapshotFlatHierarchy(t *testing.T) {
	parentQueue0 := &enginev2.Queue{
		ObjectMeta: v1.ObjectMeta{
			Name: "department0",
			Labels: map[string]string{
				nodePoolNameLabel: "nodepool-a",
			},
		},
		Spec: enginev2.QueueSpec{
			Resources: &enginev2.QueueResources{
				GPU: enginev2.QueueResource{
					Quota:           4,
					OverQuotaWeight: 2,
					Limit:           10,
				},
			},
		},
	}
	parentQueue1 := parentQueue0.DeepCopy()
	parentQueue1.Name = "department1"

	queue0 := &enginev2.Queue{
		ObjectMeta: v1.ObjectMeta{
			Name: "queue0",
			Labels: map[string]string{
				nodePoolNameLabel: "nodepool-a",
			},
		},
		Spec: enginev2.QueueSpec{
			ParentQueue: parentQueue0.Name,
		},
	}
	queue1 := &enginev2.Queue{
		ObjectMeta: v1.ObjectMeta{
			Name: "queue1",
			Labels: map[string]string{
				nodePoolNameLabel: "nodepool-a",
			},
		},
		Spec: enginev2.QueueSpec{
			ParentQueue: parentQueue1.Name,
		},
	}
	objects := []runtime.Object{parentQueue0, parentQueue1, queue0, queue1}
	params := &conf.SchedulingNodePoolParams{
		NodePoolLabelKey:   nodePoolNameLabel,
		NodePoolLabelValue: "nodepool-a"}
	clusterInfo := newClusterInfoTestsInner(t, []runtime.Object{}, objects, params, false)

	snapshot, err := clusterInfo.Snapshot()
	assert.Nil(t, err)
	assert.Equal(t, 3, len(snapshot.Queues))

	defaultParentQueueId := common_info.QueueID(defaultQueueName)
	parentQueue, found := snapshot.Queues[defaultParentQueueId]
	assert.True(t, found)
	assert.Equal(t, parentQueue.Name, defaultQueueName)
	assert.Equal(t, parentQueue.UID, defaultParentQueueId)
	assert.Equal(t, parentQueue.Resources, queue_info.QueueQuota{
		GPU: queue_info.ResourceQuota{
			Quota:           -1,
			OverQuotaWeight: 1,
			Limit:           -1,
		},
		CPU: queue_info.ResourceQuota{
			Quota:           -1,
			OverQuotaWeight: 1,
			Limit:           -1,
		},
		Memory: queue_info.ResourceQuota{
			Quota:           -1,
			OverQuotaWeight: 1,
			Limit:           -1,
		},
	})
	snapshotQueue0, found := snapshot.Queues[common_info.QueueID(queue0.Name)]
	assert.True(t, found)
	assert.Equal(t, snapshotQueue0.ParentQueue, defaultParentQueueId)

	snapshotQueue1, found := snapshot.Queues[common_info.QueueID(queue1.Name)]
	assert.True(t, found)
	assert.Equal(t, snapshotQueue1.ParentQueue, defaultParentQueueId)
}

func TestGetPodGroupPriority(t *testing.T) {
	kubeObjects := []runtime.Object{
		&v12.PriorityClass{
			ObjectMeta: v1.ObjectMeta{
				Name: "my-priority",
			},
			Value: 2,
		},
	}
	podGroup := &enginev2alpha2.PodGroup{
		Spec: enginev2alpha2.PodGroupSpec{
			PriorityClassName: "my-priority",
		},
	}

	clusterInfo := newClusterInfoTests(t, kubeObjects, []runtime.Object{})

	priority := getPodGroupPriority(podGroup, 1, clusterInfo.dataLister)
	assert.Equal(t, int32(2), priority)
}

func TestSnapshotStorageObjects(t *testing.T) {
	kubeObjects := []runtime.Object{
		&storage.CSIDriver{
			ObjectMeta: v1.ObjectMeta{Name: "csi-driver"},
			Spec: storage.CSIDriverSpec{
				StorageCapacity: ptr.To(true),
			},
		},
		&storage.StorageClass{
			ObjectMeta: v1.ObjectMeta{
				Name: "storage-class",
			},
			Provisioner:       "csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingWaitForFirstConsumer))),
		},
		&storage.StorageClass{
			ObjectMeta: v1.ObjectMeta{
				Name: "non-csi-storage-class",
			},
			Provisioner:       "non-csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingWaitForFirstConsumer))),
		},
		&storage.StorageClass{
			ObjectMeta: v1.ObjectMeta{
				Name: "immediate-binding-storage-class",
			},
			Provisioner:       "csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingImmediate))),
		},
		&v1core.PersistentVolumeClaim{
			ObjectMeta: v1.ObjectMeta{
				Name:      nonOwnedClaimName,
				Namespace: testNamespace,
				UID:       "csi-pvc-uid",
			},
			Spec: v1core.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("storage-class"),
			},
			Status: v1core.PersistentVolumeClaimStatus{Phase: v1core.ClaimBound},
		},
		&v1core.PersistentVolumeClaim{
			ObjectMeta: v1.ObjectMeta{
				Name:      ownedClaimName,
				Namespace: testNamespace,
				UID:       "owned-csi-pvc-uid",
				OwnerReferences: []v1.OwnerReference{
					{
						APIVersion: "v1",
						Kind:       "pod",
						Name:       "owner-pod",
						UID:        "owner-pod-uid",
					},
				},
			},
			Spec: v1core.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("storage-class"),
			},
			Status: v1core.PersistentVolumeClaimStatus{Phase: v1core.ClaimBound},
		},
		&v1core.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name:      "owner-pod",
				Namespace: testNamespace,
				UID:       "owner-pod-uid",
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod: "podGroup-0",
				},
			},
			Spec: v1core.PodSpec{
				Volumes: []v1core.Volume{
					{
						VolumeSource: v1core.VolumeSource{
							PersistentVolumeClaim: &v1core.PersistentVolumeClaimVolumeSource{
								ClaimName: ownedClaimName,
							},
						},
					},
				},
			},
		},
	}

	kubeAiSchedOjbs := []runtime.Object{
		&enginev2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "queue-0",
			},
		},
		&enginev2alpha2.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Name: "podGroup-0",
				UID:  "ABC",
			},
			Spec: enginev2alpha2.PodGroupSpec{
				Queue: "queue-0",
			},
		},
	}

	clusterInfo := newClusterInfoTests(t, kubeObjects, kubeAiSchedOjbs)

	snapshot, err := clusterInfo.Snapshot()
	assert.Nil(t, err)

	expectedStorageClasses := map[common_info.StorageClassID]*storageclass_info.StorageClassInfo{
		"storage-class": {
			ID:          "storage-class",
			Provisioner: "csi-driver",
		},
	}

	assert.Equal(t, expectedStorageClasses, snapshot.StorageClasses)
	expectedStorageClaims := map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{
		nonOwnedClaimKey: {
			Key:               nonOwnedClaimKey,
			Name:              nonOwnedClaimName,
			Namespace:         testNamespace,
			Size:              resource.NewQuantity(0, resource.BinarySI),
			Phase:             v1core.ClaimBound,
			StorageClass:      "storage-class",
			PodOwnerReference: nil,
			DeletedOwner:      true,
		},
		ownedClaimKey: {
			Key:          ownedClaimKey,
			Name:         ownedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(0, resource.BinarySI),
			Phase:        v1core.ClaimBound,
			StorageClass: "storage-class",
			PodOwnerReference: &storageclaim_info.PodOwnerReference{
				PodID:        "owner-pod-uid",
				PodName:      "owner-pod",
				PodNamespace: testNamespace,
			},
			DeletedOwner: false,
		},
	}

	assert.Equal(t, expectedStorageClaims[ownedClaimKey], snapshot.StorageClaims[ownedClaimKey])
}

func TestGetPodGroupPriorityNotExistingPriority(t *testing.T) {
	podGroup := &enginev2alpha2.PodGroup{
		Spec: enginev2alpha2.PodGroupSpec{
			PriorityClassName: "my-priority",
		},
	}

	clusterInfo := newClusterInfoTests(t, []runtime.Object{}, []runtime.Object{})

	priority := getPodGroupPriority(podGroup, 123, clusterInfo.dataLister)
	assert.Equal(t, int32(123), priority)
}

func TestGetDefaultPriority(t *testing.T) {
	kubeObjects := []runtime.Object{
		&v12.PriorityClass{
			ObjectMeta: v1.ObjectMeta{
				Name: "my-priority",
			},
			Value:         2,
			GlobalDefault: true,
		},
	}

	clusterInfo := newClusterInfoTests(t, kubeObjects, []runtime.Object{})

	priority, err := getDefaultPriority(clusterInfo.dataLister)
	assert.Equal(t, nil, err)
	assert.Equal(t, int32(2), priority)
}

func TestGetDefaultPriorityNotExists(t *testing.T) {
	kubeObjects := []runtime.Object{
		&v12.PriorityClass{
			ObjectMeta: v1.ObjectMeta{
				Name: "my-priority",
			},
			Value: 2,
		},
	}
	clusterInfo := newClusterInfoTests(t, kubeObjects, []runtime.Object{})
	priority, err := getDefaultPriority(clusterInfo.dataLister)
	assert.Equal(t, nil, err)
	assert.Equal(t, int32(50), priority)
}

func TestGetDefaultPriorityWithError(t *testing.T) {
	clusterInfo := newClusterInfoTests(t, []runtime.Object{}, []runtime.Object{})
	priority, err := getDefaultPriority(clusterInfo.dataLister)
	assert.Equal(t, nil, err)
	assert.Equal(t, int32(50), priority)
}

func TestPodGroupWithIndex(t *testing.T) {
	podGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: v1.ObjectMeta{
			UID: "ABC",
		},
	}
	podGroupInfo := &podgroup_info.PodGroupInfo{
		PodGroupUID: "ABC",
	}

	clusterInfo := newClusterInfoTests(t, []runtime.Object{}, []runtime.Object{})
	clusterInfo.setPodGroupWithIndex(podGroup, podGroupInfo)
}

func TestPodGroupWithIndexNonMatching(t *testing.T) {
	podGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: v1.ObjectMeta{
			UID: "ABC",
		},
	}
	podGroupInfo := &podgroup_info.PodGroupInfo{
		PodGroupUID: "MyTest",
	}

	clusterInfo := newClusterInfoTests(t, []runtime.Object{}, []runtime.Object{})
	clusterInfo.setPodGroupWithIndex(podGroup, podGroupInfo)
}

func TestIsPodGroupUpForScheduler(t *testing.T) {
	testCases := []struct {
		testName                string
		schedulingBackoff       *int32
		nodePoolName            string
		lastSchedulingCondition *enginev2alpha2.SchedulingCondition
		expectedResult          bool
	}{
		{
			testName:          "Infinite schedulingBackoff",
			schedulingBackoff: ptr.To(int32(utils.NoSchedulingBackoff)),
			nodePoolName:      "nodepoola",
			lastSchedulingCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: "nodepoola",
			},
			expectedResult: true,
		},
		{
			testName:          "Nil schedulingBackoff",
			schedulingBackoff: nil,
			nodePoolName:      "nodepoolb",
			lastSchedulingCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: "nodepoolb",
			},
			expectedResult: true,
		},
		{
			testName:                "No last scheduling condition",
			schedulingBackoff:       ptr.To(int32(utils.SingleSchedulingBackoff)),
			nodePoolName:            "nodepoolb",
			lastSchedulingCondition: nil,
			expectedResult:          true,
		},
		{
			testName:                "No last scheduling condition - default node pool",
			schedulingBackoff:       ptr.To(int32(utils.SingleSchedulingBackoff)),
			nodePoolName:            defaultNodePoolName,
			lastSchedulingCondition: nil,
			expectedResult:          true,
		},
		{
			testName:          "unassigned by condition from different node pool",
			schedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
			nodePoolName:      "nodepoola",
			lastSchedulingCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: "different-nodepool",
			},
			expectedResult: true,
		},
		{
			testName:          "unassigned by condition",
			schedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
			nodePoolName:      "nodepoolc",
			lastSchedulingCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: "nodepoolc",
			},
			expectedResult: false,
		},
		{
			testName:          "unassigned by condition - default node pool",
			schedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
			nodePoolName:      defaultNodePoolName,
			lastSchedulingCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: "different-nodepool",
			},
			expectedResult: true,
		},
		{
			testName:          "unassigned by condition - default node pool 2",
			schedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
			nodePoolName:      "nodepoolc",
			lastSchedulingCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: defaultNodePoolName,
			},
			expectedResult: true,
		},
		{
			testName:          "unassigned by condition - default node pool",
			schedulingBackoff: ptr.To(int32(utils.SingleSchedulingBackoff)),
			nodePoolName:      defaultNodePoolName,
			lastSchedulingCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: defaultNodePoolName,
			},
			expectedResult: false,
		},
	}

	for _, testData := range testCases {
		pg := createFakePodGroup("test-pg", testData.schedulingBackoff, testData.nodePoolName,
			testData.lastSchedulingCondition)
		ci := newClusterInfoTests(t, []runtime.Object{}, []runtime.Object{})
		result := ci.isPodGroupUpForScheduler(pg)
		assert.Equal(t, result, testData.expectedResult,
			"Test: <%s>, expected pod group to be up for scheduler <%t>", testData.testName,
			testData.expectedResult)
	}
}

func TestNotSchedulingPodWithTerminatingPVC(t *testing.T) {
	kubeObjects := []runtime.Object{
		&v1core.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name:      "pod-1",
				Namespace: "test",
				UID:       "pod-1",
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod: "podGroup-0",
				},
			},
			Spec: v1core.PodSpec{
				Containers: []v1core.Container{
					{
						Name: "container-1",
					},
				},
				Volumes: []v1core.Volume{
					{
						Name: "pv-1",
						VolumeSource: v1core.VolumeSource{
							PersistentVolumeClaim: &v1core.PersistentVolumeClaimVolumeSource{
								ClaimName: "pvc-1",
							},
						},
					},
				},
			},
			Status: v1core.PodStatus{
				Phase: v1core.PodPending,
			},
		},
		&storage.CSIDriver{
			ObjectMeta: v1.ObjectMeta{Name: "csi-driver"},
			Spec: storage.CSIDriverSpec{
				StorageCapacity: ptr.To(true),
			},
		},
		&storage.StorageClass{
			ObjectMeta: v1.ObjectMeta{
				Name: "storage-class",
			},
			Provisioner:       "csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingWaitForFirstConsumer))),
		},
		&v1core.Node{
			ObjectMeta: v1.ObjectMeta{
				Name: "node-1",
				Labels: map[string]string{
					"kubernetes.io/hostname": "node-1",
				},
			},
		},
		&storage.CSIStorageCapacity{
			ObjectMeta: v1.ObjectMeta{
				Name: "capacity-node-1",
			},
			NodeTopology: &v1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/hostname": "node-1",
				},
			},
			StorageClassName: "storage-class",
		},
	}

	pvc := &v1core.PersistentVolumeClaim{
		ObjectMeta: v1.ObjectMeta{
			Name:      "pvc-1",
			Namespace: "test",
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "pod-2",
					UID:        "pod-2",
				},
			},
		},
		Spec: v1core.PersistentVolumeClaimSpec{
			VolumeName:       "pv-1",
			StorageClassName: ptr.To("storage-class"),
		},
		Status: v1core.PersistentVolumeClaimStatus{
			Phase: v1core.ClaimPending,
		},
	}

	kubeAiSchedOjbs := []runtime.Object{
		&enginev2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "queue-0",
			},
		},
		&enginev2alpha2.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Name: "podGroup-0",
				UID:  "ABC",
			},
			Spec: enginev2alpha2.PodGroupSpec{
				Queue: "queue-0",
			},
		},
	}

	clusterInfo := newClusterInfoTests(t, append(kubeObjects, pvc), kubeAiSchedOjbs)
	snapshot, err := clusterInfo.Snapshot()
	assert.Equal(t, nil, err)
	node := snapshot.Nodes["node-1"]
	task := snapshot.PodGroupInfos["podGroup-0"].PodInfos["pod-1"]
	assert.Equal(t, node.IsTaskAllocatable(task), false)

	pvc.OwnerReferences = nil

	clusterInfo = newClusterInfoTests(t, append(kubeObjects, pvc), kubeAiSchedOjbs)
	snapshot, err = clusterInfo.Snapshot()
	assert.Equal(t, nil, err)
	node = snapshot.Nodes["node-1"]
	task = snapshot.PodGroupInfos["podGroup-0"].PodInfos["pod-1"]
	assert.Equal(t, node.IsTaskAllocatable(task), true)

}

func createFakePodGroup(name string, schedulingBackoff *int32, nodePoolName string,
	lastSchedulingCondition *enginev2alpha2.SchedulingCondition) *enginev2alpha2.PodGroup {
	result := &enginev2alpha2.PodGroup{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{},
			Name:   name,
		},
		Spec: enginev2alpha2.PodGroupSpec{
			SchedulingBackoff: schedulingBackoff,
		},
		Status: enginev2alpha2.PodGroupStatus{
			SchedulingConditions: []enginev2alpha2.SchedulingCondition{},
		},
	}
	if nodePoolName != defaultNodePoolName && nodePoolName != "" {
		result.Labels[nodePoolNameLabel] = nodePoolName
	}
	if lastSchedulingCondition != nil {
		result.Status.SchedulingConditions = append(result.Status.SchedulingConditions,
			*lastSchedulingCondition)
	}
	return result
}

func TestSnapshotWithListerErrors(t *testing.T) {
	tests := map[string]struct {
		install func(*data_lister.MockDataLister)
	}{
		"listNodes": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return(nil, fmt.Errorf(successErrorMsg))
				mdl.EXPECT().ListPods().Return(nil, nil)
			},
		},
		"listPods": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListPods().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"twiceSamePod": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*v1core.Node{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-0",
						},
					},
				}, nil)
				mdl.EXPECT().ListPods().Return([]*v1core.Pod{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "my-pod",
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "my-pod",
						},
					},
				}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"listQueues": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*v1core.Node{}, nil)
				mdl.EXPECT().ListPods().Return([]*v1core.Pod{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"listPodGroups": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*v1core.Node{}, nil)
				mdl.EXPECT().ListPods().Return([]*v1core.Pod{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return([]*enginev2.Queue{}, nil)
				mdl.EXPECT().ListPriorityClasses().Return([]*v12.PriorityClass{}, nil)
				mdl.EXPECT().ListPodGroups().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"defaultPriorityClass": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*v1core.Node{}, nil)
				mdl.EXPECT().ListPods().Return([]*v1core.Pod{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return([]*enginev2.Queue{}, nil)
				mdl.EXPECT().ListPriorityClasses().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"getPriorityClassByNameAndPodByPodGroup": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*v1core.Node{}, nil)
				mdl.EXPECT().ListPods().Return([]*v1core.Pod{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return([]*enginev2.Queue{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "queue-0",
						},
						Spec: enginev2.QueueSpec{
							ParentQueue: "default",
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "default",
						},
						Spec: enginev2.QueueSpec{
							Resources: &enginev2.QueueResources{},
						},
					},
				}, nil).AnyTimes()
				mdl.EXPECT().ListPriorityClasses().Return([]*v12.PriorityClass{}, nil)
				mdl.EXPECT().ListPodGroups().Return([]*enginev2alpha2.PodGroup{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "my-pg",
						},
						Spec: enginev2alpha2.PodGroupSpec{Queue: "queue-0"},
					},
				}, nil)
				mdl.EXPECT().GetPriorityClassByName(gomock.Any()).Return(nil, fmt.Errorf(successErrorMsg))
				mdl.EXPECT().ListPodByIndex(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"listPodDisruptionBudgets": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListPods().Return([]*v1core.Pod{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListNodes().Return([]*v1core.Node{}, nil)
				mdl.EXPECT().ListQueues().Return([]*enginev2.Queue{}, nil)
				mdl.EXPECT().ListPriorityClasses().Return([]*v12.PriorityClass{}, nil)
				mdl.EXPECT().ListPodGroups().Return([]*enginev2alpha2.PodGroup{}, nil)
				mdl.EXPECT().ListPodDisruptionBudgets().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"ListBindRequests": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListPods().Return([]*v1core.Pod{}, nil)
				mdl.EXPECT().ListNodes().Return([]*v1core.Node{}, nil)
				mdl.EXPECT().ListBindRequests().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	for name, test := range tests {
		t.Logf("Running test: %s", name)
		dl := data_lister.NewMockDataLister(ctrl)
		clusterInfo := newClusterInfoTests(t, []runtime.Object{}, []runtime.Object{})
		test.install(dl)
		clusterInfo.dataLister = dl
		_, err := clusterInfo.Snapshot()
		assert.NotNil(t, err)
	}
}

func TestNewClusterInfoErrorPartitionSelector(t *testing.T) {
	kubeFakeClient, kubeAiFakeClient := newFakeClients([]runtime.Object{}, []runtime.Object{})
	informerFactory := informers.NewSharedInformerFactory(kubeFakeClient, 0)
	kubeAiSchedulerInformerFactory := kubeAiSchedulerInfo.NewSharedInformerFactory(kubeAiFakeClient, 0)

	controller := gomock.NewController(t)
	clusterPodAffinityInfo := pod_affinity.NewMockClusterPodAffinityInfo(controller)
	clusterPodAffinityInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
	clusterPodAffinityInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()

	params := &conf.SchedulingNodePoolParams{
		NodePoolLabelKey:   "@!A",
		NodePoolLabelValue: "!@#",
	}
	_, err := New(informerFactory, kubeAiSchedulerInformerFactory, params, false, clusterPodAffinityInfo, false, true, nil)

	assert.NotNil(t, err)
}

func fakeIndexFunc(obj interface{}) ([]string, error) {
	return nil, nil
}

func TestNewClusterInfoAddIndexerFails(t *testing.T) {
	kubeFakeClient, kubeAiSchedulerFakeClient := newFakeClients([]runtime.Object{}, []runtime.Object{})
	informerFactory := informers.NewSharedInformerFactory(kubeFakeClient, 0)
	kubeAiSchedulerInformerFactory := kubeAiSchedulerInfo.NewSharedInformerFactory(kubeAiSchedulerFakeClient, 0)
	podInformer := informerFactory.Core().V1().Pods()
	go podInformer.Informer().Run(nil)
	for !podInformer.Informer().HasSynced() {
		time.Sleep(500 * time.Millisecond)
	}

	err := podInformer.Informer().AddIndexers(
		cache.Indexers{
			"podByPodGroupIndexer": fakeIndexFunc,
		})
	assert.Nil(t, err, "Failed to add fake indexer")

	controller := gomock.NewController(t)
	clusterPodAffinityInfo := pod_affinity.NewMockClusterPodAffinityInfo(controller)
	clusterPodAffinityInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
	clusterPodAffinityInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()

	_, err = New(informerFactory, kubeAiSchedulerInformerFactory, nil, false,
		clusterPodAffinityInfo, false, true, nil)
	assert.NotNil(t, err, "Expected error for conflicting indexers")
}

func newClusterInfoTests(t *testing.T, kubeObjects, kaiSchedulerObjects []runtime.Object) *ClusterInfo {
	params := &conf.SchedulingNodePoolParams{
		NodePoolLabelKey:   nodePoolNameLabel,
		NodePoolLabelValue: "",
	}
	return newClusterInfoTestsInner(t, kubeObjects, kaiSchedulerObjects, params, true)
}

func newClusterInfoTestsInner(t *testing.T, kubeObjects, kaiSchedulerObjects []runtime.Object,
	nodePoolParams *conf.SchedulingNodePoolParams, fullHierarchyFairness bool) *ClusterInfo {
	kubeFakeClient, kubeAiSchedulerFakeClient := newFakeClients(kubeObjects, kaiSchedulerObjects)
	informerFactory := informers.NewSharedInformerFactory(kubeFakeClient, 0)
	kubeAiSchedulerInformerFactory := kubeAiSchedulerInfo.NewSharedInformerFactory(kubeAiSchedulerFakeClient, 0)

	controller := gomock.NewController(t)
	clusterPodAffinityInfo := pod_affinity.NewMockClusterPodAffinityInfo(controller)
	clusterPodAffinityInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
	clusterPodAffinityInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()

	clusterInfo, _ := New(informerFactory, kubeAiSchedulerInformerFactory, nodePoolParams, false,
		clusterPodAffinityInfo, true, fullHierarchyFairness, nil)

	stopCh := context.Background().Done()
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)
	kubeAiSchedulerInformerFactory.Start(stopCh)
	kubeAiSchedulerInformerFactory.WaitForCacheSync(stopCh)

	return clusterInfo
}

func newFakeClients(kubernetesObjects, kaiSchedulerObjects []runtime.Object) (kubernetes.Interface, kubeAiSchedulerClient.Interface) {
	return fake.NewSimpleClientset(kubernetesObjects...), kubeAiSchedulerClientFake.NewSimpleClientset(kaiSchedulerObjects...)
}

func TestSnapshotPodsInPartition(t *testing.T) {
	clusterObjects := []runtime.Object{
		&v1core.Node{
			ObjectMeta: v1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					nodePoolNameLabel: "foo",
				},
			},
		},
		&v1core.Node{
			ObjectMeta: v1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					nodePoolNameLabel: "bar",
				},
			},
		},
		&v1core.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name: "pod1",
				Labels: map[string]string{
					nodePoolNameLabel: "foo",
				},
			},
			Spec: v1core.PodSpec{
				NodeName: "node1",
			},
		},
		&v1core.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name: "pod2",
				Labels: map[string]string{
					nodePoolNameLabel: "bar",
				},
			},
			Spec: v1core.PodSpec{
				NodeName: "node2",
			},
		},
	}

	clusterInfo := newClusterInfoTestsInner(
		t, clusterObjects,
		[]runtime.Object{},
		&conf.SchedulingNodePoolParams{
			NodePoolLabelKey:   nodePoolNameLabel,
			NodePoolLabelValue: "foo",
		},
		true,
	)
	snapshot, err := clusterInfo.Snapshot()
	assert.Nil(t, err)
	assert.Len(t, snapshot.Pods, 1)
	assert.Equal(t, "pod1", snapshot.Pods[0].Name)
}

func newCompletedPod(pod *v1core.Pod) *v1core.Pod {
	newPod := pod.DeepCopy()
	newPod.Status.Phase = v1core.PodSucceeded
	newPod.Status.Conditions = []v1core.PodCondition{
		{
			Type:   v1core.PodReady,
			Status: v1core.ConditionTrue,
		},
	}
	return newPod
}

func newPodOnNode(pod *v1core.Pod, nodeName string) *v1core.Pod {
	newPod := pod.DeepCopy()
	newPod.Spec.NodeName = nodeName
	newPod.Name = fmt.Sprintf("%s-%s", pod.Name, nodeName)
	return newPod
}
