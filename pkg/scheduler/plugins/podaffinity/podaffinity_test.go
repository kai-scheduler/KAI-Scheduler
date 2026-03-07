// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podaffinity

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

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	k8splugins "github.com/kai-scheduler/kai-scheduler/pkg/scheduler/k8s_internal/plugins"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/scores"
)

type testInput struct {
	nodes         []*v1.Node
	pods          []*v1.Pod
	task          *pod_info.PodInfo
	expectedScore float64
}

var _ = Describe("Preferred Pod Affinity", func() {
	Context("Pod Affinity", func() {
		var (
			clusterAffinityInfo cache.K8sClusterPodAffinityInfo
		)

		nodeLabelName := "node-label"
		exampleLabels := map[string]string{"my-label": "a"}
		exampleAffinity := &v1.Affinity{
			PodAffinity: &v1.PodAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
					{
						Weight: 10,
						PodAffinityTerm: v1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: exampleLabels,
							},
							TopologyKey: nodeLabelName,
						},
					},
				},
			},
		}

		for testName, testData := range map[string]testInput{
			"No Pod Affinity": {
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
						},
					},
				},
				pods: []*v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "existing-pod-1",
							Namespace: "test",
						},
						Spec: v1.PodSpec{
							NodeName: "node-1",
						},
					},
				},
				task: pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test",
					},
				}, nil, resource_info.NewResourceVectorMap()),
				expectedScore: 0,
			},
			"Not fitting Pod Affinity": {
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
							Labels: map[string]string{
								nodeLabelName: "node-1",
							},
						},
					},
				},
				pods: []*v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "existing-pod-1",
							Namespace: "test",
							Labels:    exampleLabels,
						},
						Spec: v1.PodSpec{
							NodeName: "node-1",
							Affinity: exampleAffinity.DeepCopy(),
						},
					},
				},
				task: pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test",
					},
				}, nil, resource_info.NewResourceVectorMap()),
				expectedScore: 0,
			},
			"Matching Pod Affinity": {
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
							Labels: map[string]string{
								nodeLabelName: "node-1",
							},
						},
					},
				},
				pods: []*v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "existing-pod-1",
							Namespace: "test",
							Labels:    exampleLabels,
						},
						Spec: v1.PodSpec{
							NodeName: "node-1",
							Affinity: exampleAffinity.DeepCopy(),
						},
					},
				},
				task: pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test",
						Labels:    exampleLabels,
					},
					Spec: v1.PodSpec{
						Affinity: exampleAffinity.DeepCopy(),
					},
				}, nil, resource_info.NewResourceVectorMap()),
				expectedScore: 2 * 10 * scores.K8sPlugins,
			},
		} {
			testName := testName
			testData := testData
			It(testName, func() {
				clusterAffinityInfo = *cache.NewK8sClusterPodAffinityInfo()
				testPodPreferredAffinity(testData, &clusterAffinityInfo)
			})
		}
	})
})

func testPodPreferredAffinity(testData testInput, clusterAffinityInfo pod_affinity.ClusterPodAffinityInfo) {
	controller := gomock.NewController(GinkgoT())
	mockCache := cache.NewMockCache(controller)

	kubernetesObjects := make([]runtime.Object, 0, len(testData.nodes)+len(testData.pods))
	nodeInfos := map[string]*node_info.NodeInfo{}

	// Build shared vectorMap from all nodes first
	nodeResources := make([]v1.ResourceList, 0, len(testData.nodes))
	for _, node := range testData.nodes {
		nodeResources = append(nodeResources, node.Status.Allocatable)
	}
	vectorMap := resource_info.NewResourceVectorMap()
	for _, nodeResource := range nodeResources {
		for resourceName := range nodeResource {
			vectorMap.AddResource(string(resourceName))
		}
	}

	for _, node := range testData.nodes {
		kubernetesObjects = append(kubernetesObjects, node)
		nodePodAffinityInfo := cluster_info.NewK8sNodePodAffinityInfo(node, clusterAffinityInfo)
		nodeInfos[node.Name] = node_info.NewNodeInfo(node, nodePodAffinityInfo, vectorMap)
	}
	for _, pod := range testData.pods {
		kubernetesObjects = append(kubernetesObjects, pod)
		err := nodeInfos[pod.Spec.NodeName].AddTask(pod_info.NewTaskInfo(pod, nil, vectorMap))
		Expect(err).To(Succeed(), "Expected to add task to node")
	}
	kubernetesObjects = append(kubernetesObjects, testData.task.Pod)

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

	sessionId := "1"
	ssn, err := framework.OpenSession(
		mockCache,
		&conf.SchedulerConfiguration{Tiers: []conf.Tier{}},
		&conf.SchedulerParams{},
		sessionId,
		nil,
	)
	Expect(err).To(Succeed())

	Expect(ssn.NodePreOrderFns).To(HaveLen(0), "Expected no node pre-order functions")
	Expect(ssn.NodeOrderFns).To(HaveLen(0), "Expected no node order functions")
	plugin := New(map[string]string{})
	plugin.OnSessionOpen(ssn)
	Expect(ssn.NodePreOrderFns).To(HaveLen(1), "Expected one node pre-order function")
	Expect(ssn.NodeOrderFns).To(HaveLen(1), "Expected one node order function")

	err = ssn.NodePreOrderFns[len(ssn.NodePreOrderFns)-1](testData.task, maps.Values(nodeInfos))
	Expect(err).To(BeNil())
	score, err := ssn.NodeOrderFns[len(ssn.NodeOrderFns)-1](
		testData.task, nodeInfos[testData.nodes[0].Name])
	Expect(score).To(Equal(testData.expectedScore))
}

func TestPreferredPodAffinity(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "preferred pod affinity")
}
