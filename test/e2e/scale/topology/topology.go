// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"fmt"
	"maps"
	"strings"

	kwok "github.com/run-ai/kwok-operator/api/v1beta1"

	kaiv1alpha1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Config represents the input configuration
type Config struct {
	Levels       []TopologyLevel `yaml:"levels"`
	NodesPerLeaf int             `yaml:"nodesPerLeaf"`
}

// TopologyLevel represents a single topology level in the config
type TopologyLevel struct {
	Name      string `yaml:"name"`
	ShortName string `yaml:"shortName"`
	Count     int    `yaml:"count"`
}

func GenerateTopology(levels []TopologyLevel, name string) kaiv1alpha1.Topology {
	topologyLevels := make([]kaiv1alpha1.TopologyLevel, 0, len(levels)+1)

	for _, level := range levels {
		topologyLevels = append(topologyLevels, kaiv1alpha1.TopologyLevel{
			NodeLabel: level.Name,
		})
	}

	return kaiv1alpha1.Topology{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Topology",
			APIVersion: "kai.scheduler/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kaiv1alpha1.TopologySpec{
			Levels: topologyLevels,
		},
	}
}

func GenerateNodePools(levels []TopologyLevel, nodesPerLeaf int, extraLabels map[string]string) []kwok.NodePool {
	var nodePools []kwok.NodePool

	// Build all possible values for each level
	levelValues := [][]string{}
	for _, level := range levels {
		values := []string{}
		for j := range level.Count {
			values = append(values,
				fmt.Sprintf("%s%d", level.ShortName, j+1),
			)
		}
		levelValues = append(levelValues, values)
	}

	combinations := cartesian(levelValues)

	for _, combination := range combinations {
		labels := maps.Clone(extraLabels)
		for i, value := range combination {
			labels[levels[i].Name] = value
		}
		name := strings.Join(combination, "-")
		np := generateNodePool(labels, name, nodesPerLeaf)
		nodePools = append(nodePools, np)
	}

	return nodePools
}

// cartesian returns all combinations of the level values
// for example, if levels is [{"zone-1", "zone-2"}, {"block-1", "block-2", "block-3"}]
// the result will be:
//
//	{
//		{"zone-1", "block-1"},
//		{"zone-1", "block-2"},
//		{"zone-1", "block-3"},
//		{"zone-2", "block-1"},
//		{"zone-2", "block-2"},
//		{"zone-2", "block-3"},
//	}
func cartesian(allCombinations [][]string) [][]string {
	if len(allCombinations) == 0 {
		return [][]string{}
	}

	if len(allCombinations) == 1 {
		result := [][]string{}
		for _, combination := range allCombinations[0] {
			result = append(result, []string{combination})
		}
		return result
	}

	lowerTiers := cartesian(allCombinations[1:])

	product := [][]string{}
	for _, tier := range allCombinations[0] {
		for _, lowerTier := range lowerTiers {
			product = append(product, append([]string{tier}, lowerTier...))
		}
	}

	return product
}

func generateNodePool(labels map[string]string, name string, nodeCount int) kwok.NodePool {
	labels["run.ai/simulated-gpu-node-pool"] = "default"
	labels["type"] = "kwok"

	return kwok.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: kwok.NodePoolSpec{
			NodeCount: int32(nodeCount),
			NodeTemplate: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("32"),
						corev1.ResourceMemory: resource.MustParse("256Gi"),
						corev1.ResourcePods:   resource.MustParse("110"),
					},
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("32"),
						corev1.ResourceMemory: resource.MustParse("256Gi"),
						corev1.ResourcePods:   resource.MustParse("110"),
					},
					NodeInfo: corev1.NodeSystemInfo{
						Architecture:     "amd64",
						KubeProxyVersion: "fake",
						KubeletVersion:   "fake",
						OperatingSystem:  "linux",
					},
					Phase: "Running",
				},
			},
		},
	}
}
