// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_affinities

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
)

func newTestSession(t *testing.T, nodes map[string]*node_info.NodeInfo, podGroups map[common_info.PodGroupID]*podgroup_info.PodGroupInfo) *framework.Session {
	t.Helper()
	ctrl := gomock.NewController(t)
	cacheMock := cache.NewMockCache(ctrl)

	fakeClient := fake.NewSimpleClientset()
	cacheMock.EXPECT().KubeClient().AnyTimes().Return(fakeClient)
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)

	ctx := context.Background()
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	cacheMock.EXPECT().KubeInformerFactory().AnyTimes().Return(informerFactory)
	cacheMock.EXPECT().SnapshotSharedLister().AnyTimes().Return(cache.NewK8sClusterPodAffinityInfo())

	k8sPlugins := k8splugins.InitializeInternalPlugins(
		cacheMock.KubeClient(), cacheMock.KubeInformerFactory(), cacheMock.SnapshotSharedLister(),
	)
	cacheMock.EXPECT().InternalK8sPlugins().AnyTimes().Return(k8sPlugins)

	clusterInfo := api.NewClusterInfo()
	clusterInfo.Nodes = nodes
	if podGroups != nil {
		clusterInfo.PodGroupInfos = podGroups
	}

	return &framework.Session{
		Cache:       cacheMock,
		ClusterInfo: clusterInfo,
	}
}

func newNode(name string, labels map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func newNodeInfo(node *v1.Node) *node_info.NodeInfo {
	return node_info.NewNodeInfo(node, nil, resource_info.NewResourceVectorMap())
}

func podWithNodeSelector(uid, name, jobID string, selector map[string]string) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      name,
			Namespace: "test",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: jobID,
			},
		},
		Spec: v1.PodSpec{
			NodeSelector: selector,
		},
	}, nil, resource_info.NewResourceVectorMap())
}

func podWithNodeAffinity(uid, name, jobID, labelKey, labelValue string) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      name,
			Namespace: "test",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: jobID,
			},
		},
		Spec: v1.PodSpec{
			Affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      labelKey,
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{labelValue},
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil, resource_info.NewResourceVectorMap())
}

func podWithNodeAffinityMatchFields(uid, name, jobID, targetNodeName string) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      name,
			Namespace: "test",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: jobID,
			},
		},
		Spec: v1.PodSpec{
			Affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchFields: []v1.NodeSelectorRequirement{
									{
										Key:      "metadata.name",
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{targetNodeName},
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil, resource_info.NewResourceVectorMap())
}

func podWithPreferredNodeAffinityOnly(uid, name, jobID, labelKey, labelValue string, weight int32) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      name,
			Namespace: "test",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: jobID,
			},
		},
		Spec: v1.PodSpec{
			Affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
						{
							Weight: weight,
							Preference: v1.NodeSelectorTerm{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      labelKey,
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{labelValue},
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil, resource_info.NewResourceVectorMap())
}

func podWithoutAffinity(uid, name, jobID string) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      name,
			Namespace: "test",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: jobID,
			},
		},
		Spec: v1.PodSpec{},
	}, nil, resource_info.NewResourceVectorMap())
}

func victimPodOnNode(uid, name, jobID, nodeName string) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      name,
			Namespace: "test",
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: jobID,
			},
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
		},
	}, nil, resource_info.NewResourceVectorMap())
}

func TestNewNodeAffinitiesFilter_NilScenario(t *testing.T) {
	ssn := newTestSession(t, map[string]*node_info.NodeInfo{}, nil)
	filter := NewNodeAffinitiesFilter(nil, map[string]*node_info.NodeInfo{}, ssn)
	assert.Nil(t, filter)
}

func TestNewNodeAffinitiesFilter_NoPendingTasksWithNodeAffinity(t *testing.T) {
	// Pending tasks exist but none have node affinity — NewNodeAffinitiesFilter should return nil
	task := podWithoutAffinity("uid-1", "plain-pod", "job-1")
	pg := podgroup_info.NewPodGroupInfo("job-1", task)
	ssn := newTestSession(t, map[string]*node_info.NodeInfo{}, map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"job-1": pg})
	tasks := []*pod_info.PodInfo{}
	for _, task := range pg.GetAllPodsMap() {
		tasks = append(tasks, task)
	}

	sc := scenario.NewByNodeScenario(ssn, nil, tasks, []*pod_info.PodInfo{}, []*podgroup_info.PodGroupInfo{})
	filter := NewNodeAffinitiesFilter(sc, map[string]*node_info.NodeInfo{}, ssn)
	assert.Nil(t, filter)
}

func TestNewNodeAffinitiesFilter_PreferredOnlyNodeAffinityReturnsNil(t *testing.T) {
	// Preferred-only node affinity is not a hard constraint — filter should not be created
	task := podWithPreferredNodeAffinityOnly("uid-1", "pod-1", "job-1", "gpu-type", "A100", 100)
	pg := podgroup_info.NewPodGroupInfo("job-1", task)
	ssn := newTestSession(t, map[string]*node_info.NodeInfo{}, map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"job-1": pg})
	tasks := []*pod_info.PodInfo{}
	for _, task := range pg.GetAllPodsMap() {
		tasks = append(tasks, task)
	}

	sc := scenario.NewByNodeScenario(ssn, nil, tasks, []*pod_info.PodInfo{}, []*podgroup_info.PodGroupInfo{})
	filter := NewNodeAffinitiesFilter(sc, map[string]*node_info.NodeInfo{}, ssn)
	assert.Nil(t, filter)
}

func TestNodeAffinitiesFilter_Filter(t *testing.T) {
	tests := []struct {
		name             string
		allNodes         map[string]*node_info.NodeInfo
		feasibleNodes    map[string]*node_info.NodeInfo
		pendingTasks     []*pod_info.PodInfo
		victimTasks      []*pod_info.PodInfo
		wantFilterResult bool
	}{
		{
			name: "pending pod matches node selector - filter passes",
			allNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			pendingTasks:     []*pod_info.PodInfo{podWithNodeSelector("uid-1", "pod-1", "job-1", map[string]string{"gpu-type": "A100"})},
			wantFilterResult: true,
		},
		{
			name: "pending pod node selector has no matching node - filter fails",
			allNodes: map[string]*node_info.NodeInfo{
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			pendingTasks:     []*pod_info.PodInfo{podWithNodeSelector("uid-1", "pod-1", "job-1", map[string]string{"gpu-type": "A100"})},
			wantFilterResult: false,
		},
		{
			name: "pending pod with NodeAffinity matches node - filter passes",
			allNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			pendingTasks:     []*pod_info.PodInfo{podWithNodeAffinity("uid-1", "pod-1", "job-1", "gpu-type", "A100")},
			wantFilterResult: true,
		},
		{
			name: "mixed pending pods: affinity pod matches, pod without affinity is skipped - filter passes",
			allNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			pendingTasks: []*pod_info.PodInfo{
				podWithNodeSelector("uid-1", "pod-affinity", "job-1", map[string]string{"gpu-type": "A100"}),
				podWithoutAffinity("uid-2", "pod-plain", "job-1"),
			},
			wantFilterResult: true,
		},
		{
			name: "mixed pending pods: affinity pod has no matching node - filter fails",
			allNodes: map[string]*node_info.NodeInfo{
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			pendingTasks: []*pod_info.PodInfo{
				podWithNodeSelector("uid-1", "pod-affinity", "job-1", map[string]string{"gpu-type": "A100"}),
				podWithoutAffinity("uid-2", "pod-plain", "job-1"),
			},
			wantFilterResult: false,
		},
		{
			name: "victim running on matching node expands feasible set - filter passes",
			allNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			// Only V100 node is initially feasible — doesn't match the A100 selector
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			pendingTasks: []*pod_info.PodInfo{
				podWithNodeSelector("uid-1", "pod-1", "job-1", map[string]string{"gpu-type": "A100"}),
			},
			// Victim is on A100 node — evicting it makes that node feasible
			victimTasks: []*pod_info.PodInfo{
				victimPodOnNode("victim-uid", "victim-pod", "job-victim", "node-a100"),
			},
			wantFilterResult: true,
		},
		{
			// MatchFields metadata.name targets a specific node. The filter checks allNodeInfos
			// (not just feasibleNodes), so a node that exists anywhere in the cluster satisfies it —
			// even if not currently feasible (it can be freed via preemption).
			name: "MatchFields targets node that exists in cluster but is not feasible - filter passes",
			allNodes: map[string]*node_info.NodeInfo{
				"node-specific": newNodeInfo(newNode("node-specific", nil)),
				"node-other":    newNodeInfo(newNode("node-other", nil)),
			},
			// node-specific is in the cluster (allNodes) but not currently feasible
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-other": newNodeInfo(newNode("node-other", nil)),
			},
			pendingTasks:     []*pod_info.PodInfo{podWithNodeAffinityMatchFields("uid-1", "pod-1", "job-1", "node-specific")},
			wantFilterResult: true,
		},
		{
			// When the MatchFields target node doesn't exist in the cluster at all,
			// allNodeInfos has no entry for it and the filter returns false.
			name: "MatchFields targets node absent from the cluster - filter fails",
			allNodes: map[string]*node_info.NodeInfo{
				"node-other": newNodeInfo(newNode("node-other", nil)),
			},
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-other": newNodeInfo(newNode("node-other", nil)),
			},
			pendingTasks:     []*pod_info.PodInfo{podWithNodeAffinityMatchFields("uid-1", "pod-1", "job-1", "node-specific")},
			wantFilterResult: false,
		},
		{
			name: "victim on non-matching node does not satisfy affinity - filter fails",
			allNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			// Only V100 node is initially feasible — doesn't match the A100 selector
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-v100": newNodeInfo(newNode("node-v100", map[string]string{"gpu-type": "V100"})),
			},
			pendingTasks: []*pod_info.PodInfo{
				podWithNodeSelector("uid-1", "pod-1", "job-1", map[string]string{"gpu-type": "A100"}),
			},
			// Victim is also on V100 — evicting it doesn't add an A100 node
			victimTasks: []*pod_info.PodInfo{
				victimPodOnNode("victim-uid", "victim-pod", "job-victim", "node-v100"),
			},
			wantFilterResult: false,
		},
		{
			name: "mixed pending pods: required affinity matches, preferred-only pod present - filter passes",
			allNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			feasibleNodes: map[string]*node_info.NodeInfo{
				"node-a100": newNodeInfo(newNode("node-a100", map[string]string{"gpu-type": "A100"})),
			},
			pendingTasks: []*pod_info.PodInfo{
				podWithNodeAffinity("uid-1", "pod-required", "job-1", "gpu-type", "A100"),
				podWithPreferredNodeAffinityOnly("uid-2", "pod-preferred", "job-1", "gpu-type", "A100", 100),
			},
			wantFilterResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pendingPG := podgroup_info.NewPodGroupInfo("job-1", tt.pendingTasks...)
			podGroups := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"job-1": pendingPG}

			if len(tt.victimTasks) > 0 {
				podGroups["job-victim"] = podgroup_info.NewPodGroupInfo("job-victim", tt.victimTasks...)
			}

			ssn := newTestSession(t, tt.allNodes, podGroups)

			// Create the initial scenario (no victims) used to construct the filter
			initialSc := scenario.NewByNodeScenario(ssn, nil, tt.pendingTasks, []*pod_info.PodInfo{}, []*podgroup_info.PodGroupInfo{})
			filter := NewNodeAffinitiesFilter(initialSc, tt.feasibleNodes, ssn)
			assert.NotNil(t, filter, "expected filter to be created since pending tasks have node affinities")

			// Create the filter scenario (with victims) and call Filter
			filterSc := scenario.NewByNodeScenario(ssn, nil, tt.pendingTasks, tt.victimTasks, []*podgroup_info.PodGroupInfo{})
			got, err := filter.Filter(filterSc)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantFilterResult, got)
		})
	}
}
