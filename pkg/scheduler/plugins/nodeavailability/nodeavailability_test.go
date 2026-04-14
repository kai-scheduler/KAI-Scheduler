// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeavailability_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/nodeavailability"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/resources_fake"
)

func TestNodeAvailabilityPlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Availability Plugin test")
}

var _ = Describe("NodeAvailability", func() {
	Describe("nodeAvailabilityPlugin", func() {
		Describe("nodeOrderFn", func() {
			var (
				pp          framework.Plugin
				nodeOrderFn api.NodeOrderFn
				ssn         framework.Session
			)
			BeforeEach(func() {
				pp = nodeavailability.New(map[string]string{})
				ssn = framework.Session{}

				Expect(ssn.NodeOrderFns).To(HaveLen(0), "NodeOrderFns should be empty")
				pp.OnSessionOpen(&ssn)
				Expect(ssn.NodeOrderFns).To(HaveLen(1), "NodeOrderFns should have one element")
				nodeOrderFn = ssn.NodeOrderFns[len(ssn.NodeOrderFns)-1]
			})
			Context("scoring a node for a task", func() {
				It("Returns 10 score if node can allocate", func() {
					task := createFakeTask("task-1")
					setTaskResources(task, 1)
					node := createFakeNode("node-1", 1)
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(float64(scores.Availability)))
				})

				It("Returns 0 score if node cannot allocate", func() {
					task := createFakeTask("task-1")
					setTaskResources(task, 2)
					node := createFakeNode("node-1", 1)
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(0.0))
				})
			})
		})
	})
})

var testVectorMap = resource_info.NewResourceVectorMap()

func createFakeTask(taskName string) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		Name:      taskName,
		VectorMap: testVectorMap,
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.Now(),
			},
		},
	}
}

func createFakeNode(nodeName string, idleGpu int) *node_info.NodeInfo {
	idleGPUString := fmt.Sprintf("%d", idleGpu)
	totalGPUString := fmt.Sprintf("%d", idleGpu+1)
	nodeIdleResource := resources_fake.BuildResourceList(nil, nil, &idleGPUString, nil)
	nodeResource := resources_fake.BuildResourceList(nil, nil, &totalGPUString, nil)
	node := nodes_fake.BuildNode(nodeName, nodeResource, nodeIdleResource)
	clusterPodAffinityInfo := cache.NewK8sClusterPodAffinityInfo()
	podAffinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, clusterPodAffinityInfo)
	vectorMap := resource_info.NewResourceVectorMap()
	for resourceName := range node.Status.Allocatable {
		vectorMap.AddResource(resourceName)
	}
	return node_info.NewNodeInfo(node, podAffinityInfo, vectorMap)
}

func setTaskResources(task *pod_info.PodInfo, gpu float64) {
	task.GpuRequirement = *resource_info.NewGpuResourceRequirementWithGpus(gpu, 0)
	task.ResReqVector = resource_info.NewResourceVectorWithValues(0, 0, gpu, testVectorMap)
}
