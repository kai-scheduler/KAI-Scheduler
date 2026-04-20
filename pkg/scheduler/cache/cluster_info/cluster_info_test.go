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
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	v12 "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"

	kubeAiSchedulerClient "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned"
	kubeAiSchedulerClientFake "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	kubeAiSchedulerInfo "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/informers/externalversions"
	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclass_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info/data_lister"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb"
	fakeusage "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/utils"
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-pod",
						Namespace: "my-ns",
					},
				},
			},
			kaiSchedulerObjects: []runtime.Object{
				&enginev2.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-department",
					},
					Spec: enginev2.QueueSpec{
						Resources: &enginev2.QueueResources{},
					},
				},
				&enginev2.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-queue",
					},
					Spec: enginev2.QueueSpec{
						ParentQueue: "my-department",
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
				},
			},
			kaiSchedulerObjects: []runtime.Object{
				&enginev2.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-department",
					},
					Spec: enginev2.QueueSpec{
						Resources: &enginev2.QueueResources{},
					},
				},
				&enginev2.Queue{
					ObjectMeta: metav1.ObjectMeta{
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
		clusterInfo := newClusterInfoTests(t, clusterInfoTestParams{
			kubeObjects:         test.kubeObjects,
			kaiSchedulerObjects: test.kaiSchedulerObjects,
		})
		snapshot, err := clusterInfo.Snapshot()
		assert.Equal(t, nil, err)
		assert.Equal(t, test.expectedNodes, len(snapshot.Nodes))
		assert.Equal(t, test.expectedQueues, len(snapshot.Queues))

		assert.Equal(t, test.expectedBindRequests, len(snapshot.BindRequests))
	}
}

func TestSnapshotUsage(t *testing.T) {
	tests := []struct {
		name  string
		usage *queue_info.ClusterUsage
		err   error

		expectedUsage *queue_info.ClusterUsage
	}{
		{
			name: "BasicUsage",
			usage: &queue_info.ClusterUsage{
				Queues: map[common_info.QueueID]queue_info.QueueUsage{
					"queue-1": {
						corev1.ResourceCPU:                10,
						corev1.ResourceMemory:             10,
						commonconstants.NvidiaGpuResource: 10,
					},
				},
			},
			err: nil,
			expectedUsage: &queue_info.ClusterUsage{
				Queues: map[common_info.QueueID]queue_info.QueueUsage{
					"queue-1": {
						corev1.ResourceCPU:                10,
						corev1.ResourceMemory:             10,
						commonconstants.NvidiaGpuResource: 10,
					},
				},
			},
		},
		{
			name:          "Error only",
			usage:         nil,
			err:           fmt.Errorf("error"),
			expectedUsage: &queue_info.ClusterUsage{},
		},
		{
			name: "Error and usage",
			usage: &queue_info.ClusterUsage{
				Queues: map[common_info.QueueID]queue_info.QueueUsage{
					"queue-1": {
						corev1.ResourceCPU:                11,
						corev1.ResourceMemory:             11,
						commonconstants.NvidiaGpuResource: 11,
					},
				},
			},
			err:           fmt.Errorf("error"),
			expectedUsage: &queue_info.ClusterUsage{},
		},
	}

	compareUsage := func(t *testing.T, expected, actual *queue_info.ClusterUsage) {
		if expected == nil {
			assert.Nil(t, actual)
			return
		}
		assert.NotNil(t, actual)
		assert.Equal(t, len(expected.Queues), len(actual.Queues))
		for queueID, expectedUsage := range expected.Queues {
			actualUsage, ok := actual.Queues[queueID]
			assert.True(t, ok)
			assert.Equal(t, expectedUsage, actualUsage)
		}
	}

	for i, test := range tests {
		t.Logf("Running test %d: %s", i, test.name)
		clusterInfo := newClusterInfoTests(t, clusterInfoTestParams{
			kubeObjects:         []runtime.Object{},
			kaiSchedulerObjects: []runtime.Object{},
			clusterUsage:        test.usage,
			clusterUsageErr:     test.err,
		})
		snapshot, err := clusterInfo.Snapshot()
		assert.Equal(t, nil, err)
		usage := snapshot.QueueResourceUsage
		compareUsage(t, test.expectedUsage, &usage)
	}
}

func TestSnapshotNodes(t *testing.T) {
	examplePod := &corev1.Pod{
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu": resource.MustParse("2"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
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
	type expectedNodeData struct {
		Name      string
		Idle      *resource_info.Resource
		Used      *resource_info.Resource
		Releasing *resource_info.Resource
	}
	tests := map[string]struct {
		objs          []runtime.Object
		resultNodes   []expectedNodeData
		resultPodsLen int
		nodePoolName  string
	}{
		"BasicUsage": {
			objs: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu":  resource.MustParse("10"),
							"pods": resource.MustParse("110"),
						},
					},
				},
				examplePod,
			},
			resultNodes: []expectedNodeData{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":  resource.MustParse("8"),
							"pods": resource.MustParse("109"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("0"),
							"pods":   resource.MustParse("1"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu":  resource.MustParse("10"),
							"pods": resource.MustParse("110"),
						},
					},
				},
				newCompletedPod(examplePod),
			},
			resultNodes: []expectedNodeData{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":  resource.MustParse("10"),
							"pods": resource.MustParse("110"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":    resource.MustParse("0"),
							"memory": resource.MustParse("0"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							defaultNodePoolName: "pool-a",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu":  resource.MustParse("10"),
							"pods": resource.MustParse("110"),
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
						Labels: map[string]string{
							defaultNodePoolName: "pool-b",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu":  resource.MustParse("10"),
							"pods": resource.MustParse("110"),
						},
					},
				},
				newPodOnNode(examplePod, "node-1"),
				newPodOnNode(examplePod, "node-2"),
			},
			resultNodes: []expectedNodeData{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":  resource.MustParse("8"),
							"pods": resource.MustParse("109"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("0"),
							"pods":   resource.MustParse("1"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu":                   resource.MustParse("10"),
							"nvidia.com/mig-1g.5gb": resource.MustParse("10"),
							"pods":                  resource.MustParse("110"),
						},
					},
				},
				exampleMIGPod,
				exampleMIGPodWithPG,
			},
			resultNodes: []expectedNodeData{
				{
					Name: "node-1",
					Idle: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":                   resource.MustParse("6"),
							"nvidia.com/mig-1g.5gb": resource.MustParse("6"),
							"pods":                  resource.MustParse("108"),
						},
					),
					Used: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
							"cpu":                   resource.MustParse("4"),
							"memory":                resource.MustParse("0"),
							"nvidia.com/mig-1g.5gb": resource.MustParse("4"),
							"pods":                  resource.MustParse("2"),
						},
					),
					Releasing: resource_info.ResourceFromResourceList(
						corev1.ResourceList{
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
				nil, nil, // usage and usageErr
			)
			existingPods := map[common_info.PodID]*pod_info.PodInfo{}

			controller := gomock.NewController(t)
			clusterPodAffinityInfo := pod_affinity.NewMockClusterPodAffinityInfo(controller)
			clusterPodAffinityInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
			clusterPodAffinityInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()

			allPods, _ := clusterInfo.dataLister.ListPods()
			vectorMap := resource_info.NewResourceVectorMap()
			nodes, _, err := clusterInfo.snapshotNodes(clusterPodAffinityInfo, vectorMap)
			if err != nil {
				assert.FailNow(t, fmt.Sprintf("SnapshotNode got error in test %s", t.Name()), err)
			}
			pods, err := clusterInfo.addTasksToNodes(allPods, existingPods, nodes, nil, nil, vectorMap)

			assert.Equal(t, len(test.resultNodes), len(nodes))
			assert.Equal(t, test.resultPodsLen, len(pods))

			for _, expectedNode := range test.resultNodes {
				actualNode := nodes[expectedNode.Name]
				assert.Equal(t, expectedNode.Idle.ToVector(vectorMap), actualNode.IdleVector, "Expected idle resources to be equal")
				assert.Equal(t, expectedNode.Used.ToVector(vectorMap), actualNode.UsedVector, "Expected used resources to be equal")
				assert.Equal(t, expectedNode.Releasing.ToVector(vectorMap), actualNode.ReleasingVector, "Expected releasing resources to be equal")
			}
		})
	}
}

func TestBindRequests(t *testing.T) {
	examplePodName := "pod-1"
	namespace1 := "namespace-1"
	podGroupName := "podgroup-1"
	examplePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      examplePodName,
			Namespace: namespace1,
			UID:       types.UID(examplePodName),
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: podGroupName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu": resource.MustParse("2"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	exampleQueue := &enginev2.Queue{
		ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
				func() *corev1.Pod {
					pod := examplePod.DeepCopy()
					pod.Name = "not-" + examplePod.Name
					pod.UID = types.UID(fmt.Sprintf("not-%s", examplePod.UID))
					return pod
				}(),
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
				examplePod,
			},
			kaiSchedulerObjects: []runtime.Object{
				exampleQueue,
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace1,
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: exampleQueue.Name,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
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
		clusterInfo := newClusterInfoTests(t,
			clusterInfoTestParams{
				kubeObjects:         test.kubeObjects,
				kaiSchedulerObjects: test.kaiSchedulerObjects,
			},
		)
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
			for _, podInfo := range podGroup.GetAllPodsMap() {
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
			assert.Equal(t, float64(test.resultNodes[node.Name].MilliValue()), node.IdleVector.Get(resource_info.CPUIndex))
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
					ObjectMeta: metav1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: "queue-0",
					},
				},
			},
			kubeObjs: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				{
					Name:  "podGroup-0",
					Queue: "queue-0",
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
							WithPodInfos(pod_info.PodsMap{
								"test-pod": {
									UID: "test-pod",
								},
							}),
					},
				},
			},
		},
		"NotExistingQueue": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: "queue-1",
					},
				},
			},
			kubeObjs: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				{
					Name:  "podGroup-0",
					Queue: "queue-1",
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
							WithPodInfos(pod_info.PodsMap{
								"test-pod": {
									UID: "test-pod",
								},
							}),
					},
				},
			},
		},
		"filter unassigned pod groups - no scheduling backoff": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
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
					Name:  "podGroup-0",
					Queue: "queue-0",
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil),
					},
				},
			},
		},
		"filter unassigned pod groups - no scheduling conditions": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
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
					Name:  "podGroup-0",
					Queue: "queue-0",
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil),
					},
				},
			},
		},
		"filter unassigned pod groups - unschedulable in different nodepool": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
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
					Name:  "podGroup-0",
					Queue: "queue-0",
					PodSets: map[string]*subgroup_info.PodSet{
						podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil),
					},
				},
			},
		},
		"filter unassigned pod groups - unassigned": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
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
		"With sub groups": {
			objs: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "podGroup-0",
						UID:  "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue:     "queue-0",
						MinMember: ptr.To(int32(3)),
						SubGroups: []enginev2alpha2.SubGroup{
							{
								Name:      "SubGroup-0",
								MinMember: ptr.To(int32(1)),
							},
							{
								Name:      "SubGroup-1",
								MinMember: ptr.To(int32(2)),
							},
						},
					},
					Status: enginev2alpha2.PodGroupStatus{},
				},
			},
			kubeObjs: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testNamespace,
						Name:      "pod-0",
						UID:       types.UID(fmt.Sprintf("%s/pod-0", testNamespace)),
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
						Labels: map[string]string{
							commonconstants.SubGroupLabelKey: "SubGroup-0",
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testNamespace,
						Name:      "pod-1",
						UID:       types.UID(fmt.Sprintf("%s/pod-1", testNamespace)),
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
						Labels: map[string]string{
							commonconstants.SubGroupLabelKey: "SubGroup-1",
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testNamespace,
						Name:      "pod-2",
						UID:       types.UID(fmt.Sprintf("%s/pod-2", testNamespace)),
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-0",
						},
						Labels: map[string]string{
							commonconstants.SubGroupLabelKey: "SubGroup-1",
						},
					},
				},
			},
			results: []*podgroup_info.PodGroupInfo{
				func() *podgroup_info.PodGroupInfo {
					subGroup0 := subgroup_info.NewPodSet("SubGroup-0", 1, nil)
					subGroup1 := subgroup_info.NewPodSet("SubGroup-1", 2, nil)

					subGroup0.AssignTask(&pod_info.PodInfo{UID: "pod-0", SubGroupName: "SubGroup-0"})
					subGroup1.AssignTask(&pod_info.PodInfo{UID: "pod-1", SubGroupName: "SubGroup-1"})
					subGroup1.AssignTask(&pod_info.PodInfo{UID: "pod-2", SubGroupName: "SubGroup-1"})

					subGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
					subGroupSet.AddPodSet(subGroup0)
					subGroupSet.AddPodSet(subGroup1)

					return &podgroup_info.PodGroupInfo{
						Name:            "podGroup-0",
						Queue:           "queue-0",
						RootSubGroupSet: subGroupSet,
						PodSets: map[string]*subgroup_info.PodSet{
							"SubGroup-0": subGroup0,
							"SubGroup-1": subGroup1,
						},
					}
				}(),
			},
		},
	}

	for name, test := range tests {
		clusterInfo := newClusterInfoTests(t,
			clusterInfoTestParams{
				kubeObjects:         test.kubeObjs,
				kaiSchedulerObjects: test.objs,
			},
		)
		predefinedQueue := &queue_info.QueueInfo{Name: "queue-0"}
		existingPods := map[common_info.PodID]*pod_info.PodInfo{}
		podGroups, err := clusterInfo.snapshotPodGroups(
			map[common_info.QueueID]*queue_info.QueueInfo{"queue-0": predefinedQueue},
			existingPods, resource_info.NewResourceVectorMap())
		if err != nil {
			assert.FailNow(t, fmt.Sprintf("SnapshotNode got error in test %v", name), err)
		}

		assert.Equal(t, len(test.results), len(podGroups))
		for _, expected := range test.results {
			pg, found := podGroups[common_info.PodGroupID(expected.Name)]
			assert.True(t, found, "PodGroup not found", expected.Name)

			assert.Equal(t, expected.Name, pg.Name)
			assert.Equal(t, expected.Queue, pg.Queue)

			assert.Equal(t, len(expected.GetAllPodSets()), len(pg.GetAllPodSets()))
			for _, expectedSubGroup := range expected.GetAllPodSets() {
				for _, subGroup := range pg.GetAllPodSets() {
					if expectedSubGroup.GetName() != subGroup.GetName() {
						continue
					}
					assert.Equal(t, expectedSubGroup.GetMinAvailable(), subGroup.GetMinAvailable())
					assert.Equal(t, len(expectedSubGroup.GetPodInfos()), len(subGroup.GetPodInfos()))
					if subGroup.GetName() == podgroup_info.DefaultSubGroup {
						continue
					}
					for _, podInfo := range subGroup.GetPodInfos() {
						assert.Equal(t, subGroup.GetName(), podInfo.SubGroupName)
					}
				}
			}
		}

	}
}

func TestSnapshotPodGroups_QueueDoesNotExist_AddsJobFitError(t *testing.T) {
	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: testNamespace,
						UID:       types.UID("test-pod-uid"),
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "podGroup-missing-queue",
						},
					},
				},
			},
			kaiSchedulerObjects: []runtime.Object{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "podGroup-missing-queue",
						Namespace: testNamespace,
						UID:       "ABC",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						Queue: "nonexistent-queue",
					},
				},
			},
		},
	)

	predefinedQueue := &queue_info.QueueInfo{Name: "queue-0"}
	existingPods := map[common_info.PodID]*pod_info.PodInfo{}
	podGroups, err := clusterInfo.snapshotPodGroups(
		map[common_info.QueueID]*queue_info.QueueInfo{"queue-0": predefinedQueue},
		existingPods, resource_info.NewResourceVectorMap())

	assert.NoError(t, err)
	assert.Equal(t, 1, len(podGroups), "Expected 1 podgroup even with missing queue")

	pg, found := podGroups[common_info.PodGroupID("podGroup-missing-queue")]
	assert.True(t, found, "PodGroup not found")
	assert.Equal(t, "nonexistent-queue", string(pg.Queue))

	// Verify job fit error was added
	assert.Equal(t, 1, len(pg.JobFitErrors), "Expected 1 job fit error for missing queue")
	assert.Equal(t, enginev2alpha2.QueueDoesNotExist, pg.JobFitErrors[0].Reason())
	assert.Contains(t, pg.JobFitErrors[0].Messages()[0], "nonexistent-queue")
}

func TestSnapshotQueues(t *testing.T) {
	objs := []runtime.Object{
		&enginev2.Queue{
			ObjectMeta: metav1.ObjectMeta{
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
			ObjectMeta: metav1.ObjectMeta{
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
			ObjectMeta: metav1.ObjectMeta{
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

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         kubeObjs,
			kaiSchedulerObjects: objs,
		},
	)
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
		ObjectMeta: metav1.ObjectMeta{
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
		ObjectMeta: metav1.ObjectMeta{
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
		ObjectMeta: metav1.ObjectMeta{
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
	clusterInfo := newClusterInfoTestsInner(t,
		[]runtime.Object{},
		objects,
		params,
		false,
		nil, nil, // usage and usageErr
	)

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
			ObjectMeta: metav1.ObjectMeta{
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

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         kubeObjects,
			kaiSchedulerObjects: []runtime.Object{},
		},
	)

	priority := getPodGroupPriority(podGroup, 1, clusterInfo.dataLister)
	assert.Equal(t, int32(2), priority)
}

func TestSnapshotStorageObjects(t *testing.T) {
	kubeObjects := []runtime.Object{
		&storage.CSIDriver{
			ObjectMeta: metav1.ObjectMeta{Name: "csi-driver"},
			Spec: storage.CSIDriverSpec{
				StorageCapacity: ptr.To(true),
			},
		},
		&storage.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "storage-class",
			},
			Provisioner:       "csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingWaitForFirstConsumer))),
		},
		&storage.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "non-csi-storage-class",
			},
			Provisioner:       "non-csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingWaitForFirstConsumer))),
		},
		&storage.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "immediate-binding-storage-class",
			},
			Provisioner:       "csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingImmediate))),
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nonOwnedClaimName,
				Namespace: testNamespace,
				UID:       "csi-pvc-uid",
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("storage-class"),
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ownedClaimName,
				Namespace: testNamespace,
				UID:       "owned-csi-pvc-uid",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "v1",
						Kind:       "pod",
						Name:       "owner-pod",
						UID:        "owner-pod-uid",
					},
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("storage-class"),
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "owner-pod",
				Namespace: testNamespace,
				UID:       "owner-pod-uid",
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod: "podGroup-0",
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
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
			ObjectMeta: metav1.ObjectMeta{
				Name: "queue-0",
			},
		},
		&enginev2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "podGroup-0",
				UID:  "ABC",
			},
			Spec: enginev2alpha2.PodGroupSpec{
				Queue: "queue-0",
			},
		},
		&kaiv1alpha1.Topology{
			ObjectMeta: metav1.ObjectMeta{
				Name: "topology-0",
			},
		},
	}

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         kubeObjects,
			kaiSchedulerObjects: kubeAiSchedOjbs,
		},
	)

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
			Phase:             corev1.ClaimBound,
			StorageClass:      "storage-class",
			PodOwnerReference: nil,
			DeletedOwner:      true,
		},
		ownedClaimKey: {
			Key:          ownedClaimKey,
			Name:         ownedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(0, resource.BinarySI),
			Phase:        corev1.ClaimBound,
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

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         []runtime.Object{},
			kaiSchedulerObjects: []runtime.Object{},
		},
	)

	priority := getPodGroupPriority(podGroup, 123, clusterInfo.dataLister)
	assert.Equal(t, int32(123), priority)
}

func TestGetDefaultPriority(t *testing.T) {
	kubeObjects := []runtime.Object{
		&v12.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-priority",
			},
			Value:         2,
			GlobalDefault: true,
		},
	}

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         kubeObjects,
			kaiSchedulerObjects: []runtime.Object{},
		},
	)

	priority, err := getDefaultPriority(clusterInfo.dataLister)
	assert.Equal(t, nil, err)
	assert.Equal(t, int32(2), priority)
}

func TestGetDefaultPriorityNotExists(t *testing.T) {
	kubeObjects := []runtime.Object{
		&v12.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-priority",
			},
			Value: 2,
		},
	}
	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         kubeObjects,
			kaiSchedulerObjects: []runtime.Object{},
		},
	)
	priority, err := getDefaultPriority(clusterInfo.dataLister)
	assert.Equal(t, nil, err)
	assert.Equal(t, int32(50), priority)
}

func TestGetDefaultPriorityWithError(t *testing.T) {
	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         []runtime.Object{},
			kaiSchedulerObjects: []runtime.Object{},
		},
	)
	priority, err := getDefaultPriority(clusterInfo.dataLister)
	assert.Equal(t, nil, err)
	assert.Equal(t, int32(50), priority)
}

func TestPodGroupWithIndex(t *testing.T) {
	podGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			UID: "ABC",
		},
	}
	podGroupInfo := &podgroup_info.PodGroupInfo{
		PodGroupUID: "ABC",
	}

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         []runtime.Object{},
			kaiSchedulerObjects: []runtime.Object{},
		},
	)
	clusterInfo.setPodGroupWithIndex(podGroup, podGroupInfo)
	assert.Equal(t, types.UID("ABC"), podGroupInfo.PodGroupUID)
}

func TestPodGroupWithIndexNonMatching(t *testing.T) {
	podGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			UID: "ABC",
		},
	}
	podGroupInfo := &podgroup_info.PodGroupInfo{
		PodGroupUID: "MyTest",
	}

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         []runtime.Object{},
			kaiSchedulerObjects: []runtime.Object{},
		},
	)
	clusterInfo.setPodGroupWithIndex(podGroup, podGroupInfo)
	assert.Equal(t, types.UID("ABC"), podGroupInfo.PodGroupUID)
}

func TestPodGroupWithIndexNoSubGroups(t *testing.T) {
	podGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			UID: "ABC",
		},
		Spec: enginev2alpha2.PodGroupSpec{
			MinMember: ptr.To(int32(2)),
		},
	}
	podGroupInfo := podgroup_info.NewPodGroupInfo("MyTest")
	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         []runtime.Object{},
			kaiSchedulerObjects: []runtime.Object{},
		},
	)
	assert.Equal(t, int32(1), podGroupInfo.GetAllPodSets()[podgroup_info.DefaultSubGroup].GetMinAvailable())
	clusterInfo.setPodGroupWithIndex(podGroup, podGroupInfo)
	assert.Equal(t, int32(2), podGroupInfo.GetAllPodSets()[podgroup_info.DefaultSubGroup].GetMinAvailable())
}

func TestPodGroupWithIndexWithSubGroups(t *testing.T) {
	podGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			UID: "ABC",
		},
		Spec: enginev2alpha2.PodGroupSpec{
			MinMember: ptr.To(int32(3)),
			SubGroups: []enginev2alpha2.SubGroup{
				{
					Name:      "sub-a",
					MinMember: ptr.To(int32(1)),
				},
				{
					Name:      "sub-b",
					MinMember: ptr.To(int32(2)),
				},
			},
		},
	}
	podGroupInfo := podgroup_info.NewPodGroupInfo("MyTest")
	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         []runtime.Object{},
			kaiSchedulerObjects: []runtime.Object{},
		},
	)
	clusterInfo.setPodGroupWithIndex(podGroup, podGroupInfo)
	assert.Equal(t, int32(1), podGroupInfo.GetAllPodSets()["sub-a"].GetMinAvailable())
	assert.Equal(t, int32(2), podGroupInfo.GetAllPodSets()["sub-b"].GetMinAvailable())
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
		ci := newClusterInfoTests(t,
			clusterInfoTestParams{
				kubeObjects:         []runtime.Object{},
				kaiSchedulerObjects: []runtime.Object{},
			},
		)
		result := ci.isPodGroupUpForScheduler(pg)
		assert.Equal(t, result, testData.expectedResult,
			"Test: <%s>, expected pod group to be up for scheduler <%t>", testData.testName,
			testData.expectedResult)
	}
}

func TestNotSchedulingPodWithTerminatingPVC(t *testing.T) {
	kubeObjects := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-1",
				Namespace: "test",
				UID:       "pod-1",
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod: "podGroup-0",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "container-1",
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "pv-1",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "pvc-1",
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		},
		&storage.CSIDriver{
			ObjectMeta: metav1.ObjectMeta{Name: "csi-driver"},
			Spec: storage.CSIDriverSpec{
				StorageCapacity: ptr.To(true),
			},
		},
		&storage.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "storage-class",
			},
			Provisioner:       "csi-driver",
			VolumeBindingMode: (*storage.VolumeBindingMode)(ptr.To(string(storage.VolumeBindingWaitForFirstConsumer))),
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
				Labels: map[string]string{
					"kubernetes.io/hostname": "node-1",
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					"pods": resource.MustParse("110"),
				},
			},
		},
		&storage.CSIStorageCapacity{
			ObjectMeta: metav1.ObjectMeta{
				Name: "capacity-node-1",
			},
			NodeTopology: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/hostname": "node-1",
				},
			},
			StorageClassName: "storage-class",
		},
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-1",
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "pod-2",
					UID:        "pod-2",
				},
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName:       "pv-1",
			StorageClassName: ptr.To("storage-class"),
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}

	kubeAiSchedOjbs := []runtime.Object{
		&enginev2.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "queue-0",
			},
		},
		&enginev2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "podGroup-0",
				UID:  "ABC",
			},
			Spec: enginev2alpha2.PodGroupSpec{
				Queue: "queue-0",
			},
		},
	}

	clusterInfo := newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         append(kubeObjects, pvc),
			kaiSchedulerObjects: kubeAiSchedOjbs,
		},
	)
	snapshot, err := clusterInfo.Snapshot()
	assert.Equal(t, nil, err)
	node := snapshot.Nodes["node-1"]
	task := snapshot.PodGroupInfos["podGroup-0"].GetAllPodsMap()["pod-1"]
	assert.Equal(t, node.IsTaskAllocatable(task), false)

	pvc.OwnerReferences = nil

	clusterInfo = newClusterInfoTests(t,
		clusterInfoTestParams{
			kubeObjects:         append(kubeObjects, pvc),
			kaiSchedulerObjects: kubeAiSchedOjbs,
		},
	)
	snapshot, err = clusterInfo.Snapshot()
	assert.Equal(t, nil, err)
	node = snapshot.Nodes["node-1"]
	task = snapshot.PodGroupInfos["podGroup-0"].GetAllPodsMap()["pod-1"]
	assert.Equal(t, node.IsTaskAllocatable(task), true, "Expected task to be allocatable, but got %v", node.IsTaskAllocatable(task))
}

func createFakePodGroup(name string, schedulingBackoff *int32, nodePoolName string,
	lastSchedulingCondition *enginev2alpha2.SchedulingCondition) *enginev2alpha2.PodGroup {
	result := &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
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
				mdl.EXPECT().ListNodes().Return([]*corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-0",
						},
					},
				}, nil)
				mdl.EXPECT().ListResourceSlicesByNode().Return(map[string][]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListPods().Return([]*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "my-pod",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "my-pod",
						},
					},
				}, nil)
				mdl.EXPECT().ListResourceClaims().Return([]*resourceapi.ResourceClaim{}, nil)
				mdl.EXPECT().ListResourceSlices().Return([]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListDeviceClasses().Return([]*resourceapi.DeviceClass{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"listQueues": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*corev1.Node{}, nil)
				mdl.EXPECT().ListResourceSlicesByNode().Return(map[string][]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListPods().Return([]*corev1.Pod{}, nil)
				mdl.EXPECT().ListResourceClaims().Return([]*resourceapi.ResourceClaim{}, nil)
				mdl.EXPECT().ListResourceSlices().Return([]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListDeviceClasses().Return([]*resourceapi.DeviceClass{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"listPodGroups": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*corev1.Node{}, nil)
				mdl.EXPECT().ListResourceSlicesByNode().Return(map[string][]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListPods().Return([]*corev1.Pod{}, nil)
				mdl.EXPECT().ListResourceClaims().Return([]*resourceapi.ResourceClaim{}, nil)
				mdl.EXPECT().ListResourceSlices().Return([]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListDeviceClasses().Return([]*resourceapi.DeviceClass{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return([]*enginev2.Queue{}, nil)
				mdl.EXPECT().ListResourceUsage().Return(nil, nil)
				mdl.EXPECT().ListPriorityClasses().Return([]*v12.PriorityClass{}, nil)
				mdl.EXPECT().ListPodGroups().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"defaultPriorityClass": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*corev1.Node{}, nil)
				mdl.EXPECT().ListResourceSlicesByNode().Return(map[string][]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListPods().Return([]*corev1.Pod{}, nil)
				mdl.EXPECT().ListResourceClaims().Return([]*resourceapi.ResourceClaim{}, nil)
				mdl.EXPECT().ListResourceSlices().Return([]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListDeviceClasses().Return([]*resourceapi.DeviceClass{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return([]*enginev2.Queue{}, nil)
				mdl.EXPECT().ListResourceUsage().Return(nil, nil)
				mdl.EXPECT().ListPriorityClasses().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"getPriorityClassByNameAndPodByPodGroup": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListNodes().Return([]*corev1.Node{}, nil)
				mdl.EXPECT().ListResourceSlicesByNode().Return(map[string][]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListPods().Return([]*corev1.Pod{}, nil)
				mdl.EXPECT().ListResourceClaims().Return([]*resourceapi.ResourceClaim{}, nil)
				mdl.EXPECT().ListResourceSlices().Return([]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListDeviceClasses().Return([]*resourceapi.DeviceClass{}, nil)
				mdl.EXPECT().ListBindRequests().Return([]*schedulingv1alpha2.BindRequest{}, nil)
				mdl.EXPECT().ListQueues().Return([]*enginev2.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "queue-0",
						},
						Spec: enginev2.QueueSpec{
							ParentQueue: "default",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "default",
						},
						Spec: enginev2.QueueSpec{
							Resources: &enginev2.QueueResources{},
						},
					},
				}, nil).AnyTimes()
				mdl.EXPECT().ListResourceUsage().Return(nil, nil)
				mdl.EXPECT().ListPriorityClasses().Return([]*v12.PriorityClass{}, nil)
				mdl.EXPECT().ListPodGroups().Return([]*enginev2alpha2.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "my-pg",
						},
						Spec: enginev2alpha2.PodGroupSpec{Queue: "queue-0"},
					},
				}, nil)
				mdl.EXPECT().GetPriorityClassByName(gomock.Any()).Return(nil, fmt.Errorf(successErrorMsg))
				mdl.EXPECT().ListPodByIndex(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
		"ListBindRequests": {
			func(mdl *data_lister.MockDataLister) {
				mdl.EXPECT().ListPods().Return([]*corev1.Pod{}, nil)
				mdl.EXPECT().ListNodes().Return([]*corev1.Node{}, nil)
				mdl.EXPECT().ListResourceSlicesByNode().Return(map[string][]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListResourceClaims().Return([]*resourceapi.ResourceClaim{}, nil)
				mdl.EXPECT().ListResourceSlices().Return([]*resourceapi.ResourceSlice{}, nil)
				mdl.EXPECT().ListDeviceClasses().Return([]*resourceapi.DeviceClass{}, nil)
				mdl.EXPECT().ListBindRequests().Return(nil, fmt.Errorf(successErrorMsg))
			},
		},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	for name, test := range tests {
		t.Logf("Running test: %s", name)
		dl := data_lister.NewMockDataLister(ctrl)
		clusterInfo := newClusterInfoTests(t,
			clusterInfoTestParams{
				kubeObjects:         []runtime.Object{},
				kaiSchedulerObjects: []runtime.Object{},
			},
		)
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
	_, err := New(informerFactory, kubeAiSchedulerInformerFactory, nil, params, false, clusterPodAffinityInfo, false, true, nil)

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

	_, err = New(informerFactory, kubeAiSchedulerInformerFactory, nil, nil, false,
		clusterPodAffinityInfo, false, true, nil)
	assert.NotNil(t, err, "Expected error for conflicting indexers")
}

type clusterInfoTestParams struct {
	kubeObjects         []runtime.Object
	kaiSchedulerObjects []runtime.Object
	clusterUsage        *queue_info.ClusterUsage
	clusterUsageErr     error
}

func newClusterInfoTests(t *testing.T, testParams clusterInfoTestParams) *ClusterInfo {
	nodePoolParams := &conf.SchedulingNodePoolParams{
		NodePoolLabelKey:   nodePoolNameLabel,
		NodePoolLabelValue: "",
	}
	return newClusterInfoTestsInner(
		t, testParams.kubeObjects, testParams.kaiSchedulerObjects, nodePoolParams, true,
		testParams.clusterUsage, testParams.clusterUsageErr)
}

func newClusterInfoTestsInner(t *testing.T, kubeObjects, kaiSchedulerObjects []runtime.Object,
	nodePoolParams *conf.SchedulingNodePoolParams, fullHierarchyFairness bool,
	clusterUsage *queue_info.ClusterUsage, clusterUsageErr error) *ClusterInfo {
	kubeFakeClient, kubeAiSchedulerFakeClient := newFakeClients(kubeObjects, kaiSchedulerObjects)
	informerFactory := informers.NewSharedInformerFactory(kubeFakeClient, 0)
	kubeAiSchedulerInformerFactory := kubeAiSchedulerInfo.NewSharedInformerFactory(kubeAiSchedulerFakeClient, 0)

	controller := gomock.NewController(t)
	clusterPodAffinityInfo := pod_affinity.NewMockClusterPodAffinityInfo(controller)
	clusterPodAffinityInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
	clusterPodAffinityInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()

	fakeUsageClient := fakeusage.FakeClient{}
	fakeUsageClient.SetResourceUsage(clusterUsage, clusterUsageErr)
	usageLister := usagedb.NewUsageLister(&fakeUsageClient, ptr.To(10*time.Microsecond), ptr.To(10*time.Second), ptr.To(10*time.Second))

	clusterInfo, _ := New(informerFactory, kubeAiSchedulerInformerFactory, usageLister, nodePoolParams, false,
		clusterPodAffinityInfo, true, fullHierarchyFairness, nil)

	stopCh := context.Background().Done()
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)
	kubeAiSchedulerInformerFactory.Start(stopCh)
	kubeAiSchedulerInformerFactory.WaitForCacheSync(stopCh)
	usageLister.Start(stopCh)
	usageLister.WaitForCacheSync(stopCh)

	return clusterInfo
}

func newFakeClients(kubernetesObjects, kaiSchedulerObjects []runtime.Object) (kubernetes.Interface, kubeAiSchedulerClient.Interface) {
	return fake.NewSimpleClientset(kubernetesObjects...), kubeAiSchedulerClientFake.NewSimpleClientset(kaiSchedulerObjects...)
}

func TestSnapshotPodsInPartition(t *testing.T) {
	clusterObjects := []runtime.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					nodePoolNameLabel: "foo",
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					nodePoolNameLabel: "bar",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod1",
				Labels: map[string]string{
					nodePoolNameLabel: "foo",
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "node1",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod2",
				Labels: map[string]string{
					nodePoolNameLabel: "bar",
				},
			},
			Spec: corev1.PodSpec{
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
		nil, nil, // usage and usageErr
	)
	snapshot, err := clusterInfo.Snapshot()
	assert.Nil(t, err)
	assert.Len(t, snapshot.Pods, 1)
	assert.Equal(t, "pod1", snapshot.Pods[0].Name)
}

func newCompletedPod(pod *corev1.Pod) *corev1.Pod {
	newPod := pod.DeepCopy()
	newPod.Status.Phase = corev1.PodSucceeded
	newPod.Status.Conditions = []corev1.PodCondition{
		{
			Type:   corev1.PodReady,
			Status: corev1.ConditionTrue,
		},
	}
	return newPod
}

func newPodOnNode(pod *corev1.Pod, nodeName string) *corev1.Pod {
	newPod := pod.DeepCopy()
	newPod.Spec.NodeName = nodeName
	newPod.Name = fmt.Sprintf("%s-%s", pod.Name, nodeName)
	return newPod
}

func TestSnapshotNodesWithDRAGPUs(t *testing.T) {
	tests := map[string]struct {
		nodes           []*corev1.Node
		resourceSlices  []*resourceapi.ResourceSlice
		expectedDRAGPUs map[string]float64
		hasDRAGPUs      map[string]bool
	}{
		"Single node with DRA GPUs": {
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status:     corev1.NodeStatus{Allocatable: corev1.ResourceList{}},
				},
			},
			resourceSlices: []*resourceapi.ResourceSlice{
				createTestResourceSlice("slice-1", "node-1", "nvidia.com/gpu", 4),
			},
			expectedDRAGPUs: map[string]float64{"node-1": 4},
			hasDRAGPUs:      map[string]bool{"node-1": true},
		},
		"Multiple nodes with DRA GPUs": {
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status:     corev1.NodeStatus{Allocatable: corev1.ResourceList{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
					Status:     corev1.NodeStatus{Allocatable: corev1.ResourceList{}},
				},
			},
			resourceSlices: []*resourceapi.ResourceSlice{
				createTestResourceSlice("slice-1", "node-1", "nvidia.com/gpu", 4),
				createTestResourceSlice("slice-2", "node-2", "nvidia.com/gpu", 8),
			},
			expectedDRAGPUs: map[string]float64{"node-1": 4, "node-2": 8},
			hasDRAGPUs:      map[string]bool{"node-1": true, "node-2": true},
		},
		"Node with no DRA GPUs": {
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status:     corev1.NodeStatus{Allocatable: corev1.ResourceList{}},
				},
			},
			resourceSlices:  []*resourceapi.ResourceSlice{},
			expectedDRAGPUs: map[string]float64{"node-1": 0},
			hasDRAGPUs:      map[string]bool{"node-1": false},
		},
		"Two device classes on same node": {
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status:     corev1.NodeStatus{Allocatable: corev1.ResourceList{}},
				},
			},
			resourceSlices: []*resourceapi.ResourceSlice{
				createTestResourceSlice("slice-nvidia", "node-1", "nvidia.com/gpu", 4),
				createTestResourceSlice("slice-amd", "node-1", "amd.com/gpu", 2),
			},
			expectedDRAGPUs: map[string]float64{"node-1": 6},
			hasDRAGPUs:      map[string]bool{"node-1": true},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Convert slices to map grouped by node name
			slicesByNode := make(map[string][]*resourceapi.ResourceSlice)
			for _, slice := range test.resourceSlices {
				nodeName := ""
				if slice.Spec.NodeName != nil {
					nodeName = *slice.Spec.NodeName
				}
				slicesByNode[nodeName] = append(slicesByNode[nodeName], slice)
			}

			mockLister := data_lister.NewMockDataLister(ctrl)
			mockLister.EXPECT().ListNodes().Return(test.nodes, nil)
			mockLister.EXPECT().ListResourceSlicesByNode().Return(slicesByNode, nil)

			clusterPodAffinityInfo := pod_affinity.NewMockClusterPodAffinityInfo(ctrl)
			clusterPodAffinityInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
			clusterPodAffinityInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()

			ci := &ClusterInfo{
				dataLister:             mockLister,
				nodePoolParams:         &conf.SchedulingNodePoolParams{},
				nodePoolSelector:       labels.Everything(),
				clusterPodAffinityInfo: clusterPodAffinityInfo,
			}

			vectorMap := resource_info.NewResourceVectorMap()
			nodes, _, err := ci.snapshotNodes(clusterPodAffinityInfo, vectorMap)
			assert.NoError(t, err)

			for nodeName, expectedGPUs := range test.expectedDRAGPUs {
				nodeInfo, found := nodes[nodeName]
				assert.True(t, found, "Node %s not found", nodeName)
				// Check total GPUs (DRA GPUs are merged into AllocatableVector)
				actualGPUs := nodeInfo.AllocatableVector.Get(resource_info.GPUIndex)
				assert.Equal(t, expectedGPUs, actualGPUs, "GPUs mismatch for node %s", nodeName)
				expectedFlag := test.hasDRAGPUs[nodeName]
				assert.Equal(t, expectedFlag, nodeInfo.HasDRAGPUs, "HasDRAGPUs mismatch for node %s", nodeName)
			}
		})
	}
}

func createTestResourceSlice(name, nodeName, driver string, deviceCount int) *resourceapi.ResourceSlice {
	devices := make([]resourceapi.Device, deviceCount)
	for i := 0; i < deviceCount; i++ {
		devices[i] = resourceapi.Device{
			Name: fmt.Sprintf("device-%d", i),
		}
	}

	return &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: resourceapi.ResourceSliceSpec{
			NodeName: ptr.To(nodeName),
			Driver:   driver,
			Devices:  devices,
		},
	}
}
