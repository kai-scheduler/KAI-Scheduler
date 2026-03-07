// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resourcetype_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/resourcetype"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/scores"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/resources_fake"
)

func TestResourceTypePlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CPU Only Preference Plugin test")
}

var _ = Describe("resourceType", func() {
	Describe("resourceTypePlugin", func() {
		Describe("nodeOrderFn", func() {
			var (
				pp          framework.Plugin
				nodeOrderFn api.NodeOrderFn
				ssn         framework.Session
			)
			BeforeEach(func() {
				pp = resourcetype.New(map[string]string{})
				ssn = framework.Session{}

				Expect(ssn.NodeOrderFns).To(HaveLen(0), "NodeOrderFns should be empty")
				pp.OnSessionOpen(&ssn)
				Expect(ssn.NodeOrderFns).To(HaveLen(1), "NodeOrderFns should have one element")
				nodeOrderFn = ssn.NodeOrderFns[len(ssn.NodeOrderFns)-1]
			})

			Context("scoring a node for a task", func() {
				It("Returns 10 score if both task is CPU only and node doesn't have GPUs", func() {
					task := createFakeTask("task-1", 0, 500)
					node := createFakeNode("node-1", 0, map[v1.ResourceName]int{})
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(float64(scores.ResourceType)))
				})

				// even though we expect this to fall on filtering
				It("Returns 0 score if task is NOT CPU only and node doesn't have GPUs", func() {
					task := createFakeTask("task-1", 2, 500)
					node := createFakeNode("node-1", 0, map[v1.ResourceName]int{})
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(0.0))
				})
				It("Returns 0 score if task is CPU only and node has GPUs", func() {
					task := createFakeTask("task-1", 0, 500)
					node := createFakeNode("node-1", 1, map[v1.ResourceName]int{})
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(0.0))
				})
				It("Returns 0 score if task is CPU only and node has MIG GPU", func() {
					task := createFakeTask("task-1", 0, 500)
					node := createFakeNode("node-1", 0, map[v1.ResourceName]int{
						"nvidia.com/mig-1g.5gb": 1,
					})
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(0.0))
				})
				It("Returns 0 score if task is NOT CPU only and node has GPUs", func() {
					task := createFakeTask("task1", 1, 500)
					node := createFakeNode("node1", 1, map[v1.ResourceName]int{})
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(0.0))
				})
				It("Returns 0 score if task has only DRA GPU requests and node is CPU only", func() {
					task := createFakeTaskWithDRA("task-1", 2)
					node := createFakeNode("node-1", 0, map[v1.ResourceName]int{})
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(0.0))
				})
				It("Returns 0 score if task is CPU only and node has only DRA GPUs", func() {
					task := createFakeTask("task-1", 0, 500)
					node := createFakeNodeWithDRA("node-1", 4)
					score, _ := nodeOrderFn(task, node)
					Expect(score).To(Equal(0.0))
				})
			})
		})
	})
})

func createFakeTask(taskName string, gpu float64, cpu float64) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		Name:   taskName,
		ResReq: createResource(gpu, cpu),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.Now(),
			},
		},
	}
}

func createFakeTaskWithDRA(taskName string, draGpuCount int64) *pod_info.PodInfo {
	req := resource_info.EmptyResourceRequirements()
	req.GpuResourceRequirement.SetDraGpus(map[string]int64{"nvidia.com/gpu": draGpuCount})
	return &pod_info.PodInfo{
		Name:   taskName,
		ResReq: req,
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.Now(),
			},
		},
	}
}

func createFakeNode(nodeName string, capacityGPU int, migProfiles map[v1.ResourceName]int) *node_info.NodeInfo {
	gpuString := fmt.Sprintf("%d", capacityGPU)
	cpuString := "2000"
	nodeResource := resources_fake.BuildResourceList(&cpuString, nil, &gpuString, migProfiles)
	node := nodes_fake.BuildNode(nodeName, nodeResource, nodeResource)
	clusterPodAffinityInfo := cache.NewK8sClusterPodAffinityInfo()
	podAffinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, clusterPodAffinityInfo)
	vectorMap := resource_info.NewResourceVectorMap()
	for resourceName := range node.Status.Allocatable {
		vectorMap.AddResource(string(resourceName))
	}
	return node_info.NewNodeInfo(node, podAffinityInfo, vectorMap)
}

func createFakeNodeWithDRA(nodeName string, draGPUs int) *node_info.NodeInfo {
	gpuString := fmt.Sprintf("%d", draGPUs)
	cpuString := "2000"
	nodeResource := resources_fake.BuildResourceList(&cpuString, nil, &gpuString, nil)
	node := nodes_fake.BuildNode(nodeName, nodeResource, nodeResource)
	clusterPodAffinityInfo := cache.NewK8sClusterPodAffinityInfo()
	podAffinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, clusterPodAffinityInfo)
	vectorMap := resource_info.NewResourceVectorMap()
	for resourceName := range node.Status.Allocatable {
		vectorMap.AddResource(string(resourceName))
	}
	ni := node_info.NewNodeInfo(node, podAffinityInfo, vectorMap)
	ni.HasDRAGPUs = true
	return ni
}

func createResource(gpu float64, cpu float64) *resource_info.ResourceRequirements {
	return resource_info.NewResourceRequirements(gpu, cpu, 0)
}
