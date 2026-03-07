// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"slices"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

var (
	cpuNode = &node_info.NodeInfo{
		Name:      "cpu-node",
		Idle:      resource_info.NewResource(1000, 2000, 0),
		Releasing: resource_info.EmptyResource(),
	}
	idleGPUNode = &node_info.NodeInfo{
		Name:      "idle-gpu-node",
		Idle:      resource_info.NewResource(0, 0, 1),
		Releasing: resource_info.EmptyResource(),
	}
	releasingGPUNode = &node_info.NodeInfo{
		Name:      "releasing-gpu-node",
		Idle:      resource_info.EmptyResource(),
		Releasing: resource_info.NewResource(0, 0, 1),
	}
	idleFractionNode = &node_info.NodeInfo{
		Name:                   "idle-fraction-node",
		Idle:                   resource_info.NewResource(0, 0, 0),
		MemoryOfEveryGpuOnNode: 200,
		GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
			AllocatedSharedGPUsMemory: map[string]int64{"abc": 100},
		},
		Releasing: resource_info.EmptyResource(),
	}
	releasingFractionNode = &node_info.NodeInfo{
		Name:                   "releasing-fraction-node",
		Idle:                   resource_info.EmptyResource(),
		Releasing:              resource_info.NewResource(0, 0, 0),
		MemoryOfEveryGpuOnNode: 200,
		GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
			ReleasingSharedGPUsMemory: map[string]int64{"abc": 100},
		},
	}
	idleMIGNode = &node_info.NodeInfo{
		Name: "idle-mig-node",
		Idle: resource_info.ResourceFromResourceList(
			v1.ResourceList{"nvidia.com/mig-1g.10gb": resource.MustParse("1")}),
		Releasing: resource_info.EmptyResource(),
	}
	releasingMIGNode = &node_info.NodeInfo{
		Name: "releasing-mig-node",
		Idle: resource_info.EmptyResource(),
		Releasing: resource_info.ResourceFromResourceList(
			v1.ResourceList{"nvidia.com/mig-1g.10gb": resource.MustParse("1")}),
	}

	allNodes = []*node_info.NodeInfo{
		cpuNode,
		idleGPUNode, releasingGPUNode,
		idleFractionNode, releasingFractionNode,
		idleMIGNode, releasingMIGNode,
	}
	gpuNodeNames = []string{
		idleGPUNode.Name, releasingGPUNode.Name,
		idleFractionNode.Name, releasingFractionNode.Name,
		idleMIGNode.Name, releasingMIGNode.Name,
	}
	allNodeNames = append(gpuNodeNames, cpuNode.Name)
)

func TestFeasibleNodes(t *testing.T) {
	tests := []struct {
		name              string
		job               *podgroup_info.PodGroupInfo
		nodes             []*node_info.NodeInfo
		expectedNodeNames []string
	}{
		{
			name: "no nodes",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeRegular,
								ResReq:              resource_info.NewResourceRequirementsWithGpus(1),
							},
						}),
				},
			},
			nodes:             []*node_info.NodeInfo{},
			expectedNodeNames: []string{},
		},
		{
			name: "CPU only job",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeRegular,
								ResReq:              resource_info.NewResourceRequirements(0, 1000, 0),
							},
						}),
				},
			},
			nodes:             allNodes,
			expectedNodeNames: allNodeNames,
		},
		{
			name: "whole GPU job",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeRegular,
								ResReq: &resource_info.ResourceRequirements{
									GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(2, 0),
								},
							},
						}),
				},
			},
			nodes:             allNodes,
			expectedNodeNames: gpuNodeNames,
		},
		{
			name: "distributed whole GPU job",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeRegular,
								ResReq: &resource_info.ResourceRequirements{
									GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(2, 0),
								},
							},
							"pod2": &pod_info.PodInfo{
								UID: "pod2",
								ResReq: &resource_info.ResourceRequirements{
									GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(2, 0),
								},
							},
						}),
				},
			},
			nodes:             allNodes,
			expectedNodeNames: gpuNodeNames,
		},
		{
			name: "Mixed requests (whole GPU and CPU pods)",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeRegular,
								ResReq: &resource_info.ResourceRequirements{
									GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(2, 0),
								},
							},
							"pod2": &pod_info.PodInfo{
								UID:    "pod2",
								ResReq: resource_info.NewResourceRequirements(0, 1000, 2000),
							},
						}),
				},
			},
			nodes:             allNodes,
			expectedNodeNames: allNodeNames,
		},
		{
			name: "Fraction GPU job",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeFraction,
								ResReq: &resource_info.ResourceRequirements{
									GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(0.5, 0),
								},
							},
						}),
				},
			},
			nodes:             allNodes,
			expectedNodeNames: gpuNodeNames,
		},
		{
			name: "GPU Memory job",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeGpuMemory,
								ResReq: &resource_info.ResourceRequirements{
									GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(
										0, 500),
								},
							},
						}),
				},
			},
			nodes:             allNodes,
			expectedNodeNames: gpuNodeNames,
		},
		{
			name: "MIG job",
			job: &podgroup_info.PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod1": &pod_info.PodInfo{
								UID:                 "pod1",
								ResourceRequestType: pod_info.RequestTypeMigInstance,
								ResReq: &resource_info.ResourceRequirements{
									GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithMig(
										map[v1.ResourceName]int64{
											"nvidia.com/mig-1g.10gb": 1,
										},
									),
								},
							},
						}),
				},
			},
			nodes:             allNodes,
			expectedNodeNames: gpuNodeNames,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualFeasibleNodes := FeasibleNodesForJob(test.nodes, test.job)
			if len(actualFeasibleNodes) != len(test.expectedNodeNames) {
				t.Errorf("Expected different number of feasible nodes. expected: %v, actual: %v",
					test.expectedNodeNames, getNodeNames(actualFeasibleNodes))
			}

			for _, actualNode := range actualFeasibleNodes {
				if !slices.Contains(test.expectedNodeNames, actualNode.Name) {
					t.Errorf("Could not find node %s in actual feasible nodes %v",
						actualNode.Name, getNodeNames(actualFeasibleNodes))
				}
			}
		})
	}
}

func getNodeNames(nodes []*node_info.NodeInfo) []string {
	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	return nodeNames
}
