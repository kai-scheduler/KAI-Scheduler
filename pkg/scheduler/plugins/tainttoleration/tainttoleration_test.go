// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package tainttoleration

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

func TestTaintToleration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "taint toleration")
}

func preferNoScheduleTaint(key string) v1.Taint {
	return v1.Taint{Key: key, Value: "true", Effect: v1.TaintEffectPreferNoSchedule}
}

func newNode(name string, taints ...v1.Taint) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1.NodeSpec{Taints: taints},
	}
}

func newTask(tolerations ...v1.Toleration) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "test", UID: "test-pod-uid"},
		Spec:       v1.PodSpec{Tolerations: tolerations},
	}, resource_info.NewResourceVectorMap())
}

var _ = Describe("PreferNoSchedule taint scoring", func() {
	DescribeTable("scores each node by the PreferNoSchedule taints the pod does not tolerate",
		func(nodes []*v1.Node, task *pod_info.PodInfo, expectedScores map[string]float64) {
			Expect(scoreNodes(nodes, task)).To(Equal(expectedScores))
		},
		Entry("node without taints is not penalized",
			[]*v1.Node{newNode("node-1")},
			newTask(),
			map[string]float64{"node-1": 0},
		),
		Entry("intolerable taint is penalized",
			[]*v1.Node{newNode("clean"), newNode("tainted", preferNoScheduleTaint("evacuated"))},
			newTask(),
			map[string]float64{"clean": 0, "tainted": -scores.K8sPlugins},
		),
		Entry("penalty grows with each intolerable taint",
			[]*v1.Node{
				newNode("one-taint", preferNoScheduleTaint("a")),
				newNode("two-taints", preferNoScheduleTaint("a"), preferNoScheduleTaint("b")),
			},
			newTask(),
			map[string]float64{"one-taint": -scores.K8sPlugins, "two-taints": -2 * scores.K8sPlugins},
		),
		Entry("tolerated taint is not penalized",
			[]*v1.Node{newNode("tainted", preferNoScheduleTaint("evacuated"))},
			newTask(v1.Toleration{
				Key: "evacuated", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectPreferNoSchedule,
			}),
			map[string]float64{"tainted": 0},
		),
		Entry("toleration without an effect tolerates PreferNoSchedule",
			[]*v1.Node{newNode("tainted", preferNoScheduleTaint("evacuated"))},
			newTask(v1.Toleration{Key: "evacuated", Operator: v1.TolerationOpExists}),
			map[string]float64{"tainted": 0},
		),
		Entry("only PreferNoSchedule taints affect the score",
			[]*v1.Node{newNode("no-schedule", v1.Taint{
				Key: "dedicated", Value: "true", Effect: v1.TaintEffectNoSchedule,
			})},
			newTask(),
			map[string]float64{"no-schedule": 0},
		),
	)
})

func scoreNodes(nodes []*v1.Node, task *pod_info.PodInfo) map[string]float64 {
	controller := gomock.NewController(GinkgoT())
	mockCache := cache.NewMockCache(controller)
	clusterAffinityInfo := cache.NewK8sClusterPodAffinityInfo()

	kubernetesObjects := []runtime.Object{task.Pod}
	nodeInfos := map[string]*node_info.NodeInfo{}
	vectorMap := resource_info.NewResourceVectorMap()
	for _, node := range nodes {
		kubernetesObjects = append(kubernetesObjects, node)
		nodePodAffinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, clusterAffinityInfo)
		nodeInfos[node.Name] = node_info.NewNodeInfo(node, nodePodAffinityInfo, vectorMap)
	}

	kubeFakeClient := fake.NewSimpleClientset(kubernetesObjects...)
	informerFactory := informers.NewSharedInformerFactory(kubeFakeClient, 0)

	mockCache.EXPECT().KubeClient().AnyTimes().Return(kubeFakeClient)
	mockCache.EXPECT().KubeInformerFactory().AnyTimes().Return(informerFactory)
	mockCache.EXPECT().SnapshotSharedLister().AnyTimes().Return(clusterAffinityInfo)
	mockCache.EXPECT().Snapshot().AnyTimes().Return(api.NewClusterInfo(), nil)
	k8sPlugins := k8splugins.InitializeInternalPlugins(
		mockCache.KubeClient(), mockCache.KubeInformerFactory(), mockCache.SnapshotSharedLister(),
	)
	mockCache.EXPECT().InternalK8sPlugins().AnyTimes().Return(k8sPlugins)

	ssn, err := framework.OpenSession(
		mockCache,
		&conf.SchedulerConfiguration{Tiers: []conf.Tier{}},
		&conf.SchedulerParams{},
		"1",
		nil,
	)
	Expect(err).To(Succeed())

	New(map[string]string{}).OnSessionOpen(ssn)
	Expect(ssn.NodePreOrderFns).To(HaveLen(1))
	Expect(ssn.NodeOrderFns).To(HaveLen(1))

	Expect(ssn.NodePreOrderFns[0](task, maps.Values(nodeInfos))).To(Succeed())
	nodeScores := map[string]float64{}
	for name, nodeInfo := range nodeInfos {
		score, err := ssn.NodeOrderFns[0](task, nodeInfo)
		Expect(err).To(Succeed())
		nodeScores[name] = score
	}
	return nodeScores
}
