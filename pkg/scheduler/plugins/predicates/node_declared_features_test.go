// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/component-helpers/nodedeclaredfeatures/features/restartallcontainers"
	"k8s.io/kubernetes/pkg/features"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
	k8spredicates "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/predicates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
)

func TestNodeDeclaredFeatures(t *testing.T) {
	for _, tc := range []struct {
		name            string
		enabled         bool
		requiresFeature bool
		initContainer   bool
		declared        []string
		wantSkip        bool
		wantRejected    bool
	}{
		{name: "matching node", enabled: true, requiresFeature: true, declared: []string{restartallcontainers.RestartAllContainersOnContainerExits}},
		{name: "missing declarations", enabled: true, requiresFeature: true, wantRejected: true},
		{name: "unrelated declaration", enabled: true, requiresFeature: true, declared: []string{"UnrelatedFeature"}, wantRejected: true},
		{name: "init container requirement", enabled: true, requiresFeature: true, initContainer: true, wantRejected: true},
		{name: "ordinary pod", enabled: true, wantSkip: true},
		{name: "disabled gate", requiresFeature: true, wantSkip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NodeDeclaredFeatures, tc.enabled)
			vectorMap := resource_info.NewResourceVectorMap()
			lister := cache.NewK8sClusterPodAffinityInfo()
			nodes := nodes_fake.BuildNodesInfoMap(map[string]nodes_fake.TestNodeBasic{"node": {}}, nil, lister, vectorMap)
			node := nodes["node"]
			node.Node = node.Node.DeepCopy()
			node.Node.Status.DeclaredFeatures = tc.declared

			client := fake.NewSimpleClientset()
			plugins := k8splugins.InitializeInternalPlugins(client, informers.NewSharedInformerFactory(client, 0), lister)
			require.NotNil(t, plugins.NodeDeclaredFeatures)
			mockCache := cache.NewMockCache(gomock.NewController(t))
			mockCache.EXPECT().InternalK8sPlugins().Return(plugins)
			mockCache.EXPECT().SnapshotSharedLister().Return(lister).AnyTimes()
			ssn := &framework.Session{Cache: mockCache, ClusterInfo: &api.ClusterInfo{Nodes: nodes}}
			registered := k8spredicates.NewSessionPredicates(ssn)
			predicate, found := registered[k8spredicates.NodeDeclaredFeatures]
			require.True(t, found, "node feature checks must be registered in KAI's predicate pipeline")
			predicates := k8s_internal.SessionPredicates{k8spredicates.NodeDeclaredFeatures: predicate}
			pp := &predicatesPlugin{skipPredicates: SkipPredicates{}}
			pp.initializeK8sNodeInfos(ssn)

			container := v1.Container{Name: "worker", Image: "test"}
			if tc.requiresFeature {
				restartPolicy := v1.ContainerRestartPolicyNever
				container.RestartPolicy = &restartPolicy
				container.RestartPolicyRules = []v1.ContainerRestartRule{{
					Action: v1.ContainerRestartRuleActionRestartAllContainers,
					ExitCodes: &v1.ContainerRestartRuleOnExitCodes{
						Operator: v1.ContainerRestartRuleOnExitCodesOpIn,
						Values:   []int32{1},
					},
				}}
			}
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "test", UID: "pod"}, Spec: v1.PodSpec{Containers: []v1.Container{container}}}
			if tc.initContainer {
				pod.Spec.InitContainers = []v1.Container{container}
				pod.Spec.Containers = []v1.Container{{Name: "worker", Image: "test"}}
			}
			task := pod_info.NewTaskInfo(pod, vectorMap)
			require.NoError(t, pp.evaluateTaskOnPrePredicate(task, predicates))
			require.Equal(t, tc.wantSkip, pp.skipPredicates.ShouldSKip(task.UID, k8spredicates.NodeDeclaredFeatures))
			evaluate := func() error {
				return pp.evaluateTaskOnPredicates(task, nil, node, predicates,
					isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable, func() bool { return false }, pp.skipPredicates)
			}
			if tc.wantRejected {
				require.ErrorContains(t, evaluate(), "unsatisfied requirements: "+restartallcontainers.RestartAllContainersOnContainerExits)
				// A new node snapshot must refresh the upstream NodeInfo feature set.
				node.Node = node.Node.DeepCopy()
				node.Node.Status.DeclaredFeatures = []string{restartallcontainers.RestartAllContainersOnContainerExits}
				pp.initializeK8sNodeInfos(ssn)
				require.NoError(t, evaluate())
				node.Node = node.Node.DeepCopy()
				node.Node.Status.DeclaredFeatures = nil
				pp.initializeK8sNodeInfos(ssn)
				require.ErrorContains(t, evaluate(), "unsatisfied requirements")
			} else {
				require.NoError(t, evaluate())
			}
		})
	}
}
