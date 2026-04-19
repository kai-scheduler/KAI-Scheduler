// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	kubeaischedulerver "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	kueuev1alpha1 "sigs.k8s.io/kueue/apis/kueue/v1alpha1"
	kueuefake "sigs.k8s.io/kueue/client-go/clientset/versioned/fake"
)

func TestTopologyPlugin_initializeTopologyTree(t *testing.T) {
	fakeKubeClient := fake.NewSimpleClientset()
	fakeKubeAISchedulerClient := kubeaischedulerver.NewSimpleClientset()
	fakeKueueClient := kueuefake.NewSimpleClientset()

	testNodes := []*v1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-0",
				Labels: map[string]string{
					"test-topology-label/block": "test-block-1",
					"test-topology-label/rack":  "test-rack-1",
				},
			},
			Spec: v1.NodeSpec{
				Unschedulable: false,
			},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					"cpu":                 resource.MustParse("1"),
					constants.GpuResource: resource.MustParse("1"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-1",
				Labels: map[string]string{
					"test-topology-label/block": "test-block-1",
					"test-topology-label/rack":  "test-rack-2",
				},
			},
			Spec: v1.NodeSpec{
				Unschedulable: false,
			},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					"cpu":                 resource.MustParse("1"),
					constants.GpuResource: resource.MustParse("1"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-2",
				Labels: map[string]string{
					"test-topology-label/block": "test-block-2",
					"test-topology-label/rack":  "test-rack-1",
				},
			},
			Spec: v1.NodeSpec{
				Unschedulable: false,
			},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					"cpu":                 resource.MustParse("1"),
					constants.GpuResource: resource.MustParse("3"),
				},
			},
		},
	}

	testTopology := &kueuev1alpha1.Topology{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-topology",
		},
		Spec: kueuev1alpha1.TopologySpec{
			Levels: []kueuev1alpha1.TopologyLevel{
				{
					NodeLabel: "test-topology-label/block",
				},
				{
					NodeLabel: "test-topology-label/rack",
				},
			},
		},
	}

	schedulerConfig := &conf.SchedulerConfiguration{
		Actions: "allocate",
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name: "predicates",
					},
				},
			},
		},
	}

	schedulerParams := conf.SchedulerParams{
		SchedulerName:   "test-scheduler",
		PartitionParams: &conf.SchedulingNodePoolParams{},
	}

	schedulerCache := cache.New(&cache.SchedulerCacheParams{
		KubeClient:                  fakeKubeClient,
		KAISchedulerClient:          fakeKubeAISchedulerClient,
		KueueClient:                 fakeKueueClient,
		SchedulerName:               schedulerParams.SchedulerName,
		NodePoolParams:              schedulerParams.PartitionParams,
		RestrictNodeScheduling:      false,
		DetailedFitErrors:           false,
		ScheduleCSIStorage:          false,
		FullHierarchyFairness:       true,
		NumOfStatusRecordingWorkers: 1,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error

	for _, node := range testNodes {
		_, err = fakeKubeClient.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	_, err = fakeKueueClient.KueueV1alpha1().Topologies().Create(ctx, testTopology, metav1.CreateOptions{})
	assert.NoError(t, err)

	schedulerCache.Run(ctx.Done())
	schedulerCache.WaitForCacheSync(ctx.Done())

	session := &framework.Session{
		Config:          schedulerConfig,
		SchedulerParams: schedulerParams,
		Cache:           schedulerCache,
		Nodes: map[string]*node_info.NodeInfo{
			testNodes[0].Name: {
				Node:        testNodes[0],
				Allocatable: resource_info.ResourceFromResourceList(testNodes[0].Status.Allocatable),
				Used:        resource_info.NewResource(500, 0, 1),
			},
			testNodes[1].Name: {
				Node:        testNodes[1],
				Allocatable: resource_info.ResourceFromResourceList(testNodes[1].Status.Allocatable),
				Used:        resource_info.NewResource(500, 0, 0),
			},
			testNodes[2].Name: {
				Node:        testNodes[2],
				Allocatable: resource_info.ResourceFromResourceList(testNodes[2].Status.Allocatable),
				Used:        resource_info.NewResource(1000, 0, 3),
			},
		},
		Topologies: []*kueuev1alpha1.Topology{testTopology},
	}

	plugin := New(nil)
	plugin.OnSessionOpen(session)

	topologyTrees := plugin.(*topologyPlugin).TopologyTrees
	assert.Equal(t, 1, len(topologyTrees))
	testTopologyObj := topologyTrees["test-topology"]
	assert.Equal(t, "test-topology", testTopologyObj.Name)
	assert.Equal(t, 3, len(testTopologyObj.DomainsByLevel))

	blockDomains := testTopologyObj.DomainsByLevel["test-topology-label/block"]
	rackDomains := testTopologyObj.DomainsByLevel["test-topology-label/rack"]

	assert.Equal(t, "test-block-1", blockDomains["test-block-1"].Name)
	assert.Equal(t, "test-topology-label/block", blockDomains["test-block-1"].Level)
	assert.Equal(t, 1, blockDomains["test-block-1"].Depth)
	assert.Equal(t, 2, len(blockDomains["test-block-1"].Children))

	assert.Equal(t, "test-rack-1", rackDomains["test-block-1.test-rack-1"].Name)
	assert.Equal(t, "test-block-1", rackDomains["test-block-1.test-rack-1"].Parent.Name)
	assert.Equal(t, 2, rackDomains["test-block-1.test-rack-1"].Depth)
	assert.Equal(t, 0, len(rackDomains["test-block-1.test-rack-1"].Children))

	assert.Equal(t, "test-rack-2", rackDomains["test-block-1.test-rack-2"].Name)
	assert.Equal(t, "test-block-1", rackDomains["test-block-1.test-rack-2"].Parent.Name)
	assert.Equal(t, 2, rackDomains["test-block-1.test-rack-2"].Depth)
	assert.Equal(t, 0, len(rackDomains["test-block-1.test-rack-2"].Children))

	assert.Equal(t, "test-block-2", blockDomains["test-block-2"].Name)
	assert.Equal(t, 1, blockDomains["test-block-2"].Depth)
	assert.Equal(t, 1, len(blockDomains["test-block-2"].Children))

	assert.Equal(t, "test-rack-1", rackDomains["test-block-2.test-rack-1"].Name)
	assert.Equal(t, "test-block-2", rackDomains["test-block-2.test-rack-1"].Parent.Name)
	assert.Equal(t, 2, rackDomains["test-block-2.test-rack-1"].Depth)
	assert.Equal(t, 0, len(rackDomains["test-block-2.test-rack-1"].Children))
}
