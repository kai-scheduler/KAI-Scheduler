// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	kubeaischedulerver "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
)

func TestTopologyPlugin_initializeTopologyTree(t *testing.T) {
	fakeKubeClient := fake.NewSimpleClientset()
	fakeKubeAISchedulerClient := kubeaischedulerver.NewSimpleClientset()

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
					"cpu":                       resource.MustParse("1"),
					constants.NvidiaGpuResource: resource.MustParse("1"),
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
					"cpu":                       resource.MustParse("1"),
					constants.NvidiaGpuResource: resource.MustParse("1"),
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
					"cpu":                       resource.MustParse("1"),
					constants.NvidiaGpuResource: resource.MustParse("3"),
				},
			},
		},
	}

	testTopology := &kaiv1alpha1.Topology{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-topology",
		},
		Spec: kaiv1alpha1.TopologySpec{
			Levels: []kaiv1alpha1.TopologyLevel{
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
		SchedulerName:               schedulerParams.SchedulerName,
		NodePoolParams:              schedulerParams.PartitionParams,
		RestrictNodeScheduling:      false,
		DetailedFitErrors:           false,
		ScheduleCSIStorage:          false,
		FullHierarchyFairness:       true,
		NumOfStatusRecordingWorkers: 1,
		DiscoveryClient:             fakeKubeClient.Discovery(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error

	for _, node := range testNodes {
		_, err = fakeKubeClient.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	_, err = fakeKubeAISchedulerClient.KaiV1alpha1().Topologies().Create(ctx, testTopology, metav1.CreateOptions{})
	assert.NoError(t, err)

	schedulerCache.Run(ctx.Done())
	schedulerCache.WaitForCacheSync(ctx.Done())

	session := &framework.Session{
		Config:          schedulerConfig,
		SchedulerParams: schedulerParams,
		Cache:           schedulerCache,
		ClusterInfo: &api.ClusterInfo{
			Nodes: map[string]*node_info.NodeInfo{
				testNodes[0].Name: {
					Node:        testNodes[0],
					Allocatable: resource_info.ResourceFromResourceList(testNodes[0].Status.Allocatable),
					Idle:        resource_info.NewResource(500, 0, 0),
					Releasing:   resource_info.NewResource(0, 0, 0),
					Used:        resource_info.NewResource(500, 0, 1),
				},
				testNodes[1].Name: {
					Node:        testNodes[1],
					Allocatable: resource_info.ResourceFromResourceList(testNodes[1].Status.Allocatable),
					Idle:        resource_info.NewResource(500, 0, 0),
					Releasing:   resource_info.NewResource(0, 0, 0),
					Used:        resource_info.NewResource(500, 0, 0),
				},
				testNodes[2].Name: {
					Node:        testNodes[2],
					Allocatable: resource_info.ResourceFromResourceList(testNodes[2].Status.Allocatable),
					Idle:        resource_info.NewResource(0, 0, 0),
					Releasing:   resource_info.NewResource(0, 0, 0),
					Used:        resource_info.NewResource(1000, 0, 3),
				},
			},
			Topologies: []*kaiv1alpha1.Topology{testTopology},
		},
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

	assert.Equal(t, DomainLevel("test-topology-label/block"), blockDomains["test-block-1"].Level)
	assert.Equal(t, 2, len(blockDomains["test-block-1"].Children))

	assert.Equal(t, 0, len(rackDomains["test-block-1.test-rack-1"].Children))

	assert.Equal(t, 0, len(rackDomains["test-block-1.test-rack-2"].Children))

	assert.Equal(t, 1, len(blockDomains["test-block-2"].Children))

	assert.Equal(t, 0, len(rackDomains["test-block-2.test-rack-1"].Children))
}
