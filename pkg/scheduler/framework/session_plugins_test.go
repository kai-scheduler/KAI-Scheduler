/*
Copyright 2023 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package framework

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
)

func TestMutateBindRequestAnnotations(t *testing.T) {
	tests := []struct {
		name                string
		mutateFns           []api.BindRequestMutateFn
		expectedAnnotations map[string]string
	}{
		{
			name:                "no mutate functions",
			mutateFns:           []api.BindRequestMutateFn{},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "single mutate function",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1"}
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1"},
		},
		{
			name: "multiple mutate functions with different keys",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1"}
				},
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key2": "value2"}
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1", "key2": "value2"},
		},
		{
			name: "multiple mutate functions with overlapping keys - later should override",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1", "common": "first"}
				},
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key2": "value2", "common": "second"}
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1", "key2": "value2", "common": "second"},
		},
		{
			name: "mutate function returns nil map",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1"}
				},
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return nil
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := &Session{
				BindRequestMutateFns: tt.mutateFns,
			}
			pod := &pod_info.PodInfo{
				Name: "test-pod",
			}
			nodeName := "test-node"
			annotations := ssn.MutateBindRequestAnnotations(pod, nodeName)
			assert.Equal(t, tt.expectedAnnotations, annotations)
		})
	}
}

func TestPartitionMultiImplementation(t *testing.T) {
	nodes := []*node_info.NodeInfo{
		{
			Name: "cluster1rack0-1",
		},
		{
			Name: "cluster0rack0",
		},
		{
			Name: "cluster1rack1-1",
		},
		{
			Name: "cluster0rack1",
		},
		{
			Name: "cluster1rack0-2",
		},
		{
			Name: "cluster1rack1-2",
		},
	}

	shardClusterSubseting := func(_ *podgroup_info.PodGroupInfo, _ *subgroup_info.SubGroupInfo, _ map[string]*subgroup_info.PodSet, _ []*pod_info.PodInfo, nodeset node_info.NodeSet) ([]node_info.NodeSet, error) {
		var subset1 []*node_info.NodeInfo
		var subset2 []*node_info.NodeInfo
		for _, node := range nodeset {
			if strings.Contains(node.Name, "cluster0") {
				subset1 = append(subset1, node)
			} else {
				subset2 = append(subset2, node)
			}
		}
		return []node_info.NodeSet{subset1, subset2}, nil
	}

	topologySubseting := func(_ *podgroup_info.PodGroupInfo, _ *subgroup_info.SubGroupInfo, _ map[string]*subgroup_info.PodSet, _ []*pod_info.PodInfo, nodeset node_info.NodeSet) ([]node_info.NodeSet, error) {
		var subset1 []*node_info.NodeInfo
		var subset2 []*node_info.NodeInfo
		for _, node := range nodeset {
			if strings.Contains(node.Name, "rack0") {
				subset1 = append(subset1, node)
			} else {
				subset2 = append(subset2, node)
			}
		}
		return []node_info.NodeSet{subset1, subset2}, nil
	}

	ssn := &Session{}

	ssn.AddSubsetNodesFn(shardClusterSubseting)
	ssn.AddSubsetNodesFn(topologySubseting)

	partitions, _ := ssn.SubsetNodesFn(podgroup_info.NewPodGroupInfo("a"), nil, nil, nil, nodes)

	assert.Equal(t, 4, len(partitions))

	assert.Equal(t, len(partitions[0]), 1)
	assert.Equal(t, partitions[0][0].Name, "cluster0rack0")

	assert.Equal(t, len(partitions[1]), 1)
	assert.Equal(t, partitions[1][0].Name, "cluster0rack1")

	assert.Equal(t, len(partitions[2]), 2)
	assert.Equal(t, partitions[2][0].Name, "cluster1rack0-1")
	assert.Equal(t, partitions[2][1].Name, "cluster1rack0-2")

	assert.Equal(t, len(partitions[3]), 2)
	assert.Equal(t, partitions[3][0].Name, "cluster1rack1-1")
	assert.Equal(t, partitions[3][1].Name, "cluster1rack1-2")
}
