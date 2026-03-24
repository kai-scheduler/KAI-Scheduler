// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeplacement_test

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/nodeplacement"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/resources_fake"
)

type testNodeMetaData = struct {
	nodeAllocatableGPUs string
	nodeIdleGPUs        string
	nodeExpectedScore   float64
}

type testTopologyMetadata = struct {
	name                string
	testNodeMetadataMap map[string]testNodeMetaData
	taskName            string
}

func TestNodePack(t *testing.T) {
	testsMetadata := getTestsMetadata()

	for i, testMetadata := range testsMetadata {
		ssn, task, expectedScoreMap := buildSingleTestParams(testMetadata)

		t.Run(testMetadata.name, func(t *testing.T) {
			testScoresOfCurrentTopology(t, i, testMetadata.name, ssn, task, expectedScoreMap)
		})
	}
}

func getTestsMetadata() []testTopologyMetadata {
	return []testTopologyMetadata{
		{
			name: "4 nodes, 3 with GPUs small gaps",
			testNodeMetadataMap: map[string]testNodeMetaData{
				"n1": {
					nodeAllocatableGPUs: "4",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   scores.MaxHighDensity,
				},
				"n2": {
					nodeAllocatableGPUs: "4",
					nodeIdleGPUs:        "3",
					nodeExpectedScore:   scores.MaxHighDensity / float64(4),
				},
				"n3": {
					nodeAllocatableGPUs: "4",
					nodeIdleGPUs:        "4",
					nodeExpectedScore:   0,
				},
				"n4_no_gpus": {
					nodeAllocatableGPUs: "0",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
			},
			taskName: "job0, requires 1 GPU",
		}, {
			name: "3 nodes, 3 with GPUs all full",
			testNodeMetadataMap: map[string]testNodeMetaData{
				"n1": {
					nodeAllocatableGPUs: "8",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
				"n2": {
					nodeAllocatableGPUs: "4",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
				"n3": {
					nodeAllocatableGPUs: "16",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
			},
			taskName: "job1, requires 1 GPU",
		}, {
			name: "3 nodes, 3 with GPUs all with same allocatable GPUs",
			testNodeMetadataMap: map[string]testNodeMetaData{
				"n1": {
					nodeAllocatableGPUs: "8",
					nodeIdleGPUs:        "1",
					nodeExpectedScore:   scores.MaxHighDensity,
				},
				"n2": {
					nodeAllocatableGPUs: "4",
					nodeIdleGPUs:        "1",
					nodeExpectedScore:   scores.MaxHighDensity,
				},
				"n3": {
					nodeAllocatableGPUs: "16",
					nodeIdleGPUs:        "1",
					nodeExpectedScore:   scores.MaxHighDensity,
				},
			},
			taskName: "job3, requires 1 GPU",
		}, {
			name: "3 nodes, 3 with GPUs all free",
			testNodeMetadataMap: map[string]testNodeMetaData{
				"n1": {
					nodeAllocatableGPUs: "8",
					nodeIdleGPUs:        "8",
					nodeExpectedScore:   scores.MaxHighDensity * (1 - float64(1.0/3.0)),
				},
				"n2": {
					nodeAllocatableGPUs: "4",
					nodeIdleGPUs:        "4",
					nodeExpectedScore:   scores.MaxHighDensity,
				},
				"n3": {
					nodeAllocatableGPUs: "16",
					nodeIdleGPUs:        "16",
					nodeExpectedScore:   0,
				},
			},
			taskName: "job4, requires 2 GPU",
		}, {
			name: "3 nodes, 3 with GPUs, 1 with big gap, 2 with small gaps",
			testNodeMetadataMap: map[string]testNodeMetaData{
				"n1": {
					nodeAllocatableGPUs: "2",
					nodeIdleGPUs:        "1",
					nodeExpectedScore:   scores.MaxHighDensity * float64(1.0-(1.0-0.0)/(64.0)),
				},
				"n2": {
					nodeAllocatableGPUs: "2",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   scores.MaxHighDensity,
				},
				"n3": {
					nodeAllocatableGPUs: "64",
					nodeIdleGPUs:        "64",
					nodeExpectedScore:   0,
				},
			},
			taskName: "job5, requires 2 GPU",
		}, {
			name: "4 nodes, 1 with GPUs",
			testNodeMetadataMap: map[string]testNodeMetaData{
				"n1": {
					nodeAllocatableGPUs: "0",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
				"n2": {
					nodeAllocatableGPUs: "0",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
				"n3": {
					nodeAllocatableGPUs: "4",
					nodeIdleGPUs:        "4",
					nodeExpectedScore:   scores.MaxHighDensity,
				},
			},
			taskName: "job6, requires 2 GPU",
		}, {
			name: "3 nodes, 0 with GPUs",
			testNodeMetadataMap: map[string]testNodeMetaData{
				"n1": {
					nodeAllocatableGPUs: "0",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
				"n2": {
					nodeAllocatableGPUs: "0",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
				"n3": {
					nodeAllocatableGPUs: "0",
					nodeIdleGPUs:        "0",
					nodeExpectedScore:   0,
				},
			},
			taskName: "job7, requires 2 GPU",
		},
	}
}

func buildSingleTestParams(testMetadata testTopologyMetadata) (*framework.Session, *pod_info.PodInfo,
	map[string]float64) {
	nodeInfoMap := map[string]*node_info.NodeInfo{}
	expectedScoreMap := map[string]float64{}

	for nodeName, nodeMetadata := range testMetadata.testNodeMetadataMap {
		expectedScoreMap[nodeName] = nodeMetadata.nodeExpectedScore

		nodeResource := resources_fake.BuildResourceList(nil, nil, &nodeMetadata.nodeAllocatableGPUs,
			nil)
		node := nodes_fake.BuildNode(nodeName, nodeResource, nodeResource)
		clusterPodAffinityInfo := cache.NewK8sClusterPodAffinityInfo()
		podAffinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, clusterPodAffinityInfo)
		vectorMap := resource_info.NewResourceVectorMap()
		for resourceName := range node.Status.Allocatable {
			vectorMap.AddResource(resourceName)
		}
		nodeInfo := node_info.NewNodeInfo(node, podAffinityInfo, vectorMap)
		idleResources := resources_fake.BuildResourceList(nil, nil,
			&nodeMetadata.nodeIdleGPUs, nil)
		nodeInfo.IdleVector = resource_info.ResourceFromResourceList(*idleResources).ToVector(vectorMap)
		nodeInfoMap[nodeName] = nodeInfo
	}

	fakeTask := createFakeTask(testMetadata.taskName)
	ssn := createFakeTestSession(nodeInfoMap)

	return ssn, fakeTask, expectedScoreMap
}

func testScoresOfCurrentTopology(t *testing.T, testNumber int, testName string, ssn *framework.Session, task *pod_info.PodInfo, expectedScoreMap map[string]float64) {
	nodePackPlugin := nodeplacement.New(map[string]string{
		constants.GPUResource: constants.BinpackStrategy,
		constants.CPUResource: constants.BinpackStrategy,
	})

	nodePackPlugin.OnSessionOpen(ssn)

	var nodes []*node_info.NodeInfo
	for _, node := range ssn.ClusterInfo.Nodes {
		nodes = append(nodes, node)
	}

	ssn.NodePreOrderFn(task, nodes)

	for _, node := range ssn.ClusterInfo.Nodes {
		score, err := ssn.NodeOrderFn(task, node)
		if err != nil {
			t.Errorf("Test number: %d (%s) has failed! Node: %v, Unexpected error has occurred: %v",
				testNumber, testName, node.Name, err)
		}

		if !reflect.DeepEqual(expectedScoreMap[node.Name], score) {
			t.Errorf("Test number: %d (%s) has failed! Node: %v, Expected score: %v, Actual score: %v",
				testNumber, testName, node.Name, expectedScoreMap[node.Name], score)
		}
	}
}

func createFakeTestSession(nodes map[string]*node_info.NodeInfo) *framework.Session {
	return &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			Nodes: nodes,
		},
	}
}

func createFakeTask(taskName string) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: taskName,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							resource_info.GPUResourceName: resource.MustParse("1"),
						},
					},
				},
			},
		},
	}, nil, resource_info.NewResourceVectorMap())
}
