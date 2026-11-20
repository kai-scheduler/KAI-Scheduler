// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"strconv"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func BenchmarkCapturePods(b *testing.B) {
	for _, size := range []int{10000, 50000} {
		name := "10k"
		if size == 50000 {
			name = "50k"
		}
		b.Run(name, func(b *testing.B) {
			session := sessionWithPods(size)
			opened := time.Now()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cycle := buildCycle(session, nil, opened)
				if cycle.RawObjects == nil || len(cycle.RawObjects.Pods) != size {
					b.Fatalf("captured %d pods", len(cycle.RawObjects.Pods))
				}
			}
		})
	}
}

func sessionWithPods(count int) *framework.Session {
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}
	info := &node_info.NodeInfo{
		Name:     node.Name,
		Node:     node,
		PodInfos: map[common_info.PodID]*pod_info.PodInfo{},
	}
	for i := 0; i < count; i++ {
		id := strconv.Itoa(i)
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: "ns", UID: types.UID(id)},
			Spec: v1.PodSpec{
				NodeName: node.Name,
				Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1m")}},
				}},
			},
		}
		info.PodInfos[common_info.PodID(id)] = &pod_info.PodInfo{Pod: pod, NodeName: node.Name}
	}
	return &framework.Session{
		ID: "bench",
		ClusterInfo: &api.ClusterInfo{
			Nodes: map[string]*node_info.NodeInfo{node.Name: info},
		},
	}
}
