// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
)

func hostPortPod(name string, port int32) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name)},
		Spec: v1.PodSpec{NodeName: "node0", Containers: []v1.Container{{
			Ports: []v1.ContainerPort{{ContainerPort: port, HostPort: port, Protocol: v1.ProtocolTCP}},
		}}},
	}
}

// A pod pipelined onto a terminating pod's host port shares it; rolling the pipelined pod back
// must not free the port the terminating pod still holds.
func TestK8sNodePodAffinityInfoRemovePodKeepsSharedHostPort(t *testing.T) {
	clusterInfo := pod_affinity.NewMockClusterPodAffinityInfo(gomock.NewController(t))
	clusterInfo.EXPECT().AddNode(gomock.Any(), gomock.Any()).AnyTimes()
	clusterInfo.EXPECT().UpdateNodeAffinity(gomock.Any()).AnyTimes()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node0"}}
	info := NewK8sNodePodAffinityInfo(node, clusterInfo).(*K8sNodePodAffinityInfo)

	terminating := hostPortPod("terminating", 8080)
	pipelined := hostPortPod("pipelined", 8080)
	info.AddPod(terminating)
	info.AddPod(pipelined)

	require.NoError(t, info.RemovePod(pipelined))
	assert.True(t, info.NodeInfo.UsedPorts.CheckConflict("", string(v1.ProtocolTCP), 8080))

	require.NoError(t, info.RemovePod(terminating))
	assert.False(t, info.NodeInfo.UsedPorts.CheckConflict("", string(v1.ProtocolTCP), 8080))
}
