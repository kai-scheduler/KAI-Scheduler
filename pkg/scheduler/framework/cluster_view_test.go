// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
)

func TestFutureClusterViewK8sNodeInfo(t *testing.T) {
	hostPortPod := func(name string, port int32) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name)},
			Spec: v1.PodSpec{NodeName: "node0", Containers: []v1.Container{{
				Ports: []v1.ContainerPort{{ContainerPort: port, HostPort: port, Protocol: v1.ProtocolTCP}},
			}}},
		}
	}
	podNames := func(nodeInfo *k8sframework.NodeInfo) []string {
		var names []string
		for _, podInfo := range nodeInfo.GetPods() {
			names = append(names, podInfo.GetPod().Name)
		}
		return names
	}

	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node0"}}
	affinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, cache.NewK8sClusterPodAffinityInfo())
	nodeInfo := &node_info.NodeInfo{Name: "node0", Node: node, PodAffinityInfo: affinityInfo}
	terminating := hostPortPod("terminating", 8080)
	affinityInfo.AddPod(terminating)

	view := &futureClusterView{
		terminating: &terminatingPods{
			byNode: map[string][]*pod_info.PodInfo{"node0": {{Name: "terminating", Namespace: "test", Pod: terminating}}},
			uids:   sets.New[types.UID]("terminating"),
		},
	}

	future := view.K8sNodeInfo(nodeInfo)
	assert.Empty(t, podNames(future))
	assert.Empty(t, future.UsedPorts)
	assert.Equal(t, []string{"terminating"}, podNames(liveK8sNodeInfo(nodeInfo)), "the live node is not changed")

	affinityInfo.AddPod(hostPortPod("pipelined", 9090))
	assert.Equal(t, []string{"pipelined"}, podNames(view.K8sNodeInfo(nodeInfo)), "reflects the session's changes to the node")
}
